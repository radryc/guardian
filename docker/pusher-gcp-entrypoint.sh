#!/bin/sh
# Reads /etc/guardian-pusher/gcp-pusher.yaml and environment variables, then
# starts guardian-pusher-gcp with the appropriate CLI flags.
set -eu

CONFIG_FILE="${GUARDIAN_PUSHER_CONFIG:-/etc/guardian-pusher/gcp-pusher.yaml}"

pusher_name=""
project=""
region=""
monofs_router=""
state_bucket=""
kms_key=""
work_dir=""

if [ -f "$CONFIG_FILE" ]; then
  pusher_name=$(yq '.pusherName // ""' "$CONFIG_FILE")
  project=$(yq '.project // ""' "$CONFIG_FILE")
  region=$(yq '.region // ""' "$CONFIG_FILE")
  monofs_router=$(yq '.monofsRouter // ""' "$CONFIG_FILE")
  state_bucket=$(yq '.stateBucket // ""' "$CONFIG_FILE")
  kms_key=$(yq '.kmsKey // ""' "$CONFIG_FILE")
  work_dir=$(yq '.workDir // ""' "$CONFIG_FILE")
fi

: "${GUARDIAN_GCP_PROJECT:=${project}}"
: "${GUARDIAN_GCP_REGION:=${region}}"
: "${GUARDIAN_MONOFS_ROUTER:=${monofs_router:-localhost:9090}}"
: "${GUARDIAN_MONOFS_TOKEN:=guardian-dev-token}"
: "${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES:=true}"
: "${GUARDIAN_GCP_STATE_BUCKET:=${state_bucket}}"
: "${GUARDIAN_GCP_KMS_KEY:=${kms_key}}"
: "${GUARDIAN_GCP_WORK_DIR:=${work_dir:-/var/lib/guardian/pusher-gcp}}"
: "${GUARDIAN_TOFU_BINARY:=tofu}"

if [ -z "$GUARDIAN_GCP_PROJECT" ]; then
  echo "project not set in $CONFIG_FILE and GUARDIAN_GCP_PROJECT is not set" >&2
  exit 1
fi
if [ -z "$GUARDIAN_GCP_STATE_BUCKET" ]; then
  echo "stateBucket not set in $CONFIG_FILE and GUARDIAN_GCP_STATE_BUCKET is not set" >&2
  exit 1
fi

if [ -z "$GUARDIAN_PUSHER_NAME" ]; then
  # The binary derives gcp-<project> when --pusher-name is omitted.
  unset GUARDIAN_PUSHER_NAME || true
fi

mkdir -p "$GUARDIAN_GCP_WORK_DIR"

set -- \
  --project "$GUARDIAN_GCP_PROJECT" \
  --work-dir "$GUARDIAN_GCP_WORK_DIR" \
  --tofu-binary "$GUARDIAN_TOFU_BINARY" \
  --state-bucket "$GUARDIAN_GCP_STATE_BUCKET"

if [ -n "${GUARDIAN_PUSHER_NAME:-}" ]; then
  set -- "$@" --pusher-name "$GUARDIAN_PUSHER_NAME"
fi

if [ -n "$GUARDIAN_GCP_REGION" ]; then
  set -- "$@" --region "$GUARDIAN_GCP_REGION"
fi
if [ -n "$GUARDIAN_GCP_KMS_KEY" ]; then
  set -- "$@" --kms-key "$GUARDIAN_GCP_KMS_KEY"
fi
if [ -n "$GUARDIAN_MONOFS_ROUTER" ]; then
  set -- "$@" \
    --monofs-router "$GUARDIAN_MONOFS_ROUTER" \
    --monofs-token "$GUARDIAN_MONOFS_TOKEN" \
    --monofs-use-external-addresses="$GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES"
fi

if [ "${GUARDIAN_GCPREAD_DISABLED:-false}" = "true" ]; then
  set -- "$@" --read-disabled
else
  if [ -n "${GUARDIAN_GCPREAD_ADDR:-}" ]; then
    set -- "$@" --read-addr "$GUARDIAN_GCPREAD_ADDR"
  fi
  if [ -n "${GUARDIAN_GCPREAD_TOKEN:-}" ]; then
    set -- "$@" --read-token "$GUARDIAN_GCPREAD_TOKEN"
  fi
  if [ -n "${GUARDIAN_GCPREAD_TOKEN_FILE:-}" ]; then
    set -- "$@" --read-token-file "$GUARDIAN_GCPREAD_TOKEN_FILE"
  fi
  if [ -n "${GUARDIAN_GCPREAD_TLS_CERT:-}" ]; then
    set -- "$@" --read-tls-cert "$GUARDIAN_GCPREAD_TLS_CERT"
  fi
  if [ -n "${GUARDIAN_GCPREAD_TLS_KEY:-}" ]; then
    set -- "$@" --read-tls-key "$GUARDIAN_GCPREAD_TLS_KEY"
  fi
  if [ -n "${GUARDIAN_GCPREAD_TLS_CLIENT_CA:-}" ]; then
    set -- "$@" --read-tls-client-ca "$GUARDIAN_GCPREAD_TLS_CLIENT_CA"
  fi
fi

exec /usr/local/bin/guardian-pusher-gcp "$@"
