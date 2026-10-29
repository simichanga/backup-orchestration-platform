#!/usr/bin/env bash
# bootstrap.sh - one-time, idempotent setup of a host that runs a BOP
# controller under its login user (no sudo needed). Run ON the target host
# from a checkout or copy of this deploy/ directory:
#
#   VAULT_HOST=10.12.0.166 VAULT_USER=alexandru.siminischi \
#   VAULT_KNOWN_HOSTS=/path/to/vault-host-keys \
#   BOP_CHANNEL=stable ./deploy/bootstrap.sh
#
# What it does (each step is skipped if already done):
#   - ~/bop layout: releases/, data/, bin/bop-update, update.env
#   - restic $RESTIC_VERSION into ~/.local/bin, checksum-verified
#   - ~/.ssh/bop_local: key the controller uses to SSH to *this* host (the
#     mariadb/postgres plugins always go over SSH), authorized only from
#     loopback and with `restrict`
#   - ~/bop/data/known_hosts for 127.0.0.1, built from this host's own
#     /etc/ssh host keys - trusted locally, not from a network scan
#   - ~/.ssh/bop_vault + a `bop-vault` ~/.ssh/config alias for restic's
#     sftp backend, with the vault's host key pinned from VAULT_KNOWN_HOSTS
#   - restic repository password and API read token (mode 600, never printed)
#
# Run again with VAULT_ALIAS=bop-<name> to add a further destination for a
# config.yaml storage.repositories entry (e.g. bop-logs): it gets its own
# key (~/.ssh/bop_<name>), pinned host keys (data/<name>_known_hosts) and
# repository password (~/bop/restic-password-<name>).
#   - systemd --user units, linger, and the updater timer
#
# It does NOT write config.yaml/inventory.yaml (host-specific - see
# deploy/README.md), authorize bop_vault on the vault (that needs access to
# the vault account; the public key is printed at the end), or start
# bop.service before a release is installed.

set -euo pipefail

RESTIC_VERSION="${RESTIC_VERSION:-0.19.1}"
BOP_CHANNEL="${BOP_CHANNEL:-stable}"
VAULT_ALIAS="${VAULT_ALIAS:-bop-vault}"
[[ "$VAULT_ALIAS" =~ ^bop-[a-z0-9_-]+$ ]] || { echo "VAULT_ALIAS must look like bop-<name>" >&2; exit 1; }
dest="${VAULT_ALIAS#bop-}"
dest_key="$HOME/.ssh/bop_$dest"
dest_known_hosts="$HOME/bop/data/${dest}_known_hosts"
if [[ "$dest" == vault ]]; then dest_password="$HOME/bop/restic-password"; else dest_password="$HOME/bop/restic-password-$dest"; fi
: "${VAULT_HOST:?set VAULT_HOST}" "${VAULT_USER:?set VAULT_USER}" "${VAULT_KNOWN_HOSTS:?set VAULT_KNOWN_HOSTS (file with the vault host key lines)}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$HOME/bop"
log() { echo "bootstrap: $*"; }

umask 077
mkdir -p "$ROOT/releases" "$ROOT/data/tmp" "$ROOT/bin" "$HOME/.local/bin" "$HOME/.config/systemd/user" "$HOME/.ssh"

# --- restic -----------------------------------------------------------------
if [[ "$("$HOME/.local/bin/restic" version 2>/dev/null | awk '{print $2}')" != "$RESTIC_VERSION" ]]; then
	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	asset="restic_${RESTIC_VERSION}_linux_amd64.bz2"
	base="https://github.com/restic/restic/releases/download/v$RESTIC_VERSION"
	curl -fsSL -o "$work/$asset" "$base/$asset"
	curl -fsSL -o "$work/SHA256SUMS" "$base/SHA256SUMS"
	(cd "$work" && grep -E "  $asset\$" SHA256SUMS | sha256sum -c --status) || { echo "restic checksum mismatch" >&2; exit 1; }
	bunzip2 -c "$work/$asset" >"$HOME/.local/bin/restic.new"
	chmod 755 "$HOME/.local/bin/restic.new"
	mv "$HOME/.local/bin/restic.new" "$HOME/.local/bin/restic"
	log "installed restic $RESTIC_VERSION"
fi

# --- SSH to this host (for plugins) -------------------------------------------
[[ -f "$HOME/.ssh/bop_local" ]] || ssh-keygen -q -t ed25519 -N '' -C "bop-local@$(hostname)" -f "$HOME/.ssh/bop_local"
touch "$HOME/.ssh/authorized_keys"
if ! grep -qF "$(awk '{print $2}' "$HOME/.ssh/bop_local.pub")" "$HOME/.ssh/authorized_keys"; then
	cp -p "$HOME/.ssh/authorized_keys" "$HOME/.ssh/authorized_keys.bak-$(date +%Y%m%d%H%M%S)"
	echo "from=\"127.0.0.1,::1\",restrict $(cat "$HOME/.ssh/bop_local.pub")" >>"$HOME/.ssh/authorized_keys"
	log "authorized bop_local for loopback-only SSH"
fi
for k in /etc/ssh/ssh_host_*_key.pub; do
	echo "127.0.0.1 $(awk '{print $1, $2}' "$k")"
done >"$ROOT/data/known_hosts"

# --- restic sftp backend to the destination -------------------------------------
[[ -f "$dest_key" ]] || ssh-keygen -q -t ed25519 -N '' -C "$VAULT_ALIAS@$(hostname)" -f "$dest_key"
cp "$VAULT_KNOWN_HOSTS" "$dest_known_hosts"
touch "$HOME/.ssh/config"
if ! grep -qE "^Host $VAULT_ALIAS\$" "$HOME/.ssh/config"; then
	cp -p "$HOME/.ssh/config" "$HOME/.ssh/config.bak-$(date +%Y%m%d%H%M%S)"
	cat >>"$HOME/.ssh/config" <<EOF

# BOP backup destination (restic sftp backend) - managed by deploy/bootstrap.sh
Host $VAULT_ALIAS
  HostName $VAULT_HOST
  User $VAULT_USER
  IdentityFile $dest_key
  IdentitiesOnly yes
  BatchMode yes
  StrictHostKeyChecking yes
  UserKnownHostsFile $dest_known_hosts
  ServerAliveInterval 30
EOF
	log "added $VAULT_ALIAS ssh alias"
fi

# --- secrets -------------------------------------------------------------------
[[ -s "$dest_password" ]] || { head -c 48 /dev/urandom | base64 -w0 >"$dest_password"; log "generated restic repository password ($dest_password)"; }
[[ -s "$ROOT/data/api-read-token" ]] || { head -c 32 /dev/urandom | base64 -w0 | tr '+/' '-_' >"$ROOT/data/api-read-token"; log "generated API read token"; }

# --- updater + units ----------------------------------------------------------
install -m 755 "$here/bop-update.sh" "$ROOT/bin/bop-update"
echo "BOP_CHANNEL=$BOP_CHANNEL" >"$ROOT/update.env"
install -m 644 "$here"/systemd/*.service "$here"/systemd/*.timer "$HOME/.config/systemd/user/"
loginctl enable-linger "$USER"
systemctl --user daemon-reload
systemctl --user enable bop.service >/dev/null
systemctl --user enable --now bop-update.timer >/dev/null

log "done. Authorize this key on $VAULT_HOST, sftp-only:"
echo "restrict,command=\"/usr/lib/openssh/sftp-server\" $(cat "$dest_key.pub")"
