#!/usr/bin/env bash
# Checks that everything BOP's build/dev/demo workflow needs is actually
# installed and working, pinpointing exactly which tool is missing (or
# misconfigured) and how to fix it - rather than letting `make build`/
# `make demo` fail three layers deep with a confusing error. Also
# scaffolds config.yaml/inventory.yaml/data/ from the committed
# *.example.yaml templates if they don't exist yet, so `make run`/
# `make dev` have something real to point at immediately.
#
# Safe to re-run any time: every check is read-only, and the scaffolding
# step never overwrites a file that already exists.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

any_required_missing=0
results=()

# Each result: "name|ok|detail|required|hint"
add_result() {
    results+=("$1|$2|$3|$4|$5")
}

echo "==> Checking required tools (build/run/dev)"

if command -v go >/dev/null 2>&1; then
    add_result "Go" 1 "$(go version)" 1 ""
else
    add_result "Go" 0 "" 1 "Install from https://go.dev/dl/, then reopen your terminal (or: your distro's package manager, if it has a recent enough version)."
fi

if command -v node >/dev/null 2>&1; then
    add_result "Node.js" 1 "$(node --version)" 1 ""
else
    add_result "Node.js" 0 "" 1 "Install from https://nodejs.org/ (npm comes with it) or via nvm/your package manager."
fi

if command -v npm >/dev/null 2>&1; then
    add_result "npm" 1 "v$(npm --version)" 1 ""
else
    add_result "npm" 0 "" 1 "Comes with Node.js - install from https://nodejs.org/ or via nvm/your package manager."
fi

if command -v git >/dev/null 2>&1; then
    add_result "Git" 1 "$(git --version)" 1 ""
else
    add_result "Git" 0 "" 1 "Install via your package manager (e.g. apt install git)."
fi

echo "==> Checking extras (needed for make demo / real backups)"

if ! command -v docker >/dev/null 2>&1; then
    add_result "Docker" 0 "" 0 "Install Docker: https://docs.docker.com/engine/install/ (only needed for make demo)."
elif docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
    add_result "Docker" 1 "engine $(docker version --format '{{.Server.Version}}' 2>/dev/null)" 0 ""
else
    add_result "Docker" 0 "installed, daemon not responding" 0 "Docker is installed but its daemon isn't reachable - start it (systemctl start docker, or your distro's equivalent) and check your user is in the docker group (only needed for make demo)."
fi

if command -v restic >/dev/null 2>&1; then
    add_result "restic" 1 "$(restic version)" 0 ""
else
    add_result "restic" 0 "" 0 "Install from https://restic.net/ (only needed to actually run a backup, not to build)."
fi

if command -v ssh-keygen >/dev/null 2>&1; then
    add_result "ssh-keygen" 1 "$(command -v ssh-keygen)" 0 ""
else
    add_result "ssh-keygen" 0 "" 0 "Install openssh-client via your package manager (only needed for make demo)."
fi

echo ""
echo "==> Results"
for r in "${results[@]}"; do
    IFS='|' read -r name ok detail required hint <<<"$r"
    if [ "$ok" = "1" ]; then
        printf '[OK]   %-11s %s\n' "$name" "$detail"
    elif [ "$required" = "1" ]; then
        printf '[FAIL] %-11s not found\n' "$name"
        any_required_missing=1
    else
        printf '[--]   %-11s not found\n' "$name"
    fi
    if [ "$ok" = "0" ] && [ -n "$hint" ]; then
        printf '       -> %s\n' "$hint"
    fi
done

if [ "$any_required_missing" = "1" ]; then
    echo ""
    echo "Missing required tools above - fix those first, then re-run ./scripts/setup.sh."
    exit 1
fi

echo ""
echo "==> Setting up config.yaml / inventory.yaml / data/"

copy_if_missing() {
    if [ -f "$2" ]; then
        echo "  $2 already exists - left untouched."
    else
        cp "$1" "$2"
        echo "  Created $2 from $(basename "$1")"
    fi
}

random_token() {
    LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32
}

write_if_missing() {
    if [ -f "$1" ]; then
        echo "  $1 already exists - left untouched."
    else
        printf '%s' "$2" >"$1"
        echo "  Created $1"
    fi
}

cd "$repo_root"
mkdir -p data

copy_if_missing config.example.yaml config.yaml
copy_if_missing inventory.example.yaml inventory.yaml

# Matches config.example.yaml's data/ paths - a real restic password and
# real bearer tokens, generated locally, never committed (data/ is
# gitignored). Not a substitute for docs/03-getting-started/
# configuration.md#secrets-management in production.
write_if_missing data/restic-password.txt "$(random_token)"
write_if_missing data/api-tokens.txt "$(random_token)"
write_if_missing data/api-write-tokens.txt "$(random_token)"
write_if_missing data/known_hosts ""

echo ""
echo "==> Ready"
echo "  make run   - build and run against config.yaml (the placeholder inventory host won't actually connect - edit inventory.yaml with a real one, see docs/03-getting-started/inventory-reference.md)"
echo "  make dev   - backend + frontend dev server together, hot-reloading"
echo "  make demo  - a real, fully working demo against a throwaway Docker target, no editing required"
echo ""
echo "  Read tokens:  cat data/api-tokens.txt"
echo "  Write tokens: cat data/api-write-tokens.txt"
