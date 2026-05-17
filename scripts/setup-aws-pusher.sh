#!/usr/bin/env bash

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

usage() {
  cat <<'EOF'
Setup phase-1 AWS prerequisites for guardian-pusher-aws.

This script:
1. creates or updates a target-account IAM role trusted by the pusher principal
2. attaches a managed policy to that role (AdministratorAccess by default)
3. bootstraps AWS CDK in the target account and region

Required tools:
- aws
- cdk

Examples:
  scripts/setup-aws-pusher.sh \
    --region eu-west-1 \
    --trusted-principal-arn arn:aws:iam::111122223333:role/guardian-pusher-runtime

  scripts/setup-aws-pusher.sh \
    --profile prod-admin \
    --region eu-west-1 \
    --use-current-caller
EOF
}

log() {
  printf '[%s] %s\n' "$SCRIPT_NAME" "$*"
}

fail() {
  printf '[%s] error: %s\n' "$SCRIPT_NAME" "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

aws_cmd() {
  if [[ -n "${PROFILE:-}" ]]; then
    aws --profile "$PROFILE" "$@"
    return
  fi
  aws "$@"
}

cdk_cmd() {
  if [[ -n "${PROFILE:-}" ]]; then
    cdk --profile "$PROFILE" "$@"
    return
  fi
  cdk "$@"
}

PROFILE=""
REGION=""
TARGET_ACCOUNT_ID=""
TRUSTED_PRINCIPAL_ARN=""
USE_CURRENT_CALLER="false"
ROLE_NAME="GuardianCdkDeployRole"
ROLE_STACK_NAME="guardian-pusher-aws-role"
BOOTSTRAP_STACK_NAME="CDKToolkit"
MANAGED_POLICY_ARN="arn:aws:iam::aws:policy/AdministratorAccess"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILE="${2:-}"
      shift 2
      ;;
    --region)
      REGION="${2:-}"
      shift 2
      ;;
    --target-account-id)
      TARGET_ACCOUNT_ID="${2:-}"
      shift 2
      ;;
    --trusted-principal-arn)
      TRUSTED_PRINCIPAL_ARN="${2:-}"
      shift 2
      ;;
    --use-current-caller)
      USE_CURRENT_CALLER="true"
      shift
      ;;
    --role-name)
      ROLE_NAME="${2:-}"
      shift 2
      ;;
    --role-stack-name)
      ROLE_STACK_NAME="${2:-}"
      shift 2
      ;;
    --bootstrap-stack-name)
      BOOTSTRAP_STACK_NAME="${2:-}"
      shift 2
      ;;
    --managed-policy-arn)
      MANAGED_POLICY_ARN="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

require_cmd aws
require_cmd cdk

[[ -n "$REGION" ]] || fail "--region is required"

CALLER_ARN="$(aws_cmd sts get-caller-identity --query Arn --output text)"
CALLER_ACCOUNT_ID="$(aws_cmd sts get-caller-identity --query Account --output text)"

if [[ -z "$TARGET_ACCOUNT_ID" ]]; then
  TARGET_ACCOUNT_ID="$CALLER_ACCOUNT_ID"
fi

if [[ "$USE_CURRENT_CALLER" == "true" ]]; then
  TRUSTED_PRINCIPAL_ARN="$CALLER_ARN"
fi

[[ -n "$TRUSTED_PRINCIPAL_ARN" ]] || fail "either --trusted-principal-arn or --use-current-caller is required"

TEMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

TEMPLATE_PATH="$TEMP_DIR/guardian-pusher-aws-role.yaml"

cat >"$TEMPLATE_PATH" <<EOF
AWSTemplateFormatVersion: '2010-09-09'
Description: Phase-1 role for guardian-pusher-aws CDK deployments
Parameters:
  RoleName:
    Type: String
  TrustedPrincipalArn:
    Type: String
  ManagedPolicyArn:
    Type: String
Resources:
  GuardianPusherDeployRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: !Ref RoleName
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              AWS: !Ref TrustedPrincipalArn
            Action: sts:AssumeRole
      ManagedPolicyArns:
        - !Ref ManagedPolicyArn
      MaxSessionDuration: 43200
      Description: Guardian phase-1 CDK deploy role
Outputs:
  RoleArn:
    Value: !GetAtt GuardianPusherDeployRole.Arn
EOF

log "target account: $TARGET_ACCOUNT_ID"
log "target region:  $REGION"
log "trusted ARN:    $TRUSTED_PRINCIPAL_ARN"
log "role name:      $ROLE_NAME"
log "policy ARN:     $MANAGED_POLICY_ARN"

CURRENT_ACCOUNT="$(aws_cmd sts get-caller-identity --query Account --output text)"
if [[ "$CURRENT_ACCOUNT" != "$TARGET_ACCOUNT_ID" ]]; then
  fail "active AWS credentials are for account $CURRENT_ACCOUNT, but --target-account-id is $TARGET_ACCOUNT_ID. Switch credentials/profile first."
fi

log "deploying IAM role stack $ROLE_STACK_NAME"
aws_cmd cloudformation deploy \
  --region "$REGION" \
  --stack-name "$ROLE_STACK_NAME" \
  --template-file "$TEMPLATE_PATH" \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides \
    "RoleName=$ROLE_NAME" \
    "TrustedPrincipalArn=$TRUSTED_PRINCIPAL_ARN" \
    "ManagedPolicyArn=$MANAGED_POLICY_ARN"

ROLE_ARN="$(aws_cmd cloudformation describe-stacks \
  --region "$REGION" \
  --stack-name "$ROLE_STACK_NAME" \
  --query "Stacks[0].Outputs[?OutputKey=='RoleArn'].OutputValue" \
  --output text)"

log "bootstrapping CDK stack $BOOTSTRAP_STACK_NAME in aws://$TARGET_ACCOUNT_ID/$REGION"
cdk_cmd bootstrap "aws://$TARGET_ACCOUNT_ID/$REGION" \
  --toolkit-stack-name "$BOOTSTRAP_STACK_NAME" \
  --cloudformation-execution-policies "$MANAGED_POLICY_ARN"

cat <<EOF

Setup complete.

Deploy role ARN:
  $ROLE_ARN

Recommended guardian-pusher-aws command:
  ./guardian-pusher-aws \\
    --pusher-name aws \\
    --account $TARGET_ACCOUNT_ID \\
    --region $REGION \\
    --assume-role-name $ROLE_NAME

Authentication model:
  - guardian-pusher-aws starts with normal AWS credentials from the default SDK chain
  - it then assumes arn:aws:iam::$TARGET_ACCOUNT_ID:role/$ROLE_NAME before CDK/CloudFormation work
  - the starting credentials must match the trusted principal configured above
EOF
