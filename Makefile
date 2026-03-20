.PHONY: build test test-integration lint install clean

build:
	go build -o bin/procet ./cmd/procet

test:
	go test ./... -v -count=1

test-integration:
	go test -tags integration ./internal/integration/... -v -timeout 60s

lint:
	golangci-lint run

install:
	go install ./cmd/procet

clean:
	rm -rf bin/
