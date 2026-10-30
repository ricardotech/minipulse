VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run test check clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o minipulse ./cmd/minipulse

run: build
	./minipulse serve

test:
	go test ./...

check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needs a run:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...

clean:
	rm -f minipulse
