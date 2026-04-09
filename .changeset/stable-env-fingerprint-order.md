---
"conflux": patch
---

Keep generated Docker Compose env files in a stable key order.

Resolved env output now sorts keys before writing so reconcile fingerprints stay
consistent across loops and unchanged stacks are not redeployed unnecessarily.
