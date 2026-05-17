#!/bin/sh
# Reads /etc/guardian-pusher/docker-pusher.yaml and env vars, then starts
# the guardian-pusher-docker binary with the appropriate CLI flags.
set -eu

CONFIG_FILE="${GUARDIAN_PUSHER_CONFIG:-/etc/guardian-pusher/docker-pusher.yaml}"

# Read fields from the YAML config file (requires yq).
pusher_name=""
cluster=""
monofs_router=""
state_dir=""
add_hosts_raw=""

if [ -f "$CONFIG_FILE" ]; then
  pusher_name=$(yq '.pusherName // ""' "$CONFIG_FILE")
  cluster=$(yq '.cluster // ""' "$CONFIG_FILE")
  monofs_router=$(yq '.monofsRouter // ""' "$CONFIG_FILE")
  state_dir=$(yq '.stateDir // ""' "$CONFIG_FILE")
  # Join the addHosts list into a comma-separated string.
  add_hosts_raw=$(yq '.addHosts // [] | join(",")' "$CONFIG_FILE")
fi

# Allow env var overrides.
: "${GUARDIAN_PUSHER_NAME:=${pusher_name}}"
: "${GUARDIAN_CLUSTER:=${cluster}}"
: "${GUARDIAN_MONOFS_ROUTER:=${monofs_router:-localhost:9090}}"
: "${GUARDIAN_MONOFS_TOKEN:=guardian-dev-token}"
: "${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES:=true}"
: "${GUARDIAN_STATE_DIR:=${state_dir:-/var/lib/guardian/pusher-docker}}"
: "${GUARDIAN_ADD_HOSTS:=${add_hosts_raw}}"

if [ -z "$GUARDIAN_PUSHER_NAME" ]; then
  echo "pusherName not set in $CONFIG_FILE and GUARDIAN_PUSHER_NAME is not set" >&2
  exit 1
fi
if [ -z "$GUARDIAN_CLUSTER" ]; then
  echo "cluster not set in $CONFIG_FILE and GUARDIAN_CLUSTER is not set" >&2
  exit 1
fi

mkdir -p "$GUARDIAN_STATE_DIR"

set -- \
  --pusher-name "$GUARDIAN_PUSHER_NAME" \
  --cluster "$GUARDIAN_CLUSTER" \
  --monofs-router "$GUARDIAN_MONOFS_ROUTER" \
  --monofs-token "$GUARDIAN_MONOFS_TOKEN" \
  --monofs-use-external-addresses="$GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES" \
  --docker-state-dir "$GUARDIAN_STATE_DIR"

if [ -n "$GUARDIAN_ADD_HOSTS" ]; then
  set -- "$@" --docker-add-hosts "$GUARDIAN_ADD_HOSTS"
fi

exec /usr/local/bin/guardian-pusher-docker "$@"
