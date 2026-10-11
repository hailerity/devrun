.PHONY: build test test-integration lint install clean demo

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

# Re-record the README hero. Needs vhs (brew install vhs), and runs
# natively rather than in the VHS container, which is Linux.
demo: build
	vhs validate assets/demo.tape
	vhs validate assets/screenshot.tape
	vhs assets/demo.tape
	vhs assets/screenshot.tape
	rm -f assets/screenshot-unused.gif
