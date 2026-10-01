.PHONY: default help build api web test check-ui clean

default: build

help:
	@printf '%s\n' \
	  'make (default)  Build bin/loom with the embedded UI' \
	  'make build      Build bin/loom with the embedded UI' \
	  'make web        Run the web dashboard on port 8091 via go run' \
	  'make api        Alias for the same web dashboard command' \
	  'make test       Run the full Go test suite (go test ./...)' \
	  'make check-ui   Syntax-check ES modules and run Node UI tests' \
	  'make clean      Remove bin/' \
	  'make help       Show this target list'

build:
	mkdir -p bin
	go build -o bin/loom ./cmd/loom

api:
	go run ./cmd/loom web 8091

web: api

test:
	go test ./...

# Syntax check of the UI modules. Plain `node --check` treats .js as
# CommonJS and misses module syntax errors, hence the module default type.
check-ui:
	@for f in $$(find internal/loom/ui/next/js -name '*.js'); do node --experimental-default-type=module --check "$$f" || exit 1; done
	node --test internal/loom/ui/tests/*.test.mjs

clean:
	rm -rf bin/
