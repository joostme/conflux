---
"conflux": minor
---

Support parallel stack deployment via `parallel_deploy` config option.

A new `parallel_deploy` field can be set under `stacks` in `conflux.yaml`
to control how many stacks are deployed concurrently. Defaults to `1`
(sequential, preserving existing behaviour). Set to a higher value to
speed up reconciliation for repos with many stacks.

```yaml
stacks:
  directory: stacks
  parallel_deploy: 4
```
