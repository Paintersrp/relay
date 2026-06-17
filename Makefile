.PHONY: dev dev-server build assets install sqlc templ db-migrate test fmt vet clean mcp-build mcp-test mcp-smoke mcp-clean

install:
	npm install

assets:
	npm run build

sqlc:
	sqlc generate

templ:
	templ generate

db-migrate:
	goose -dir internal/db/migrations sqlite3 data/relay.sqlite up

build: assets sqlc templ
	go build -o bin/relay.exe ./cmd/relay

dev:
	RELAY_DEV_RELOAD=1 npm run dev

dev-server:
	RELAY_DEV_RELOAD=1 air -c .air.toml

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin/ tmp/ web/static/app.css web/static/app.js web/static/app.css.map web/static/app.js.map

## MCP targets

mcp-build:
	go build -o bin/relay-mcpserver.exe ./cmd/mcpserver

mcp-test:
	go test ./internal/mcp/... ./cmd/mcpserver/...

mcp-smoke: mcp-build
	go run ./cmd/mcp-smoke

mcp-clean:
	rm -f bin/relay-mcpserver.exe bin/relay-mcpserver
