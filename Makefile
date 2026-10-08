.PHONY: default help build api web dev test check-ui agents-watch clean

DEV_HOME ?= $(CURDIR)/.project-local/runtime
DEV_HOST ?= 127.0.0.1
DEV_PORT ?= 2594

default: build

help:
	@printf '%s\n' \
	  'make (default)  Build bin/loom with the embedded UI' \
	  'make build      Build bin/loom with the embedded UI' \
	  'make web        Run the web dashboard on port 2510 via go run' \
	  'make api        Alias for the same web dashboard command' \
	  'make dev        Rebuild/run isolated development data on port 2594' \
	  '                Set DEV_HOST to a LAN IP for phone testing (auth required)' \
	  'make test       Run the full Go test suite (go test ./...)' \
	  'make check-ui   Syntax-check ES modules and run Node UI tests' \
	  'make agents-watch  Check installed agent contracts; AGENTS_WATCH_ARGS=--install for latest' \
	  'make clean      Remove bin/' \
	  'make help       Show this target list'

build:
	mkdir -p bin
	go build -o bin/loom ./cmd/loom

api:
	go run ./cmd/loom web 2510

web: api

# Foreground only: no install, production data or production service names.
# go run embeds the current UI again on every launch, including uncommitted edits.
dev:
	LOOM_HOME="$(DEV_HOME)" LOOM_WEB_HOST="$(DEV_HOST)" \
	  LOOM_SERVICE=loom-dev-engine LOOM_UI_SERVICE=loom-dev-ui \
	  go run ./cmd/loom web "$(DEV_PORT)"

test:
	go test ./...

# Syntax check of the UI modules. Plain `node --check` treats .js as
# CommonJS and misses module syntax errors, so each file is checked through an
# .mjs link (works on every Node version).
check-ui:
	@tmp=$$(mktemp -d); for f in $$(find internal/loom/ui/next/js -name '*.js'); do cp "$$f" "$$tmp/check.mjs"; node --check "$$tmp/check.mjs" || { echo "$$f"; rm -rf "$$tmp"; exit 1; }; done; rm -rf "$$tmp"
	node --test internal/loom/ui/tests/*.test.mjs

clean:
	rm -rf bin/

# Local runs write reports only; --publish explicitly enables GitHub review writes.
AGENTS_WATCH_ARGS ?=
agents-watch:
	go run ./tools/agents-watch $(AGENTS_WATCH_ARGS)
