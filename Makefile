.PHONY: build worker web up down status install list timeline start clean

# Build all binaries
build:
	go build -o bin/worker ./cmd/worker
	go build -o bin/wf-client ./cmd/client
	go build -o bin/hook-handler ./cmd/hook-handler
	go build -o bin/wf-web ./cmd/web

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
