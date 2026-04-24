---
"conflux": minor
---

Validate newly detected repo changes before reconciliation.

Conflux now checks configuration, managed networks, env and secret resolution,
and Docker Compose stack definitions before applying a new Git state. Invalid
changes are rejected before the host is touched, a concise notification is sent
when notifications are configured, and the local checkout rolls back to the last
accepted commit to keep future removal detection correct.
