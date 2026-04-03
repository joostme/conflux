---
"conflux": patch
---

Skip reconciliation on no-change poll cycles.

When the remote ref has not changed since the last sync, the poll loop now
skips the full reconcile pipeline (config parse, stack discovery, docker
compose up) entirely. On startup and restarts, all stacks are still
reconciled to ensure they are running.
