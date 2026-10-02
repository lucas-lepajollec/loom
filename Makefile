.PHONY: default help build api web test check-ui clean

default: build

help:
	@printf '%s\n' \
	  'make (default)  Build bin/loom with the embedded UI' \
	  'make build      Build bin/loom with the embedded UI' \
	  'make web        Run the web dashboard on port 2510 via go run' \
	  'make api        Alias for the same web dashboard command' \
	  'make test       Run the full Go test suite (go test ./...)' \
	  'make check-ui   Syntax-check ES modules and run Node UI tests' \
	  'make clean      Remove bin/' \
	  'make help       Show this target list'

build:
	mkdir -p bin
	go build -o bin/loom ./cmd/loom

api:
	go run ./cmd/loom web 2510

web: api

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
