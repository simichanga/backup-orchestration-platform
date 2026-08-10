# BOP - Backup Orchestration Platform
# Run `make` or `make help` to see everything below with descriptions.

BINARY := bop
CMD := ./cmd/bop
BIN_DIR := bin
VERSION := $(shell git describe --tags --always --dirty || echo dev)

# A real deployment config is deployment-specific and never committed -
# override with `make run CONFIG=path\to\config.yaml`. For zero-config,
# use `make demo` instead, which builds a real one for you.
CONFIG ?= config.yaml

ifeq ($(OS),Windows_NT)
# GNU Make on Windows defaults to hunting for sh.exe to run every recipe
# line. Git for Windows ships one, but it's only on PATH from inside Git
# Bash - from a plain PowerShell or cmd prompt, Make can't find it and
# fails with a confusing "the system cannot find the path specified"
# before anything actually runs. cmd.exe needs no PATH search - it's
# always resolvable via COMSPEC - so use that instead on Windows.
SHELL := cmd.exe
.SHELLFLAGS := /C
BINARY := bop.exe
RM := if exist $(BIN_DIR) rmdir /S /Q $(BIN_DIR)

# Chocolatey's `make` (and most other Windows ports of it) is a 32-bit
# binary. A 32-bit process that launches "powershell.exe" by bare name
# gets silently redirected by Windows (WOW64 file system redirection) to
# the 32-bit copy under SysWOW64 - which then can't see 64-bit-only
# System32 tools like OpenSSH's ssh-keygen, so scripts/*.ps1 fail with
# "missing ssh-keygen" even though it's right there on PATH in a normal
# terminal. Sysnative is Windows' documented escape hatch: a virtual path
# only a 32-bit process can see, that bypasses the redirection and reaches
# the real 64-bit System32. Only use it if it actually resolves, though -
# it doesn't exist from a 64-bit process's point of view at all, which
# `make` sometimes is (nothing here assumes chocolatey specifically).
# Note: $(SystemRoot), not $(WINDIR) - Windows' actual environment block
# stores the variable as lowercase "windir", and Make's auto-import from
# the environment is case-sensitive unlike the OS itself, so $(WINDIR)
# silently expands to nothing. SystemRoot is the officially documented
# name and doesn't have this trap.
SYSNATIVE_PWSH := $(SystemRoot)\Sysnative\WindowsPowerShell\v1.0\powershell.exe
ifneq ($(wildcard $(SYSNATIVE_PWSH)),)
POWERSHELL := $(SYSNATIVE_PWSH)
else
POWERSHELL := powershell
endif
else
RM := rm -rf $(BIN_DIR)
POWERSHELL := powershell
endif

BIN := $(BIN_DIR)/$(BINARY)

# Forward slashes in $(BIN) are fine as a `go build -o` argument on any OS
# - Go handles either separator - but cmd.exe can't resolve "bin/bop.exe"
# as a command to actually launch (it reads the whole thing up to the
# first space, then chokes on the "/" instead of treating it as part of a
# path), so the run: target needs a backslash form specifically on Windows.
ifeq ($(OS),Windows_NT)
RUN_BIN := $(BIN_DIR)\$(BINARY)
else
RUN_BIN := $(BIN)
endif

.PHONY: help build build-web run dev demo demo-clean test vet fmt clean

.DEFAULT_GOAL := help

help:
	@echo BOP - available make targets:
	@echo   make build         Build bop (rebuilds the web UI first)
	@echo   make build-web     Rebuild only the embedded web UI
	@echo   make run           Build, then run the bundled binary against CONFIG= (default config.yaml)
	@echo   make dev           Run the backend and the frontend dev server together, hot-reloading (Windows, needs CONFIG=)
	@echo   make demo          Zero-config real demo: throwaway Docker target + real controller + browser (Windows)
	@echo   make demo-clean    Remove the demo's Docker container and temp files
	@echo   make test          go test ./...
	@echo   make vet           go vet ./...
	@echo   make fmt           List any files gofmt would reformat
	@echo   make clean         Remove bin/
	@echo Override CONFIG like this: make run CONFIG=path\to\config.yaml

# internal/webui embeds web/'s build output at compile time (see
# internal/webui/webui.go) - go build alone reuses whatever's already
# committed there, build-web is only needed after changing web/ itself.
build: build-web
	go build -ldflags "-X bop/internal/cli.Version=$(VERSION)" -o $(BIN) $(CMD)

build-web:
	npm --prefix web ci
	npm --prefix web run build

run: build
	$(RUN_BIN) --config $(CONFIG) controller

dev:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File scripts\dev.ps1 -Config $(CONFIG)

demo:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File scripts\try-it-out.ps1

demo-clean:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File scripts\try-it-out.ps1 -Cleanup

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	$(RM)
