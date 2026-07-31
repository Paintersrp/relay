.PHONY: dev dev-server build assets install sqlc templ test fmt vet clean validate mcp-http-test mcp-http-smoke

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
	go test ./...
	cd apps/web && npm run typecheck
	cd apps/web && npm run test
	cd apps/web && npm run build

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin/ tmp/ web/static/app.css web/static/app.js web/static/app.css.map web/static/app.js.map

mcp-http-test:
	go test ./internal/mcp/... ./internal/server/...

mcp-http-smoke:
	@if [ -z "$$RELAY_MCP_URL" ] || [ -z "$$RELAY_MCP_AUTH_TOKEN" ]; then \
		echo "ERROR: RELAY_MCP_URL and RELAY_MCP_AUTH_TOKEN environment variables must be set for mcp-http-smoke."; \
		echo "Usage: make mcp-http-smoke RELAY_MCP_URL=http://localhost:8080/mcp/planner RELAY_MCP_AUTH_TOKEN=dev-token"; \
		exit 1; \
	fi
	go run ./cmd/mcp-smoke

workflow-db-status:
	goose -dir internal/db/workflow_migrations sqlite3 data/workflow/relay-workflow.sqlite status

workflow-db-migrate:
	goose -dir internal/db/workflow_migrations sqlite3 data/workflow/relay-workflow.sqlite up
