BINARY := bop
CMD := ./cmd/bop
BIN_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet fmt lint clean

build:
	go build -ldflags "-X bop/internal/cli.Version=$(VERSION)" -o $(BIN_DIR)/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf $(BIN_DIR)
