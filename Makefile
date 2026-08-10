BINARY := bop
CMD := ./cmd/bop
BIN_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-web test vet fmt lint clean

# internal/webui embeds web/'s build output at compile time (see
# internal/webui/webui.go) - go build alone reuses whatever's already
# committed there, build-web is only needed after changing web/ itself.
build: build-web
	go build -ldflags "-X bop/internal/cli.Version=$(VERSION)" -o $(BIN_DIR)/$(BINARY) $(CMD)

build-web:
	npm --prefix web ci
	npm --prefix web run build

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf $(BIN_DIR)
