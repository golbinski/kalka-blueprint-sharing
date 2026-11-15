# The web UI is built into internal/ui/dist and embedded by `go build`.
# Without `make web` the binary works but serves a "UI not built" page.

.PHONY: all web build desktop test test-web release clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	go build -o kalka ./cmd/kalka
	go build -o kalka-plugin-gh ./cmd/kalka-plugin-gh

# The desktop shell for this host (cgo on macOS and Linux; Linux needs
# libgtk-4-dev and libwebkitgtk-6.0-dev). make release builds Kalka.app and
# the other desktop packages.
desktop:
	MACOSX_DEPLOYMENT_TARGET=11.0 go build -tags desktop,production -o kalka-desktop ./cmd/kalka-desktop

test: test-web
	go test -race ./...

test-web:
	cd web && npm test

# Every artifact this host can build, in dist/: make release VERSION=0.2.0
# Signing settings: see tools/release/signing.go (unsigned without them).
release: web
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=x.y.z" && exit 2)
	go run ./tools/release -version $(VERSION)

clean:
	rm -rf kalka kalka-desktop kalka-plugin-gh internal/ui/dist/assets internal/ui/dist/index.html
