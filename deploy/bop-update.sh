#!/usr/bin/env bash
# bop-update - pull-based continuous delivery for a host running BOP.
#
# Runs from a systemd --user timer (deploy/systemd/bop-update.timer). Asks
# GitHub for the newest release on this host's channel, and if it differs
# from what's installed: downloads the linux-amd64 tarball, verifies it
# against the release's checksums.txt, unpacks it under releases/<tag>,
# atomically repoints the `current` symlink, restarts bop.service, and
# rolls back to the previous release if the new one doesn't come up.
#
# Pull rather than push: the hosts sit on a private network GitHub-hosted
# runners can't reach, and a self-hosted runner attached to a *public*
# repository would execute workflow code from fork pull requests on a
# production machine. Nothing here needs inbound access or a credential
# stored in GitHub.
#
# Channels (BOP_CHANNEL, set in ~/bop/update.env):
#   stable      - /releases/latest: non-prerelease tags only (vX.Y.Z). prod.
#   prerelease  - newest release of any kind, incl. vX.Y.Z-rc.N. staging,
#                 which therefore canaries every release candidate.
#
# Never restarts mid-backup: if the controller's local API reports a job
# in_progress or queued, this run is skipped and the next tick retries.

set -euo pipefail

REPO="${BOP_REPO:-simichanga/backup-orchestration-platform}"
CHANNEL="${BOP_CHANNEL:-stable}"
ROOT="${BOP_ROOT:-$HOME/bop}"
API_ADDR="${BOP_API_ADDR:-127.0.0.1:9311}"
TOKEN_FILE="${BOP_API_TOKEN_FILE:-$ROOT/data/api-read-token}"
UNIT="${BOP_UNIT:-bop.service}"
KEEP="${BOP_KEEP_RELEASES:-3}"
ASSET_SUFFIX="linux-amd64.tar.gz"

log() { echo "bop-update: $*"; }
die() { echo "bop-update: ERROR: $*" >&2; exit 1; }

gh_api() {
	curl -fsSL --retry 3 --max-time 30 -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/$1"
}

case "$CHANNEL" in
stable) tag="$(gh_api releases/latest | jq -r '.tag_name')" ;;
prerelease) tag="$(gh_api 'releases?per_page=20' | jq -r '[.[] | select(.draft | not)][0].tag_name')" ;;
*) die "unknown BOP_CHANNEL '$CHANNEL' (want stable or prerelease)" ;;
esac
[[ -n "$tag" && "$tag" != null ]] || die "no release found on channel '$CHANNEL'"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "refusing unexpected tag name '$tag'"

current=""
if [[ -L "$ROOT/current" ]]; then
	current="$(basename "$(readlink "$ROOT/current")")"
fi
if [[ "$tag" == "$current" ]]; then
	exit 0
fi
log "update available: ${current:-<none>} -> $tag ($CHANNEL)"

jobs_in() {
	curl -fsS --max-time 10 -H "Authorization: Bearer $(grep -v '^#' "$TOKEN_FILE" | head -n1)" \
		"http://$API_ADDR/v1/jobs?status=$1" | jq 'length'
}
if systemctl --user is-active --quiet "$UNIT"; then
	# An unreachable API on a running controller is treated as busy, not
	# idle: restarting blind is exactly what this check exists to avoid.
	busy="$(( $(jobs_in in_progress || echo 1) + $(jobs_in queued || echo 1) ))"
	if (( busy > 0 )); then
		log "controller busy ($busy job(s) in progress/queued, or API unreachable) - retrying next tick"
		exit 0
	fi
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
asset="bop-$tag-$ASSET_SUFFIX"
base="https://github.com/$REPO/releases/download/$tag"
curl -fsSL --retry 3 --max-time 300 -o "$work/$asset" "$base/$asset"
curl -fsSL --retry 3 --max-time 30 -o "$work/checksums.txt" "$base/checksums.txt"
(cd "$work" && grep -E "  $asset\$" checksums.txt | sha256sum -c --status) || die "checksum mismatch for $asset"

dest="$ROOT/releases/$tag"
rm -rf "$dest.partial"
mkdir -p "$dest.partial"
tar -xzf "$work/$asset" -C "$dest.partial"
"$dest.partial/bop" version >/dev/null || die "$tag: bop binary does not run"
rm -rf "$dest"
mv "$dest.partial" "$dest"

previous="$(readlink "$ROOT/current" 2>/dev/null || true)"
swap_to() {
	ln -sfn "$1" "$ROOT/current.new"
	mv -T "$ROOT/current.new" "$ROOT/current"
}
healthy() {
	sleep 15
	systemctl --user is-active --quiet "$UNIT" && jobs_in completed >/dev/null
}

swap_to "releases/$tag"
systemctl --user restart "$UNIT"
if ! healthy; then
	log "$tag failed to come up healthy"
	if [[ -n "$previous" ]]; then
		swap_to "$previous"
		systemctl --user restart "$UNIT"
		log "rolled back to $(basename "$previous")"
	fi
	journalctl --user -u "$UNIT" -n 30 --no-pager >&2 || true
	die "deploy of $tag failed"
fi
log "deployed $tag"

# Keep the newest $KEEP releases, never the one now running or the one
# just replaced (the rollback target).
keep_previous="$(basename "${previous:-none}")"
ls -1t "$ROOT/releases" | grep -v '\.partial$' | tail -n +"$((KEEP + 1))" | while read -r old; do
	[[ "$old" == "$tag" || "$old" == "$keep_previous" ]] && continue
	rm -rf "${ROOT:?}/releases/$old"
	log "pruned old release $old"
done
