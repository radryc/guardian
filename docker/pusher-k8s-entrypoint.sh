#!/bin/sh
# Reads /etc/guardian-pusher/k8s-pusher.yaml and env vars, then starts
# the guardian-pusher-k8s binary with the appropriate CLI flags.
set -eu

CONFIG_FILE="${GUARDIAN_PUSHER_CONFIG:-/etc/guardian-pusher/k8s-pusher.yaml}"

pusher_name=""
cluster=""
monofs_router=""
kubeconfig=""
kube_context=""

if [ -f "$CONFIG_FILE" ]; then
  pusher_name=$(yq '.pusherName // ""' "$CONFIG_FILE")
  cluster=$(yq '.cluster // ""' "$CONFIG_FILE")
  monofs_router=$(yq '.monofsRouter // ""' "$CONFIG_FILE")
  kubeconfig=$(yq '.kubeconfig // ""' "$CONFIG_FILE")
  kube_context=$(yq '.kubeContext // ""' "$CONFIG_FILE")
fi

: "${GUARDIAN_PUSHER_NAME:=${pusher_name}}"
: "${GUARDIAN_CLUSTER:=${cluster}}"
: "${GUARDIAN_MONOFS_ROUTER:=${monofs_router:-localhost:9090}}"
: "${GUARDIAN_MONOFS_TOKEN:=guardian-dev-token}"
: "${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES:=true}"
: "${GUARDIAN_KUBECTL_BINARY:=kubectl}"
: "${GUARDIAN_KUBECONFIG:=${kubeconfig}}"
: "${GUARDIAN_KUBE_CONTEXT:=${kube_context}}"

if [ -z "$GUARDIAN_PUSHER_NAME" ]; then
  echo "pusherName not set in $CONFIG_FILE and GUARDIAN_PUSHER_NAME is not set" >&2
  exit 1
fi
if [ -z "$GUARDIAN_CLUSTER" ]; then
  echo "cluster not set in $CONFIG_FILE and GUARDIAN_CLUSTER is not set" >&2
  exit 1
fi

set -- \
  --pusher-name "$GUARDIAN_PUSHER_NAME" \
  --cluster "$GUARDIAN_CLUSTER" \
  --monofs-router "$GUARDIAN_MONOFS_ROUTER" \
  --monofs-token "$GUARDIAN_MONOFS_TOKEN" \
  --monofs-use-external-addresses="$GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES" \
  --kubectl-binary "$GUARDIAN_KUBECTL_BINARY"

if [ -n "$GUARDIAN_KUBECONFIG" ]; then
  set -- "$@" --kubeconfig "$GUARDIAN_KUBECONFIG"
fi

if [ -n "$GUARDIAN_KUBE_CONTEXT" ]; then
  set -- "$@" --kube-context "$GUARDIAN_KUBE_CONTEXT"
fi

exec /usr/local/bin/guardian-pusher-k8s "$@"
