export GOTOOLCHAIN := auto

.PHONY: build test lint fmt demo

build:
	go build -o bin/earth-profile ./cmd/earth-profile

test:
	go test -race ./...

lint:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...

fmt:
	gofmt -w cmd internal

demo: build
	./bin/earth-profile --db /tmp/earth-profile-demo.db import testdata/demo.json
	./bin/earth-profile --db /tmp/earth-profile-demo.db browse
