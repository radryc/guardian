# AWS pusher

Phase 1 adds `guardian-pusher-aws` and a `CDKStack` asset type.

## How authentication works

`guardian-pusher-aws` uses the normal AWS SDK credential chain first:

- environment variables
- shared config / shared credentials files
- AWS SSO / profile-based auth
- EC2 or ECS task role credentials when running inside AWS

After it has those base credentials, the pusher does one of two things:

1. If `--assume-role-name` is set, it assumes:

   `arn:aws:iam::<target.account>:role/<assume-role-name>`

   This is the default and recommended mode.

2. If `--assume-role-name` is empty, it uses the base credentials directly.

That means the pusher itself does **not** need to run inside the target AWS account. It only needs credentials that can assume the target-account deploy role.

## Where the pusher should run

Two deployment models are supported.

### Recommended: run it in AWS

Run `guardian-pusher-aws` in a dedicated tools or control-plane account, for example on:

- ECS/Fargate
- EC2
- EKS

Why this is better:

- long-lived worker
- no dependency on a developer laptop
- easier to attach a stable runtime role
- better network path for AWS APIs and future private assets

In this model:

- the runtime role in the tools account is trusted by the target account's `GuardianCdkDeployRole`
- the pusher assumes that target-account role for each deployment

### Acceptable for development: run it locally

You can also run it on your workstation.

In that case:

- authenticate with `aws sso login`, a profile, or environment credentials
- trust your current IAM user or IAM role in the target account's `GuardianCdkDeployRole`

This is good for development and early testing, but it is not the preferred steady-state deployment model.

## What must exist in AWS

For each target account and region, the pusher expects:

1. a deploy role, default name `GuardianCdkDeployRole`
2. CDK bootstrap stack, default name `CDKToolkit`

The helper script sets up both.

## Helper script

Use:

```bash
../monotools/setup-aws-pusher.sh \
  --region eu-west-1 \
  --trusted-principal-arn arn:aws:iam::111122223333:role/guardian-pusher-runtime
```

Or, for local development:

```bash
../monotools/setup-aws-pusher.sh \
  --profile prod-admin \
  --region eu-west-1 \
  --use-current-caller
```

What it does:

1. creates or updates an IAM role stack in the target account
2. configures trust for the pusher principal
3. attaches a managed policy to the role
4. runs `cdk bootstrap` in the target account and region

Defaults:

- role name: `GuardianCdkDeployRole`
- role stack name: `guardian-pusher-aws-role`
- bootstrap stack name: `CDKToolkit`
- managed policy: `arn:aws:iam::aws:policy/AdministratorAccess`

**Security note:** `AdministratorAccess` is overly broad. Tighten this to the minimum permissions required for your CDK stacks before using in production.

## Example pusher startup

```bash
./guardian-pusher-aws \
  --pusher-name aws \
  --account 123456789012 \
  --region eu-west-1 \
  --assume-role-name GuardianCdkDeployRole \
  --store-dir /path/to/store
```

Or with MonoFS:

```bash
./guardian-pusher-aws \
  --pusher-name aws \
  --account 123456789012 \
  --region eu-west-1 \
  --assume-role-name GuardianCdkDeployRole \
  --monofs-router host:9090 \
  --monofs-token '...'
```

## CDK asset shape

Example intent asset:

```yaml
- type: CDKStack
  name: network
  payload:
    aws: /partitions/demo/payloads/aws/network/stack.yaml
  properties:
    context:
      envName: prod
    env:
      TEAM_TOKEN:
        secret_ref: monofs-secret://shared/team-token
```

Example payload manifest:

```yaml
sourceType: cdk-ts
sourceDir: /partitions/demo/payloads/aws/network/src
entrypoint: bin/app.ts
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: npm
outputMap:
  vpcId: VpcId
```
