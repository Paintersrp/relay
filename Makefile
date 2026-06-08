.PHONY: dev build assets install sqlc templ db-migrate test fmt clean

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

dev: assets
	go run ./cmd/relay

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin/ tmp/ web/static/app.css web/static/app.js web/static/app.css.map web/static/app.js.map
