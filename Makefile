.PHONY: build test test-integration lint install clean

build:
	go build -o bin/devrun ./cmd/devrun

test:
	go test ./... -v -count=1

test-integration:
	go test -tags integration ./internal/integration/... -v -timeout 60s

lint:
	golangci-lint run

install:
	go install ./cmd/devrun

clean:
	rm -rf bin/
