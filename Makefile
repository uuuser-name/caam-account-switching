.PHONY: build test test-race test-audit prepush-preflight ci-e2e-quality-gate quality-trend-diff auto-regression-bead validate-regression-autocreate release-attestation validate-e2e-schema validate-failure-packet validate-e2e-parity failure-packet failure-packet-publish failure-packet-latest lint-test-realism clean install lint fmt tidy run tui help dev-setup release-snapshot

# Binary name
BINARY=caam

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w \
	-X github.com/Dicklesworthstone/coding_agent_account_manager/internal/version.Version=$(VERSION) \
	-X github.com/Dicklesworthstone/coding_agent_account_manager/internal/version.Commit=$(COMMIT) \
	-X github.com/Dicklesworthstone/coding_agent_account_manager/internal/version.Date=$(DATE)"

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/caam

# Run tests
test:
	go test -v ./...

# Run audit pipeline for coverage/reality/e2e inventory artifacts
test-audit:
	./scripts/test_audit.sh

# Local mirror of CI preflight gate for developers before push
prepush-preflight:
	./scripts/prepush_preflight.sh

# CI quality gate for E2E logging/forensics/parity/traceability semantics
ci-e2e-quality-gate:
	./scripts/ci_e2e_quality_gate.sh

# Build quality trend diff artifact (baseline deltas + regression signals)
quality-trend-diff:
	./scripts/build_quality_trend_diff.sh

# Auto-create remediation bead from trend regression input
auto-regression-bead:
	./scripts/auto_create_regression_bead.sh

# Validate regression auto-create behavior in no-regression and dry-run regression modes
validate-regression-autocreate:
	./scripts/validate_regression_bead_autocreate.sh

# Build and enforce release readiness attestation artifact
release-attestation:
	./scripts/release_readiness_attestation.sh

# Validate e2e JSONL logs against the canonical schema contract
validate-e2e-schema:
	./scripts/validate_e2e_log_fixtures.sh

# Validate failure-packet generation/publish/fetch pipeline with deterministic fixtures
validate-failure-packet:
	./scripts/validate_failure_packet_pipeline.sh

# Validate dry-run/live-run parity checker using canonical fixture pairs
validate-e2e-parity:
	./scripts/validate_e2e_dry_live_parity_fixtures.sh

# Generate a concise failure summary and packaged debug artifact bundle
failure-packet:
	./scripts/generate_failure_packet.sh

# Publish latest generated failure packet into local/CI registry index
failure-packet-publish:
	./scripts/failure_packet_ctl.sh publish

# Retrieve latest published failure packet metadata
failure-packet-latest:
	./scripts/failure_packet_ctl.sh latest

# Fail when undocumented core-scope doubles are present
lint-test-realism:
	./scripts/lint_test_realism.sh --strict

# Run tests with race detector
test-race:
	go test -race -v ./...

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

# Tidy dependencies
tidy:
	go mod tidy

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/caam

# Clean build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/

# Run the application
run: build
	./$(BINARY)

# Run the TUI
tui: build
	./$(BINARY)

# Build for all platforms (requires goreleaser)
release-snapshot:
	goreleaser release --snapshot --skip=sign --clean

# Development setup
dev-setup:
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/goreleaser/goreleaser@latest

# Show help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  test            - Run tests"
	@echo "  test-race       - Run tests with race detector"
	@echo "  test-audit      - Generate coverage/reality/e2e audit artifacts"
	@echo "  prepush-preflight - Run local mirror of CI preflight checks"
	@echo "  ci-e2e-quality-gate - Run E2E quality gate (schema/forensics/parity/traceability)"
	@echo "  quality-trend-diff - Build quality trend delta artifact against baseline"
	@echo "  auto-regression-bead - Auto-create remediation bead when trend status is regression"
	@echo "  validate-regression-autocreate - Validate regression bead auto-create behavior"
	@echo "  release-attestation - Build and enforce release readiness attestation"
	@echo "  validate-e2e-schema - Validate e2e JSONL log fixtures against schema"
	@echo "  validate-failure-packet - Validate failure-packet generation/publish/fetch pipeline"
	@echo "  validate-e2e-parity - Validate dry-run/live-run parity checker fixtures"
	@echo "  failure-packet  - Build failure summary + packaged debug bundle"
	@echo "  failure-packet-publish - Publish latest failure packet into registry index"
	@echo "  failure-packet-latest - Print latest published failure packet metadata"
	@echo "  lint-test-realism - Fail on undocumented core-scope test doubles"
	@echo "  lint            - Run linter"
	@echo "  fmt             - Format code"
	@echo "  tidy            - Tidy dependencies"
	@echo "  install         - Install to GOPATH/bin"
	@echo "  clean           - Clean build artifacts"
	@echo "  run             - Build and run"
	@echo "  tui             - Build and run TUI"
	@echo "  release-snapshot - Build for all platforms (skip signing)"
	@echo "  dev-setup       - Install development tools"
