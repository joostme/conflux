# Conflux

Docker Compose GitOps — auto-deploy compose stacks from a git repo.

Conflux is a lightweight GitOps controller for Docker Compose. It polls a git repository, discovers compose stacks, decrypts secrets with SOPS+AGE, and runs `docker compose up -d` to keep your services in sync with your repo. Think of it as Flux CD for your homelab.

> **conflux** *(noun)* — from Latin *confluere*, "to flow together." A place where streams merge.
> Conflux is where your git repo and Docker Compose stacks flow together.

## How It Works

```
┌─────────────┐     poll      ┌──────────┐    discover    ┌────────────────┐
│  Git Repo   │◄──────────────│ Conflux  │───────────────►│ Stack: whoami  │
│             │               │          │                │ Stack: nginx   │
│ conflux.yaml│  clone/pull   │ decrypt  │  compose up    │ Stack: ...     │
│ stacks/     │──────────────►│ secrets  │───────────────►│                │
└─────────────┘               └──────────┘                └────────────────┘
```

1. **Poll** — Fetches the configured git branch on a regular interval
2. **Detect** — Compares local vs remote HEAD to detect changes
3. **Parse** — Reads `conflux.yaml` from the repo root
4. **Networks** — Ensures global Docker networks exist (skips existing ones)
5. **Decrypt** — Decrypts secret files using SOPS with AGE keys
6. **Discover** — Scans the stacks directory for compose files
7. **Deploy** — Runs `docker compose up -d --remove-orphans` per stack
8. **Cleanup** — Tears down stacks that were removed from the repo

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CONFLUX_GIT_URL` | *(required)* | Git repository URL |
| `CONFLUX_GIT_BRANCH` | `main` | Branch to track |
| `CONFLUX_GIT_KEY` | | Path to SSH private key for git auth |
| `CONFLUX_POLL_INTERVAL` | `30s` | How often to check for changes |
| `CONFLUX_SOPS_AGE_KEY` | | Path to AGE key file for secret decryption |
| `CONFLUX_REPO_DIR` | `/data/repo` | Where to clone the repository |
| `CONFLUX_WORK_DIR` | `/data/work` | Working directory for decrypted files |
| `CONFLUX_CONFIG_FILE` | `conflux.yaml` | Config filename in repo root |
| `CONFLUX_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### Config File (`conflux.yaml`)

Place this in the root of your git repository:

```yaml
global:
    secrets:
        - secrets.env             # SOPS-encrypted, decrypted at runtime
    environment:
        - environment.env       # Plain-text env vars

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
    file: compose.yaml          # Compose filename to look for in each stack
    secrets: secrets.env        # Default per-stack secrets filename
    environment: environment.env # Default per-stack env filename
```

### Networks

The `networks` section lets you pre-create Docker networks before any stacks are deployed. This is useful for shared networks (e.g. a reverse proxy network) that multiple stacks connect to.

Networks support the full set of Docker compose network options:

| Field | Type | Description |
|---|---|---|
| `name` | string | Custom name (defaults to the map key) |
| `driver` | string | Network driver (`bridge`, `overlay`, etc.) |
| `driver_opts` | map | Driver-specific options |
| `enable_ipv4` | bool | Enable/disable IPv4 |
| `enable_ipv6` | bool | Enable/disable IPv6 |
| `internal` | bool | Restrict external access |
| `attachable` | bool | Allow manual container attachment |
| `labels` | map | Metadata labels |
| `ipam` | object | IP address management config |

IPAM configuration:

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

**Behavior:** On each reconcile, Conflux checks if a network with the same name already exists. If it does, the network is skipped (not recreated or modified). If it doesn't exist, it's created with the full set of configured options. Networks are always ensured *before* any stacks are deployed.

### Repository Structure

```
my-infra-repo/
├── conflux.yaml            # Conflux configuration
├── environment.env         # Global env vars (applied to all stacks)
├── secrets.env             # Global secrets (SOPS-encrypted)
└── stacks/
    ├── whoami/
    │   └── compose.yaml
    ├── nginx/
    │   ├── compose.yaml
    │   └── environment.env # Stack-level env (additive, overrides globals on conflict)
    └── postgres/
        ├── compose.yaml
        ├── environment.env # Stack-level env (additive, overrides globals on conflict)
        └── secrets.env     # Stack-level secrets (additive, highest priority)
```

### Environment File Precedence

For each stack, Conflux builds a list of `--env-file` arguments in this order (last wins):

1. **Global environment files** — always applied
2. **Global secret files** — always applied, override global env on conflict
3. **Stack environment files** — if present, override globals on conflict
4. **Stack secret files** — if present, highest priority

Global files are **always** included. Stack-level files don't replace them — they're appended after, so they take priority for any overlapping variable names. Docker compose uses last-wins semantics for `--env-file`.

## SOPS + AGE Setup

### Generate an AGE key pair

```bash
age-keygen -o age.key
# Public key: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### Encrypt a secrets file

```bash
sops --encrypt --age age1xxxxxxxx --in-place secrets.env
```

### Provide the key to Conflux

Mount the key file into the container and set `CONFLUX_SOPS_AGE_KEY`:

```bash
docker run -v /path/to/age.key:/age.key:ro \
  -e CONFLUX_SOPS_AGE_KEY=/age.key \
  ...
```

## Running Conflux

### Docker Run

```bash
docker run -d \
  --name conflux \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v conflux-data:/data \
  -v /path/to/age.key:/age.key:ro \
  -v /path/to/ssh-key:/ssh.key:ro \
  -e CONFLUX_GIT_URL=git@github.com:yourorg/infra.git \
  -e CONFLUX_GIT_BRANCH=main \
  -e CONFLUX_GIT_KEY=/ssh.key \
  -e CONFLUX_SOPS_AGE_KEY=/age.key \
  -e CONFLUX_POLL_INTERVAL=60s \
  conflux
```

### Docker Compose

```yaml
services:
  conflux:
    image: conflux
    build: .
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - conflux-data:/data
      - ./age.key:/age.key:ro
      - ~/.ssh/id_ed25519:/ssh.key:ro
    environment:
      CONFLUX_GIT_URL: git@github.com:yourorg/infra.git
      CONFLUX_GIT_BRANCH: main
      CONFLUX_GIT_KEY: /ssh.key
      CONFLUX_SOPS_AGE_KEY: /age.key
      CONFLUX_POLL_INTERVAL: 60s

volumes:
  conflux-data:
```

## Design Decisions

- **No complex reconciliation** — Just runs `docker compose up -d` and lets Docker handle whether containers need recreation. Simple and predictable.
- **Git polling, not webhooks** — No need to expose ports or configure webhook endpoints. Works behind NATs and firewalls.
- **Native git via go-git** — Uses [go-git](https://github.com/go-git/go-git) for all git operations (clone, fetch, reset). No git CLI dependency at runtime. SSH key auth is handled natively.
- **Stack-level overrides are additive** — Global env/secret files are always applied to every stack. Stack-level files are appended after globals, so they override on conflict (last `--env-file` wins in docker compose).
- **Errors don't cascade** — A failure in one stack doesn't prevent other stacks from deploying.
