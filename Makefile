SHELL := /bin/bash

define PY_CODEQL_SUMMARY
import glob, json
from collections import Counter

sarifs = sorted(glob.glob(".tmp/codeql/*.sarif"))
if not sarifs:
    print("No SARIF files found under .tmp/codeql/. Did make code-ql run successfully?")
    raise SystemExit(2)

def first_location(result):
    locs = result.get("locations") or []
    if not locs:
        return ("unknown", 0)
    pl = (locs[0].get("physicalLocation") or {})
    file = ((pl.get("artifactLocation") or {}).get("uri") or "unknown")
    line = ((pl.get("region") or {}).get("startLine") or 0)
    return (file, line)

total = 0
levels = Counter()
rows = []

for path in sarifs:
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    for run in data.get("runs", []):
        for r in (run.get("results") or []):
            lvl = (r.get("level") or "warning").lower()
            rule = r.get("ruleId") or "no-rule"
            msg = (r.get("message") or {}).get("text") or "no-message"
            file, line = first_location(r)
            levels[lvl] += 1
            total += 1
            rows.append((lvl, rule, file, line, msg))

print("\nCodeQL SARIF Summary:")
for p in sarifs:
    print(f"  - {p}")
print(f"\n  Total findings: {total}")
if total:
    print("  By level: " + ", ".join(f"{k}={v}" for k, v in sorted(levels.items())))
print("")

priority = {"error": 0, "warning": 1, "note": 2, "recommendation": 3}
rows.sort(key=lambda x: (priority.get(x[0], 9), x[2], x[3], x[1]))

limit = 50
if not rows:
    print("  OK: No findings.")
else:
    print(f"  Top {min(limit, len(rows))} findings:")
    for i, (lvl, rule, file, line, msg) in enumerate(rows[:limit], 1):
        msg = msg.replace("\n", " ").strip()
        if len(msg) > 120:
            msg = msg[:117] + "..."
        print(f"  {i:>2}. [{lvl}] {rule}")
        print(f"      {file}:{line}")
        print(f"      {msg}")
        print("")
endef
export PY_CODEQL_SUMMARY

define PY_CODEQL_GATE
import glob, json
sarifs = sorted(glob.glob(".tmp/codeql/*.sarif"))
errors = 0
warnings = 0
for path in sarifs:
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    for run in data.get("runs", []):
        for r in (run.get("results") or []):
            lvl = (r.get("level") or "warning").lower()
            if lvl == "error":
                errors += 1
            elif lvl == "warning":
                warnings += 1

if errors > 0:
    print(f"\nCodeQL gate failed: {errors} blocking error(s) found.")
    if warnings > 0:
        print(f"Also found {warnings} non-blocking warning(s).")
    raise SystemExit(1)

print("\nCodeQL gate passed: no blocking errors found.")
if warnings > 0:
    print(f"Note: {warnings} non-blocking warning(s) were found.")
endef
export PY_CODEQL_GATE

.PHONY: all build binaries release-binaries run start stop restart clean test check test-smoke vet race fmt fmt-check commit-check code-ql code-ql-summary code-ql-gate helm-check docker docker-build docker-run docs docs-check

BIN_DIR=bin
KAF_MIRROR_BINARY=$(BIN_DIR)/kaf-mirror
ADMIN_CLI_BINARY=$(BIN_DIR)/admin-cli
MIRROR_CLI_BINARY=$(BIN_DIR)/mirror-cli
RELEASE_DIR=dist
RELEASE_CC?=x86_64-linux-gnu-gcc
PID_FILE=/tmp/kaf-mirror.pid

all: build

# The 'docs' target generates API documentation using Swagger.
# It is intentionally kept separate from the main 'build' target
# to avoid issues with the swag tool parsing non-application Go files.
docs:
	@echo "Generating Swagger docs..."
	@GOPATH=$(shell go env GOPATH); \
	if [ -z "$$GOPATH" ]; then \
		echo "GOPATH is not set. Please set it and try again."; \
		exit 1; \
	fi; \
	go install github.com/swaggo/swag/cmd/swag@latest; \
	$$GOPATH/bin/swag init -g cmd/kaf-mirror/main.go --output web/docu/swagger --exclude scripts

build: docs docs-cli docs-html binaries

binaries:
	@echo "Building..."
	@mkdir -p $(BIN_DIR)
	$(eval VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null))
	@go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(KAF_MIRROR_BINARY) ./cmd/kaf-mirror
	@go build -ldflags="-s -w" -o $(ADMIN_CLI_BINARY) ./cmd/admin-cli
	@go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(MIRROR_CLI_BINARY) ./cmd/mirror-cli

