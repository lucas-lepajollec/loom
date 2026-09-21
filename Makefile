.PHONY: default assemble-ui build api web test clean

default: build

assemble-ui:
	go run ./tools/assemble-ui

build: assemble-ui
	mkdir -p bin
	go build -o bin/loom ./cmd/ajean

api:
	go run ./cmd/ajean web 8091

web: api

test:
	go test ./...

clean:
	rm -rf bin/
