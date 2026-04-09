# conflux

## 0.2.2

### Patch Changes

- fc5d096: Keep generated Docker Compose env files in a stable key order.

  Resolved env output now sorts keys before writing so reconcile fingerprints stay
  consistent across loops and unchanged stacks are not redeployed unnecessarily.

## 0.2.1

### Patch Changes

- 52a170f: Preserve literal env values when generating Docker Compose env files.

  Resolved stack env files now write parsed values directly instead of re-marshaling
  them through `godotenv.Marshal`, which incorrectly escaped characters like `!`
  for Compose `env_file` usage.

## 0.2.0

### Minor Changes

- 2a6d161: Support parallel stack deployment via `parallel_deploy` config option.

  A new `parallel_deploy` field can be set under `stacks` in `conflux.yaml`
  to control how many stacks are deployed concurrently. Defaults to `1`
  (sequential, preserving existing behaviour). Set to a higher value to
  speed up reconciliation for repos with many stacks.

  ```yaml
  stacks:
    directory: stacks
    parallel_deploy: 4
  ```

- 0ddaeaa: Skip `docker compose up` for unchanged stacks during reconciliation.

  Conflux now fingerprints each stack's compose file and fully resolved env,
  stores the last successful fingerprint in persisted state, and only runs
  `docker compose up` when that stack's desired state changes. The fingerprint
  state defaults to `/data/reconcile-state.json`, so it survives container
  restarts when `/data` is mounted on a persistent volume.

### Patch Changes

- e226439: Add an `auto_prune` stacks config option to reclaim unused Docker images, volumes, and networks after a fully successful reconcile.

  Automatic pruning is skipped when any stack deployment fails so existing reconcile behavior stays unchanged while stale compose resources can still be cleaned up.

- 2a6d161: Skip reconciliation on no-change poll cycles.

  When the remote ref has not changed since the last sync, the poll loop now
  skips the full reconcile pipeline (config parse, stack discovery, docker
  compose up) entirely. On startup and restarts, all stacks are still
  reconciled to ensure they are running.

## 0.1.0

### Minor Changes

- 781ab7e: Initial release of Conflux — a lightweight GitOps controller for Docker Compose that polls a git repo, decrypts SOPS+AGE secrets, and auto-deploys your compose stacks.
