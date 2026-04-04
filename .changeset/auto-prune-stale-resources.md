---
"conflux": patch
---

Add an `auto_prune` stacks config option to reclaim unused Docker images, volumes, and networks after a fully successful reconcile.

Automatic pruning is skipped when any stack deployment fails so existing reconcile behavior stays unchanged while stale compose resources can still be cleaned up.
