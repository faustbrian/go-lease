GO ?= go
GOLANGCI_LINT ?= golangci-lint
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
GREMLINS_VERSION ?= v0.6.0
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
STATICCHECK_VERSION ?= v0.8.0-rc.1

.PHONY: api-compat backend-hardening benchmark check coverage docs format \
	format-check fuzz integration lint mutation nilaway race staticcheck stress \
	test vet vuln workflows

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

stress:
	./scripts/check-stress.sh

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

nilaway:
	-$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) \
		-include-pkgs='github.com/faustbrian/golib/pkg/lease' ./...

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	$(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash --workers 2 --timeout-coefficient 10 \
		--threshold-efficacy 100 --threshold-mcover 100 ./memory
	./scripts/check-fence-mutations.sh

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

integration:
	$(GO) test -race -count=1 -timeout=15m ./postgres ./valkey

backend-hardening:
	./scripts/check-backend-faults.sh

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

workflows:
	$(ACTIONLINT) .github/workflows/*.yml

check: format-check vet test race stress coverage fuzz benchmark docs api-compat
