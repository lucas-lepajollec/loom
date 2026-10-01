.PHONY: default assemble-ui build api web test check-ui clean

default: build

assemble-ui:
	go run ./tools/assemble-ui

build: assemble-ui
	mkdir -p bin
	go build -o bin/loom ./cmd/loom

api:
	go run ./cmd/loom web 8091

web: api

test:
	go test ./...

# Syntax check of the new UI modules. Plain `node --check` treats .js as
# CommonJS and misses module syntax errors, hence the module default type.
check-ui:
	@for f in $$(find internal/loom/ui/next/js -name '*.js'); do node --experimental-default-type=module --check "$$f" || exit 1; done
	node --test internal/loom/ui/tests/*.test.mjs

clean:
	rm -rf bin/
