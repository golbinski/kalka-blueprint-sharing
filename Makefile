# The web UI is built into internal/ui/dist and embedded by `go build`.
# Without `make web` the binary works but serves a "UI not built" page.

.PHONY: all web build test test-web clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	go build -o kalka ./cmd/kalka

test: test-web
	go test -race ./...

test-web:
	cd web && npm test

clean:
	rm -rf kalka internal/ui/dist/assets internal/ui/dist/index.html
