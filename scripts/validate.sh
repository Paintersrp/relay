#!/usr/bin/env bash
set -e

echo "Running validation checks..."

echo "1. Formatting Go code..."
rtk.exe test "go fmt ./..." || go fmt ./...

echo "2. Vetting Go code..."
rtk.exe test "go vet ./..." || go vet ./...

echo "3. Generating templ files..."
rtk.exe test "templ generate" || templ generate

echo "4. Generating sqlc code..."
rtk.exe test "sqlc generate" || sqlc generate

echo "5. Testing Go code..."
rtk.exe test "go test ./..." || go test ./...

echo "6. Building frontend assets..."
rtk.exe test "npm run build" || npm run build

echo "Validation successful!"
