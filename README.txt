guardianctl quick guide
======================

Part of the Strata platform.

What this is
------------
`guardianctl` is the command-line operator for Guardian partitions, intents,
partition reconcile triggers, rollouts, rollback, and asset docs.

You do not need the shell deploy scripts to use it.


Prerequisites
-------------
Building Guardian requires Go 1.25 and two sibling repositories checked out
at the same directory level:

  ../monofs   (github.com/radryc/monofs)
  ../kvs      (github.com/rydzu/ainfra/kvs)

The `go.mod` replace directives point to these relative paths. Both must be
present before `go build` or `go run` will work.

Build the `guardianctl` binary:

  go build -o guardianctl ./cmd/guardianctl

The Guardian UI is a Vite/TypeScript app built separately. Node 20+ required:

  npm install
  npm run build


Where to run it from
--------------------
From the Guardian repository root:

1. Without a separate build step:

   go run ./cmd/guardianctl partition list

2. Using a locally built binary:

   ./guardianctl partition list


Simplest way to connect
-----------------------
If the Guardian UI/API is reachable from your machine, use Guardian discovery:

  export GUARDIAN_API_URL=http://127.0.0.1:8090
  ./guardianctl partition list

`GUARDIAN_URL` also works. `GUARDIAN_API_URL` is accepted as the same thing.

If Guardian UI requires an authenticated discovery endpoint, also set the
discovery token:

  export GUARDIAN_CLIENT_DISCOVERY_TOKEN=...

`GUARDIAN_DISCOVERY_TOKEN` also works. `GUARDIAN_CLIENT_DISCOVERY_TOKEN` is
accepted as the same thing.

If you want to bypass Guardian discovery and talk to MonoFS directly:

  export GUARDIAN_MONOFS_ROUTER=host-or-ip:9090
  export GUARDIAN_MONOFS_TOKEN=...
  ./guardianctl partition list


Which secret/token you need
---------------------------
There are two common cases.

1. Discovery token (`GUARDIAN_CLIENT_DISCOVERY_TOKEN`)

    Use this when you connect with `GUARDIAN_API_URL` / `--guardian-url` and
    the Guardian UI is not loopback-only.

    In a Kubernetes Guardian deployment, this value comes from:

    - secret: `guardian-configs-secrets`
    - key: `client-discovery-token`

    Example fetch command:

      export GUARDIAN_CLIENT_DISCOVERY_TOKEN="$(
        kubectl -n guardian-configs get secret guardian-configs-secrets \
          -o jsonpath='{.data.client-discovery-token}' | base64 -d
      )"

    This token lets `guardianctl` call Guardian's `/api/client-config` endpoint
    to learn the MonoFS router address and token it should use next.

2. MonoFS token (`GUARDIAN_MONOFS_TOKEN`)

   Use this when you connect directly with `GUARDIAN_MONOFS_ROUTER` /
   `--monofs-router`.

    In a Kubernetes deployment, this is stored in:

    - secret: `guardian-configs-secrets`
    - key: `monofs-token`

    Example fetch command:

      export GUARDIAN_MONOFS_TOKEN="$(
        kubectl -n guardian-configs get secret guardian-configs-secrets \
          -o jsonpath='{.data.monofs-token}' | base64 -d
      )"


Most useful commands
--------------------
List partitions:

  ./guardianctl partition list

Push a local partition bundle (write desired state only, do not reconcile):

  ./guardianctl partition push --dir ./partitions/my-partition

Push and immediately trigger one reconcile cycle:

  This is asynchronous overall. It queues the next reconcile step but does
  not wait for DIFF, CHECK, APPLY, or final Healthy state.

  ./guardianctl partition push --dir ./partitions/my-partition
  ./guardianctl partition reconcile --partition my-partition

Detailed control-loop notes live in:

  docs/guardian-control-loop.md

AWS CDK pusher setup notes live in:

  docs/aws-pusher.md

Delete a partition record from Guardian/MonoFS:

  ./guardianctl partition delete --partition my-partition

Run one reconcile cycle manually:

  ./guardianctl partition reconcile --partition my-partition

Wait for a partition to reach a healthy state:

  ./guardianctl partition wait --partition my-partition

List intents inside a partition:

  ./guardianctl intent list --partition my-partition

Show supported asset types:

  ./guardianctl asset catalog


Examples
--------
Against a local Guardian UI:

  export GUARDIAN_API_URL=http://127.0.0.1:8090
  export GUARDIAN_CLIENT_DISCOVERY_TOKEN="$(
    kubectl -n guardian-configs get secret guardian-configs-secrets \
      -o jsonpath='{.data.client-discovery-token}' | base64 -d
  )"
  go run ./cmd/guardianctl partition push --dir ./partitions/my-partition
  go run ./cmd/guardianctl partition reconcile --partition my-partition

Against a Kubernetes-exposed Guardian UI:

  export GUARDIAN_API_URL=https://guardian.example.com
  export GUARDIAN_CLIENT_DISCOVERY_TOKEN='paste-the-client-discovery-token-here'
  ./guardianctl partition list

Directly against MonoFS:

  export GUARDIAN_MONOFS_ROUTER=monofs.example.com:9090
  export GUARDIAN_MONOFS_TOKEN='paste-the-monofs-token-here'
  ./guardianctl partition list


If it fails
-----------
If `partition list` works, your connection settings are correct.

If discovery fails:

- make sure `GUARDIAN_API_URL` points at a reachable Guardian UI/API
- if Guardian says `client config requires loopback access or a valid discovery token`,
  use `GUARDIAN_CLIENT_DISCOVERY_TOKEN` from
  `secret/guardian-configs-secrets` key `client-discovery-token`
- make sure Guardian is configured with an externally reachable
  `monofs.clientApiEndpoint`
- if you get `rpc error: code = Unavailable desc = name resolver error: produced zero addresses`,
  Guardian discovery did not return a usable MonoFS router address; use direct
  `GUARDIAN_MONOFS_ROUTER` + `GUARDIAN_MONOFS_TOKEN` until
  `monofs.clientApiEndpoint` is fixed

If Guardian discovery works but `guardianctl` still fails with:

  rpc error: code = Unavailable desc = name resolver error: produced zero addresses

then MonoFS is usually advertising Kubernetes-only node addresses instead of
host-reachable ones. Rerun the storage deploy flow so the routers pick up the
real external endpoints, then retry.

If Guardian UI itself loads and then shows:

  rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp [::1]:9005: connect: connection refused"

then the managed control plane is using MonoFS external node addresses for its
own in-cluster store client. The correct split is:

- `GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES=false` for in-cluster `guardiand` and `guardian-pusher-k8s`
- `GUARDIAN_MONOFS_CLIENT_USE_EXTERNAL_ADDRESSES=true` for the discovery config served to `guardianctl`

Rebuild and redeploy Guardian after updating those settings.

If direct MonoFS access fails:

- check `GUARDIAN_MONOFS_ROUTER`
- check `GUARDIAN_MONOFS_TOKEN`
- check that the MonoFS router port is reachable from your machine
