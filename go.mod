module github.com/faustbrian/golib/pkg/lease

go 1.26.6

require (
	github.com/faustbrian/golib/pkg/migrations v0.0.0-20260729101900-306a65952c01
	github.com/faustbrian/golib/pkg/queue v0.0.0-20260729185237-7c0f934ffa24
	github.com/faustbrian/golib/pkg/service v0.0.0-20260729185121-c56b7cb53124
	github.com/jackc/pgx/v5 v5.10.0
	github.com/valkey-io/valkey-go v1.0.76
	github.com/valkey-io/valkey-go/mock v1.0.76
	go.uber.org/mock v0.6.0
)

require (
	github.com/faustbrian/golib/pkg/cli v0.0.0-20260729183302-ac9562ceb0b5 // indirect
	github.com/faustbrian/golib/pkg/correlation v0.0.0-20260729185016-600a2ffaf74d // indirect
	github.com/faustbrian/golib/pkg/identifier v0.0.0-20260729183302-ac9562ceb0b5 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

exclude (
	github.com/faustbrian/golib/pkg/cli v0.0.0
	github.com/faustbrian/golib/pkg/correlation v0.0.0
	github.com/faustbrian/golib/pkg/identifier v0.0.0
	github.com/faustbrian/golib/pkg/service v0.0.0
)