release-binaries:
	@test -n "$(VERSION)" || { echo "Set VERSION, for example make release-binaries VERSION=v1.4.0"; exit 1; }
	@mkdir -p $(RELEASE_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(RELEASE_CC) go build -v -ldflags="-s -w -X main.Version=$(VERSION)" -o $(RELEASE_DIR)/kaf-mirror-linux-amd64 ./cmd/kaf-mirror
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(RELEASE_CC) go build -v -ldflags="-s -w" -o $(RELEASE_DIR)/admin-cli-linux-amd64 ./cmd/admin-cli
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(RELEASE_CC) go build -v -ldflags="-s -w -X main.Version=$(VERSION)" -o $(RELEASE_DIR)/mirror-cli-linux-amd64 ./cmd/mirror-cli
	GOOS=windows GOARCH=amd64 go build -v -ldflags="-s -w -X main.Version=$(VERSION)" -o $(RELEASE_DIR)/mirror-cli-windows-amd64.exe ./cmd/mirror-cli
	GOOS=darwin GOARCH=amd64 go build -v -ldflags="-s -w -X main.Version=$(VERSION)" -o $(RELEASE_DIR)/mirror-cli-darwin-amd64 ./cmd/mirror-cli

build-linux:
	@echo "Linux binaries are downloaded from GitHub releases during deployment."
	@echo "This target is a placeholder."

run: build
	@echo "Running..."
	@./$(KAF_MIRROR_BINARY)

start: build
	@echo "Starting..."
	@./$(KAF_MIRROR_BINARY) & echo $$! > $(PID_FILE)
	@echo "PID: `cat $(PID_FILE)`"

stop:
	@echo "Stopping..."
	@pkill -f kaf-mirror || echo "Process not found."
	@rm -f $(PID_FILE)

restart: stop start

update: build restart

clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f $(PID_FILE)

test: check vet race

check: test-smoke

test-smoke:
	@echo "Running smoke tests..."
	@go test ./...

vet:
	@echo "Running go vet..."
	@go vet ./...

race:
	@echo "Running race tests..."
	@go test -race ./...

helm-check:
	@echo "Checking the Helm chart..."
	@bash scripts/check-helm-chart.sh

fmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Found unformatted files. Auto-formatting:"; \
		echo "$$unformatted"; \
		gofmt -w .; \
	else \
		echo "All Go files are formatted correctly."; \
	fi

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		echo "Run 'make fmt' or 'gofmt -w .' to fix formatting."; \
		exit 1; \
	fi

commit-check: fmt vet test-smoke race code-ql-gate
	@echo "commit-check completed."

code-ql:
	bash scripts/codeql_local.sh

code-ql-summary: code-ql
	@printf "%s\n" "$$PY_CODEQL_SUMMARY" | python3 -

code-ql-gate: code-ql-summary
	@printf "%s\n" "$$PY_CODEQL_GATE" | python3 -

docker-build:
	@echo "Building Docker image..."
	@docker build -t kaf-mirror:latest .

docker-run: docker-build
	@echo "Running Docker container..."
	@docker run -p 8080:8080 --name kaf-mirror -v kaf-mirror-data:/app/data -v kaf-mirror-logs:/app/logs kaf-mirror:latest

docs-html:
	@echo "Generating HTML documentation..."
	@go run scripts/generate-docs.go
	@go run scripts/generate-rbac-html.go

docs-cli:
	@echo "Generating CLI documentation (Markdown)..."
	@go run ./cmd/mirror-cli docs generate

docs-check: docs-cli docs-html
	@git diff --exit-code -- docs web/docu || { echo "Generated docs are out of date. Run 'make docs-cli docs-html' and commit the result."; exit 1; }

# Detect docker compose command (plugin or standalone)
COMPOSE ?= $(shell if docker compose version >/dev/null 2>&1; then echo "docker compose"; elif command -v docker-compose >/dev/null 2>&1; then echo "docker-compose"; else echo ""; fi)
PROJECT_NAME ?= kaf-mirror

dev-up:
	@if [ -z "$(COMPOSE)" ]; then \
		echo "docker compose not found. Install Docker Desktop/Compose or ensure compose plugin is available."; \
		exit 1; \
	fi; \
	$(COMPOSE) -p $(PROJECT_NAME) up -d --build

dev-down:
	@if [ -z "$(COMPOSE)" ]; then \
		echo "docker compose not found. Install Docker Desktop/Compose or ensure compose plugin is available."; \
		exit 1; \
	fi; \
	$(COMPOSE) -p $(PROJECT_NAME) down -v --remove-orphans || true; \
	docker network rm $(PROJECT_NAME)_kaf-mirror 2>/dev/null || true
