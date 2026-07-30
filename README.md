# Freizone Gateway

A small, self-hostable relay that delivers push-wake notifications to Android (via Firebase Cloud Messaging) and, eventually, iOS (via APNs) on behalf of one or more [freizone-server](https://github.com/behringer24/freizone-server) instances.

**Why this exists:** freizone-server's own push path (UnifiedPush/Web Push) only reaches devices that have a UnifiedPush distributor installed. Reaching an ordinary Android or iOS install means talking to FCM/APNs -- but that requires platform credentials (a Firebase service-account key, an APNs signing key), and a chat server operator shouldn't have to hold those just to run a chat server. So the gateway is a separate, small service: it holds the platform credentials, and any freizone-server (yours, or someone else's) can point at any gateway instance (yours, or someone else's) and ask it to deliver a wake. The relationship between a chat server and a gateway is configuration, not a hardcoded dependency -- **anyone can run their own gateway with their own FCM/APNs project**, independent of anyone else's.

**Status:** Android/FCM delivery works. APNs is configured-but-not-implemented -- the config surface and sender interface exist, but there's no Apple credentials to test against yet, so `internal/push/apns.go` is a stub. UnifiedPush is untouched by any of this; it's an entirely separate path on freizone-server that keeps working exactly as before.

## What the gateway never sees

Every wake this gateway ever sends is a **content-free, data-only push** -- no message text, no sender/recipient identity beyond the raw platform token it was asked to deliver to, no metadata. It carries no more information than "go sync", exactly like freizone-server's own Web Push path. The gateway is not, and cannot become, a place where plaintext or metadata accumulates.

## Security model: no registration, revoke by key

There is deliberately no admin step where a freizone-server operator "signs up" with a gateway. Instead, every freizone-server mints its own Ed25519 identity the first time it boots, and signs each `POST /v1/push/send` request with it -- reusing the exact per-request signature scheme ([`pkg/httpsig`](https://github.com/behringer24/freizone-server/tree/master/pkg/httpsig)) freizone-server already uses to authenticate devices, except here the caller's own public key *is* the `Signature-Key-Id` (self-describing -- no lookup, no prior handshake). Any freizone-server can call in the moment it exists.

If a specific caller turns out to be abusive (spam, quota abuse, whatever), the gateway operator revokes that exact public key locally:

```sh
freizone-gateway -revoke-key <base64-encoded-caller-public-key>
```

This appends the key to a local revocation list (`<GATEWAY_DATA_DIR>/revoked_keys.txt`, one base64 key per line) that a *running* gateway reloads every ~30 seconds -- no restart needed. A revoked caller's signed requests are rejected with `401` from then on. There's no way to un-revoke via the CLI today; edit the file directly if that's ever needed.

Replay protection (a caller can't resend the exact same signed request to trigger duplicate pushes) is an in-memory cache, not a database -- the worst case after a restart is a replay window no wider than the 5-minute clock-skew tolerance, which is an acceptable trade-off for a wake-only, non-sensitive action, and keeps the gateway free of any durable per-request state beyond the revocation list.

## Local development (no TLS, no real FCM project)

```sh
go build -o freizone-gateway ./cmd/gateway

GATEWAY_TLS_MODE=off \
GATEWAY_HTTP_ADDR=127.0.0.1:8080 \
./freizone-gateway
```

`curl http://127.0.0.1:8080/healthz` should reply `{"status":"ok"}`; `curl http://127.0.0.1:8080/v1/capabilities` should reply `{"apns":false,"fcm":false}` (nothing configured yet -- that's expected without a `GATEWAY_FCM_CREDENTIALS_FILE`).

## Setting up FCM (Android)

1. In the [Google Cloud / Firebase console](https://console.firebase.google.com/), create (or reuse) a Firebase project, then generate a **service account key** (Project Settings → Service Accounts → Generate new private key) -- this downloads a JSON file. Keep it secret; it's a credential, not something to commit.
2. Point the gateway at it:
   ```sh
   GATEWAY_FCM_CREDENTIALS_FILE=/path/to/service-account.json ./freizone-gateway
   ```
3. `curl http://127.0.0.1:8080/v1/capabilities` should now reply `{"fcm":true,...}`.

## Running with Docker

Tagged releases are published to GitHub Container Registry by CI, so you can
pull a prebuilt image instead of building one:

```sh
docker pull ghcr.io/behringer24/freizone-gateway:latest   # or :v0.3.0
```

Substitute that image name for `freizone-gateway` below if you use it. To
build it yourself instead:

```sh
docker build -t freizone-gateway .
docker volume create freizone-gateway-data

docker run -d \
  --name freizone-gateway \
  --restart unless-stopped \
  -p 8080:8080 \
  -v freizone-gateway-data:/data \
  -v /path/to/service-account.json:/secrets/fcm.json:ro \
  -e GATEWAY_TLS_MODE=off \
  -e GATEWAY_FCM_CREDENTIALS_FILE=/secrets/fcm.json \
  freizone-gateway
```

`GATEWAY_TLS_MODE=off` is the right choice if you're terminating TLS yourself (e.g. your own nginx + Let's Encrypt reverse proxy in front of this and your chat servers) -- the gateway just needs to be reachable as plain HTTP from that proxy. Set `GATEWAY_TLS_MODE=autocert` (with `GATEWAY_DOMAIN` set) instead if you want the gateway to obtain its own Let's Encrypt certificate directly, the same way freizone-server can.

To revoke a caller against the running container:
```sh
docker run --rm -v freizone-gateway-data:/data freizone-gateway -revoke-key <base64-key>
```

## Wiring a freizone-server to this gateway

On the freizone-server side, set:
```sh
FREIZONE_PUSH_GATEWAY_URL=https://gateway.example.org
```
freizone-server generates its own signing identity automatically on first boot and starts calling this gateway for any device that has registered an FCM/APNs push-target (as opposed to a UnifiedPush subscription -- a device uses exactly one of the two). No further setup is required on the gateway side; it will simply start receiving signed requests. See freizone-server's own README/PROTOCOL.md for the device-registration side of this.

## Configuration reference

All configuration is via environment variables (there is no config file):

| Variable | Default | Description |
|---|---|---|
| `GATEWAY_DOMAIN` | -- | Required when `GATEWAY_TLS_MODE=autocert`. |
| `GATEWAY_HTTP_ADDR` | `:8080` | Plain HTTP bind address -- the ACME challenge/redirect listener in `autocert` mode, or the only listener in `off` mode. |
| `GATEWAY_HTTPS_ADDR` | `:8443` | TLS bind address (`manual`/`autocert` modes). |
| `GATEWAY_TLS_MODE` | `off` | `off` · `manual` (supply your own cert) · `autocert` (automatic Let's Encrypt). |
| `GATEWAY_TLS_CERT_FILE` / `GATEWAY_TLS_KEY_FILE` | -- | Required when `GATEWAY_TLS_MODE=manual`. |
| `GATEWAY_DATA_DIR` | `./data` | Holds the revocation list and the autocert certificate cache. |
| `GATEWAY_FCM_CREDENTIALS_FILE` | -- | Path to a Firebase service-account JSON key. Unset disables FCM sending (`/v1/capabilities` reflects this). |
| `GATEWAY_APNS_KEY_FILE`, `GATEWAY_APNS_KEY_ID`, `GATEWAY_APNS_TEAM_ID`, `GATEWAY_APNS_BUNDLE_ID`, `GATEWAY_APNS_ENVIRONMENT` | -- / `production` | Parsed and validated, but no sender consumes them yet -- APNs isn't implemented (see Status above). |

## API

- `GET /healthz` -- liveness check.
- `GET /v1/capabilities` -- public, no auth: `{"fcm": true, "apns": false}`, reflecting whether that platform's config vars are populated -- **not** whether a sender is actually wired up. Since APNs isn't implemented yet (see Status above), setting all the `GATEWAY_APNS_*` vars makes this report `"apns": true` even though `POST /v1/push/send` for `apns` will still fail (with `502`, not the `501` an unconfigured platform gets).
- `POST /v1/push/send` -- signed (see Security model above). Body: `{"platform": "fcm" | "apns", "token": "..."}`. `200 {"status":"sent"}` · `400` bad platform/body · `401` bad/missing/revoked signature or replayed nonce · `410 {"code": "token_invalid"}` the upstream service reported this token **permanently** dead (app uninstalled, data cleared, wrong sender id) · `501` platform recognized but not configured on this instance · `502` the upstream FCM/APNs call itself failed (currently always, for `apns`, since sending isn't implemented).

  The `410`/`502` distinction is the one worth honouring in a caller: `502` means *this attempt* failed and the token may well still be good, so keep it; `410` means it will never work again, so stop wasting a request on every future message. freizone-server acts on exactly this — it clears the device's stored push target on `410` and leaves it alone on `502`, while the device itself stays reachable over SSE/poll and registers a fresh token on its next app start.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

## License

Copyright (C) 2026 Andreas Behringer

Freizone Gateway is free software: you can redistribute it and/or modify it
under the terms of the **GNU Affero General Public License** as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version. See the [`LICENSE`](LICENSE) file for the full text.

The AGPL is used deliberately: because the gateway is offered over a network,
anyone who runs a **modified** Freizone gateway as a service must make their
modified source available to that service's users. Running an unmodified
gateway, or self-hosting for yourself, carries no such obligation.

`SPDX-License-Identifier: AGPL-3.0-or-later`
