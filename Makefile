# The Cru Trading API SDK for Go. `make` lists the targets.
#
# The tools run with `go run pkg@version` at the versions pinned here, so nothing needs
# installing. CI runs these same targets.

GOLANGCI_LINT_VERSION ?= v2.15.0
GOVULNCHECK_VERSION   ?= v1.9.0
APIDIFF_VERSION       ?= v0.0.0-20261014143641-f0251eb4aa3e

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK   := go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

FUZZTIME    ?= 30s
CI_FUZZTIME ?= 10s
COVER_MIN   ?= 85
BASE        ?= origin/main

.DEFAULT_GOAL := help
.PHONY: help generate verify-generated fmt fmt-check vet lint test race cover fuzz vuln integration examples tidy tidy-check apidiff ci

help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

generate: ## Regenerate the API surface from the spec (needs CRU_API_REPO, Cru's API checkout)
	@hack/generate.sh

verify-generated: ## Fail if regenerating changes anything (needs CRU_API_REPO)
	@hack/verify-generated.sh

fmt: ## Format every Go file with gofmt
	gofmt -w .

fmt-check: ## Fail if a Go file is not gofmt-formatted
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "Run make fmt; not formatted:"; echo "$$files"; exit 1; fi

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint with .golangci.yml
	$(GOLANGCI_LINT) run

test: ## Run the tests
	go test ./...

race: ## Run the tests with the race detector
	go test -race ./...

cover: ## Write coverage.out; print each package's coverage and the total; fail under COVER_MIN%
	@hack/cover.sh $(COVER_MIN)

fuzz: ## Run every fuzz target for FUZZTIME each (default 30s)
	@hack/fuzz.sh $(FUZZTIME)

vuln: ## Run govulncheck
	$(GOVULNCHECK) ./...

integration: ## Replay the captures in CRU_CAPTURES through the public API; CRU_LIVE=1 adds the live smoke test
	@if [ -z "$(CRU_CAPTURES)" ]; then echo "make integration needs CRU_CAPTURES, a capture run directory"; exit 1; fi
	go test -race -count=1 -v ./test/integration/...

examples: ## Build the example programs
	go build ./examples/...

tidy: ## Tidy go.mod
	go mod tidy

tidy-check: ## Fail if go.mod is not tidy
	go mod tidy -diff

apidiff: ## Report exported API changes against BASE (default origin/main); ALLOW_BREAKING=1 lets an incompatible one pass
	@APIDIFF_VERSION=$(APIDIFF_VERSION) hack/apidiff.sh $(BASE)

ci: fmt-check vet tidy-check examples lint vuln race cover ## Run every check CI runs, with a short fuzz run
	@$(MAKE) --no-print-directory fuzz FUZZTIME=$(CI_FUZZTIME)
