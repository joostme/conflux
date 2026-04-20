# Conflux

<p align="center">
  <img src="assets/icon.svg" alt="Conflux Icon" width="200"/>
</p>

Docker Compose GitOps — auto-deploy compose stacks from a git repo.

Conflux polls a git repository, discovers Docker Compose stacks, decrypts SOPS-encrypted secrets, and runs `docker compose up -d` only for stacks whose desired state has changed.

## Features

- **GitOps via polling** — no webhooks, no exposed ports
- **Per-stack fingerprinting** — only changed stacks redeploy
- **SOPS + AGE secrets** — encrypted at rest, decrypted in memory
- **Layered env/secret merging** — global + per-stack with `${VAR}` substitution
- **Managed Docker networks** — pre-create shared networks before stacks deploy
- **Notifications** — Shoutrrr-powered alerts on changes
- **Auto-prune** — optional cleanup of unused images, volumes, and networks
- **SSH git auth via [go-git](https://github.com/go-git/go-git)** — no git CLI required

## Quick Start

### 1. Lay out your infra repo

```
my-infra/
├── conflux.yaml
└── stacks/
    └── whoami/
        └── compose.yaml
```

`conflux.yaml`:

```yaml
stacks:
  directory: stacks
  file: compose.yaml
```

`stacks/whoami/compose.yaml`:

```yaml
services:
  whoami:
    image: traefik/whoami
    ports:
      - "8080:80"
```

Push it to a git remote.

### 2. Run Conflux

```yaml
services:
  conflux:
    image: ghcr.io/joostme/conflux:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - conflux-data:/data
      - ~/.ssh/id_ed25519:/ssh.key:ro
    environment:
      CONFLUX_GIT_URL: git@github.com:you/my-infra.git
      CONFLUX_GIT_KEY: /ssh.key
      CONFLUX_POLL_INTERVAL: 60s

volumes:
  conflux-data:
```

`docker compose up -d` — Conflux clones your repo and deploys `whoami`.

### 3. Iterate

Edit `stacks/whoami/compose.yaml` in your infra repo, push, wait for the next poll. Only the changed stack redeploys.

**Next:** [add secrets](#secrets-with-sops--age), [shared networks](#networks), [notifications](#notifications).

## Repository Layout

```
my-infra/
├── conflux.yaml            # Conflux config
├── environment.env         # Global env vars (optional)
├── secrets.env             # Global secrets, SOPS-encrypted (optional)
└── stacks/
    ├── whoami/
    │   └── compose.yaml
    ├── nginx/
    │   ├── compose.yaml
    │   └── environment.env # Stack-level env (optional)
    └── postgres/
        ├── compose.yaml
        ├── environment.env
        └── secrets.env     # Stack-level secrets (optional)
```

Each subdirectory under `stacks/` containing the configured compose file is one stack. Stacks added or removed from the repo are deployed or torn down on the next reconcile.

## `conflux.yaml`

Full annotated example:

```yaml
global:
  secrets:
    - secrets.env             # SOPS-encrypted, decrypted at runtime
  environment:
    - environment.env         # Plain-text env vars

networks:
  proxy:
    driver: bridge
    attachable: true
  internal:
    driver: bridge
    internal: true
    ipam:
      config:
        - subnet: 172.28.0.0/16
          gateway: 172.28.0.1

stacks:
  directory: stacks           # Where to find stack subdirectories
  file: compose.yaml          # Compose filename in each stack
  parallel_deploy: 1          # Stacks deployed concurrently
  auto_prune: false           # Prune unused docker resources after a clean reconcile
  secrets:                    # Default per-stack secret filenames
    - secrets.env
  environment:                # Default per-stack env filenames
    - environment.env
```

The repo includes a [JSON schema](conflux.schema.json) for editor autocomplete:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/joostme/conflux/main/conflux.schema.json
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CONFLUX_GIT_URL` | *(required)* | Git repository URL |
| `CONFLUX_GIT_BRANCH` | `main` | Branch to track |
| `CONFLUX_GIT_KEY` | | Path to SSH private key for git auth |
| `CONFLUX_POLL_INTERVAL` | `3600s` | How often to check for changes |
| `SOPS_AGE_KEY_FILE` | | Path to AGE key file for secret decryption |
| `CONFLUX_REPO_DIR` | `/data/repo` | Where to clone the repository |
| `CONFLUX_CONFIG_FILE` | `conflux.yaml` | Config filename in repo root |
| `CONFLUX_STATE_FILE` | `/data/reconcile-state.json` | Where stack fingerprints are persisted |
| `CONFLUX_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `CONFLUX_NOTIFY_URLS` | | Comma- or newline-separated [Shoutrrr](https://containrrr.dev/shoutrrr/latest/) URLs |

## Environment & Secret Merging

For each stack, all env and secret files are merged into a **single resolved env file** passed to `docker compose up` as `--env-file`.

**Merge order (last wins):**

1. Global environment files
2. Global secret files (SOPS-decrypted)
3. Stack environment files
4. Stack secret files

Variables can reference earlier-defined variables with `${VAR}`:

```env
# global secrets.env
DB_PASSWORD=hunter2

# stacks/myapp/environment.env
DATABASE_URL=postgres://app:${DB_PASSWORD}@db:5432/mydb
```

Undefined references expand to empty strings.

**Fingerprinting:** Conflux hashes each stack's compose file plus its resolved env. If the hash matches the last successful apply, `docker compose up` is skipped.

## Secrets with SOPS + AGE

[SOPS](https://github.com/getsops/sops) encrypts files using [AGE](https://github.com/FiloSottile/age) keys. Conflux decrypts them in memory at reconcile time — encrypted files stay encrypted on disk and in git.

### Generate a key

```bash
age-keygen -o age.key
# Public key: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### Encrypt a secrets file

```bash
sops --encrypt --age age1xxxxxxxx --in-place secrets.env
```

Commit the encrypted file. Keep `age.key` out of git.

### Mount the key

```yaml
volumes:
  - ./age.key:/age.key:ro
environment:
  SOPS_AGE_KEY_FILE: /age.key
```

Global secrets live at the repo root and apply to every stack. Stack-level secrets live next to the compose file and override globals on key conflict.

## Networks

Pre-create networks under `networks:` in `conflux.yaml`. They're ensured before any stacks deploy. Existing networks with the same name are left untouched.

| Field | Type | Description |
|---|---|---|
| `name` | string | Custom name (defaults to map key) |
| `driver` | string | `bridge`, `overlay`, etc. |
| `driver_opts` | map | Driver-specific options |
| `enable_ipv4` | bool | Enable/disable IPv4 |
| `enable_ipv6` | bool | Enable/disable IPv6 |
| `internal` | bool | Restrict external access |
| `attachable` | bool | Allow manual container attachment |
| `labels` | map | Metadata labels |
| `ipam` | object | IP address management |

IPAM example:

```yaml
networks:
  mynet:
    ipam:
      driver: default
      config:
        - subnet: 172.28.0.0/16
          ip_range: 172.28.5.0/24
          gateway: 172.28.5.254
          aux_addresses:
            host1: 172.28.1.5
      options:
        foo: bar
```

## Notifications

Set `CONFLUX_NOTIFY_URLS` to one or more [Shoutrrr](https://containrrr.dev/shoutrrr/latest/) URLs. Notifications fire only when a reconcile actually changes something — a stack deployed, removed, or a network removed.

```yaml
environment:
  CONFLUX_NOTIFY_URLS: >-
    telegram://BOT_TOKEN@telegram?channels=@mychannel,
    discord://TOKEN@CHANNEL_ID
```

Or via env file:

```env
CONFLUX_NOTIFY_URLS=telegram://BOT_TOKEN@telegram?channels=@mychannel,discord://TOKEN@CHANNEL_ID
```

## Operations

**State.** Stack fingerprints are persisted to `CONFLUX_STATE_FILE` (default `/data/reconcile-state.json`). Mount `/data` as a volume so state survives restarts.

**Logging.** Set `CONFLUX_LOG_LEVEL=debug` to see fingerprint comparisons and merged env contents.

**Auto-prune.** Set `stacks.auto_prune: true` in `conflux.yaml` to run Docker's daemon-wide prune (images, volumes, networks) after a reconcile. Prune only runs when at least one `docker compose up` succeeded *and* no stack failed.

**Image tags.** Images are published to GHCR for `linux/amd64` and `linux/arm64`:

- `ghcr.io/joostme/conflux:vX.Y.Z` — pinned patch
- `ghcr.io/joostme/conflux:vX.Y` — pinned minor
- `ghcr.io/joostme/conflux:latest` — rolling
