BINARY := bop
CMD := ./cmd/bop
BIN_DIR := bin

.PHONY: build test vet fmt lint clean

build:
	go build -o $(BIN_DIR)/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf $(BIN_DIR)
