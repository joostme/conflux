---
"conflux": patch
---

Preserve literal env values when generating Docker Compose env files.

Resolved stack env files now write parsed values directly instead of re-marshaling
them through `godotenv.Marshal`, which incorrectly escaped characters like `!`
for Compose `env_file` usage.
