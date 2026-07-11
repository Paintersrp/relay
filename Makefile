.PHONY: dev dev-server build assets install sqlc templ test fmt vet clean validate validate-fast validate-broad validate-full validate-touched validate-changed mcp-build mcp-test mcp-smoke mcp-clean mcp-http-test mcp-http-smoke

MCP_BINARY := bin/relay-mcpserver
ifeq ($(OS),Windows_NT)
MCP_BINARY := bin/relay-mcpserver.exe
endif

install:
	npm install

assets:
	npm run build

sqlc:
	sqlc generate

templ:
	templ generate

build: assets sqlc templ
	go build -o bin/relay.exe ./cmd/relay

dev:
	RELAY_DEV_RELOAD=1 npm run dev

dev-server:
	RELAY_DEV_RELOAD=1 air -c .air.toml

test:
	go test ./...

validate:
	bash scripts/validate.sh

validate-fast:
	RELAY_VALIDATE_TIER=fast bash scripts/validate.sh

validate-broad:
	RELAY_VALIDATE_TIER=broad bash scripts/validate.sh

validate-full:
	RELAY_VALIDATE_TIER=full bash scripts/validate.sh

validate-touched:
	RELAY_VALIDATE_SCOPE=touched PATHS="$(PATHS)" bash scripts/validate.sh

validate-changed:
	RELAY_VALIDATE_SCOPE=changed bash scripts/validate.sh

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin/ tmp/ web/static/app.css web/static/app.js web/static/app.css.map web/static/app.js.map

mcp-build:
	go build -o $(MCP_BINARY) ./cmd/mcpserver

mcp-test:
	go test ./internal/mcp/... ./cmd/mcpserver/...

mcp-smoke: mcp-build
	RELAY_MCP_URL='' RELAY_MCP_AUTH_TOKEN='' RELAY_MCP_BINARY='$(MCP_BINARY)' go run ./cmd/mcp-smoke

mcp-http-test:
	go test ./internal/mcp/... ./internal/server/...

mcp-http-smoke:
	@if [ -z "$$RELAY_MCP_URL" ] || [ -z "$$RELAY_MCP_AUTH_TOKEN" ]; then \
		echo "ERROR: RELAY_MCP_URL and RELAY_MCP_AUTH_TOKEN environment variables must be set for mcp-http-smoke."; \
		echo "Usage: make mcp-http-smoke RELAY_MCP_URL=http://localhost:8080/mcp RELAY_MCP_AUTH_TOKEN=dev-token"; \
		exit 1; \
	fi
	go run ./cmd/mcp-smoke

mcp-clean:
	rm -f bin/relay-mcpserver.exe bin/relay-mcpserver

workflow-db-status:
	goose -dir internal/db/workflow_migrations sqlite3 data/workflow/relay-workflow.sqlite status

workflow-db-migrate:
	goose -dir internal/db/workflow_migrations sqlite3 data/workflow/relay-workflow.sqlite up
