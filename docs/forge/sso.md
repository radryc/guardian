# Forge behind SSO

Forge is a loopback authoring tool, not a public service. The supported SSO
model is a **reverse proxy in front of `forge ui`**: the proxy performs OIDC
login (Authentik, Keycloak, Entra ID, Okta, …) and forwards the authenticated
user to Forge with a trusted header.

```
browser ──HTTPS──▶ ingress/oauth2-proxy ──HTTP 127.0.0.1──▶ forge ui
                       │
                       └─ OIDC login, identity header
```

Forge never stores OIDC client secrets. Keep the MonoFS token in the **Forge
process environment**, not in the browser.

## Forge flags

```bash
FORGE_PROXY_AUTH_HEADER=X-Forwarded-User \
FORGE_ALLOWED_ORIGINS=https://forge.example.com \
MONOFS_TOKEN=… \
./bin/forge ui \
  --root /srv/partition-repo \
  --listen 127.0.0.1:8787 \
  --no-open
```

| Flag / env | Purpose |
| --- | --- |
| `--proxy-auth-header` / `FORGE_PROXY_AUTH_HEADER` | Identity header from the proxy (for example `X-Forwarded-User`). Accepted **only** when the TCP peer is loopback. |
| `--allowed-origins` / `FORGE_ALLOWED_ORIGINS` | Comma-separated browser origins allowed to send non-GET requests, for example `https://forge.example.com`. |
| `--listen` | Keep `127.0.0.1:PORT` so only the local proxy can reach Forge. |
| `MONOFS_TOKEN` / `--monofs-token` | MonoFS publish credential; still required for `submit`, live status, and live plan. |

When the proxy header is configured, the URL token is optional. Direct local
access can still use the printed `http://127.0.0.1:8787/?token=…` URL as a
second factor.

## nginx + oauth2-proxy

`oauth2-proxy` with `--set-xauthrequest` adds `X-Auth-Request-User`:

```nginx
location /oauth2/ {
    proxy_pass       http://127.0.0.1:4180;
    proxy_set_header Host                    $host;
    proxy_set_header X-Real-IP               $remote_addr;
    proxy_set_header X-Scheme                $scheme;
    proxy_set_header X-Auth-Request-Redirect $scheme://$host$request_uri;
}

location = /oauth2/auth {
    internal;
    proxy_pass       http://127.0.0.1:4180;
    proxy_set_header Host      $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Scheme  $scheme;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
}

location / {
    auth_request /oauth2/auth;
    error_page 401 = /oauth2/sign_in;

    auth_request_set $user $upstream_http_x_auth_request_user;

    proxy_pass http://127.0.0.1:8787;
    proxy_set_header Host              $host;
    proxy_set_header X-Forwarded-User  $user;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Start Forge with:

```bash
FORGE_PROXY_AUTH_HEADER=X-Forwarded-User \
FORGE_ALLOWED_ORIGINS=https://forge.example.com \
MONOFS_TOKEN=… \
./bin/forge ui --root /srv/partition-repo --listen 127.0.0.1:8787 --no-open
```

The proxy must **strip any client-supplied `X-Forwarded-User`** before adding
its own. With nginx, `proxy_set_header X-Forwarded-User $user` replaces the
incoming value; never use `$http_x_forwarded_user`.

## Traefik + Authentik/forward-auth

Use a forward-auth middleware that returns the username, then copy it into the
header Forge trusts:

```yaml
http:
  middlewares:
    forge-auth:
      forwardAuth:
        address: http://authentik-outpost:9000/outpost.goauthentik.io/auth/traefik
        authResponseHeaders:
          - X-authentik-username
    forge-headers:
      headers:
        customRequestHeaders:
          X-Forwarded-User: ""   # strip client value; replace in the chain
  routers:
    forge:
      rule: Host(`forge.example.com`)
      middlewares: [forge-auth]
      service: forge
  services:
    forge:
      loadBalancer:
        servers:
          - url: http://127.0.0.1:8787
```

If your Traefik version cannot map `X-authentik-username` to
`X-Forwarded-User` directly, put a small auth sidecar or nginx in front that
does the mapping.

## Security checklist

- Bind `forge ui` to `127.0.0.1` only; do not expose the loopback port.
- Trust one header only, and only from loopback. Never accept it from the
  public listener.
- Strip client-supplied identity headers at the proxy.
- Set `--allowed-origins` to the exact public origin (`https://host[:port]`).
- Keep MonoFS authorization in place: SSO proves the user, MonoFS still decides
  what they may publish.
- Use TLS at the proxy; Forge itself speaks HTTP on loopback.
- If the proxy runs in another container/host, the TCP peer is not loopback, so
  the header is ignored. Terminate OIDC on the same host, use host networking,
  or put a tiny loopback sidecar in front of Forge.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `401 invalid forge token` | Identity header missing, header name mismatch, or the proxy is not connecting from loopback. |
| `403 cross-origin request rejected` | The browser `Origin` is not in `--allowed-origins` (scheme and port must match exactly). |
| Works for reads, fails on Save/Submit | Usually the `Origin` allowlist. Reads are GET and do not check Origin. |
| Submit fails with MonoFS auth | `MONOFS_TOKEN` is missing or lacks publish rights; SSO does not replace it. |
| UI loads but `/api/catalog` is empty | Browser is on a stale tab/process; restart `forge ui` and hard-refresh. |
