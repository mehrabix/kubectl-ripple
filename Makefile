BINARY  := kubectl-ripple
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/mehrabix/kubectl-ripple/internal/cli.Version=$(VERSION) \
	-X github.com/mehrabix/kubectl-ripple/internal/cli.Commit=$(COMMIT) \
	-X github.com/mehrabix/kubectl-ripple/internal/cli.BuildDate=$(DATE)

.PHONY: all
all: lint test build

.PHONY: build
build: ## build the plugin into bin/
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: install
install: ## install the plugin into $GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

.PHONY: test
test: ## run the unit tests
	go test ./... -race -cover

.PHONY: e2e
e2e: build ## run the end-to-end test against the current kube-context
	BIN=$(CURDIR)/bin/$(BINARY) ./test/e2e/run.sh

.PHONY: fmt
fmt: ## format the source
	gofmt -w .

.PHONY: lint
lint: ## check formatting and run go vet
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...

.PHONY: cover
cover: ## write an html coverage report
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

.PHONY: chart
chart: ## package the helm chart into dist/
	helm package deploy/helm/ripple --destination dist

.PHONY: clean
clean:
	rm -rf bin dist coverage.out coverage.html

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
