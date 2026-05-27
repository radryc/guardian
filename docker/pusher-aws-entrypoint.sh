#!/bin/sh
# Reads /etc/guardian-pusher/aws-pusher.yaml and env vars, then starts
# the guardian-pusher-aws binary with the appropriate CLI flags.
set -eu

CONFIG_FILE="${GUARDIAN_PUSHER_CONFIG:-/etc/guardian-pusher/aws-pusher.yaml}"

pusher_name=""
account=""
region=""
monofs_router=""
cdk_binary=""
aws_state_dir=""
assume_role_name=""
assume_role_external_id=""
bootstrap_stack_name=""

if [ -f "$CONFIG_FILE" ]; then
  pusher_name=$(yq '.pusherName // ""' "$CONFIG_FILE")
  account=$(yq '.account // ""' "$CONFIG_FILE")
  region=$(yq '.region // ""' "$CONFIG_FILE")
  monofs_router=$(yq '.monofsRouter // ""' "$CONFIG_FILE")
  cdk_binary=$(yq '.cdkBinary // ""' "$CONFIG_FILE")
  aws_state_dir=$(yq '.awsStateDir // ""' "$CONFIG_FILE")
  assume_role_name=$(yq '.assumeRoleName // ""' "$CONFIG_FILE")
  assume_role_external_id=$(yq '.assumeRoleExternalID // ""' "$CONFIG_FILE")
  bootstrap_stack_name=$(yq '.bootstrapStackName // ""' "$CONFIG_FILE")
fi

: "${GUARDIAN_PUSHER_NAME:=${pusher_name}}"
: "${GUARDIAN_ACCOUNT:=${account}}"
: "${GUARDIAN_REGION:=${region}}"
: "${GUARDIAN_MONOFS_ROUTER:=${monofs_router:-localhost:9090}}"
: "${GUARDIAN_MONOFS_TOKEN:=guardian-dev-token}"
: "${GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES:=true}"
: "${GUARDIAN_CDK_BINARY:=${cdk_binary:-cdk}}"
: "${GUARDIAN_AWS_STATE_DIR:=${aws_state_dir:-/var/lib/guardian/pusher-aws}}"
: "${GUARDIAN_ASSUME_ROLE_NAME:=${assume_role_name:-GuardianCdkDeployRole}}"
: "${GUARDIAN_ASSUME_ROLE_EXTERNAL_ID:=${assume_role_external_id}}"
: "${GUARDIAN_BOOTSTRAP_STACK_NAME:=${bootstrap_stack_name:-CDKToolkit}}"

if [ -z "$GUARDIAN_ACCOUNT" ]; then
  echo "account not set in $CONFIG_FILE and GUARDIAN_ACCOUNT is not set" >&2
  exit 1
fi

if [ -z "$GUARDIAN_PUSHER_NAME" ]; then
  GUARDIAN_PUSHER_NAME="aws-${GUARDIAN_ACCOUNT}"
fi

mkdir -p "$GUARDIAN_AWS_STATE_DIR"

set -- \
  --pusher-name "$GUARDIAN_PUSHER_NAME" \
  --account "$GUARDIAN_ACCOUNT" \
  --monofs-router "$GUARDIAN_MONOFS_ROUTER" \
  --monofs-token "$GUARDIAN_MONOFS_TOKEN" \
  --monofs-use-external-addresses="$GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES" \
  --cdk-binary "$GUARDIAN_CDK_BINARY" \
  --aws-state-dir "$GUARDIAN_AWS_STATE_DIR" \
  --assume-role-name "$GUARDIAN_ASSUME_ROLE_NAME" \
  --bootstrap-stack-name "$GUARDIAN_BOOTSTRAP_STACK_NAME"

if [ -n "$GUARDIAN_REGION" ]; then
  set -- "$@" --region "$GUARDIAN_REGION"
fi

if [ -n "$GUARDIAN_ASSUME_ROLE_EXTERNAL_ID" ]; then
  set -- "$@" --assume-role-external-id "$GUARDIAN_ASSUME_ROLE_EXTERNAL_ID"
fi

exec /usr/local/bin/guardian-pusher-aws "$@"
