#!/usr/bin/env bash
# Spins up a real, throwaway backup target in Docker, builds bop, runs a
# real controller, and prints the URL - so you can see BOP work end to
# end without configuring anything by hand.
#
# Everything it creates lives under a temp folder and a temp Docker
# container - safe to run repeatedly. Ctrl+C the controller when you're
# done, then run with --cleanup to remove the Docker container.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
demo_dir="${TMPDIR:-/tmp}/bop-try-it-out"
container_name="bop-demo-ssh"

if [ "${1:-}" = "--cleanup" ]; then
    docker rm -f "$container_name" >/dev/null 2>&1 || true
    rm -rf "$demo_dir"
    echo "Cleaned up - the demo container and its temp files are gone."
    exit 0
fi

require() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Missing '$1'. $2" >&2
        exit 1
    fi
}

require go "Install Go from https://go.dev/dl/, then reopen your terminal."
require docker "Install Docker: https://docs.docker.com/engine/install/"
require restic "Install restic from https://restic.net/."
require ssh-keygen "Install openssh-client via your package manager."

# BOP's SSH plugin executor always connects on port 22 (see
# internal/sshexec) - the demo container has to bind exactly there, so
# nothing else can already be listening on it.
if ss -ltn 2>/dev/null | grep -q ':22 ' && ! docker ps --filter "name=$container_name" --format '{{.Names}}' | grep -q .; then
    echo "Port 22 is already in use by something else on this machine (not this script)." >&2
    echo "Stop whatever that is (e.g. an existing SSH server) and try again." >&2
    exit 1
fi

echo "==> Building bop"
(cd "$repo_root" && go build -o bin/bop ./cmd/bop)

echo "==> Setting up a throwaway demo backup target"
rm -rf "$demo_dir"
mkdir -p "$demo_dir/data" "$demo_dir/repo" "$demo_dir/tmp"
echo "hello from BOP, generated $(date)" >"$demo_dir/data/hello.txt"
printf 'demo-restic-password' >"$demo_dir/restic-password.txt"
printf 'demo-read-token' >"$demo_dir/read-token.txt"
printf 'demo-write-token' >"$demo_dir/write-token.txt"

ssh-keygen -t ed25519 -N "" -f "$demo_dir/id_ed25519" -q

docker rm -f "$container_name" >/dev/null 2>&1 || true
sshd_script='
apk add --no-cache openssh tar >/dev/null &&
adduser -D bopuser && passwd -u bopuser &&
mkdir -p /home/bopuser/.ssh && cp /tmp/authorized_keys /home/bopuser/.ssh/authorized_keys &&
chown -R bopuser:bopuser /home/bopuser/.ssh && chmod 700 /home/bopuser/.ssh && chmod 600 /home/bopuser/.ssh/authorized_keys &&
ssh-keygen -A &&
exec /usr/sbin/sshd -D -e
'
docker run -d --name "$container_name" -p 127.0.0.1:22:22 \
    -v "$demo_dir/id_ed25519.pub:/tmp/authorized_keys:ro" \
    -v "$demo_dir/data:/home/bopuser/data:ro" \
    alpine:3.20 sh -c "$sshd_script" >/dev/null

echo "==> Waiting for the demo SSH target to come up"
sleep 3
# Not ssh-keyscan: reusing the same approach scripts/try-it-out.ps1 uses
# on Windows (that script's own comment explains why - a KEX mismatch
# with Windows' bundled ssh-keyscan) so both platforms behave the same
# way rather than one trusting a network handshake and the other not.
# Every key type the container generated goes in, not just one - a
# known_hosts entry for the wrong algorithm reads as "key mismatch"
# (looks like progress, still fails) same as no entry at all.
#
# The glob has to expand inside the container, via its own shell (sh -c)
# - /etc/ssh/ssh_host_*_key.pub doesn't exist on the host running this
# script, so a host-side loop over that pattern never matches anything
# and falls back to the literal, unexpanded string; `docker exec ... cat`
# with no shell in between doesn't glob at all, so it'd fail to find that
# literal "file" and produce nothing.
: >"$demo_dir/known_hosts"
docker exec "$container_name" sh -c 'cat /etc/ssh/ssh_host_*_key.pub' | while IFS= read -r key; do
    echo "127.0.0.1 $key" >>"$demo_dir/known_hosts"
done

cat >"$demo_dir/inventory.yaml" <<EOF
servers:
  demo-host:
    host: 127.0.0.1
    ssh_user: bopuser
    ssh_key: $demo_dir/id_ed25519
    plugins:
      filesystem:
        config:
          paths:
            - /home/bopuser/data
    retention:
      daily: 7
EOF

cat >"$demo_dir/config.yaml" <<EOF
inventory: $demo_dir/inventory.yaml
storage:
  provider: restic
  restic:
    repository: $demo_dir/repo
    password_file: $demo_dir/restic-password.txt
metadata:
  driver: sqlite
  dsn: $demo_dir/metadata.db
controller:
  temp_dir: $demo_dir/tmp
ssh:
  known_hosts_file: $demo_dir/known_hosts
api:
  enabled: true
  addr: "127.0.0.1:9091"
  tokens_file: $demo_dir/read-token.txt
  write_tokens_file: $demo_dir/write-token.txt
metrics:
  port: 19093
EOF

echo "==> Creating the restic repository"
RESTIC_PASSWORD_FILE="$demo_dir/restic-password.txt" restic -r "$demo_dir/repo" init >/dev/null

echo ""
echo "==> Starting bop controller"
echo "    Web UI:      http://127.0.0.1:9091/"
echo "    Read token:  demo-read-token   (view only)"
echo "    Write token: demo-write-token  (view, and trigger backups)"
echo ""
echo "    Press Ctrl+C to stop the controller when you're done."
echo "    Then run: ./scripts/try-it-out.sh --cleanup"
echo ""

if command -v xdg-open >/dev/null 2>&1; then
    xdg-open "http://127.0.0.1:9091/" >/dev/null 2>&1 &
fi

exec "$repo_root/bin/bop" --config "$demo_dir/config.yaml" controller
