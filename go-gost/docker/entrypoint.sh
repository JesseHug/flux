#!/bin/sh
# Runs the agent from the data volume so the panel's one-click upgrade
# survives restarts. A newly pulled image (different version) puts its own
# binary back in place.
set -e

# Old Compose files bind-mount /etc/flyx-agent or /etc/flux_agent. Keep that mount
# and binary name so existing state and old in-panel upgraders remain usable.
DATA_DIR=${FLUX_AGENT_DATA_DIR:-${FLYX_AGENT_DATA_DIR:-}}
if [ -z "$DATA_DIR" ]; then
  has_current=0
  has_flyx=0
  has_legacy=0
  [ -f /etc/flux-agent/config.json ] && has_current=1
  [ -d /etc/flyx-agent ] && has_flyx=1
  [ -d /etc/flux_agent ] && has_legacy=1
  found=$((has_current + has_flyx + has_legacy))
  if [ "$found" -gt 1 ]; then
    echo "Multiple agent data directories exist; set FLUX_AGENT_DATA_DIR explicitly" >&2
    exit 1
  fi
  case 1 in
    "$has_flyx") DATA_DIR=/etc/flyx-agent ;;
    "$has_legacy") DATA_DIR=/etc/flux_agent ;;
    *) DATA_DIR=/etc/flux-agent ;;
  esac
fi
case "$DATA_DIR" in
  /etc/flyx-agent) AGENT_NAME=flyx-agent ;;
  /etc/flux_agent) AGENT_NAME=flux_agent ;;
  *) AGENT_NAME=flux-agent ;;
esac
BIN="$DATA_DIR/$AGENT_NAME"
IMAGE_VERSION=${FLUX_AGENT_IMAGE_VERSION-${FLYX_AGENT_IMAGE_VERSION:-dev}}
MARK="$DATA_DIR/.image_version"

mkdir -p "$DATA_DIR"
cd "$DATA_DIR"

if [ ! -x "$BIN" ] || [ "$(cat "$MARK" 2>/dev/null)" != "$IMAGE_VERSION" ]; then
  src=/usr/local/bin/flux-agent
  [ -x "$src" ] || src=/usr/local/bin/flyx-agent
  cp "$src" "$BIN.new"
  chmod 755 "$BIN.new"
  mv "$BIN.new" "$BIN"
  echo "$IMAGE_VERSION" > "$MARK"
fi

[ -f gost.json ] || echo '{}' > gost.json
chmod 600 gost.json

# Crashing helpers (nft, kernels) would otherwise write core files into the
# data directory on hosts with a relative core_pattern.
ulimit -c 0

exec "$BIN" "$@"
