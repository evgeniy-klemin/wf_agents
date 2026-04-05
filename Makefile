.PHONY: build worker web up down status install list timeline start clean lint frontend-build

LOCAL_BIN := $(CURDIR)/.bin
GO_VERSION := 1.26.1
GOLANGCI_LINT_VERSION := v2.11.4
GOLANGCI_LINT := $(LOCAL_BIN)/golangci-lint

# Install frontend dependencies
web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci

# Build frontend
frontend-build: web/node_modules
	cd web && npm run build
	@printf '%s' '<!-- Placeholder for go:embed. Replaced by Vite build output. -->' > cmd/web/static/placeholder

# Build all binaries
build: frontend-build
	go build -o bin/worker ./cmd/worker
	go build -o bin/wf-client ./cmd/client
	go build -o bin/hook-handler ./cmd/hook-handler
	go build -o bin/wf-web ./cmd/web
	go build -o bin/feedback-poll ./cmd/feedback-poll
	go build -o bin/pipeline-poll ./cmd/pipeline-poll

# Start Temporal infrastructure
up:
	docker compose up -d

# Stop Temporal infrastructure
down:
	docker compose down

# Run the Temporal worker
worker: build
	./bin/worker

# Shortcuts for client commands
start:
	@test -n "$(SESSION)" || (echo "Usage: make start SESSION=my-session TASK='description'"; exit 1)
	./bin/wf-client start --session $(SESSION) --task "$(TASK)"

status:
	@test -n "$(ID)" || (echo "Usage: make status ID=my-session"; exit 1)
	./bin/wf-client status $(ID)

timeline:
	@test -n "$(ID)" || (echo "Usage: make timeline ID=my-session"; exit 1)
	./bin/wf-client timeline $(ID)

list:
	./bin/wf-client list

# Run the web dashboard
web: build
	./bin/wf-web

# Install as Claude Code plugin
install: build
	@echo "Plugin ready at $(CURDIR)"
	@echo "Use: claude --plugin-dir $(CURDIR)"

# Clean build artifacts
clean:
	rm -rf bin/

$(GOLANGCI_LINT):
	GOTOOLCHAIN=go$(GO_VERSION) GOBIN=$(LOCAL_BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...
