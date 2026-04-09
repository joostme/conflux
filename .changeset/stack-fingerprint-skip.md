---
"conflux": minor
---

Skip `docker compose up` for unchanged stacks during reconciliation.

Conflux now fingerprints each stack's compose file and fully resolved env,
stores the last successful fingerprint in persisted state, and only runs
`docker compose up` when that stack's desired state changes. The fingerprint
state defaults to `/data/reconcile-state.json`, so it survives container
restarts when `/data` is mounted on a persistent volume.
