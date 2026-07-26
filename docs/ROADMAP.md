# Freizone Roadmap — freizone-gateway

Planned changes whose **essential** work lands in this repo (the central
FCM/APNs push relay for devices without a UnifiedPush distributor). Cross-repo
and protocol-level items live in freizone-server's `docs/ROADMAP.md` (core).

Each item has a short **reference code**; the prefix names the owning repo:

- `SRV-` — freizone-server (core)
- `APP-` — freizone-app
- `GAW-`  — freizone-gateway (this file)

A change spanning several repos is listed **once**, in the repo where the
essential work happens; its entry names the other repos it touches.

Status values: `planned` · `in progress` · `done` · `deferred`.

## Planned

### GAW-01 — APNs push delivery for the iOS client
Status: planned · Depends on: APP-03 · Also affects: freizone-app
The gateway relays FCM for Android today. Once the iOS client exists (APP-03),
the APNs delivery path needs to be exercised and completed so iOS devices
without a UnifiedPush distributor still get push wakes.
