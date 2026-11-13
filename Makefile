# The web UI is built into internal/ui/dist and embedded by `go build`.
# Without `make web` the binary works but serves a "UI not built" page.

.PHONY: all web build test test-web release clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	go build -o kalka ./cmd/kalka
	go build -o kalka-plugin-gh ./cmd/kalka-plugin-gh

test: test-web
	go test -race ./...

test-web:
	cd web && npm test

# Reproducible archives for every target in dist/: make release VERSION=0.2.0
release: web
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=x.y.z" && exit 2)
	go run ./tools/release -version $(VERSION)

clean:
	rm -rf kalka kalka-plugin-gh internal/ui/dist/assets internal/ui/dist/index.html
