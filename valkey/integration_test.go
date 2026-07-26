package valkey_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	leasevalkey "github.com/faustbrian/golib/pkg/lease/valkey"
	valkeygo "github.com/valkey-io/valkey-go"
)

func TestLiveBackendConformance(t *testing.T) {
	address := os.Getenv("VALKEY_ADDRESS")
	if address == "" {
		t.Skip("VALKEY_ADDRESS is not set")
	}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	prefix := "lease-integration-" + time.Now().UTC().Format("150405.000000000")
	store, err := leasevalkey.Open(context.Background(), client, prefix)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	leasetest.RunBackendConformance(t, func(*testing.T) leasetest.BackendFixture {
		return leasetest.BackendFixture{Backend: store, Expire: time.Sleep}
	})
	key, _ := lease.NewKey("integration", "noscript")
	record, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire(NOSCRIPT) error = %v", err)
	}
	if err := client.Do(
		context.Background(), client.B().ScriptFlush().Build(),
	).Error(); err != nil {
		t.Fatalf("SCRIPT FLUSH error = %v", err)
	}
	if _, err := store.Renew(context.Background(), record, time.Second); err != nil {
		t.Fatalf("Renew(after SCRIPT FLUSH) error = %v", err)
	}
}

func TestLivePartitionOutcome(t *testing.T) {
	address := os.Getenv("VALKEY_PARTITION_ADDRESS")
	readyPath := os.Getenv("VALKEY_PARTITION_READY")
	triggerPath := os.Getenv("VALKEY_PARTITION_TRIGGER")
	if address == "" || readyPath == "" || triggerPath == "" {
		t.Skip("Valkey partition coordination is not configured")
	}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress:      []string{address},
		Dialer:           net.Dialer{Timeout: 100 * time.Millisecond},
		ConnWriteTimeout: 100 * time.Millisecond,
		DisableRetry:     true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	store, err := leasevalkey.New(client, "lease-partition")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// #nosec G703 -- the local fault harness owns this temporary signal path.
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write ready signal: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		// #nosec G703 -- the local fault harness owns this temporary signal path.
		if _, err := os.Stat(triggerPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partition trigger timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	key, _ := lease.NewKey("integration", "partition")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := store.TryAcquire(ctx, key, "owner", time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("TryAcquire(partition) error = %v", err)
	}
}

func TestLiveFenceContinuity(t *testing.T) {
	address := os.Getenv("VALKEY_CONTINUITY_ADDRESS")
	phase := os.Getenv("VALKEY_CONTINUITY_PHASE")
	if address == "" || phase == "" {
		t.Skip("Valkey continuity phase is not configured")
	}
	options := valkeygo.ClientOption{
		InitAddress: []string{address}, Username: os.Getenv("VALKEY_CONTINUITY_USERNAME"),
		Password:     os.Getenv("VALKEY_CONTINUITY_PASSWORD"),
		DisableRetry: true, Dialer: net.Dialer{Timeout: 500 * time.Millisecond},
		ConnWriteTimeout: 500 * time.Millisecond,
	}
	if caFile := os.Getenv("VALKEY_CONTINUITY_CA_FILE"); caFile != "" {
		// #nosec G304 G703 -- the disposable fault harness owns this CA path.
		certificate, err := os.ReadFile(caFile)
		if err != nil {
			t.Fatalf("read continuity CA: %v", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			t.Fatal("continuity CA contains no certificate")
		}
		options.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: roots,
			ServerName: os.Getenv("VALKEY_CONTINUITY_SERVER_NAME"),
		}
	}
	client, err := valkeygo.NewClient(options)
	if err != nil {
		if phase == "reject" {
			assertContinuityRejectionRedacted(t, err)

			return
		}
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	if phase == "seed" || phase == "reset" {
		if err := client.Do(context.Background(), client.B().Flushdb().Build()).Error(); err != nil {
			t.Fatalf("FLUSHDB(%s) error = %v", phase, err)
		}
	}
	store, err := leasevalkey.Open(context.Background(), client, "lease-continuity")
	if phase == "reject" {
		assertContinuityRejectionRedacted(t, err)

		return
	}
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	key, _ := lease.NewKey("fault", "continuity")
	record, err := store.TryAcquire(
		context.Background(), key, "owner-"+phase, time.Second,
	)
	if err != nil {
		t.Fatalf("TryAcquire(%s) error = %v", phase, err)
	}
	switch phase {
	case "seed", "reset":
		if record.Token != 1 {
			t.Fatalf("%s token = %d, want 1", phase, record.Token)
		}
	case "verify":
		if record.Token <= 1 {
			t.Fatalf("verify token = %d, continuity reset", record.Token)
		}
	case "rollback":
		maximum, err := strconv.ParseUint(os.Getenv("VALKEY_CONTINUITY_MAX_TOKEN"), 10, 64)
		if err != nil || record.Token > lease.Token(maximum) {
			t.Fatalf("rollback token = %d, protected maximum = %d", record.Token, maximum)
		}
	default:
		t.Fatalf("unknown continuity phase %q", phase)
	}
	if err := store.Release(context.Background(), record); err != nil {
		t.Fatalf("Release(%s) error = %v", phase, err)
	}
}

func assertContinuityRejectionRedacted(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("rejected continuity client error = nil")
	}
	if password := os.Getenv("VALKEY_CONTINUITY_PASSWORD"); password != "" && strings.Contains(err.Error(), password) {
		t.Fatalf("rejected continuity client leaked password: %v", err)
	}
	if username := os.Getenv("VALKEY_CONTINUITY_USERNAME"); username != "" && strings.Contains(err.Error(), username) {
		t.Fatalf("rejected continuity client leaked username: %v", err)
	}
}
