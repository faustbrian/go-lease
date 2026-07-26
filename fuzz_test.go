package lease_test

import (
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

func FuzzKeyParsing(f *testing.F) {
	f.Add("queue/job")
	f.Add("missing")
	f.Add("namespace/\x00")
	f.Fuzz(func(t *testing.T, value string) {
		key, err := lease.ParseKey(value)
		if err != nil {
			return
		}
		if key.String() != value || len(key.String()) > lease.MaxKeyBytes {
			t.Fatalf("ParseKey(%q) = %q", value, key.String())
		}
	})
}

func FuzzPolicyBounds(f *testing.F) {
	f.Add(int64(time.Second), int64(0), int64(time.Millisecond), uint32(1))
	f.Add(int64(-1), int64(0), int64(0), uint32(0))
	f.Fuzz(func(t *testing.T, ttl, wait, retry int64, attempts uint32) {
		policy, err := lease.NewPolicy(lease.PolicyOptions{
			TTL: time.Duration(ttl), Wait: time.Duration(wait),
			Retry: time.Duration(retry), MaxAttempts: attempts,
		})
		if err != nil {
			return
		}
		if policy.TTL() <= 0 || policy.TTL() > lease.MaxTTL ||
			policy.Wait() < 0 || policy.Wait() > lease.MaxWait ||
			policy.MaxAttempts() == 0 || policy.MaxAttempts() > lease.MaxAttempts {
			t.Fatalf("accepted out-of-bounds policy")
		}
	})
}
