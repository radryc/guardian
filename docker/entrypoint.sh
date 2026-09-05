#!/bin/sh
set -eu

: "${GUARDIAN_MONOFS_ROUTER:=localhost:9090}"
: "${GUARDIAN_MONOFS_CLIENT_API_ENDPOINT:=}"
: "${GUARDIAN_MONOFS_TOKEN:=guardian-dev-token}"
: "${GUARDIAN_MONOFS_PRINCIPAL:=guardiand}"
: "${GUARDIAN_MONOFS_MOUNT_PATH:=/}"
: "${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES:=false}"
: "${GUARDIAN_MONOFS_CLIENT_USE_EXTERNAL_ADDRESSES:=${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES}}"
: "${GUARDIAN_PRINCIPAL_ID:=${GUARDIAN_MONOFS_PRINCIPAL}}"
: "${GUARDIAN_UI_LISTEN:=127.0.0.1:8090}"
: "${GUARDIAN_UI_BASE_URL:=}"
: "${GUARDIAN_RECONCILE_INTERVAL:=1m}"
: "${GUARDIAN_DEBOUNCE_MS:=250}"
: "${GUARDIAN_CLIENT_DISCOVERY_TOKEN:=}"
: "${GUARDIAN_PUSHERS:=local:/.queues/local,docker-main:/.queues/docker-main,k8s-main:/.queues/k8s-main}"
: "${GUARDIAN_OTEL_ENDPOINT:=localhost:14317}"
: "${GUARDIAN_OTEL_INSECURE:=true}"
: "${GUARDIAN_OTEL_SERVICE_NAME:=guardian}"
: "${GUARDIAN_OTEL_METRIC_INTERVAL:=15s}"

config_path="/tmp/guardian-config.yaml"

cat >"${config_path}" <<EOF
monofs:
  apiEndpoint: ${GUARDIAN_MONOFS_ROUTER}
  clientApiEndpoint: ${GUARDIAN_MONOFS_CLIENT_API_ENDPOINT}
  token: ${GUARDIAN_MONOFS_TOKEN}
  principalID: ${GUARDIAN_MONOFS_PRINCIPAL}
  mountPath: ${GUARDIAN_MONOFS_MOUNT_PATH}
  useExternalAddresses: ${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES}
  clientUseExternalAddresses: ${GUARDIAN_MONOFS_CLIENT_USE_EXTERNAL_ADDRESSES}
guardian:
  principalID: ${GUARDIAN_PRINCIPAL_ID}
  reconcileInterval: ${GUARDIAN_RECONCILE_INTERVAL}
  debounceMs: ${GUARDIAN_DEBOUNCE_MS}
  uiListenAddress: ${GUARDIAN_UI_LISTEN}
  uiBaseURL: ${GUARDIAN_UI_BASE_URL}
  clientDiscoveryToken: ${GUARDIAN_CLIENT_DISCOVERY_TOKEN}
pushers:
EOF

old_ifs="${IFS}"
IFS=','
for pusher in ${GUARDIAN_PUSHERS}; do
  name="${pusher%%:*}"
  queue_dir="${pusher#*:}"
  if [ -z "${name}" ] || [ -z "${queue_dir}" ] || [ "${name}" = "${queue_dir}" ]; then
    echo "invalid GUARDIAN_PUSHERS entry: ${pusher}" >&2
    exit 1
  fi
  cat >>"${config_path}" <<EOF
  - name: ${name}
    queueDir: ${queue_dir}
EOF
done
IFS="${old_ifs}"

exec /usr/local/bin/guardiand --config "${config_path}" "$@"
