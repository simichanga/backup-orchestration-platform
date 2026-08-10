#!/usr/bin/env bash
# Runs the backend and the frontend dev server together: the backend
# serves the real API against a real config, and the frontend dev server
# (Vite) proxies /v1 to it (see web/vite.config.ts) and hot-reloads on
# every save - no rebuilding the whole embedded binary just to see a UI
# change. Both processes' output interleaves in this same terminal;
# Ctrl+C stops both cleanly (kill 0 - see the trap below - hits every
# background job this script started, including npm's own node/vite
# child, since none of them ever left this script's process group).
#
# For a zero-config real demo instead (no config to write by hand), use
# scripts/try-it-out.sh.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
    echo "Usage: $0 --config path/to/config.yaml" >&2
    echo "Config needs api.enabled: true. The frontend dev server's proxy" >&2
    echo "target is hardcoded in web/vite.config.ts to 127.0.0.1:9091, so" >&2
    echo "api.addr should match that (or edit vite.config.ts)." >&2
    exit 1
}

config=""
while [ $# -gt 0 ]; do
    case "$1" in
    --config)
        config="${2:-}"
        shift 2
        ;;
    *) usage ;;
    esac
done
[ -n "$config" ] || usage

if [ ! -f "$config" ]; then
    echo "Config not found: $config" >&2
    echo "Pass a real one: ./scripts/dev.sh --config path/to/config.yaml" >&2
    echo "Or for a zero-config demo instead: ./scripts/try-it-out.sh" >&2
    exit 1
fi

echo "==> Building backend"
if ! (cd "$repo_root" && go build -o bin/bop-dev ./cmd/bop); then
    echo "go build failed - see the error above." >&2
    exit 1
fi

cleanup() {
    echo ""
    echo "==> Stopping..."
    kill 0 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "==> Starting backend (bin/bop-dev controller)"
"$repo_root/bin/bop-dev" --config "$config" controller &

echo "==> Starting frontend dev server (npm run dev)"
(cd "$repo_root/web" && npm run dev) &

echo ""
echo "Frontend (hot reload): http://localhost:5173/"
echo "Backend/API directly:  whatever api.addr says in $config"
echo "Press Ctrl+C to stop both."
echo ""

wait -n
