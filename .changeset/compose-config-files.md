---
"conflux": minor
---

Inject Compose `configs:` and `secrets:` with `file:` into containers through the Docker API, so they work when Conflux runs in a container. Files are tracked in the stack fingerprint, and those marked `x-conflux-sops: true` are decrypted with SOPS in memory.

Also pass the loaded project to Compose's start phase so configs and secrets are injected at all, including inline `content:` ones. Services are recreated when the content of any config or secret they reference changes, whether it comes from a file, inline `content:` or an environment variable.
