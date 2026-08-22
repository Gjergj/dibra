# dibra

A minimal Ansible-like configuration management tool written in Go.

## Quick Reference

```bash
# Build
go build ./...

# Build with version info (outputs to bin/)
make build-dev

# Install to $GOPATH/bin
make install

# Check version
dibra --version

# Run playbook (released binary — auto-downloads agent from GitHub Releases)
dibra -config playbook.yaml

# Run playbook (development — builds agent from source, requires Go)
go run ./cmd/controller -config playbook.yaml --agent-build

# Use a specific pre-built agent binary
dibra -config playbook.yaml --agent-path /path/to/dibra-agent

# Verbose output
dibra -config playbook.yaml -v

# Check mode (unsupported mutating modules are safely skipped)
dibra -config playbook.yaml --check

# Request structured diffs from modules that implement them
dibra -config playbook.yaml --diff

# Run already-notified handlers even if a later task fails
dibra -config playbook.yaml --force-handlers

# Force re-upload agent (even if versions match)
dibra -config playbook.yaml --force-agent-upload


# Run playbook with external inventory (YAML)
dibra -config playbook.yaml -i inventory.yaml

# Long form
dibra -config playbook.yaml --inventory inventory.yaml

# Validate config without execution
dibra validate -config playbook.yaml

# Run the Linux local pull runner (requires root)
sudo dibra-deploy

# Install the released Linux local pull runner and systemd unit
curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh | sh
sudo systemctl enable --now dibra-deploy.service

# dibra-deploy development agent modes
sudo dibra-deploy --agent-build
sudo dibra-deploy --agent-path /path/to/dibra-agent

# Run integration tests
make test-integration

# Run integration tests (container must be running)
make test-integration-only

# Run the Ubuntu 22.04 non-Docker core certification lane
make test-core-integration

# Run core families against an already-running core host
make test-core-execution-integration-only
make test-core-files-content-integration-only
make test-core-system-packages-integration-only
make test-core-network-vcs-integration-only

# Start/stop the Docker-free managed core host
make test-core-integration-up
make test-core-integration-down

# Run the future-platform transport/execution smoke profiles
make test-platform-fedora-systemd-smoke
make test-platform-alpine-nonsystemd-smoke

# Validate and regenerate the ansible-core feature parity report
go run ./cmd/ansible-core-parity-report -upstream-root ../ansible
go run ./cmd/ansible-core-parity-report -upstream-root ../ansible -check

# Run the exact Engine 29.7.2 docker_container certification lane
make test-docker-container-integration

# Run that lane with the integration container already running
make test-docker-container-integration-only

# Run the exact Engine 29.7.2 image-family certification lane
make test-docker-image-integration

# Run that lane with the integration container already running
make test-docker-image-integration-only

# Run the exact Engine 29.7.2 resource/info certification lane
make test-docker-resource-integration

# Run that lane with the integration container already running
make test-docker-resource-integration-only

# Run the exact Compose 5.4.0 docker_compose_v2 certification lane
make test-docker-compose-integration

# Run that lane with the integration container already running
make test-docker-compose-integration-only

# Run the Engine 29.7.2 Swarm node/service/config/secret/stack certification lane
make test-docker-swarm-integration

# Run that lane with the integration container already running
make test-docker-swarm-integration-only

# Run only dibra-deploy integration tests
make test-deploy-integration

# Run dibra-deploy integration tests (container must be running)
make test-deploy-integration-only

# Start/stop test container
make test-integration-up
make test-integration-down
```

## dibra-deploy

`dibra-deploy` is the Linux root daemon that polls the fixed local endpoint,
downloads ZIP projects, and executes their manifest-listed playbooks against
the local machine through the standalone `dibra-agent` process. It does not use
SSH and must preserve the existing controller and agent behavior.

See [`docs/DibraDeploy.md`](docs/DibraDeploy.md) for the complete archive and
manifest contract, polling behavior, agent resolution modes, reboot rules,
systemd installation, and release packaging.

The black-box integration suite is in `test/deploy_integration`. It installs the
sample service in the privileged Linux test container and uses a real deploy
binary and agent with an in-container task server. Reboot coverage always uses
the fake `/usr/local/bin/dibra-fake-reboot` command; never point these tests at
the host machine.

## Distribution & Releases

### Versioning

Version info is embedded at build time via ldflags. The `internal/version` package provides:
- `Version` - Semantic version (e.g., `v1.2.3`)
- `Commit` - Git commit hash
- `Date` - Build timestamp

### Local Development Builds

```bash
make build-dev          # Build with version info → bin/
make install            # Install to $GOPATH/bin
make snapshot           # GoReleaser snapshot (all platforms)
make release-dry        # GoReleaser dry-run release
make release-check      # Validate .goreleaser.yaml
```

### Creating a Release

1. Tag the release: `git tag v0.1.0`
2. Push the tag: `git push origin v0.1.0`
3. GitHub Actions runs `.github/workflows/release.yml` which:
   - Cross-compiles for Windows/macOS/Linux (amd64/arm64)
   - Creates `.tar.gz` (Unix) and `.zip` (Windows) archives
   - Generates `.deb` and `.rpm` packages
   - Publishes to GitHub Releases
   - Updates Homebrew tap formula
   - Updates Scoop bucket manifest
   - Publishes to Chocolatey (if configured)

### Installation Methods

| Platform | Method |
|----------|--------|
| **macOS** | `brew install Gjergj/tap/dibra` |
| **macOS/Linux** | `curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra.sh \| sh` |
| **Linux dibra-deploy** | `curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh \| sh` |
| **Linux (deb)** | Download `.deb` from releases, `sudo dpkg -i dibra_*.deb` |
| **Linux (rpm)** | Download `.rpm` from releases, `sudo rpm -i dibra_*.rpm` |
| **Windows** | `scoop bucket add dibra https://github.com/Gjergj/scoop-bucket && scoop install dibra` |
| **Windows** | `choco install dibra` |
| **Any** | Download binary from [GitHub Releases](https://github.com/Gjergj/dibra/releases) |

### Required GitHub Secrets

For automated releases, configure these secrets in the repo settings:

| Secret | Purpose | How to get |
|--------|---------|------------|
| `HOMEBREW_TAP_TOKEN` | Push to `Gjergj/homebrew-tap` | GitHub PAT with Contents:write on homebrew-tap repo |
| `SCOOP_BUCKET_TOKEN` | Push to `Gjergj/scoop-bucket` | GitHub PAT with Contents:write on scoop-bucket repo |
| `CHOCOLATEY_API_KEY` | Publish to Chocolatey | From https://community.chocolatey.org/account |

### GoReleaser Configuration

The `.goreleaser.yaml` configures:
- **Builds**: `controller` (all platforms) and `agent` (Linux/macOS only)
- **Archives**: Separate archives for controller and agent
- **Packages**: `.deb` and `.rpm` via nfpm
- **Homebrew**: Auto-updates `Gjergj/homebrew-tap`
- **Scoop**: Auto-updates `Gjergj/scoop-bucket`
- **Chocolatey**: Auto-publishes to community repo

## Architecture

We chose an **agent-based execution model** (Option 3 from initial design):

```
┌─────────────────────┐         SSH          ┌─────────────────────┐
│   Controller (CLI)  │ ───────────────────► │   Agent Binary      │
│                     │  1. Resolve agent    │                     │
│  - Parse YAML       │  2. Upload agent     │  - Receive JSON     │
│  - SSH connection   │  3. Execute with     │  - Execute modules  │
│  - Orchestrate      │     JSON stdin       │  - Return JSON      │
│  - File transfer    │  4. Parse response   │  - Syscalls only    │
└─────────────────────┘                      └─────────────────────┘
```

### Module Registry

The current Docker module family is registered once in
[`docs/ModuleRegistry.md`](docs/ModuleRegistry.md). The registry owns canonical
and short names, strict typed decoding, check/diff capability declarations,
sensitivity and deprecation metadata, and agent handlers.

When adding a registered Docker module, update the registry and its tests. Do
not add a Docker-specific field to `config.Task`, a controller argument-mapping
case, or an agent dispatch case. Builtin modules that have not migrated still
use their existing explicit paths.

### Docker Connection Compatibility

All Docker modules must use the shared connection implementation in
`internal/modules/docker/connection.go`. Do not construct a Moby client, Docker
CLI connection flags, or Docker connection environment independently in an
executor.

The common arguments are `docker_host`, `api_version`, `timeout`, `tls`,
`validate_certs`, `ca_path`, `client_cert`, `client_key`, `tls_hostname`,
`cli_context`, `debug`, and `use_ssh_client`. The resolver owns defaults,
environment fallbacks, TLS hostname derivation, and validation. In particular:

- Connection values use upstream precedence: an explicitly supplied module
  argument wins over its environment variable, which wins over the default.
  Explicit `false`, `0`, and empty-string values are still supplied values and
  must suppress environment fallbacks. The pointer fields in
  `docker.CommonArgs` preserve this omitted-versus-explicit distinction across
  JSON decoding; do not replace them with plain value fields.
- API-backed modules consume `DOCKER_HOST`, `DOCKER_API_VERSION`,
  `DOCKER_TIMEOUT`, `DOCKER_CERT_PATH`, `DOCKER_TLS`, `DOCKER_TLS_VERIFY`, and
  `DOCKER_TLS_HOSTNAME`. `DOCKER_CERT_PATH` supplies `ca.pem`, `cert.pem`, and
  `key.pem` only for the corresponding omitted arguments.
- CLI-backed modules resolve their supported connection environment through
  the CLI resolver, scrub conflicting ambient Docker variables from the child
  process, and pass the effective host/TLS settings as flags. API-backed
  connections derive an omitted TLS hostname from `docker_host`; CLI-backed
  connections leave hostname selection to the Docker CLI unless
  `tls_hostname` or `DOCKER_TLS_HOSTNAME` is set.
- `client_cert` and `client_key` must be supplied together.
- `validate_certs: true` implies TLS; `tls: true` without validation permits an
  unverified TLS connection.
- `docker_host` and `cli_context` are mutually exclusive. `cli_context` is only
  available to modules implemented through the Docker CLI.
- API-backed modules negotiate the Engine API when `api_version: auto` is used.
  A fixed version disables negotiation.
- `ssh://` daemon hosts use the system OpenSSH client and Docker's
  `system dial-stdio` transport. Dibra has no Paramiko transport, so
  `use_ssh_client` is accepted for upstream compatibility and both values use
  OpenSSH.

The Engine SDK is pinned through the supported split Moby modules
`github.com/moby/moby/client` and `github.com/moby/moby/api`. These pins describe
the maximum Engine API surface Dibra compiles against; they do not select the
Docker Compose version. Compose-backed modules invoke the installed
`docker compose` plugin and support only the current Compose behavior documented
by this repository. `github.com/docker/cli v28.5.2` supplies only the OpenSSH
connection helper compatible with Dibra's Go 1.24 toolchain; it neither bundles
nor selects the runtime Docker CLI or Compose plugin.

Docker task-module parity targets exact Docker Engine 29.7.2 on rootful Linux.
Older Engine releases and their removed API branches are intentionally out of
scope. The integration host runs its own pinned daemon and fails before the
suite when the server version differs, so a developer's host Docker socket can
never silently substitute a different Engine. Local-socket and TLS transport
coverage, where applicable, use this same Engine baseline; this is not a
cross-version matrix. Compose has its own independent version pin below.
Integration `TestMain` also requires the exact pinned Compose 5.4.0 plugin.

Docker executor side effects are injected through `docker.Dependencies` in
`internal/modules/docker/dependencies.go`. Every Docker executor keeps the
production `Execute(req)` entry point and also exposes
`ExecuteWithDependencies(req, dependencies)` for direct unit tests. Use the
injected Moby `client.APIClient` factory, `FileSystem`, `Clock`, `Environment`,
and `CLIRunner`; do not call `docker.GetClient`, `os` filesystem functions,
`time.Now`/`time.Sleep`, or `exec.Command` directly from a Docker executor.
Fakes can embed `client.APIClient`, `docker.FileSystem`, or `docker.Clock` and
override only the methods exercised by the test. Connection precedence belongs
in table-driven unit tests; the integration lane also invokes the agent with a
controlled remote environment to prove fallback and explicit-argument
precedence against a real daemon.

### Shared Docker Compatibility Helpers

Docker modules must use the shared helpers under `internal/modules/docker` for
image references, registry authentication, JSON streams, image archives, port
ranges, and IP/CIDR comparisons. Do not reimplement these behaviors in an
executor.

- `ParseImageReference`, `NormalizeImageReference`, and `JoinImageNameTag` own
  registry/path/tag/digest parsing and Docker Hub normalization.
- `EncodeRegistryAuthForImage`, `ResolveRegistryAuthForImageContext`, and
  `AllRegistryAuthConfigs` own Engine auth headers, registry aliases, inline
  `auths`, `credsStore`, and per-registry `credHelpers`. Credential helpers
  must run through the injected `CLIRunner`; per-registry helpers override the
  global store, missing helper entries fall back to inline auth, and
  `"<token>"` maps to an Engine identity token.
- `DecodeJSONStream` and the specialized pull/build/load parsers must surface
  embedded `errorDetail` failures even when Docker omits the top-level `error`.
- `ReadImageArchiveManifest` owns safe, non-extracting `manifest.json` parsing.
  Export idempotency compares image IDs and requested tags, not file existence.
- `BuildPortBindings` expands ranges into individual Engine port keys. When
 host and container ranges differ in length, the request fails with
 `Port ranges don't match in length`; a host range for one container port is
 preserved as Docker's random-available range.
- `NormalizeIPAddress`, `NormalizeIPNetwork`, and
  `NormalizeEndpointAddress` own canonical IPv4/IPv6 and CIDR comparisons.

Compose support is intentionally pinned to current Compose 5 behavior rather
than upstream's historical Compose 2 matrix. Compose executors call
`CheckComposeVersion`, require the exact pinned 5.4.0 version, request JSON
progress, and use `ParseComposeJSONEvents`. The integration host asserts that
same 5.4.0 plugin version in `TestMain` before any suite runs. Compose 5.4.0
emits both the new `Working`/`Done` image events and older JSON text/status
arrangements, so both shapes remain required fixtures. Every Compose version
change requires an explicit baseline review and update.

### Docker Parity Inventory

`docs/community-docker-parity.yaml` is the authoritative feature-level source
for the pinned `community.docker` baseline. It contains all 38 task modules,
top-level and nested options (including shared API/CLI/Compose fragments),
return fields, and check/diff/idempotency/error contracts. Status is recorded
per feature; module presence never implies parity.

Keep the behavioral reference as a sibling checkout pinned to the audited
collection commit:

```bash
git clone https://github.com/ansible-collections/community.docker.git ../community.docker
git -C ../community.docker checkout 44812d46a5072eec78175a41a1100ee77218c8a2
git -C ../community.docker describe --tags --always # 5.2.2 / 44812d46
```

Use `../community.docker/plugins/modules/` and
`../community.docker/tests/integration/targets/` as behavioral references.
The checkout is GPL-licensed: inspect documentation, implementation, and tests,
but reimplement behavior cleanly in Go and do not copy upstream source.

After changing a Docker contract, update the relevant feature rows and run:

```bash
go run ./cmd/parity-report
go run ./cmd/parity-report -check
```

The first command validates the manifest and regenerates
`docs/community-docker-parity.md`; `-check` fails on stale output, invalid local
paths, duplicate IDs, missing upstream provenance, invalid statuses,
unsupported recorded status transitions, and any `verified` feature without a
Dibra implementation and test. The `-bootstrap-upstream` flag is only for an
explicit baseline rebuild from the exact pinned upstream checkout; do not use
it for normal feature updates because it resets audited statuses.

### Ansible-core Parity Inventory

`docs/ansible-core-parity.yaml` is the authoritative feature-level source for
the pinned `ansible-core` baseline. It contains the 70 public canonical
builtins (72 module files minus `async_wrapper.py`, with `systemd.py`
collapsed into `systemd_service`), plus top-level options, return fields, and
check/diff/idempotency/error contracts. Status is recorded per feature; a
short-name execution path is not a parity claim. Canonical
`ansible.builtin.*` names are not accepted yet.

Keep the behavioral reference as a sibling checkout pinned to the audited
commit:

```bash
git clone https://github.com/ansible/ansible.git ../ansible
git -C ../ansible checkout 9cf16a4aca7898481c257f1e17ad28d0b67b1f85
git -C ../ansible describe --tags --always # 2.22.0.dev0 / 9cf16a4a
```

Use `../ansible/lib/ansible/modules/` and
`../ansible/test/integration/targets/` as behavioral references. The checkout
is GPL-licensed: inspect documentation, implementation, and tests, but
reimplement behavior cleanly in Go and do not copy upstream source.

After changing an ansible-core contract, update the relevant feature rows and
run:

```bash
go run ./cmd/ansible-core-parity-report -upstream-root ../ansible
go run ./cmd/ansible-core-parity-report -upstream-root ../ansible -check
```

The first command validates the manifest and regenerates
`docs/ansible-core-parity.md`; `-check` fails on stale output, invalid local
paths, duplicate IDs, missing upstream provenance, invalid statuses,
unsupported recorded status transitions, a `matched` feature without a Dibra
implementation and test, and a `passed` certification without named upstream
and Dibra cases plus a real Make lane. The `-bootstrap-upstream` flag is only
for an explicit baseline rebuild from the exact pinned upstream checkout; do
not use it for normal feature updates because it resets audited statuses.

The first certified existing-module slice is feature-level coverage for
`command`, `shell`, `file`, `copy`, `template`, and `lineinfile`. Those
modules remain `partial` at module level. Core certification runs on the
Docker-free Ubuntu 22.04 host documented in [`docs/CoreTestLanes.md`](docs/CoreTestLanes.md).

### Module Invocation State

Every controller-to-agent module request uses the shared envelope in
`internal/execution/request.go`:

```json
{"module":"community.docker.docker_container_info","args":{"name":"web"},"check_mode":true,"diff":false}
```

`controller.RunOptions.CheckMode` and `DiffMode` hold global state. A task can
set `check_mode` or `diff`; these nullable task values override the
corresponding global value independently, including an explicit `false`. The
controller resolves the effective state before sending the request, and the
agent passes it to registered handlers separately from module arguments. Do
not add `check_mode` or `diff` fields to individual Docker request structs.

The controller exposes global `--check` and `--diff` flags, and `dibra
completion` generates bash, zsh, fish, and PowerShell completions from the
registered flag set. The envelope is transport plumbing, not a claim that
every executor already honors check or diff mode. The registry distinguishes
the pinned upstream `Capabilities` contract from Dibra's
`ImplementedCapabilities`. In check mode, the controller and agent skip a
module before execution unless its Dibra implementation has explicitly opted
in. The read-only Docker `*_info` modules are the initial safe implementations.
Controller primitives (`debug`, `fail`, `assert`, `set_fact`, `include_vars`,
`pause`, `meta`) still run in check mode and never use the agent envelope.
Legacy builtin modules that have not opted in are skipped.

### Module Results and Redaction

Registered Docker handlers may return module-specific fields, but the registry
normalizes every response to include boolean `changed` and `failed` fields plus
a string `msg`. Diffs use the shared top-level shape
`{"diff":{"before":...,"after":...}}`; do not introduce field-centric diff
objects such as `{"image":{"before":...,"after":...}}`. Empty diffs are
omitted. The controller preserves module-specific return fields for `register`
and shows structured diffs only when effective diff mode is enabled.

Every registered module must declare sensitive argument and result JSON paths
in `registry.Definition.Sensitivity`. The controller uses those paths to redact
verbose requests, result fields, echoed values in `msg`/`stdout`/`stderr`, and
malformed-response diagnostics with
`VALUE_SPECIFIED_IN_NO_LOG_PARAMETER`. All Docker definitions implicitly mark
`client_key` as sensitive. Explicitly add passwords, registry and Swarm tokens,
secret/config data, stdin or copied content, build arguments, and any new TLS
private material. Redaction is applied to display copies; never mutate the
executable request while preparing logs.

### Config Loading

The controller supports two configuration formats:

| Format | Detection | Loader |
|--------|-----------|--------|
| **YAML** | `.yaml`/`.yml` extension | `config.Load()` — traditional playbook format |

Both produce the same `config.Config` struct. Everything downstream (SSH, agent, modules) is identical.

```
.yaml files ──► config.Load()   ──► config.Config ──► Controller
```

**Format detection** :
- File with `.yaml`/`.yml` extension → YAML
- Directory with only `.yaml`/`.yml` files → YAML
- Empty directory or unknown extension → YAML (default)

### Agent Resolution

The controller resolves the agent binary using a three-mode strategy:

| Mode | Flag | When to use |
|------|------|-------------|
| **Auto-download** | _(default)_ | Released binaries (brew, deb, rpm). Downloads agent from GitHub Releases matching controller version. |
| **Build from source** | `--agent-build` | Development and integration tests. Requires Go installed. |
| **Explicit path** | `--agent-path <path>` | Use a specific pre-built agent binary. |

**Auto-download flow** (default for released binaries):
1. Controller connects to remote host via SSH
2. Detects remote OS/arch via `uname -s` / `uname -m`
3. Checks local cache at `~/.dibra/cache/agents/<version>/`
4. If not cached, downloads from `github.com/Gjergj/dibra/releases/download/v<version>/dibra-agent_<version>_<os>_<arch>.tar.gz`
5. Extracts and caches the agent binary locally
6. Checks remote agent version via `/tmp/.dibra-agent --version`
7. Uploads only if remote agent is missing or version mismatches

**Build from source flow** (`--agent-build`):
1. Controller connects to remote host via SSH
2. Detects remote OS/arch via `uname -s` / `uname -m`
3. Cross-compiles agent with `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0`
4. Caches by source hash + target (e.g., `/tmp/dibra-cache/dibra-agent-linux-amd64-<hash>`)
5. Uploads to remote

The development-agent source hash must cover `cmd/agent`,
`internal/execution`, `internal/modules`, `internal/version`, `go.mod`, and
`go.sum`. Changes to any non-test Go source used by the agent must invalidate
the cached binary; otherwise controller/agent protocol changes can reuse an
incompatible agent.

**Version mismatch handling**: If `version.Version == "dev"` (local builds without ldflags), auto-download mode fails with an actionable error directing the user to `--agent-build` or `--agent-path`.

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Agent Delivery** | Auto-download from GitHub Releases | No Go required for end users; falls back to build-from-source for developers |
| **Agent Caching** | Local cache + version check | Agent cached at `~/.dibra/cache/agents/`; remote stored at `/tmp/.dibra-agent`; version compared before upload |
| **Privilege Escalation** | Controller wraps with `sudo -S` | Follows Ansible pattern; agent runs as root, doesn't handle sudo itself |
| **Communication** | JSON over stdin/stdout | Agent reads JSON request from stdin, writes JSON response to stdout |
| **Idempotency** | Check before change | All modules check current state before making changes |
| **File Transfer** | SCP protocol | Controller uploads files to `/tmp/.dibra-copy-<hash>` before copy module runs |
| **Checksum Validation** | SHA1 | Matches Ansible; computed locally, verified on remote after transfer |

### Privilege Escalation Flow

When connecting as non-root user with `become: true`:

```
Controller executes via SSH:
  echo "$PASSWORD" | sudo -S -p '' /tmp/.dibra-agent

Agent stdin receives:
  password\n{"module":"apt","args":{...}}

Agent skips to first '{' character before parsing JSON.
```

When connecting as root: sudo wrapper is skipped entirely.

## Project Structure

```
dibra/
├── cmd/
│   ├── controller/main.go                 # CLI orchestrator
│   ├── agent/main.go                      # Remote agent binary
│   ├── parity-report/                     # community.docker feature inventory reporter
│   └── ansible-core-parity-report/        # ansible-core feature inventory reporter
├── internal/
│   ├── agent/                # Agent resolution (auto-download, build, explicit path)
│   │   ├── resolver.go       # Core resolution logic (3 modes)
│   │   ├── remote.go         # Remote OS/arch detection, version check
│   │   ├── download.go       # GitHub Releases download
│   │   └── extract.go        # tar.gz archive extraction
│   ├── config/config.go      # YAML playbook parsing (structs shared by YAML)
│   │   ├── import_tasks.go   # import_tasks static expansion
│   │   └── controller_primitives.go  # debug/fail/assert/set_fact/include_vars/pause
│   ├── controller/           # Play execution, including controller-side primitives
│   ├── ssh/client.go         # SSH connection, SCP upload, agent execution
│   └── modules/
│       ├── apt/              # Package management
│       ├── apt_key/          # GPG key management
│       ├── apt_repository/   # Repository management
│       ├── command/          # Execute commands on targets
│       ├── shell/            # Execute shell commands (supports pipes, redirects, etc.)
│       ├── copy/             # File copy (local→remote, content, remote_src)
│       ├── fetch/            # File fetch (remote→local)
│       ├── file/             # File/directory/symlink management
│       ├── lineinfile/       # Line-in-file management
│       ├── ping/             # Connectivity test module
│       ├── stat/             # File stat (used internally by fetch)
│       ├── tempfile/         # Temporary file/directory creation
│       ├── uri/              # HTTP/HTTPS requests
│       ├── cron/             # Crontab management
│       ├── service/          # Generic service management
│       ├── user/             # User account management
│       ├── group/            # Group management
│       ├── iptables/         # Iptables firewall rule management
│       ├── reboot/           # Reboot machine and wait for it to come back
│       ├── script/           # Run local scripts on remote hosts
│       ├── slurp/            # Read file contents from remote hosts (base64)
│       ├── find/             # Find files/directories matching criteria
       └── docker_container/ # Docker container management
├── docs/
│   ├── ansible-core-parity.yaml   # ansible-core feature inventory (edit this)
│   ├── ansible-core-parity.md     # generated ansible-core parity report
│   └── CoreTestLanes.md           # Docker-free core certification lanes
├── test/
│   ├── Dockerfile            # Ubuntu 22.04 + systemd + SSH (Docker-capable host)
│   ├── docker-compose.yaml   # Default integration container (SSH on 2222)
│   ├── core/                 # Ubuntu 22.04 Docker-free core host (SSH on 2223)
│   ├── platforms/            # Fedora systemd and Alpine non-systemd smoke hosts
│   └── integration/
│       └── integration_test.go  # Playbook-based integration tests
├── Makefile                  # Build and test commands
├── playbook.yaml             # Example playbook
└── AGENTS.md                 # This file
```

## Secrets Management

Dibra supports fetching secrets directly from **Bitwarden** and **1Password** to avoid hardcoding credentials in inventory files.

### Concepts

1. **Prefix-based resolution**: Secrets are identified by a special prefix in variable values:
   - `!bw:` for Bitwarden
   - `!op://` for 1Password
2. **Early binding**: Secrets are resolved **before** the playbook starts, so they can be used in `ssh_pass`, `become_password`, or any other variable.
3. **Variable Reuse**: You can define a secret once in a `vars` block and reuse it multiple times via `{{ variable }}`.

### Providers

#### Bitwarden (`!bw:`)

Requires the Bitwarden CLI (`bw`) to be installed and unlocked (e.g., via `BW_SESSION` environment variable).

**Syntax**: `!bw:<item-name>/<field>`
- `item-name`: The name of the item in Bitwarden
- `field`: `password`, `username`, `notes`, or a custom field name

**Example**:
```yaml
all:
  vars:
    # Fetch 'password' field from item 'webserver-ssh'
    server_pass: "!bw:webserver-ssh/password"
  hosts:
    web1:
      ssh_pass: "{{ server_pass }}"
```

#### 1Password (`!op://`)

Requires the 1Password CLI (`op`) to be installed and authenticated (e.g., via `op signin` or service account token).

**Syntax**: `!op://<vault>/<item>/<field>` (Native 1Password reference syntax)

**Example**:
```yaml
all:
  vars:
    db_pass: "!op://Infrastructure/database-prod/password"
  hosts:
    db1:
      db_password: "{{ db_pass }}"
```

## Modules
`WHEN ADDING A NEW COMMAND OR FLAG YOU MUST ALSO ADD IT IN THE SHELL COMPLETIONS`

Controller primitives (`debug`, `fail`, `assert`, `set_fact`, `include_vars`,
`pause`, and `meta: noop`) run on the controller. They honor `when`,
`register`, `changed_when`, `notify`, and loops, execute during `--check`, and
never invoke the agent. Playbooks use the short names below;
`ansible.builtin.*` FQCN names are not accepted yet.

`meta: flush_handlers`, `meta: end_host`, and `meta: end_play` are controller
control actions rather than primitives: they honor `when` and `register`, run
during `--check`, do not loop, and do not notify handlers from their own
result.

### debug

Prints a message or inspects a variable on the controller. Always reports
`changed: false` unless `changed_when` overrides it.

```yaml
# Default message
- name: Hello
  debug:

# Templated message
- name: Notify from controller
  debug:
    msg: "hello {{ inventory_hostname }}"

# Inspect a variable or expression
- name: Show loaded vars
  debug:
    var: loaded

# Only print when -v is set
- name: Verbose-only debug
  debug:
    msg: details
    verbosity: 1
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `msg` | `"Hello world!"` | Value to print. Mutually exclusive with `var`. Any YAML type is rendered and returned. |
| `var` | | Variable or expression to evaluate. Mutually exclusive with `msg`. The result field uses this expression as the key. |
| `verbosity` | `0` | Skip unless controller verbosity meets this threshold. `-v` provides verbosity `1`; without `-v` the available verbosity is `0`. |

**Returns**:
```json
{
  "changed": false,
  "failed": false,
  "msg": "Hello world!"
}
```

When `var` is set, the evaluated value is stored under that expression instead
of `msg`. When `verbosity` is higher than the current run, the task is skipped
with `skipped_reason: "Verbosity threshold not met."`

**Notes**:
- `msg` and `var` cannot both be supplied.
- Unknown arguments fail at parse time.

### fail

Fails the current host with a message. Always reports `changed: false` and
`failed: true` when the task runs.

```yaml
- name: Stop this host
  fail:
    msg: expected controller failure

- name: Skip the failure
  fail:
    msg: must not fail
  when: false
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `msg` | `"Failed as requested from task"` | Failure message. Templated like other primitive values. |

**Returns**:
```json
{
  "changed": false,
  "failed": true,
  "msg": "Failed as requested from task"
}
```

**Notes**:
- `when: false` skips the task; the host is not failed.
- Unknown arguments fail at parse time.

### assert

Evaluates one or more conditions against the current variable context. All
assertions must pass.

```yaml
- name: Validate runtime state
  assert:
    that:
      - selected == "omega"
      - loaded.nested.enabled
    success_msg: controller state is valid

- name: Fail with a custom message
  assert:
    that: value == 42
    fail_msg: invalid
    quiet: true
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `that` | required | One condition or a list. Same syntax as `when` (string expression, boolean, number, or list). |
| `fail_msg` | `"Assertion failed"` | Message used when an assertion fails. Alias of `msg`. |
| `msg` | | Alias of `fail_msg`. Cannot be supplied together with `fail_msg`. |
| `success_msg` | `"All assertions passed"` | Message used when every assertion passes. |
| `quiet` | `false` | Hide the success message in controller output. The registered result still includes `msg`. |

**Returns** on success:
```json
{
  "changed": false,
  "failed": false,
  "msg": "All assertions passed"
}
```

**Returns** on failure:
```json
{
  "changed": false,
  "failed": true,
  "evaluated_to": false,
  "assertion": "value == 42",
  "msg": "Assertion failed"
}
```

**Notes**:
- `that` is required. A missing `that` fails with `assert: missing required arguments: that`.
- Failed results include the original assertion that did not pass.

### set_fact

Sets per-host runtime variables for later tasks on the same host.

```yaml
- name: Set runtime values
  set_fact:
    answer: 42
    copied: "{{ value }}"

- name: Set facts from a loop
  set_fact:
    selected: "{{ item }}"
  loop: "{{ values }}"

- name: Also record facts under ansible_facts
  set_fact:
    answer: 42
    cacheable: true
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `<name>` | required | One or more variable names mapped to values. Names and values are templated. At least one fact is required. |
| `cacheable` | `false` | When `true`, also merge the facts into the host's `ansible_facts` map. There is no persistent fact cache across playbook runs. |

**Returns**:
```json
{
  "changed": false,
  "failed": false,
  "msg": "",
  "ansible_facts": {"answer": 42},
  "_ansible_facts_cacheable": false
}
```

**Notes**:
- Facts are written into the host runtime layer immediately. The `ansible_facts` result field is return data and is not reinjected as `ansible_*` names.
- Variable names must start with a letter or underscore and contain only letters, digits, and underscores.
- A loop over `set_fact` keeps the last iteration's values.
- Scalar `set_fact: answer=42` is rejected; use a mapping.
- Extra vars still override these facts. Task `vars` override them for that task only.

### include_vars

Loads variables from a file or directory on the **controller** (not the managed
host) into the host runtime layer.

```yaml
# Free-form file path
- name: Include free-form vars
  include_vars: vars/runtime.yml

# Explicit file, wrapped under a name
- name: Load runtime vars
  include_vars:
    file: runtime-vars.yml
    name: loaded

# Directory, merge into existing maps
- name: Include directory vars
  include_vars:
    dir: vars
    depth: 1
    files_matching: '\.ya?ml$'
    ignore_files: [ignored.yml]
    extensions: [yaml, yml]
    ignore_unknown_extensions: true
    hash_behaviour: merge
    name: loaded
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `file` | | Vars file to load. Mutually exclusive with `dir`. Free-form `include_vars: path` sets `file`. |
| `dir` | | Directory to scan. Mutually exclusive with `file`. |
| `name` | | Wrap the loaded mapping under this variable name. |
| `depth` | `0` | Maximum relative depth for `dir` (`0` = unlimited). `1` loads only files directly in the directory. |
| `files_matching` | | Regular expression matched against filenames when using `dir`. |
| `ignore_files` | `[]` | Filename regular expressions to skip (matched against the end of the name). |
| `extensions` | `json`, `yaml`, `yml` | Allowed file extensions when using `dir`. A leading `.` is ignored. |
| `ignore_unknown_extensions` | `false` | Skip unknown extensions instead of failing. |
| `hash_behaviour` | `replace` | `replace` overwrites existing keys. `merge` deep-merges loaded maps with existing values of the same keys. |

**Returns**:
```json
{
  "changed": false,
  "failed": false,
  "msg": "",
  "ansible_facts": {"included_answer": 42},
  "ansible_included_var_files": ["/path/to/runtime-vars.yml"]
}
```

**Notes**:
- Relative paths resolve relative to the file that contains the task.
- Files are parsed as YAML (JSON objects are accepted). Empty files load as `{}`.
- Directory files are loaded in sorted path order; later files overwrite earlier keys. `hash_behaviour: merge` then deep-merges that combined mapping with existing context values.
- Exactly one of `file` or `dir` is required.
- `name` must be a valid variable name. Loaded keys are not reinjected as `ansible_*` names.
- Unknown arguments fail at parse time.

### pause

Waits on the controller for a timed delay. Prompt-only pauses do not block on
stdin; they continue immediately.

```yaml
# Timed pause
- name: Wait 30 seconds
  pause:
    seconds: 30

# Timed pause in minutes (templated)
- name: Wait
  pause:
    minutes: "{{ wait_minutes }}"
    prompt: wait
    echo: false

# Non-interactive prompt (does not wait)
- name: Continue without waiting
  pause:
    prompt: Press enter
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `minutes` | | Sleep this many minutes. Mutually exclusive with `seconds`. |
| `seconds` | | Sleep this many seconds. Mutually exclusive with `minutes`. Values below `1` are clamped to `1`. |
| `prompt` | `""` | Recorded on the result when non-empty. Does not wait for keyboard input. |
| `echo` | `true` | Returned on the result. There is no interactive input to hide. |

**Returns** (timed):
```json
{
  "changed": false,
  "failed": false,
  "msg": "",
  "rc": 0,
  "stdout": "Paused for 30.00 seconds",
  "stderr": "",
  "start": "2026-08-19 08:14:00.000000",
  "stop": "2026-08-19 08:14:30.000000",
  "delta": 30,
  "echo": true,
  "user_input": ""
}
```

**Returns** (no duration):
```json
{
  "changed": false,
  "failed": false,
  "msg": "stdin is not interactive; continuing without waiting",
  "stdout": "Paused for 0.0 minutes",
  "delta": 0,
  "user_input": ""
}
```

**Notes**:
- `minutes` and `seconds` cannot both be supplied.
- `delta` is elapsed whole seconds. `user_input` is always empty.
- Unknown arguments fail at parse time.

### meta

Controller control actions. `flush_handlers`, `end_host`, and `end_play` are
scalar actions handled by the runner. `noop` is a controller primitive.

```yaml
- name: Run pending handlers now
  meta: flush_handlers

- name: No-op (can loop)
  meta: noop
  loop: [first, second]

- name: Stop remaining tasks on this host
  meta: end_host

- name: Stop the play for every remaining host
  meta: end_play
```

| Action | Description |
|--------|-------------|
| `flush_handlers` | Run already-notified handlers immediately. A later change can notify them again. |
| `noop` | Succeeds unchanged with `msg: "noop"`. Honors loops, `when`, `register`, `changed_when`, and `notify`. |
| `end_host` | Skip remaining tasks on this host. Pending handlers for the host are **not** flushed. Result `msg` is `ending play for <host>`. |
| `end_play` | Skip remaining tasks on this host and do not start later hosts. Pending handlers are **not** flushed. Result `msg` is `ending play`. |

**Notes**:
- `when: false` skips the action.
- `flush_handlers`, `end_host`, and `end_play` do not loop.
- Handlers may use `meta: noop`. Other `meta` actions from a handler fail with `meta actions cannot be used from a handler`.
- Unsupported Ansible actions such as `reset_connection` fail at parse time.

### ping

A trivial test module to verify SSH connectivity. Returns "pong" on success.

```yaml
# Basic connectivity test
- name: Test connectivity
  ping:

# Return custom data
- name: Ping with custom response
  ping:
    data: hello

# Trigger intentional failure (for testing)
- name: Test failure handling
  ping:
    data: crash
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `data` | `pong` | Data to return in the `ping` response field. If set to `crash`, causes intentional failure. |

**Returns**: `{"changed": false, "ping": "<data>"}`

**Note**: Unlike Ansible's ping which checks Python availability, this module only verifies SSH connectivity and agent execution.

### docker_container

Manages the lifecycle and configuration of Docker Engine containers. This is
an Engine API module; the Compose 5.4.0 pin does not select or constrain its
behavior.

```yaml
- name: Start a container
  docker_container:
    name: my-container
    image: alpine:latest
    state: started
    command: ["sleep", "infinity"]

- name: Create a container with canonical options
  docker_container:
    name: web
    image: nginx:latest
    state: started
    published_ports:
      - "8080:80"
    capabilities:
      - NET_BIND_SERVICE
    security_opts:
      - no-new-privileges:true
    comparisons:
      env: strict

- name: Remove a container
  docker_container:
    name: old-container
    state: absent
```

The canonical option contract is the pinned `community.docker` 5.2.2
inventory in [`docs/community-docker-parity.yaml`](docs/community-docker-parity.yaml).
Important behavior:

- `state` supports `absent`, `present`, `started`, `stopped`, and `healthy`;
  `healthy_wait_timeout`, `paused`, `restart`, foreground `detach: false`,
  `cleanup`, `force_kill`, and removal waits participate in lifecycle handling.
- Omitted booleans and zero values remain omitted. Do not replace pointer fields
  in the request: `container_default_behavior` and `comparisons` depend on the
  distinction between omission and explicit `false`/`0`. The registry also
  records canonical supplied argument keys for `docker_container` and emits
  only those keys to the agent, preserving explicit empty strings while
  preventing omitted scalar zero values from becoming accidental settings.
- Canonical names include `capabilities`, `published_ports`, `security_opts`,
  string-form `ulimits`, `restart_retries`, `mounts`, and `networks` endpoint
  options. `cap_add`, `ports`, `security_opt`, mapping-form `ulimits`,
  `on-failure:<count>`, and `networks_append` remain explicit Dibra
  compatibility aliases normalized by the registry.
- `comparisons` controls strict, ignored, or allow-more-present comparisons.
  Re-creatable fields, live-update resource fields, image-derived defaults,
  endpoint reconciliation, check mode, and structured diff mode must remain
  independently tested. Network `driver_opts` and `gw_priority` are sent when
  creating or connecting an endpoint, but upstream does not reconcile those
  fields on an existing endpoint, so they do not trigger a reconnect.
- `state: present` may omit `image` when the named container already exists;
  creation still requires an image. Non-positive `healthy_wait_timeout`
  disables the timeout. Check/debug `actions` remain structured dictionaries,
  and unequal published-port ranges fail validation. A healthy-state timeout
  returns the latest container inspection and starts its error with
  `Timeout of {seconds} seconds exceeded while waiting for container `.
  Published-port IPv6 bind addresses require square brackets; malformed
  bracket syntax, unbracketed IPv6, and hostname bind addresses use the pinned
  upstream validation messages without a Dibra-specific error prefix.
- Container images may be names, digests, or full image IDs. Image IDs never
  trigger pulls. Pull policies, registry authentication, image/name/label
  mismatch policies, platform normalization, and image-derived environment,
  label, port, volume, and command defaults follow the pinned upstream rules.
  A missing named image with `pull: never` fails with
  `Cannot find image with name {reference}, and pull=never`; a missing image ID
  fails with `Cannot find image with ID {id}`.
- `mounts` supports the current Engine bind, volume, tmpfs, npipe, cluster, and
  image forms and validates type-specific options. Bind recursion,
  propagation, and mountpoint-creation flags, volume labels/driver/options and
  subpaths, and tmpfs options are preserved in the Engine request and
  comparison. `tmpfs_options` uses the upstream list-of-one-key-dictionaries
  shape. Mount labels and volume-driver option values use upstream's Python
  string conversion, including lowercase top-level booleans. `volume_options`
  without `volume_driver` are ignored in both the Engine request and
  comparison, matching upstream preprocessing. `read_only_non_recursive` and
  `read_only_force_recursive` are alternative runtime behaviors, and Docker
  rejects enabling both on one mount.
- Block-I/O device path/rate lists compare as sets of dictionaries. IOPS rates
  accept both integers and integer strings, matching Ansible's integer
  coercion. Device requests preserve driver, count, device IDs, nested
  capabilities, and options.
- Healthchecks preserve test, interval, timeout, start period, start interval,
  and retries. Network endpoint links use the nested `networks[].links` API
  shape and participate in endpoint reconciliation.
- The `kernel_memory` argument is accepted and validated but a nonzero value
  fails explicitly on the Engine 29.7.2 baseline. Engine API 1.42 removed that
  field, and the pinned upstream test also excludes it on newer API versions.
- Engine warnings from container create and update are returned in `warnings`
  with the upstream `Docker warning: ` prefix.
- The module uses the shared API connection and injected Docker dependencies.
  Never call the Docker CLI, construct a separate Moby client, or access the OS
  filesystem, environment, clock, or subprocesses directly from the executor.

Every contract change needs focused executor/registry tests, a real-daemon
two-run idempotency scenario with check/diff coverage where applicable, and an
update to the parity inventory and generated report. Run
`make test-docker-container-integration` for the exact Engine 29.7.2
certification lane.

### docker_image

Implements the pinned `community.docker.docker_image` 5.2.2 compatibility
module. It combines pull, daemon-API build, archive export, archive load, local
lookup, tagging, pushing, and removal. New code may prefer the focused image
modules, but this combined module must preserve its upstream contract.

```yaml
- name: Pull alpine image
  docker_image:
    name: alpine:latest
    source: pull
    pull:
      platform: linux/amd64

- name: Build with the Docker daemon API
  docker_image:
    name: myapp
    source: build
    build:
      path: /srv/myapp
      dockerfile: Dockerfile
      args:
        RELEASE: "2026.08"
      cache_from: [myapp:latest]
      container_limits:
        memory: 512MB
        memswap: 1GB
        cpushares: 512
        cpusetcpus: "0-1"
      etc_hosts:
        registry.internal: host-gateway
      labels:
        release: "2026.08"
      network: host
      nocache: false
      platform: linux/amd64
      pull: true
      rm: true
      shm_size: 128MB
      target: runtime

- name: Archive an existing local image
  docker_image:
    name: alpine:latest
    source: local
    archive_path: /tmp/alpine.tar

- name: Load the requested image from an archive
  docker_image:
    name: alpine:latest
    source: load
    load_path: /tmp/alpine.tar

- name: Tag and push a local image
  docker_image:
    name: myapp:latest
    source: local
    repository: registry.example.com/team/myapp:v1
    push: true

- name: Force remove image
  docker_image:
    name: myapp:old
    state: absent
    force_absent: true
```

Important behavior:

- `name` is required. `state` defaults to `present`, and `tag` defaults to
  `latest`; a tag embedded in `name` takes precedence. `source` is required for
  `state: present` and accepts `build`, `load`, `pull`, or `local`.
- Image IDs are accepted for `state: absent`, `source: local`, and
  `source: load`. They are rejected for pull/build and cannot be pushed unless
  `repository` supplies a name.
- `force_source` repeats the selected build/load/pull operation and compares the
  resulting image ID, `force_absent` asks Engine to force removal, and
  `force_tag` repeats tagging but remains unchanged when the target ID did not
  change.
- `pull.platform`, `build.platform`, `build.etc_hosts`, and all common API
  connection options use the shared Docker resolver. The module targets the
  exact Engine 29.7.2 baseline; historical API branches are out of scope.
- Builds use Engine's `/build` API as upstream does. This is the legacy
  daemon-builder contract, not BuildKit/buildx and not the independently pinned
  Compose 5.4.0 runtime. `docker_image_build` is the focused BuildKit module.
- Archive idempotency compares `manifest.json` image IDs and requested tags.
  Engine 29's containerd image store can produce archives whose manifest config
  ID differs from the inspect ID, so an unchanged second export is not
  guaranteed on the pinned baseline, matching the upstream note.
- Pull and push consume the complete Engine JSON stream and fail on either
  top-level `error` or `errorDetail`. Registry credentials are read from
  Docker's `config.json`; `registry_username` and `registry_password` remain a
  sensitive Dibra compatibility extension.
- Check mode predicts pull/build/load/tag/push/archive/removal without mutating
  Engine or files. As upstream documents, a requested pull is assumed changed
  in check mode. Diff mode is unsupported.
- Successful results always include `actions` and the raw Engine `image`
  inspection dictionary. Builds also return `stdout`.
- Dibra retains `force_remove` → `force_absent`, `force_pull` →
  `force_source`, top-level `build_path`/`dockerfile`, and
  `pull: missing|always|never` as explicit compatibility extensions. Canonical
  playbooks should use the upstream names and dictionary-shaped `pull`.

### docker_network

Implements the pinned `community.docker.docker_network` 5.2.2 Engine API
contract. Canonical `connected` is a list of container name strings. Config
differences recreate the network; `force: true` recreates even when the
config matches.

```yaml
- name: Create a network
  docker_network:
    name: app-net
    driver: bridge
    driver_options:
      com.docker.network.bridge.enable_icc: false
    connected:
      - web
      - api

- name: Remove a network
  docker_network:
    name: app-net
    state: absent
```

Important behavior:

- `connected` is a list of names or IDs. An empty `connected` list on an
  existing network copies currently connected names and does not disconnect
  everyone. `appends: true` keeps extra existing connections.
- Driver, IPAM, labels, `internal`, `attachable`, `ingress`, `config_only`,
  `enable_ipv4`, and `enable_ipv6` differences disconnect containers, delete,
  and recreate. `force: true` recreates even when those fields match.
- Boolean `driver_options` stringify (`false` → `"false"`). CIDR values that
  fail parsing use `"%q is not a valid CIDR"`. `config_only: true` forces
  driver `"null"`.
- Pointer booleans preserve omission versus explicit `false`. Aliases include
  `network_name`, `incremental`/`containers`, and `options` → `driver_options`.
  IPAM accepts `iprange`/`ip_range` and `aux_addresses`/`aux_address`.
- Successful results return the raw Engine inspection in `network`. Check and
  diff modes are fully implemented.

### docker_volume

Implements the pinned `community.docker.docker_volume` 5.2.2 Engine API
contract. Canonical `volume_name` aliases `name`. `recreate` is
`never|always|options-changed`; there is no `force` option.

```yaml
- name: Create a volume
  docker_volume:
    volume_name: my-data
    driver: local

- name: Recreate when options change
  docker_volume:
    name: my-data
    recreate: options-changed

- name: Remove a volume
  docker_volume:
    name: my-data
    state: absent
```

On `recreate: never` (the default), a mismatch leaves the existing volume
unchanged rather than failing. Successful results return the raw Engine
inspection in `volume`. Label strings and integers are accepted; booleans and
floating-point values fail with the pinned upstream label-sanitization error.
Check and diff modes are fully implemented.

### docker_prune

Implements the pinned `community.docker.docker_prune` 5.2.2 Engine API
contract. Canonical `builder_cache` aliases `builder`. Check and diff modes
are unsupported.

```yaml
- name: Prune unused resources
  docker_prune:
    containers: true
    images: true
    images_filters:
      dangling: true
    volumes: true
    builder_cache: true
    builder_cache_keep_storage: 1GB
```

Filters accept a string or a list; booleans become `"true"`/`"false"`.
Returned groups are present only when requested: `containers` plus
`containers_space_reclaimed`, `images` (Engine `ImagesDeleted` objects) plus
`images_space_reclaimed`, `networks`, `volumes` plus `volumes_space_reclaimed`,
and `builder_cache_space_reclaimed` plus `builder_cache_caches_deleted`.

On Engine 29 / API ≥1.42, volume prune without `all=true` removes only
anonymous volumes. Named volumes need `volumes_filters.all: true`.

### docker_login

Implements the pinned `community.docker.docker_login` 5.2.2 Engine API
contract. Canonical `registry_url` aliases `registry` and `url`. Canonical
`reauthorize` aliases `reauth` and `relogin`. There is no `validate` or
`email` option.

```yaml
- name: Login to a registry
  docker_login:
    registry_url: localhost:5000
    username: myuser
    password: mypassword

- name: Logout
  docker_login:
    registry_url: localhost:5000
    state: absent
```

Username and password are required for `state: present`. The module always
calls Engine `/auth`. Check mode authenticates but does not write
`config.json`. Matching stored credentials without `reauthorize` are
unchanged. Inline credentials are stored with mode 0600. When `credsStore` or
`credHelpers` selects a helper for the registry, login uses its `get`/`store`
protocol and logout uses `get`/`erase` without rewriting `config.json`.
Successful logins return `login_result`.

### docker_plugin

Implements the pinned `community.docker.docker_plugin` 5.2.2 Engine API
contract. `plugin_name` is required. `state` is `present`, `absent`,
`enable`, or `disable`. `alias` becomes the local name.

```yaml
- name: Install a plugin
  docker_plugin:
    plugin_name: vieux/sshfs
    alias: sshfs
    state: present
```

Install uses `AcceptAllPermissions` and installs disabled, then applies
`plugin_options`. Enable of a missing plugin installs then enables. Disable
of a missing plugin fails with `Plugin not found: Plugin does not exist.`
Plugin option values use upstream's Python string forms (`True`, `False`, and
an empty string for null). Comparison also preserves upstream's behavior:
falsey and non-string requested values continue to count as different from
Engine's string settings. For `state: present`, `actions` is omitted on a
normal real run, but debug and check mode return it, including an empty list
when no action occurred. Check and diff modes are fully implemented. Enabling
`vieux/sshfs` in integration requires `/dev/fuse`.

### docker_swarm

Implements the pinned `community.docker.docker_swarm` 5.2.2 Engine API
contract. It initializes, updates, joins, leaves, and removes Swarm nodes.

```yaml
- name: Init a swarm with default parameters
  docker_swarm:
    state: present
    advertise_addr: 192.168.1.10

- name: Update swarm configuration
  docker_swarm:
    state: present
    election_tick: 5
    autolock_managers: true
    labels:
      env: prod

- name: Init with address pools
  docker_swarm:
    state: present
    default_addr_pool:
      - 10.20.0.0/16
    subnet_size: 24

- name: Join an existing swarm
  docker_swarm:
    state: join
    advertise_addr: 192.168.1.2
    join_token: SWMTKN-1--xxxxx
    remote_addrs: ["192.168.1.1:2377"]

- name: Leave swarm
  docker_swarm:
    state: absent
    force: true
```

Important behavior:

- `state` is `present`, `join`, `absent`, or `remove`. `present` initializes a
  new swarm or updates the cluster spec of an existing manager. `force: true`
  with `present` re-inits with `ForceNewCluster`. `join` requires `remote_addrs`
  and `join_token`. `remove` requires `node_id` and only removes a node whose
  status is `down`.
- Spec options are compared only when supplied. Omitted labels are left
  unchanged; `labels: {}` clears cluster labels. Cluster labels never apply to
  the local node. `default_addr_pool`, `subnet_size`, `advertise_addr`,
  `listen_addr`, `data_path_addr`, and `data_path_port` are init/join-only and
  are not compared for idempotency.
- CA rotation uses `ca_force_rotate`, `signing_ca_cert`, and `signing_ca_key`.
  Signing material is PEM content, not paths. Snapshot knobs are
  `snapshot_interval`, `keep_old_snapshots`, and `log_entries_for_slow_followers`.
  Ticks are `election_tick`, `heartbeat_tick`, and `dispatcher_heartbeat_period`
  (nanoseconds). `autolock_managers` returns `swarm_facts.UnlockKey` only when a
  swarm is created with autolock or autolock actually changes to true.
- Successful results return `actions` and `swarm_facts`. Create results contain
  `JoinTokens` and `UnlockKey`; update results are the raw Engine inspect object.
  Check and diff modes are fully implemented.
- The module uses the shared API connection and injected Docker dependencies.

Every contract change needs focused executor tests, a real-daemon parity
scenario with check/diff coverage, and an update to the parity inventory.

### docker_swarm_service

Implements the pinned `community.docker.docker_swarm_service` 5.2.2 Engine API
contract. Nested spec options, comparison, and `resolve_image` follow the
pinned upstream module. Check and diff modes are fully implemented.

```yaml
- name: Create nginx service
  docker_swarm_service:
    name: my-web
    image: nginx:alpine
    replicas: 3
    publish:
      - published_port: 80
        target_port: 80

- name: Update replicas
  docker_swarm_service:
    name: my-web
    replicas: 5

- name: Pin the image digest
  docker_swarm_service:
    name: my-web
    image: nginx:alpine
    resolve_image: true

- name: Remove service
  docker_swarm_service:
    name: my-web
    state: absent
```

Important behavior:

- `name` is required. `state` is `present` or `absent`. `mode` is
  `replicated` (default), `global`, or `replicated-job`. Changing mode rebuilds
  the service. Omitted options are left unchanged on in-place updates; explicit
  `[]`/`{}` clears them. Create and rebuild use only specified fields.
- Nested specs are `limits`, `reservations`, `placement`, `update_config`,
  `rollback_config`, `restart_config`, `logging`, `mounts`, `networks`
  (string or `{name,aliases,options}`), `configs`, `secrets`, `healthcheck`,
  and `publish`. `command` accepts a string or list. `env` accepts a list,
  dictionary, or `KEY=VALUE` string, plus `env_files`.
- `resolve_image: true` calls Engine `inspect_distribution` and stores
  `repo:tag@digest`. Image comparison strips an active digest when the desired
  image has no `@`. Failure is `Error looking for an image named {image}: {e}`.
- `force_update: true` always changes. Network name collisions prefer a
  swarm-scoped network over a local one with the same name. Comparison
  canonicalizes network IDs to names, so Engine 29's swarm-scoped `host`
  network (which `NetworkList` omits) stays idempotent against the local
  `host` ID. Config and secret names are resolved to IDs; missing names fail
  with `Could not find a config/secret named "…"`.
- Successful results return `msg`, `changed`, `rebuilt`, `changes`, and
  `swarm_service` facts. Status messages are
  `Service created|updated|rebuilt|forcefully updated|unchanged|removed|absent`.
  Updates retry twice on `update out of sequence`. The module uses the shared
  API connection and injected Docker dependencies.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 service lane.

### docker_swarm_service_info

Implements the pinned `community.docker.docker_swarm_service_info` 5.2.2
read-only contract. It must run on a Swarm manager and returns the raw
`docker service inspect` dictionary.

```yaml
- name: Get info from a service
  docker_swarm_service_info:
    name: myservice
  register: result
```

`name` is required and accepts a service name or ID. Successful results always
include `exists` and `service`. For an existing service, `service` is the
complete Engine inspection object with Docker's original field names (`ID`,
`Spec`, `Version`, `Endpoint`); do not add `tasks` or `service_id`. For a
missing service, the module succeeds with `exists: false` and `service: null`.
Off-swarm and worker hosts fail with
`Error running docker swarm module: must run on swarm manager node`. Check
mode is fully supported. Diff mode is not applicable. Shared API connection
arguments and aliases apply.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 service lane.

### docker_node

Implements the pinned `community.docker.docker_node` 5.2.2 Engine API
contract. It updates a Swarm node's role, availability, and labels and must
run on a manager. Diff mode is unsupported.

```yaml
- name: Set node role
  docker_node:
    hostname: mynode
    role: manager

- name: Drain a node
  docker_node:
    hostname: mynode
    availability: drain

- name: Replace node labels
  docker_node:
    hostname: mynode
    labels:
      key: value
    labels_state: replace

- name: Remove all labels
  docker_node:
    hostname: mynode
    labels_state: replace
```

Important behavior:

- `hostname` is required and accepts a Swarm hostname or node ID. `self: true`
  is a Dibra compatibility extension that selects the local daemon's node.
- `labels_state` is `merge` (default) or `replace`. Empty/omitted `labels` with
  `replace` removes every label. `labels_to_remove` applies only in merge mode;
  a key listed in both `labels` and `labels_to_remove` is kept from `labels`.
- Label values are sanitized to strings; bools and floats fail.
- Check mode predicts role, availability, and label changes without calling
  update. Demoting the last manager is changed in check mode and fails on a
  real run with Engine's last-manager message.
- Successful results return the raw Engine inspect dictionary in `node`.
- Off-swarm and worker hosts fail with
  `Error running docker swarm module: must run on swarm manager node`.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 node lane.

### docker_node_info

Implements the pinned `community.docker.docker_node_info` 5.2.2 read-only
contract. It must run on a Swarm manager and returns `nodes` as a list of raw
Engine inspect dictionaries.

```yaml
- name: List all nodes
  docker_node_info:

- name: Inspect one node
  docker_node_info:
    name: mynode

- name: Inspect several nodes
  docker_node_info:
    name:
      - mynode1
      - mynode2

- name: Inspect the local manager
  docker_node_info:
    self: true
```

`name` accepts a scalar or list. Omitting `name` lists every registered node.
Missing names are omitted (`nodes: []` on success). `self: true` ignores
`name` and inspects the local daemon node. Check mode is fully supported.

### docker_compose_v2

Implements the pinned `community.docker.docker_compose_v2` 5.2.2 CLI contract
through the installed Compose 5.4.0 plugin. This is a CLI-backed module; it
does not use the Engine API. `docker_compose` remains accepted as a deprecated
alias.

```yaml
- name: Create and start services
  docker_compose_v2:
    project_src: /opt/my-project
    state: present
    pull: missing
    build: policy

- name: Inline definition
  docker_compose_v2:
    project_name: flask
    definition:
      services:
        web:
          image: alpine:latest
          command: ["sleep", "infinity"]

- name: Stop without removing
  docker_compose_v2:
    project_src: /opt/my-project
    state: stopped

- name: Remove the project
  docker_compose_v2:
    project_src: /opt/my-project
    state: absent
    remove_volumes: true
```

Important behavior:

- `state` is `present`, `absent`, `stopped`, or `restarted`. `present` runs
  `docker compose up --detach --no-color --quiet-pull`. `stopped` runs
  `up --no-start` then `stop` only when containers are not already stopped.
  `restarted` always restarts. `absent` runs `down`.
- Canonical `pull` is `always|missing|never|policy` (default `policy`). Canonical
  `build` is `always|never|policy` (default `policy`). Boolean `true`/`false`
  remain Dibra compatibility values and map to `always`/`policy`.
- `dependencies` defaults to true (`--no-deps` when false). `ignore_build_events`
  defaults to true, so a rebuild that does not recreate containers is unchanged.
  Pull events on `up` are ignored for `changed` but still appear in `actions`.
- `definition` writes a temporary `compose.yaml` and requires `project_name`.
  It is mutually exclusive with `project_src` and `files`.
- Check mode passes `--dry-run` and does not count a predicted pull as a
  container change. Diff mode is unsupported. Successful results return
  `actions` as `{what,id,status}` records plus raw Compose `containers` and
  `images` lists. If the secondary `docker compose images` query fails after
  the state command, the module fails rather than suppressing the error,
  matching upstream.
- The module uses the shared CLI connection resolver and injected Docker
  dependencies. Never call `os/exec` or construct a Moby client from the
  executor.

Run `make test-docker-compose-integration` for the Compose 5.4.0 certification
lane.

### docker_compose_v2_pull

Implements the pinned `community.docker.docker_compose_v2_pull` 5.2.2 CLI
contract through the installed Compose 5.4.0 plugin. This is a CLI-backed
module; it does not use the Engine API.

```yaml
- name: Pull images for a Compose project
  docker_compose_v2_pull:
    project_src: /opt/my-project

- name: Pull only when images are missing
  docker_compose_v2_pull:
    project_src: /opt/my-project
    policy: missing

- name: Pull a service and its dependencies
  docker_compose_v2_pull:
    project_src: /opt/my-project
    services:
      - web
    include_deps: true
    ignore_buildable: true
    ignore_pull_failures: true
```

Important behavior:

- `policy` is `always` (default) or `missing`. `--policy` is omitted for
  `always`. Compose 5.4.0 always supports `missing`.
- `ignore_buildable`, `ignore_pull_failures`, and `include_deps` map to the
  matching `docker compose pull` flags. `services` are appended after `--`.
- `definition` writes a temporary `compose.yaml` and requires `project_name`.
  It is mutually exclusive with `project_src` and `files`.
- Changed detection is the inverse of `docker_compose_v2` `up` pulls:
  `policy=always` on a real run ignores `Pulling` for `changed` (layer
  progress still counts), while check mode and `policy=missing` count
  `Pulling`. Check mode with `policy=always` therefore always reports
  changed when Compose emits pull events. Diff mode is unsupported.
- Successful results return `actions` as `{what,id,status}` records with
  status `Pulling`. There is no containers or images list.
- The module uses the shared CLI connection resolver and injected Docker
  dependencies. Never call `os/exec` or construct a Moby client from the
  executor.

### docker_compose_v2_exec

Implements the pinned `community.docker.docker_compose_v2_exec` 5.2.2 CLI
contract through the installed Compose 5.4.0 plugin. It executes a command in
an already running Compose service container.

```yaml
- name: Read application configuration
  docker_compose_v2_exec:
    project_src: /opt/my-project
    service: web
    command: cat /app/config.json

- name: Execute in the second worker replica
  docker_compose_v2_exec:
    project_src: /opt/my-project
    service: worker
    index: 2
    argv: ["/bin/sh", "-c", "echo \"$JOB_ID\""]
    env:
      JOB_ID: "42"
    tty: false
```

Important behavior:

- `service` and exactly one of `command` or `argv` are required. `command`
  uses shell-style argument splitting but does not invoke a shell itself.
- Synchronous execution always reports changed and returns `rc`, `stdout`, and
  `stderr`. A nonzero command or Compose exit code is returned as data and does
  not fail the module.
- Detached execution returns no module-specific fields and fails when Compose
  cannot start the command. `stdin` is forbidden with `detach: true`.
- `stdin_add_newline` and `strip_empty_ends` default to true. Environment
  values must be strings.
- `index` selects a scaled service replica. Workdir, user, privileged mode,
  TTY, environment, inline definitions, project files, environment files,
  profiles, custom CLI paths, and shared CLI connection options follow the
  pinned upstream contract.
- Compose 5.4.0 uses `--no-tty`; this preserves the pinned module semantics
  after the older upstream `--no-TTY` spelling was removed.
- Check and diff modes are unsupported. The registry skips check-mode
  invocation before any command reaches a container.

### docker_compose_v2_run

Implements the pinned `community.docker.docker_compose_v2_run` 5.2.2 CLI
contract through the installed Compose 5.4.0 plugin. It creates a new one-off
container for a Compose service; it does not execute inside an existing
container.

```yaml
- name: Run a one-off command
  docker_compose_v2_run:
    project_src: /opt/my-project
    service: web
    command: /bin/sh -c "ls -lah"
    chdir: /app
    cleanup: true

- name: Start a detached one-off task
  docker_compose_v2_run:
    project_src: /opt/my-project
    service: worker
    argv: ["/bin/sh", "-c", "sleep 60"]
    detach: true
```

Important behavior:

- `service` is required. `command` uses shell-style argument splitting but
  does not itself invoke a shell; `argv` passes arguments directly. The two
  options are mutually exclusive.
- Synchronous runs always report changed and return `rc`, `stdout`, and
  `stderr`, including empty strings and nonzero command exit codes. A nonzero
  container command exit code is data, not a module failure.
- Detached runs return `container_id` and omit `rc`, `stdout`, and `stderr`.
  `stdin` is forbidden with `detach: true`. `stdin_add_newline` and
  `strip_empty_ends` default to true.
- `env` values must be strings. `build`, capabilities, entrypoint, labels,
  name, dependency handling, published/service ports, cleanup, aliases,
  volumes, workdir, user, interactive mode, and TTY map to Compose run
  options.
- Compose 5.4.0 spells the false interactive/TTY flags
  `--interactive=false` and `--no-tty`; these preserve the pinned module
  semantics after the older upstream spellings were removed.
- `definition`, project files, environment files, profiles, custom CLI paths,
  and shared CLI connection options use the same project preparation and
  connection resolvers as the other Compose modules.
- Check and diff modes are unsupported. The registry skips check-mode
  invocation before a one-off container can be created.

### docker_secret

Implements the pinned `community.docker.docker_secret` 5.2.2 Engine API
contract. It creates and removes Swarm secrets. Data changes, label changes,
and `force: true` remove and recreate the secret.

```yaml
- name: Create a secret
  docker_secret:
    name: db_password
    data: opensesame!
    state: present

- name: Create from a file on the managed node
  docker_secret:
    name: db_password
    data_src: /path/to/secret/file

- name: Rotate an in-use secret
  docker_secret:
    name: app_secret
    data: new-contents
    rolling_versions: true
    versions_to_keep: 5

- name: Remove a secret
  docker_secret:
    name: db_password
    state: absent
```

Important behavior:

- `name` is required. `state` is `present` (default) or `absent`. Present
  requires exactly one of `data` or `data_src`. Empty `data: ""` is accepted by
  the module and sent to Engine; Docker Engine 29 rejects 0-byte secrets.
- Idempotency uses a SHA224 hex digest stored in the `ansible_key` label.
  Missing `ansible_key` does not count as a data change unless `force: true`.
- Label comparison is allow-more-present: extra existing labels, including
  `ansible_key`, do not recreate the secret. Adding or changing a requested
  label does. User labels are applied after `ansible_key` / `ansible_version`.
- `rolling_versions: true` creates `{name}_vN` and sets `ansible_version`.
  A data change creates the next version without deleting the current one,
  which is required when a service still references the old secret.
  `versions_to_keep` defaults to 5; `0` and `1` keep only the current
  version, and `-1` keeps every version. Pruning is skipped in check mode.
- Successful present results return `secret_id` and `secret_name`. Check mode
  is fully implemented and does not create or remove Engine objects. Diff mode
  is unsupported. Shared API connection arguments and aliases apply.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 secret lane.

### docker_config

Implements the pinned `community.docker.docker_config` 5.2.2 Engine API
contract. It creates and removes Swarm configs. Data changes, label changes,
`template_driver` changes, and `force: true` remove and recreate the config.

```yaml
- name: Create a config
  docker_config:
    name: db_password
    data: opensesame!
    state: present

- name: Create from a file on the managed node
  docker_config:
    name: db_password
    data_src: /path/to/config/file

- name: Rotate an in-use config
  docker_config:
    name: app_config
    data: new-contents
    rolling_versions: true
    versions_to_keep: 5

- name: Remove a config
  docker_config:
    name: db_password
    state: absent
```

Important behavior:

- `name` is required. `state` is `present` (default) or `absent`. Present
  requires exactly one of `data` or `data_src`. Empty `data: ""` is accepted by
  the module and sent to Engine; Docker Engine 29 rejects 0-byte configs.
- Idempotency uses a SHA224 hex digest stored in the `ansible_key` label.
  Missing `ansible_key` does not count as a data change unless `force: true`.
- Label comparison is allow-more-present: extra existing labels, including
  `ansible_key`, do not recreate the config. Adding or changing a requested
  label does. User labels are applied after `ansible_key` / `ansible_version`.
- `rolling_versions: true` creates `{name}_vN` and sets `ansible_version`.
  A data change creates the next version without deleting the current one,
  which is required when a service still references the old config.
  `versions_to_keep` defaults to 5; `0` and `1` keep only the current
  version, and `-1` keeps every version. Pruning is skipped in check mode.
- `template_driver: golang` stores Engine templating metadata. Clearing it
  from an existing golang config, or adding it to a non-templated config,
  recreates. Invalid values fail with
  `value of template_driver must be one of: golang, got: {value}`.
- Successful present results return `config_id` and `config_name`. Check mode
  is fully implemented and does not create or remove Engine objects. Diff mode
  is unsupported. Shared API connection arguments and aliases apply.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 config lane.

### docker_stack

Implements the pinned `community.docker.docker_stack` 5.2.2 CLI contract
through the installed Docker CLI. This is a CLI-backed module; it does not use
the Engine API. Check and diff modes are unsupported.

```yaml
- name: Deploy stack from a compose file
  docker_stack:
    name: mystack
    compose:
      - /opt/docker-compose.yml

- name: Deploy stack from a base file and an inline override
  docker_stack:
    name: mystack
    compose:
      - /opt/docker-compose.yml
      - version: "3"
        services:
          web:
            image: nginx:latest
            environment:
              ENVVAR: envvar

- name: Remove stack
  docker_stack:
    name: mystack
    state: absent
    absent_retries: 30
```

Important behavior:

- `name` is required. `state` is `present` (default) or `absent`. Present
  requires `compose` to be a list with at least one path string or nested
  dictionary. Dibra keeps `compose_file` as a compatibility alias for a single
  path when `compose` is omitted.
- Nested dictionaries are written to temporary YAML files and passed as extra
  `--compose-file` arguments, matching the documented file-plus-override
  example.
- `detach` defaults to true. `detach: false` adds `--detach=false` to both
  `docker stack deploy` and `docker stack rm` so Docker waits for convergence.
- `prune`, `with_registry_auth`, and `resolve_image` (`always|changed|never`)
  map to the matching deploy flags. An omitted `resolve_image` leaves Docker's
  default (`always`).
- `absent_retries` (default 0) retries `stack rm` until stderr is
  `Nothing found in stack: {name}`. `absent_retries_interval` defaults to 1
  second. A missing stack is unchanged.
- Changed detection inspects each stack service `Spec` before and after
  deploy and ignores `UpdatedAt`/`Version`. Successful changes return
  `stack_spec_diff`. Check and diff modes are unsupported; `--check` skips
  the module.
- `docker_cli` selects the executable. Shared CLI connection arguments and
  aliases apply. The module uses injected Docker CLI, filesystem, clock, and
  environment dependencies.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 stack lane.

### docker_stack_info

Implements the pinned `community.docker.docker_stack_info` 5.2.2 CLI contract
through the installed Docker CLI. This is a read-only CLI-backed module; it
does not use the Engine API.

```yaml
- name: List stacks
  docker_stack_info:
  register: result
```

Important behavior:

- There are no module-specific options. Shared CLI connection arguments and
  aliases apply, including `docker_cli`, `docker_host`/`docker_url`,
  `cli_context`, TLS options, and `api_version`.
- The module runs `docker stack ls --format={{json .}}` and returns `results`
  as a list of dictionaries with Docker's original PascalCase keys. Upstream
  asserts `Name` and `Services`; `Services` is a string. Engine 29's CLI JSON
  includes those two fields and may omit empty `Namespace`/`Orchestrator`
  keys. Docs samples that use lowercase keys are not the CLI output.
- Successful results always include `results` (an empty list when no stacks
  exist), `rc`, `stdout`, and `stderr`, and always report `changed: false`.
- Off-swarm and worker hosts fail with Docker's
  `Error response from daemon: This node is not a swarm manager` message.
- Check mode is fully implemented and does not skip execution. Diff mode is
  not applicable. The module uses injected Docker CLI and environment
  dependencies.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 stack info lane.

### docker_stack_task_info

Implements the pinned `community.docker.docker_stack_task_info` 5.2.2 CLI
contract through the installed Docker CLI. This is a read-only CLI-backed
module; it does not use the Engine API.

```yaml
- name: List tasks for a stack
  docker_stack_task_info:
    name: mystack
  register: result
```

Important behavior:

- `name` is required and selects the stack. Shared CLI connection arguments
  and aliases apply, including `docker_cli`, `docker_host`/`docker_url`,
  `cli_context`, TLS options, and `api_version`.
- The module runs `docker stack ps {name} --format={{json .}}` and returns
  `results` as a list of dictionaries with Docker's original PascalCase keys.
  Upstream asserts `DesiredState`, `Image`, and `Name` (`{stack}_{service}.1`).
  Engine 29's CLI JSON also includes `ID`, `CurrentState`, `Node`, `Error`,
  and `Ports`. Docs samples that use lowercase keys are not the CLI output.
- Successful results always include `results`, `rc`, `stdout`, and `stderr`,
  and always report `changed: false`.
- Off-swarm hosts fail with Docker's
  `Error response from daemon: This node is not a swarm manager` message.
  A missing stack fails with Docker's `nothing found in stack` message.
- Check mode is fully implemented and does not skip execution. Diff mode is
  not applicable. The module uses injected Docker CLI and environment
  dependencies.

Run `make test-docker-swarm-integration` for the Engine 29.7.2 stack task info lane.

### docker_container_exec

Execute a command in a running Docker container.

```yaml
- name: Run command in container
  docker_container_exec:
    container: my-container
    command: echo hello world

- name: Run with argv
  docker_container_exec:
    container: my-container
    argv:
      - /bin/sh
      - -c
      - "ls -la"
    user: root
    chdir: /app
```

The canonical contract follows `community.docker` 5.2.2 and uses the Engine
API, not Docker Compose. Exactly one of `command` or `argv` is required.
`command` uses shell-style argument splitting but does not itself invoke a
shell; use `argv: [/bin/sh, -c, ...]` for redirects, globbing, or environment
expansion. Synchronous executions always return `stdout`, `stderr`, and `rc`,
including empty output and a zero exit code. Detached executions return only
`exec_id`. The module always reports changed and intentionally supports neither
check mode nor diff mode.

`stdin_add_newline` and `strip_empty_ends` default to `true`. Attached stdin is
written while output is consumed and the write side is then closed so commands
such as `cat` receive EOF, including for large payloads. `env` values must be
strings. `chdir` requires Engine API 1.35 or newer. Missing and paused
containers use the pinned upstream error semantics. The shared API connection
arguments, aliases, environment precedence, TLS behavior, and OpenSSH transport
apply. As upstream documents, attached stdin does not work over TCP TLS because
the TLS connection cannot half-close to deliver EOF without closing the read
side. The create request always uses `Tty: false`; the requested TTY value is
applied to start/attach, matching upstream. `privileged` remains a Dibra
compatibility extension and is forwarded to the Engine exec-create request; it
is not an upstream 5.2.2 option.

### docker_container_copy_into

Implements the pinned `community.docker.docker_container_copy_into` 5.2.2
Engine API contract. It streams one regular file or symbolic link through the
container archive endpoint; it does not invoke `docker cp`.

```yaml
- name: Copy content into container
  docker_container_copy_into:
    container: my-container
    content: "Hello World"
    container_path: /tmp/hello.txt
    mode: "0644"
    mode_parse: modern

- name: Copy a file from the managed node into the container
  docker_container_copy_into:
    container: my-container
    path: /srv/application/file.txt
    container_path: /app/file.txt
    owner_id: 1000
    group_id: 1000
```

Important behavior:

- Exactly one of `path` and `content` is required. `path` is resolved on the
  managed node where `dibra-agent` runs, not on the controller. `content`
  requires an explicit `mode` and can be decoded with `content_is_b64`.
- Omitted `force` performs a complete content, filesystem-type, mode, UID, and
  GID comparison for regular files. A managed-node symlink is idempotent when
  the destination is a symlink with the same target; upstream intentionally
  ignores symlink mode and ownership. `force: true` always writes;
  `force: false` preserves any existing destination without comparing it.
- Omitted ownership is derived by executing `id` as the container's configured
  user. Stopped, paused, or minimal containers therefore require both
  `owner_id` and `group_id`; the two options must always be supplied together.
- `local_follow: true` follows a managed-node source link, while false copies
  the link itself, including dangling links. `follow: true` resolves container
  destination links, including relative and multi-hop targets; false replaces
  the link itself.
- `mode_parse` is `legacy` by default. `modern` treats strings as octal and
  integers as decimal values; `octal_string_only` requires an octal string.
  Prefer quoted modes with `mode_parse: modern` in new playbooks.
- Check mode performs all validation and comparisons without uploading an
  archive. Diff mode reports text before/after values and headers, and emits
  binary or size markers instead of content for binary files or files larger
  than Ansible's 104448-byte diff threshold. Temporary files use the upstream
  `(temporary file)` marker.
- Regular-file archives are streamed through the shared API client and all
  filesystem, environment, clock, and client effects use injected Docker
  dependencies. The module supports all shared API connection options.

### docker_image_build

Implements the pinned `community.docker.docker_image_build` 5.2.2 contract
through the installed Docker buildx plugin. This is a CLI-backed BuildKit
module; it does not use the Engine `/build` endpoint, and the Compose 5.4.0 pin
does not select its behavior.

```yaml
- name: Build and load an image
  docker_image_build:
    name: my-app:v1.0.0
    path: /srv/my-app
    dockerfile: Dockerfile.prod
    args:
      VERSION: "1.0.0"
    cache_from:
      - my-app:latest
    etc_hosts:
      registry.internal: host-gateway
    labels:
      release: "1.0.0"
    platform:
      - linux/amd64
    pull: true
    secrets:
      - id: npm-token
        type: env
        env: NPM_TOKEN

- name: Export a root filesystem and keep the named image
  docker_image_build:
    name: my-app:export
    path: /srv/my-app
    rebuild: always
    outputs:
      - type: tar
        dest: /tmp/my-app-rootfs.tar
```

Important behavior:

- `name` and `path` are required. An embedded tag overrides `tag`, whose
  default is `latest`. Image IDs and digest references are rejected.
- `rebuild: never` skips the build when the named image exists;
  `rebuild: always` always invokes buildx. Check mode performs validation and
  image lookup but never runs a build.
- Build arguments, hosts, labels, cache sources, target, network, pull,
  no-cache, shared-memory size, and one or more platforms map to buildx flags.
  Non-string dictionary values use upstream's Python conversion: booleans are
  lowercase, null becomes `None`, and nested lists/maps use Python-style text.
  `shm_size` is converted to bytes as upstream does.
- Secrets support `file`, `env`, and sensitive inline `value` sources. Inline
  values are passed through a generated child-process environment variable and
  are redacted from controller output.
- Outputs support `local`, `tar`, `oci`, `docker`, and `image`. If outputs do
  not retain `name:tag`, the module adds an image output for it. Multiple
  outputs require buildx 0.13.0 or newer; environment/value secrets require
  buildx 0.6.0 or newer. The integration host pins and asserts buildx 0.30.0;
  required output and multi-platform certification scenarios must fail rather
  than skip when that pinned baseline rejects them.
- `docker_cli` selects the executable. The shared CLI connection resolver owns
  host/context exclusivity, API-version environment, TLS flags and certificate
  paths. Successful builds return raw image inspection data, the buildx
  command, stdout, and stderr. Diff mode is unsupported.

### docker_image_load

Implements the pinned `community.docker.docker_image_load` 5.2.2 Engine API
contract. It loads every image represented by a Docker archive and inspects
the names and IDs reported by the daemon.

```yaml
- name: Load images from a Docker archive
  docker_image_load:
    path: /tmp/images.tar
  register: loaded
```

The module consumes the complete Engine load stream with `quiet=false`, as
upstream does. Both `Loaded image: name:tag` and `Loaded image ID: sha256:...`
records are preserved in `image_names`, including daemon-dependent duplicates
and mixed name/ID results. `images` contains a complete Engine inspection
dictionary for each recognized reference, in matching order, and `stdout`
contains the load stream text.

Loading is intentionally non-idempotent: every successful invocation reports
changed, even when the same archive is loaded repeatedly. Check mode and diff
mode are unsupported; check mode skips execution before opening the archive.
Only `sha256:` followed by exactly 64 hexadecimal characters is recognized as
an image ID. Bare or short hashes follow upstream's name/tag or warning path.
Missing files, invalid archives, embedded stream errors, and streams reporting
no loaded images fail with the pinned upstream semantics. All shared API
connection options and aliases apply.

### docker_image_export

Implements the pinned `community.docker.docker_image_export` 5.2.2 Engine API
contract. It saves one or more local image names or IDs to a Docker archive.

```yaml
- name: Export an image
  docker_image_export:
    name: alpine:latest
    path: /tmp/alpine.tar

- name: Export multiple images
  docker_image_export:
    names:
      - alpine:latest
      - redis:latest
    path: /tmp/images.tar

- name: Export one platform from an image
  docker_image_export:
    name: alpine:latest
    path: /tmp/alpine-amd64.tar
    platform: linux/amd64
```

The `name` alias accepts one value or a list; `names` is canonical. A tag
embedded in a name wins over the default `tag: latest`. Before exporting, the
module inspects every requested image and returns the complete Engine
inspection dictionaries in `images`.

Unless `force: true`, archive idempotency compares `manifest.json` image IDs
and requested tags without extracting the archive. Docker Engine 29's
containerd image store can emit config IDs that differ from inspect IDs, and
multi-platform exports are not idempotent, matching the pinned upstream
limitations. Check mode predicts the export without writing a file.
`platform` requires API 1.48 or newer. Diff mode is unsupported, and all
connections use the shared API resolver.

### docker_image_pull

Implements the pinned `community.docker.docker_image_pull` 5.2.2 Engine API
contract.

```yaml
- name: Always check the registry for the requested image
  docker_image_pull:
    name: alpine
    tag: latest
    platform: linux/amd64
    pull: always

- name: Pull only when the requested platform is not local
  docker_image_pull:
    name: registry.example.com/team/app:v1
    platform: amd64
    pull: not_present
```

An embedded tag or digest takes precedence over `tag`, whose default is
`latest`; image IDs are rejected. `pull: always` contacts the registry on every
real run but reports unchanged when the resulting image ID is unchanged.
`pull: not_present` skips when the local name and requested platform match.
Architecture-only platform values are completed using daemon information, and
`platform` requires API 1.32 or newer.

Check mode predicts a required pull without contacting the registry and uses
`unknown` as the after ID. Diff mode returns only image existence/IDs in
`before` and `after`. Successful real pulls return the complete Engine
inspection dictionary in `image`. Docker Engine 29 no longer reliably replaces
a better host-platform image with a requested foreign platform; the pinned
upstream suite excludes that cross-architecture transition on Engine 29, and
Dibra follows the same baseline. Registry credentials come from Docker's
`config.json`, and embedded stream errors are always surfaced.

### docker_image_push

Implements the pinned `community.docker.docker_image_push` 5.2.2 Engine API
contract.

```yaml
- name: Push a tagged local image
  docker_image_push:
    name: registry.example.com/team/app
    tag: v1
```

The named local image must exist. Embedded tags override `tag`; image IDs and
digest references cannot be pushed. The module reads matching inline
credentials from Docker's `config.json` and sends an explicit anonymous auth
header when none exist, as required by Docker 28.3.3 and newer.

Pushes report changed only when Engine emits `Pushing` or `Pushed` progress.
Consequently, a repeated push whose layers already exist is unchanged, while a
new or replaced image is changed. The response contains the original complete
local Engine inspection in `image` and a structured action. Check and diff
modes are unsupported; check mode skips the module before registry contact.
Embedded stream failures and authentication errors fail the task.

### docker_image_remove

Implements the pinned `community.docker.docker_image_remove` 5.2.2 Engine API
contract.

```yaml
- name: Remove one image tag
  docker_image_remove:
    name: registry.example.com/team/app
    tag: old

- name: Force-remove an image and all of its aliases
  docker_image_remove:
    name: sha256:0123456789abcdef...
    force: true
    prune: false
```

An embedded tag or digest takes precedence over `tag`, whose default is
`latest`. Names, digest references, and canonical full image IDs are accepted.
Missing images succeed unchanged. Removing a name normally untags that
reference and deletes the image only when no references remain; `force` and
`prune` map directly to Engine's remove options.

Check mode predicts `deleted` and `untagged` without mutating Engine, and diff
mode reports image existence, ID, sorted tags, and sorted digests before and
after. The original complete inspection is returned in `image`. Docker 29 can
report more or fewer digest aliases during a real force-removal by ID than can
be predicted; this is an acknowledged limitation in the pinned upstream module
and does not change the predicted or actual final existence state.

### docker_image_tag

Implements the pinned `community.docker.docker_image_tag` 5.2.2 Engine API
contract.

```yaml
- name: Add several names to an image
  docker_image_tag:
    name: alpine:latest
    repository:
      - local/alpine:stable
      - registry.example.com/team/alpine
    tag: latest
    existing_images: overwrite
```

`name` accepts a name, digest reference, or image ID. `repository` is a required
list of target names; targets cannot be image IDs or digest references, and a
target without a tag uses `tag` (default `latest`). An embedded source tag
overrides `tag` for source lookup but does not change the default applied to
untagged targets.

`existing_images: keep` preserves targets that point at another image, while
`overwrite` retags them. Targets already pointing at the requested source are
unchanged. Check mode predicts each operation without tagging, and the always
structured diff records each target's name, tag, prior ID or absence, and
resulting ID. Successful results return the original complete source inspection
in `image` and list only newly created or overwritten names in `tagged_images`.

### docker_container_info

Inspect a Docker container and return its full configuration. Read-only module.

```yaml
- name: Get container info
  docker_container_info:
    name: my-container
```

**Returns**: `exists` and `container` are always present. For an existing
container, `container` is the complete Engine inspection object with Docker's
original field names and JSON shapes (for example `Id`, `State`, `Config`,
`HostConfig`, `NetworkSettings`, and `Mounts`); do not transform it into a
smaller snake-case summary. For a missing container, the module succeeds with
`exists: false` and `container: null`.

This is a read-only Engine API module. It supports check mode fully, has no diff
output, always reports unchanged, and accepts names plus long or short container
IDs. The shared API connection arguments, aliases, environment precedence, TLS
behavior, and OpenSSH transport apply. Focused integration coverage compares
the returned object directly with `docker inspect` on Engine 29.7.2.

### docker_image_info

Implements the pinned `community.docker.docker_image_info` 5.2.2 read-only
contract.

```yaml
- name: Inspect one image (latest is implicit)
  docker_image_info:
    name: alpine

- name: Inspect multiple local images
  docker_image_info:
    name:
      - alpine:latest
      - busybox:latest

- name: Inspect all locally listed images
  docker_image_info:
```

`name` accepts a scalar or list of names, full IDs, or short IDs. Missing
requested images are silently omitted while existing results retain request
order and duplicates. Omitting `name` lists non-intermediate local images and
fully inspects each one. The module never pulls from a registry.

`images` is always present and contains the complete Engine inspection
dictionaries with Docker's original field names and JSON shapes. There is no
separate `exists` field; an empty list means that no requested image exists.
The module always reports unchanged, supports check mode fully, emits no diff,
and uses the shared API connection resolver.

### docker_network_info

Implements the pinned `community.docker.docker_network_info` 5.2.2 read-only
contract. Missing networks succeed with `exists: false` and `network: null`.
Existing results are the complete Engine inspection with original field names.

```yaml
- name: Get network info
  docker_network_info:
    name: my-network
```

The module always reports unchanged, supports check mode fully, and emits no
diff. Shared API connection arguments apply.

### docker_volume_info

Implements the pinned `community.docker.docker_volume_info` 5.2.2 read-only
contract. Missing volumes succeed with `exists: false` and `volume: null`.
Existing results are the complete Engine inspection. Canonical `name` accepts
the upstream `volume_name` alias, and non-not-found inspection failures use
the upstream `Error inspecting volume: ` prefix.

```yaml
- name: Get volume info
  docker_volume_info:
    name: my-volume
```

### docker_host_info

Implements the pinned `community.docker.docker_host_info` 5.2.2 read-only
contract. `host_info` is the raw Engine info object. `can_talk_to_docker` is
always present and is `false` when the daemon cannot be reached.

```yaml
- name: Get docker host info
  docker_host_info:
    containers: true
    containers_all: true
    images: true
    networks: true
    volumes: true
    disk_usage: true
    verbose_output: false
```

Non-verbose lists use the upstream key subsets (`Id`/`Image`/`Command`/...
for containers, `Id`/`RepoTags`/`Created`/`Size` for images, `Id`/`Driver`/
`Name`/`Scope` for networks, `Driver`/`Name` for volumes). Verbose lists
return the full Engine objects. Non-verbose `disk_usage` is `{LayersSize}`;
verbose adds the complete legacy-compatible `Images`, `Containers`, `Volumes`,
and `BuildCache` sequences. Filters accept strings or lists. Shared API
connection arguments apply.

### docker_context_info

Implements the pinned `community.docker.docker_context_info` 5.2.2 CLI
context contract. This module does not talk to the Engine API; it reads
Docker CLI context files through injected filesystem and environment
dependencies.

```yaml
- name: List Docker CLI contexts
  docker_context_info:

- name: Current context only
  docker_context_info:
    only_current: true
    cli_context: default
```

`only_current` and `name` are mutually exclusive. Current name is
`cli_context`, else `DOCKER_HOST` forces `default`, else `DOCKER_CONTEXT`,
else `~/.docker/config.json` `currentContext`, else `default`. The synthetic
default context has description `Current DOCKER_HOST based configuration`
and null `meta_path`/`tls_path`. Named Docker endpoints with an omitted host
default to the local Unix socket, while omitted `SkipTLSVerify` defaults to
true as in the pinned Docker SDK context loader. TLS material is discovered by
the `ca*`, `cert*`, and `key*` filename prefixes; client cert and key are
returned only as a pair. A TLS context with skipped verification returns
`validate_certs: null`, and nullable metadata such as `description` remains
null rather than becoming an empty string. A missing named context fails.

### current_container_facts

Implements the pinned `community.docker.current_container_facts` 5.2.2
read-only contract. There is no Docker connection. Facts are returned under
`ansible_facts` and are automatically injected into the host variable context,
including their top-level `ansible_*` names, without requiring `register`.

```yaml
- name: Detect the current container
  current_container_facts:
```

Detection uses `/proc/self/cpuset` (`/docker`, `/azpl_job`, `/actions_job`)
and falls back to `/proc/self/mountinfo` hostname paths for Docker versus
Podman. Missing proc files are skipped. An existing proc file that cannot be
read or decoded as UTF-8 fails the module, matching upstream instead of
silently returning non-container facts. Check mode is fully supported.

### docker_swarm_info

Implements the pinned `community.docker.docker_swarm_info` 5.2.2 read-only
contract. It must run on a Swarm manager; otherwise it fails with
`Error running docker swarm module: must run on swarm manager node` and still
returns `can_talk_to_docker`, `docker_swarm_active`, and `docker_swarm_manager`.

```yaml
- name: Get swarm facts
  docker_swarm_info:

- name: List nodes, services, and tasks
  docker_swarm_info:
    nodes: true
    services: true
    tasks: true
    verbose_output: true

- name: Filter nodes and retrieve the unlock key
  docker_swarm_info:
    nodes: true
    nodes_filters:
      name: mynode
    unlock_key: true
```

`swarm_facts` is the raw Engine swarm inspect object, including `JoinTokens`.
Non-verbose `nodes`, `services`, and `tasks` use the `docker node ls` /
`docker service ls` / `docker service ps` key subsets. Verbose lists return the
full Engine objects. `swarm_unlock_key` is present only when `unlock_key` is
true: `null` on an unlocked swarm, otherwise the `SWMKEY-` string. Filters
accept strings or lists. `verbose` remains a Dibra alias for `verbose_output`.
Shared API connection arguments apply. Check mode is fully supported; diff
mode is not applicable.

### command

Executes commands on targets without going through a shell. This is more secure than the shell module since shell metacharacters are not interpreted.

```yaml
# Simple command
- name: Check system uptime
  command:
    cmd: uptime

# Command with arguments using argv (safer for arguments with spaces)
- name: Echo with spaces
  command:
    argv:
      - echo
      - "hello world"

# Run only if file doesn't exist (idempotent)
- name: Create database
  command:
    cmd: /usr/bin/make_database.sh db_user db_name
    creates: /path/to/database

# Run only if file exists
- name: Remove old logs
  command:
    cmd: rm /var/log/app/old.log
    removes: /var/log/app/old.log

# Change directory before execution
- name: Run in /tmp
  command:
    cmd: ls -la
    chdir: /tmp

# Provide stdin input
- name: Send input to command
  command:
    argv:
      - cat
    stdin: "hello from stdin"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `cmd` | | The command to run as a string (split on spaces, respects quotes). |
| `argv` | | The command as a list of arguments (safer for arguments with spaces). |
| `chdir` | | Change to this directory before running the command. |
| `creates` | | A filename or glob pattern. If it exists, the command will **not** run. |
| `removes` | | A filename or glob pattern. If it exists, the command **will** run. |
| `stdin` | | Set the stdin of the command directly to this value. |
| `stdin_add_newline` | `true` | Append a newline to stdin data. |
| `strip_empty_ends` | `true` | Strip empty lines from the end of stdout/stderr. |

**Returns**:
```json
{
  "changed": true,
  "cmd": ["echo", "hello"],
  "stdout": "hello",
  "stderr": "",
  "stdout_lines": ["hello"],
  "stderr_lines": [],
  "rc": 0,
  "start": "2024-01-15 10:30:00.000000",
  "end": "2024-01-15 10:30:00.001234",
  "delta": "0:00:00.001234"
}
```

**Notes**:
- One of `cmd` or `argv` is required.
- Unlike the shell module, special characters like `<`, `>`, `|`, `;`, `&` are **not** interpreted.
- Use `argv` when arguments contain spaces or special characters.
- Use `creates`/`removes` for idempotent command execution.
- Non-zero return codes cause the task to fail.

**Idempotency**: Use `creates` or `removes` parameters to make commands idempotent.

### shell

Executes commands through a shell (`/bin/sh` by default) on the remote node. Unlike the command module, shell commands are interpreted by the shell, enabling pipes, redirects, environment variables, and other shell features.

```yaml
# Simple shell command
- name: Run shell command
  shell:
    cmd: echo hello

# Using pipes
- name: Count files
  shell:
    cmd: ls -la | wc -l

# Redirect output to file
- name: Write to file
  shell:
    cmd: echo "content" > /tmp/myfile.txt

# Using environment variables
- name: Print home directory
  shell:
    cmd: echo $HOME

# Command substitution
- name: Show current date
  shell:
    cmd: echo "Today is $(date)"

# Logical operators
- name: Check and run
  shell:
    cmd: test -f /tmp/flag && echo "exists"

# Run with custom shell
- name: Run with bash
  shell:
    cmd: echo $BASH_VERSION
    executable: /bin/bash

# Run only if file doesn't exist (idempotent)
- name: Create database
  shell:
    cmd: /usr/bin/make_database.sh
    creates: /var/lib/mydb/data

# Run only if file exists
- name: Remove old logs
  shell:
    cmd: rm /var/log/app/*.old
    removes: /var/log/app/cleanup.flag

# Change directory before execution
- name: Run in /tmp
  shell:
    cmd: pwd
    chdir: /tmp

# Provide stdin input
- name: Send input to command
  shell:
    cmd: cat
    stdin: "hello from stdin"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `cmd` | required | The shell command to run as a string. |
| `chdir` | | Change to this directory before running the command. |
| `creates` | | A filename or glob pattern. If it exists, the command will **not** run. |
| `removes` | | A filename or glob pattern. If it exists, the command **will** run. |
| `stdin` | | Set the stdin of the command directly to this value. |
| `stdin_add_newline` | `true` | Append a newline to stdin data. |
| `strip_empty_ends` | `true` | Strip empty lines from the end of stdout/stderr. |
| `executable` | `/bin/sh` | Path to the shell executable (e.g., `/bin/bash`). |

**Returns**:
```json
{
  "changed": true,
  "cmd": "echo hello | cat",
  "stdout": "hello",
  "stderr": "",
  "stdout_lines": ["hello"],
  "stderr_lines": [],
  "rc": 0,
  "start": "2024-01-15 10:30:00.000000",
  "end": "2024-01-15 10:30:00.001234",
  "delta": "0:00:00.001234"
}
```

**Shell Features Supported**:
- Pipes: `cmd1 | cmd2`
- Redirects: `>`, `>>`, `<`, `2>&1`
- Environment variables: `$HOME`, `${VAR}`
- Command substitution: `$(command)` or `` `command` ``
- Logical operators: `&&`, `||`, `;`
- Wildcards/globbing: `*.txt`
- Subshells: `(cd /tmp && pwd)`
- Here documents: `cat <<EOF`
- For loops: `for i in 1 2 3; do echo $i; done`

**Notes**:
- Use `command` module when you don't need shell features (more secure).
- Always quote variables with `{{ var | quote }}` to prevent shell injection.
- Use `creates`/`removes` for idempotent shell command execution.
- Non-zero return codes cause the task to fail.

**Idempotency**: Use `creates` or `removes` parameters to make shell commands idempotent.

### lineinfile

Ensures a particular line is in a file, or replaces an existing line using a regular expression. Useful for managing single lines in configuration files.

```yaml
# Add line at end of file (default)
- name: Add line to file
  lineinfile:
    path: /etc/hosts
    line: "192.168.1.100 myserver.local"

# Insert line at beginning of file
- name: Add line at BOF
  lineinfile:
    path: /etc/config
    line: "# Configuration file"
    insertbefore: BOF

# Insert after a pattern
- name: Add setting after section header
  lineinfile:
    path: /etc/myapp.conf
    line: "option = value"
    insertafter: "\\[section\\]"

# Insert before a pattern
- name: Add setting before section
  lineinfile:
    path: /etc/myapp.conf
    line: "global_option = true"
    insertbefore: "\\[section\\]"

# Replace line matching regexp
- name: Change SSH port
  lineinfile:
    path: /etc/ssh/sshd_config
    regexp: "^#?Port"
    line: "Port 2222"

# Replace using literal string search
- name: Replace by literal match
  lineinfile:
    path: /etc/config
    search_string: "old_value"
    line: "new_value"

# Replace with backreferences
- name: Update config with captured groups
  lineinfile:
    path: /etc/config
    regexp: "^(key)=.*$"
    line: "\\1=newvalue"
    backrefs: true

# Remove line matching pattern
- name: Remove comment lines
  lineinfile:
    path: /etc/config
    regexp: "^#.*$"
    state: absent

# Create file if it doesn't exist
- name: Ensure line in new file
  lineinfile:
    path: /etc/newconfig
    line: "setting=value"
    create: true

# Create backup before modification
- name: Modify with backup
  lineinfile:
    path: /etc/important.conf
    regexp: "^setting="
    line: "setting=new"
    backup: true

# Replace only first matching line
- name: Replace first match
  lineinfile:
    path: /etc/config
    regexp: "^duplicate"
    line: "replaced"
    firstmatch: true

# Validate before applying changes
- name: Modify sudoers safely
  lineinfile:
    path: /etc/sudoers
    line: "user ALL=(ALL) NOPASSWD: ALL"
    validate: "visudo -cf %s"

# Set file permissions
- name: Add line with permissions
  lineinfile:
    path: /etc/secure.conf
    line: "secret=value"
    mode: "0600"
    owner: root
    group: root
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `path` | required | Path to the file to modify. |
| `line` | | The line to insert/replace. Required for `state=present`. |
| `regexp` | | Regex pattern to find the line to replace. Only last match is replaced (or first if `firstmatch=true`). |
| `search_string` | | Literal string to find in the line. Mutually exclusive with `regexp`. |
| `state` | `present` | `present` to add/replace, `absent` to remove matching lines. |
| `backrefs` | `false` | Use regex capture groups in `line` (e.g., `\\1`). Requires `regexp`. If no match, file is unchanged. |
| `insertafter` | `EOF` | Insert after last match of this regex. Special: `EOF` (end of file). |
| `insertbefore` | | Insert before last match of this regex. Special: `BOF` (beginning of file). Mutually exclusive with `insertafter`. |
| `firstmatch` | `false` | Insert after/before first match instead of last. Also affects which line `regexp` replaces. |
| `create` | `false` | Create file if it doesn't exist. |
| `backup` | `false` | Create timestamped backup before modifying. |
| `validate` | | Command to validate file before applying. Use `%s` for temp file path. |
| `mode` | | File mode (e.g., `"0644"`). |
| `owner` | | File owner. |
| `group` | | File group. |

**Behavior Notes**:
- When `regexp` matches: replaces the matched line with `line`
- When `regexp` doesn't match: inserts `line` at position determined by `insertafter`/`insertbefore`
- When `backrefs=true` and `regexp` doesn't match: file is unchanged (safe behavior)
- `regexp` takes precedence over `insertafter`/`insertbefore` for determining replacement

**Idempotency**: Checks if line already exists before adding. Replaces only if content differs.

### blockinfile

Inserts, updates, or removes a block of multi-line text surrounded by customizable marker lines.

```yaml
# Basic block insertion
- name: Add SSH match block
  blockinfile:
    path: /etc/ssh/sshd_config
    block: |
      Match User dibra-agent
          PasswordAuthentication no

# Custom markers
- name: Add Apache logging config
  blockinfile:
    path: /etc/httpd/httpd.conf
    marker: "# {mark} LOGGING CONFIG"
    block: |
      ErrorLog ${APACHE_LOG_DIR}/error.log
      CustomLog ${APACHE_LOG_DIR}/access.log combined

# Insert after a pattern
- name: Add kernel parameters
  blockinfile:
    path: /etc/sysctl.conf
    insertafter: "net.ipv4.ip_forward"
    block: |
      net.ipv4.tcp_syncookies = 1
      net.core.somaxconn = 1024

# Insert before a pattern
- name: Add firewall rules
  blockinfile:
    path: /etc/firewalld/services/custom.xml
    marker: "<!-- {mark} CUSTOM RULES -->"
    insertbefore: "</service>"
    block: |
      <port protocol="tcp" port="8080"/>
      <port protocol="tcp" port="8443"/>

# Insert at beginning of file
- name: Add file header
  blockinfile:
    path: /etc/config
    insertbefore: BOF
    block: |
      # Configuration file
      # Do not edit manually

# Create file if it doesn't exist
- name: Create config with block
  blockinfile:
    path: /etc/myapp/config
    create: true
    block: |
      key1=value1
      key2=value2

# Remove block
- name: Remove old block
  blockinfile:
    path: /etc/config
    state: absent

# Add newlines around block
- name: Insert with spacing
  blockinfile:
    path: /etc/config
    prepend_newline: true
    append_newline: true
    block: |
      separated block

# Create backup before modification
- name: Modify with backup
  blockinfile:
    path: /etc/important.conf
    block: |
      new configuration
    backup: true

# Validate before applying
- name: Modify sudoers safely
  blockinfile:
    path: /etc/sudoers.d/custom
    block: |
      user ALL=(ALL) NOPASSWD: ALL
    validate: "visudo -cf %s"

# Multiple blocks with different markers
- name: Add first block
  blockinfile:
    path: /etc/config
    marker: "# {mark} BLOCK ONE"
    block: |
      block one content

- name: Add second block
  blockinfile:
    path: /etc/config
    marker: "# {mark} BLOCK TWO"
    block: |
      block two content
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `path` | required | Path to the file to modify. |
| `block` | `""` | The text to insert inside the marker lines. Empty string or missing removes the block. |
| `marker` | `"# {mark} ANSIBLE MANAGED BLOCK"` | Marker line template. `{mark}` is replaced with `marker_begin` or `marker_end`. |
| `marker_begin` | `"BEGIN"` | Text to replace `{mark}` in the opening marker. |
| `marker_end` | `"END"` | Text to replace `{mark}` in the closing marker. |
| `insertafter` | `EOF` | Insert after last match of this regex. Special: `EOF` (end of file). |
| `insertbefore` | | Insert before last match of this regex. Special: `BOF` (beginning of file). Mutually exclusive with `insertafter`. |
| `state` | `present` | `present` to add/update block, `absent` to remove block. |
| `create` | `false` | Create file if it doesn't exist. |
| `backup` | `false` | Create timestamped backup before modifying. |
| `prepend_newline` | `false` | Add blank line before block if not at beginning. |
| `append_newline` | `false` | Add blank line after block if not at end. |
| `validate` | | Command to validate file before applying. Use `%s` for temp file path. |
| `mode` | | File mode (e.g., `"0644"`). |
| `owner` | | File owner. |
| `group` | | File group. |

**Behavior Notes**:
- If markers exist: replaces the existing block content
- If markers don't exist: inserts at position determined by `insertafter`/`insertbefore`
- Empty `block` or `state=absent`: removes the block (markers and content)
- Use unique markers when managing multiple blocks in the same file
- Markers are matched exactly (including any indentation)

**Idempotency**: Compares block content with existing. Only changes if content differs.

### replace

Replaces all instances of a pattern within a file using regular expressions. Unlike `lineinfile` which manages single lines, `replace` operates on all matches throughout the file.

```yaml
# Basic replacement
- name: Replace old hostname
  replace:
    path: /etc/hosts
    regexp: 'old\.example\.com'
    replace: 'new.example.com'

# Replace with backreferences
- name: Update ServerName with captured groups
  replace:
    path: /etc/apache2/sites-available/default.conf
    regexp: '(ServerName\s+)old\.host\.name'
    replace: '\1new.host.name'

# Replace after a pattern (till end of file)
- name: Comment out everything after marker
  replace:
    path: /etc/config
    after: '# BEGIN MANAGED'
    regexp: '^(.+)$'
    replace: '# \1'

# Replace before a pattern (from beginning of file)
- name: Comment out everything before marker
  replace:
    path: /etc/config
    before: '# END HEADER'
    regexp: '^(.+)$'
    replace: '# \1'

# Replace between two markers
- name: Update config between markers
  replace:
    path: /etc/config
    after: '# BEGIN MANAGED'
    before: '# END MANAGED'
    regexp: 'old_value'
    replace: 'new_value'

# Remove matches (empty replace)
- name: Remove comments
  replace:
    path: /etc/config
    regexp: '#.*$'

# Case-insensitive replacement
- name: Replace all variants of word
  replace:
    path: /var/www/html/index.html
    regexp: '(?i)oldword'
    replace: 'newword'

# Create backup before modification
- name: Replace with backup
  replace:
    path: /etc/important.conf
    regexp: 'setting=old'
    replace: 'setting=new'
    backup: true

# Validate before applying
- name: Modify sudoers safely
  replace:
    path: /etc/sudoers
    regexp: '^#\s*(Defaults\s+env_reset)'
    replace: '\1'
    validate: 'visudo -cf %s'

# Set file permissions
- name: Replace with mode
  replace:
    path: /etc/secure.conf
    regexp: 'password=.*'
    replace: 'password=secret'
    mode: "0600"
    owner: root
    group: root
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `path` | required | Path to the file to modify. |
| `regexp` | required | Regular expression to search for (uses MULTILINE mode: `^` and `$` match line boundaries). |
| `replace` | `""` | String to replace matches with. Supports backreferences (`\1`, `\2`). Empty string removes matches. |
| `after` | | Only replace content after this pattern (uses DOTALL mode: `.` matches newlines). |
| `before` | | Only replace content before this pattern (uses DOTALL mode: `.` matches newlines). |
| `backup` | `false` | Create timestamped backup before modifying. |
| `validate` | | Command to validate file before applying. Use `%s` for temp file path. |
| `mode` | | File mode (e.g., `"0644"`). |
| `owner` | | File owner. |
| `group` | | File group. |

**Regex Modes**:
- `regexp` uses MULTILINE mode: `^` and `$` match at line boundaries
- `after`/`before` use DOTALL mode: `.` can match newlines
- Use `(?i)` flag for case-insensitive matching
- Use `(?s)` flag in regexp for DOTALL mode

**Behavior Notes**:
- Replaces ALL matches in the file (or section if after/before used)
- If `after` and `before` are both specified, only content between them is modified
- If `after`/`before` pattern doesn't match, file is unchanged
- Backreferences: `\1`, `\2`, etc. refer to captured groups

**Idempotency**: Compares content before/after. Returns unchanged if no matches found or content identical after replacement.

### apt

Manages Debian/Ubuntu packages via apt-get.

```yaml
- name: Install packages
  apt:
    name:
      - nginx
      - curl
    state: present      # present, absent, latest
    update_cache: true
    cache_valid_time: 3600
    purge: false        # Remove config files when state=absent
    autoremove: false
    upgrade: ""         # safe, full, dist
```

**Idempotency**: Checks `dpkg-query` before install/remove.

### apt_key

Manages APT GPG keys.

```yaml
- name: Add GPG key
  apt_key:
    url: https://example.com/gpg.key
    keyring: /etc/apt/keyrings/example.gpg
    state: present      # present, absent
```

**Features**:
- Downloads key from URL
- Dearmors ASCII-armored keys via `gpg --dearmor`
- Creates keyring directory if needed
- Idempotent: checks if key already exists

### apt_repository

Manages APT repository sources.

```yaml
- name: Add repository
  apt_repository:
    repo: "deb [signed-by=/etc/apt/keyrings/example.gpg] https://example.com/apt stable main"
    filename: example    # Creates /etc/apt/sources.list.d/example.list
    update_cache: true
    state: present       # present, absent
```

**Idempotency**: Checks if repo line already exists in file.

### file

Manages files, directories, and symlinks.

```yaml
# Create directory
- name: Create app directory
  file:
    path: /opt/myapp
    state: directory
    mode: "0755"
    owner: root
    group: root
    recurse: false      # Apply permissions recursively

# Create symlink
- name: Create symlink
  file:
    path: /usr/local/bin/myapp
    src: /opt/myapp/bin/myapp
    state: link
    force: false        # Replace existing file/link

# Touch file
- name: Touch file
  file:
    path: /opt/myapp/last-run
    state: touch
    mode: "0644"

# Delete
- name: Remove old directory
  file:
    path: /opt/old-app
    state: absent
```

**States**: `file`, `directory`, `link`, `hard`, `touch`, `absent`

### copy

Copies files to remote hosts.

```yaml
# Copy local file to remote
- name: Deploy config
  copy:
    src: ./config/app.yaml      # Local path
    dest: /etc/myapp/config.yaml
    mode: "0644"
    owner: root
    group: root
    backup: true                # Create timestamped backup

# Create file from inline content
- name: Create systemd unit
  copy:
    content: |
      [Unit]
      Description=My App
      [Service]
      ExecStart=/usr/bin/myapp
    dest: /etc/systemd/system/myapp.service
    mode: "0644"

# Copy file already on remote
- name: Backup config
  copy:
    src: /etc/myapp/config.yaml
    dest: /etc/myapp/config.yaml.bak
    remote_src: true
```

**File Transfer Flow** (when `src` is local):
1. Controller computes SHA1 checksum of local file
2. Controller uploads to `/tmp/.dibra-copy-<hash>`
3. Agent verifies checksum matches
4. Agent atomically moves to destination
5. Agent applies mode/owner/group

**Idempotency**: Compares SHA1 checksums; skips if destination matches.

### template

Renders a Jinja-like template on the controller and writes it to the remote host.

```yaml
# Render a template
- name: Render config
  template:
    src: ./templates/app.conf.j2
    dest: /etc/myapp/app.conf
    mode: "0644"
    owner: root
    group: root

# Custom delimiters
- name: Custom delimiters
  template:
    src: ./templates/custom.j2
    dest: /etc/myapp/custom.conf
    variable_start_string: "[["
    variable_end_string: "]]"

# Newline sequence
- name: Windows line endings
  template:
    src: ./templates/win.ini.j2
    dest: /etc/myapp/win.ini
    newline_sequence: "\r\n"

# Validate before write
- name: Validate with command
  template:
    src: ./templates/nginx.conf.j2
    dest: /etc/nginx/nginx.conf
    validate: "/usr/sbin/nginx -t -c %s"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `src` | required | Template path on controller. Relative paths resolve relative to playbook. |
| `dest` | required | Destination path on remote host. Can be directory. |
| `mode` | | File mode (e.g., `"0644"`). |
| `owner` | | File owner. |
| `group` | | File group. |
| `backup` | `false` | Backup existing file before overwriting. |
| `force` | `true` | Overwrite when content differs. |
| `follow` | `false` | Follow symlinks when writing. |
| `validate` | | Command to validate rendered content (`%s` replaced by temp file). |
| `newline_sequence` | `"\n"` | Output newline sequence: `"\n"`, `"\r"`, `"\r\n"`. |
| `variable_start_string` | `"{{"` | Variable start delimiter. |
| `variable_end_string` | `"}}"` | Variable end delimiter. |
| `block_start_string` | `"{%"` | Block start delimiter. |
| `block_end_string` | `"%}"` | Block end delimiter. |
| `comment_start_string` | `"{#"` | Comment start delimiter. |
| `comment_end_string` | `"#}"` | Comment end delimiter. |
| `trim_blocks` | `true` | Remove first newline after blocks. |
| `lstrip_blocks` | `false` | Strip leading whitespace to blocks. |

**Template Variables**:
- `dibra_managed`, `template_host`, `template_uid`, `template_path`, `template_fullpath`, `template_destpath`, `template_dest`, `template_run_date`, `template` (map containing metadata)

**Idempotency**: SHA1 checksum of rendered content; re-applies ownership/mode changes when content matches.

#### Ansible-Compatible Filters

In addition to the [built-in Jinja2 filters](https://jinja.palletsprojects.com/en/3.1.x/templates/#builtin-filters) provided by the gonja engine (`default`, `upper`, `lower`, `replace`, `join`, `length`, `first`, `last`, `int`, `float`, `trim`, `title`, `capitalize`, `reverse`, `sort`, `unique`, `tojson`, `round`, `abs`, `batch`, `dictsort`, `escape`, `filesizeformat`, `groupby`, `indent`, `list`, `map`, `max`, `min`, `pprint`, `random`, `reject`, `rejectattr`, `safe`, `select`, `selectattr`, `slice`, `string`, `striptags`, `sum`, `truncate`, `urlencode`, `urlize`, `wordcount`, `wordwrap`, `xmlattr`), dibra registers the following Ansible-compatible custom filters:

##### String Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `split(sep)` | `{{ "a,b,c" \| split(",") }}` | Split string into list. Default separator is space. |
| `quote` | `{{ value \| quote }}` | Shell-safe quoting with single quotes (escapes embedded quotes). |
| `comment(prefix='# ')` | `{{ text \| comment }}` | Prepend comment prefix to each line. |
| `regex_replace(pattern, replacement, count=0)` | `{{ value \| regex_replace('^old', 'new') }}` | Replace regex matches. `count=0` replaces all. |
| `regex_search(pattern)` | `{{ value \| regex_search('(\d+)') }}` | Return first regex match (empty string if no match). |
| `regex_findall(pattern)` | `{{ value \| regex_findall('\d+') }}` | Return list of all regex matches. |
| `regex_escape` | `{{ value \| regex_escape }}` | Escape regex special characters. |

##### Path Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `basename` | `{{ "/etc/nginx/nginx.conf" \| basename }}` → `nginx.conf` | Extract filename from path. |
| `dirname` | `{{ "/etc/nginx/nginx.conf" \| dirname }}` → `/etc/nginx` | Extract directory from path. |
| `splitext` | `{{ "file.tar.gz" \| splitext }}` → `["file.tar", ".gz"]` | Split filename and extension. |

##### Serialization Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `to_yaml` / `to_nice_yaml` | `{{ data \| to_yaml }}` | Serialize value to YAML string. |
| `from_yaml` | `{{ yaml_str \| from_yaml }}` | Parse YAML string into data structure. |
| `to_json` | `{{ data \| to_json }}` | Serialize value to compact JSON string. |
| `to_nice_json(indent=4)` | `{{ data \| to_nice_json }}` | Serialize value to indented JSON string. |
| `from_json` | `{{ json_str \| from_json }}` | Parse JSON string into data structure. |

##### Encoding & Hashing Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `b64encode` | `{{ "hello" \| b64encode }}` → `aGVsbG8=` | Base64 encode a string. |
| `b64decode` | `{{ "aGVsbG8=" \| b64decode }}` → `hello` | Base64 decode a string. |
| `hash(algo)` | `{{ "hello" \| hash("sha256") }}` | Hash string. Supported: `md5`, `sha1`, `sha256`, `sha512`. |

##### Logic & Type Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `ternary(true_val, false_val)` | `{{ enabled \| ternary("yes", "no") }}` | Return `true_val` if truthy, else `false_val`. |
| `mandatory(msg='')` | `{{ var \| mandatory }}` | Fail with error if value is nil/undefined. Optional custom message. |
| `type_debug` | `{{ var \| type_debug }}` | Return Go type name as string (debugging aid). |
| `bool` | `{{ "yes" \| bool }}` | Convert string to boolean. Recognizes `true/yes/1/on` and `false/no/0/off`. |

##### Collection Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `combine(dict)` | `{{ dict1 \| combine(dict2) }}` | Merge dictionaries. Later dict values override earlier ones. |
| `dict2items` | `{{ mydict \| dict2items }}` | Convert dict to list of `{key: k, value: v}` maps. |
| `items2dict(key_name='key', value_name='value')` | `{{ items \| items2dict }}` | Convert list of `{key, value}` maps back to dict. |
| `flatten(levels=1)` | `{{ nested \| flatten }}` | Flatten nested lists by specified depth. |
| `zip_longest(list)` | `{{ list1 \| zip_longest(list2) }}` | Zip lists, padding shorter with `nil`. |
| `product(list)` | `{{ list1 \| product(list2) }}` | Cartesian product of two lists. |
| `unique` | `{{ list \| unique }}` | Remove duplicate items from list. |
| `union(other)` | `{{ list1 \| union(list2) }}` | Set union of two lists. |
| `intersect(other)` | `{{ list1 \| intersect(list2) }}` | Set intersection of two lists. |
| `difference(other)` | `{{ list1 \| difference(list2) }}` | Items in first list not in second. |
| `symmetric_difference(other)` | `{{ list1 \| symmetric_difference(list2) }}` | Items in either list but not both. |
| `map_attribute(attr)` | `{{ list \| map_attribute("name") }}` | Extract attribute from each item in list. |

##### Date/Time Filters

| Filter | Usage | Description |
|--------|-------|-------------|
| `to_datetime(format='')` | `{{ date_str \| to_datetime }}` | Parse date string. Auto-detects RFC3339, `YYYY-MM-DD HH:MM:SS`, `YYYY-MM-DD`. Supports Python-style format codes. |
| `strftime(format)` | `{{ date \| strftime('%Y-%m-%d') }}` | Format date using Python-style format codes (`%Y`, `%m`, `%d`, `%H`, `%M`, `%S`, etc.). |

##### Filter Examples

```yaml
# Regex replacement
- name: Update config
  template:
    src: config.j2
    dest: /etc/myapp/config.conf
  # In config.j2: {{ listen_addr | regex_replace('^0\.0\.0\.0', '127.0.0.1') }}

# Base64 encoding
- name: Create auth file
  template:
    src: auth.j2
    dest: /etc/myapp/auth
  vars:
    credentials: "user:password"
  # In auth.j2: {{ credentials | b64encode }}

# Combining dictionaries
- name: Merge configs
  template:
    src: merged.j2
    dest: /etc/myapp/merged.conf
  vars:
    defaults:
      port: 8080
      host: localhost
    overrides:
      port: 9090
  # In merged.j2: {% set config = defaults | combine(overrides) %}
  #               port={{ config.port }}  {# outputs 9090 #}

# Shell-safe quoting
- name: Generate script
  template:
    src: script.j2
    dest: /usr/local/bin/run.sh
  vars:
    user_input: "hello 'world'"
  # In script.j2: echo {{ user_input | quote }}

# Hash verification
- name: Checksum file
  template:
    src: checksum.j2
    dest: /tmp/checksum.txt
  vars:
    content: "important data"
  # In checksum.j2: sha256={{ content | hash('sha256') }}

# Dict to items iteration
- name: Generate env file
  template:
    src: env.j2
    dest: /etc/myapp/.env
  vars:
    environment:
      DB_HOST: localhost
      DB_PORT: 5432
  # In env.j2:
  # {% for item in environment | dict2items %}
  # {{ item.key }}={{ item.value }}
  # {% endfor %}
```

#### Template Language Features

The template engine supports the full Jinja2 template language via the [gonja](https://github.com/aisbergg/gonja) library:

**Control Structures**:
```jinja2
{# Conditionals #}
{% if enabled and mode == "production" %}prod{% elif mode == "staging" %}staging{% else %}disabled{% endif %}
{% if "admin" in roles %}has_admin{% endif %}
{% if extra is defined %}defined{% endif %}
{% if missing is not defined %}undefined{% endif %}

{# For loops with loop variables #}
{% for item in items %}
{{ loop.index }}. {{ item }} (first={{ loop.first }}, last={{ loop.last }})
{% endfor %}

{# For loop over dict #}
{% for key, value in config %}
{{ key }}={{ value }}
{% endfor %}

{# For loop with condition #}
{% for n in numbers if n > 2 %}{{ n }}{% endfor %}

{# Set statement #}
{% set greeting = "Hello " ~ name %}
```

**Macros**:
```jinja2
{% macro render_item(name, value) %}{{ name }}={{ value }}{% endmacro %}
{{ render_item("host", hostname) }}
```

**Template Inheritance**:
```jinja2
{# base.j2 #}
header
{% block content %}default{% endblock %}
footer

{# child.j2 #}
{% extends "base.j2" %}
{% block content %}overridden content{% endblock %}
```

**Raw Blocks** (prevent template processing):
```jinja2
{% raw %}{{ this_is_not_rendered }}{% endraw %}
```

**Whitespace Control**:
```jinja2
{%- for item in items %}  {# strip before #}
  - {{ item }}
{%- endfor %}              {# strip before #}
```

**Includes**:
```jinja2
{% include 'child.j2' %}
```

### fetch

Fetches files from remote hosts to the controller. Runs entirely on the controller side.

```yaml
# Basic fetch (creates dest/hostname/src/path structure)
- name: Fetch remote config
  fetch:
    src: /etc/myapp/config.yaml
    dest: ./backups/

# Flat mode (no hostname/path structure)
- name: Fetch to specific file
  fetch:
    src: /etc/myapp/config.yaml
    dest: ./config-backup.yaml
    flat: true

# Don't fail if file missing
- name: Fetch optional file
  fetch:
    src: /var/log/myapp.log
    dest: ./logs/
    fail_on_missing: false
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `src` | required | Remote file path (directories not supported) |
| `dest` | required | Local destination path |
| `flat` | `false` | Skip hostname/path directory structure |
| `fail_on_missing` | `true` | Fail if remote file doesn't exist |
| `validate_checksum` | `true` | Verify checksum after download |

**Fetch Flow**:
1. Controller uses agent's `stat` module to get remote file checksum
2. If local file exists with matching checksum, skip (idempotent)
3. Controller downloads file via SCP
4. Controller verifies checksum matches

**Idempotency**: Compares SHA1 checksums; skips if local file matches remote.

### unarchive

Unpacks an archive after (optionally) copying it from the local machine. Supports `.tar`, `.tar.gz/.tgz`, `.tar.bz2/.tbz2`, `.tar.xz/.txz`, `.tar.zst`, and `.zip` formats.

```yaml
# Extract archive already on remote host
- name: Extract application
  unarchive:
    src: /tmp/app-v1.0.tar.gz
    dest: /opt/app
    remote_src: true

# Copy local archive to remote and extract
- name: Deploy package
  unarchive:
    src: ./dist/app.tar.gz
    dest: /opt/app

# Skip extraction if marker file exists
- name: Extract only if needed
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    creates: /opt/app/VERSION

# Extract with specific permissions
- name: Extract with permissions
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    mode: "0755"
    owner: appuser
    group: appgroup

# Extract excluding certain files
- name: Extract without logs
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    exclude:
      - "*.log"
      - "tmp/*"

# Extract only specific files
- name: Extract config only
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    include:
      - "config/*"
      - "README.md"

# Keep newer files in destination
- name: Update without overwriting newer
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    keep_newer: true

# List files in archive
- name: Extract and list files
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    list_files: true

# Use extra options for tar/unzip
- name: Extract with verbose
  unarchive:
    src: /tmp/app.tar.gz
    dest: /opt/app
    remote_src: true
    extra_opts:
      - "--verbose"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `src` | required | Path to archive. Local path if `remote_src=false`, remote path if `remote_src=true`. |
| `dest` | required | Remote directory where archive should be extracted. Must exist. |
| `remote_src` | `false` | If `true`, src is on remote system. If `false`, src is copied from controller. |
| `creates` | | Skip extraction if this path exists on remote. |
| `list_files` | `false` | Return list of files in the archive. |
| `exclude` | `[]` | List of patterns to exclude from extraction. Mutually exclusive with `include`. |
| `include` | `[]` | List of patterns to include (extract only matching). Mutually exclusive with `exclude`. |
| `keep_newer` | `false` | Do not overwrite files that are newer than those in the archive. |
| `extra_opts` | `[]` | Additional command-line options passed to tar/unzip. |
| `mode` | | Mode to apply to all extracted files (e.g., `"0755"`). |
| `owner` | | Owner for extracted files. |
| `group` | | Group for extracted files. |

**Returns**:
```json
{
  "changed": true,
  "dest": "/opt/app",
  "src": "/tmp/app.tar.gz",
  "handler": "tar",
  "files": ["file1.txt", "dir/file2.txt"],
  "msg": "archive extracted"
}
```

**Supported Formats**:
- `.tar` - Plain tar archive
- `.tar.gz`, `.tgz` - Gzip compressed tar
- `.tar.bz2`, `.tbz2` - Bzip2 compressed tar (requires `bzip2`)
- `.tar.xz`, `.txz` - XZ compressed tar (requires `xz`)
- `.tar.zst` - Zstandard compressed tar (requires `zstd`)
- `.zip` - ZIP archive (requires `unzip`)

**File Transfer Flow** (when `remote_src=false`):
1. Controller computes SHA1 checksum of local archive
2. Controller uploads to `/tmp/.dibra-unarchive-<hash>`
3. Agent verifies checksum matches
4. Agent extracts archive to destination
5. Agent applies mode/owner/group if specified

**Idempotency**:
- For tar: Uses `tar --diff` to compare archive contents with filesystem
- For zip: Checks if all files in archive exist in destination
- Use `creates` parameter for faster idempotency checks

### slurp

Reads a file from the remote host and returns its contents as base64-encoded data. This is a read-only module that never reports changes.

```yaml
# Read a file using src
- name: Read remote config
  slurp:
    src: /etc/myapp/config.yaml

# Read a file using path alias
- name: Read remote file
  slurp:
    path: /var/run/sshd.pid

# Read with variable path
- name: Read dynamic file
  slurp:
    src: "{{ config_dir }}/settings.yaml"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `src` | required | Path to the file on the remote system. Must be a file, not a directory. |
| `path` | | Alias for `src`. |

**Returns**:
```json
{
  "changed": false,
  "content": "SGVsbG8gV29ybGQK",
  "source": "/etc/myapp/config.yaml",
  "encoding": "base64"
}
```

**Error Messages**:
- File not found → `file not found: <path>`
- Permission denied → `file is not readable: <path>`
- Path is a directory → `source is a directory and must be a file: <path>`
- Other errors → `unable to slurp file: <path>: <error>`

**Notes**:
- Returns base64-encoded content; decode with `base64 -d` or equivalent
- Follows symlinks (uses `os.Stat`, not `os.Lstat`)
- Works with binary files, empty files, proc files, and unicode content
- Requires at least twice the RAM of the original file size

### stat

Internal module used by fetch. Gets file metadata and checksum.

```yaml
# Used internally - not typically called directly
- stat:
    path: /etc/myapp/config.yaml
    follow: true  # Follow symlinks
```

### find

Find files, directories, or links on remote hosts matching various criteria. This is a read-only module that never reports changes.

```yaml
# Find all .log files recursively
- name: Find log files
  find:
    paths:
      - /var/log
    patterns:
      - "*.log"
    recurse: true

# Find files older than 7 days
- name: Find old files
  find:
    paths:
      - /tmp
    age: 7d
    recurse: true

# Find large files with checksums
- name: Find large files
  find:
    paths:
      - /opt/app
    size: 100m
    get_checksum: true
    recurse: true

# Find files using regex patterns
- name: Find config files with regex
  find:
    paths:
      - /etc
    patterns:
      - ".*\\.conf$"
    use_regex: true
    recurse: true

# Find directories matching pattern
- name: Find cache directories
  find:
    paths:
      - /home
    file_type: directory
    patterns:
      - ".cache"
    recurse: true
    hidden: true

# Find files containing a pattern
- name: Find files with TODO
  find:
    paths:
      - /opt/app/src
    contains: "TODO"
    patterns:
      - "*.py"
    recurse: true

# Find with multiple criteria
- name: Find recent large logs
  find:
    paths:
      - /var/log
    patterns:
      - "*.log"
    age: "-7d"
    size: 1m
    recurse: true
    depth: 3
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `paths` | required | List of directories to search in. Aliases: `path`, `name`. |
| `patterns` | `["*"]` | Glob patterns (or regex if `use_regex=true`) to match filenames. Alias: `pattern`. |
| `excludes` | `[]` | Patterns to exclude from results. Alias: `exclude`. |
| `contains` | | Regex pattern to match against file contents (files only). |
| `read_whole_file` | `false` | Read entire file for `contains` matching (enables cross-line matches). |
| `file_type` | `file` | Type to find: `file`, `directory`, `link`, `any`. |
| `age` | | File age filter. Positive = older than, negative = newer than. Units: `s`, `m`, `h`, `d`, `w`. |
| `age_stamp` | `mtime` | Which timestamp to use for age: `mtime`, `ctime`, `atime`. |
| `size` | | File size filter. Positive = larger than, negative = smaller than. Units: `b`, `k`, `m`, `g`, `t`. |
| `recurse` | `false` | Search directories recursively. |
| `hidden` | `false` | Include hidden files/directories (starting with `.`). |
| `follow` | `false` | Follow symbolic links. |
| `get_checksum` | `false` | Compute file checksums (regular files only). |
| `checksum_algorithm` | `sha1` | Checksum algorithm: `md5`, `sha1`, `sha256`, `sha384`, `sha512`. |
| `use_regex` | `false` | Treat patterns/excludes as regex instead of glob. |
| `depth` | `0` | Maximum recursion depth (0 = unlimited). |
| `mode` | | File permission filter (octal, e.g., `"0644"`). |
| `exact_mode` | `true` | If true, match exact mode. If false, match minimum permissions. |
| `limit` | `0` | Maximum number of matches to return (0 = unlimited). |

**Returns**:
```json
{
  "changed": false,
  "msg": "All paths examined",
  "files": [
    {
      "path": "/var/log/syslog",
      "mode": "0644",
      "isdir": false,
      "isreg": true,
      "islnk": false,
      "uid": 0,
      "gid": 4,
      "size": 12345,
      "atime": 1700000000.0,
      "mtime": 1700000000.0,
      "ctime": 1700000000.0,
      "gr_name": "adm",
      "pw_name": "root",
      "checksum": "da39a3ee5e6b..."
    }
  ],
  "matched": 1,
  "examined": 10,
  "skipped_paths": {}
}
```

**Size Filter Behavior**:
- Size filter applies only to regular files, not directories
- When `file_type: "any"`, directories pass regardless of size filter
- When `file_type: "directory"`, size filter is not applied

**Idempotency**: Always returns `changed: false` (read-only module).

### tempfile

Creates temporary files and directories. Files/directories created by this module are accessible only by the creator.

```yaml
# Create a temporary file with defaults
- name: Create temp file
  tempfile:

# Create a temporary directory
- name: Create temp directory
  tempfile:
    state: directory

# Create temp file with custom prefix and suffix
- name: Create temp config file
  tempfile:
    prefix: myapp.
    suffix: .conf

# Create temp file in specific directory
- name: Create temp file in /var/tmp
  tempfile:
    path: /var/tmp
    prefix: myapp.
    suffix: .tmp
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `state` | `file` | `file` to create a temporary file, `directory` to create a temporary directory. |
| `path` | | Directory where the temp file/directory should be created. Defaults to system temp directory. |
| `prefix` | `ansible.` | Prefix for the generated file/directory name. |
| `suffix` | `""` | Suffix for the generated file/directory name. |

**Returns**:
```json
{
  "changed": true,
  "path": "/tmp/ansible.abc123",
  "state": "file"
}
```

**Notes**:
- Always returns `changed: true` since a new file/directory is created each time
- Created files have mode 0600, directories have mode 0700
- The `path` directory must exist; the module will fail if it doesn't
- Use the `file` module with `state: absent` to clean up temp files/directories

**Idempotency**: This module is NOT idempotent - it creates a new unique file/directory on each run.

### uri

Makes HTTP/HTTPS requests. Supports GET, POST, PUT, DELETE, etc.

```yaml
# Simple GET request
- name: Check API health
  uri:
    url: https://api.example.com/health

# POST with JSON body
- name: Create resource
  uri:
    url: https://api.example.com/items
    method: POST
    body: '{"name": "test", "value": 42}'
    body_format: json
    status_code:
      - 200
      - 201

# Download file
- name: Download artifact
  uri:
    url: https://releases.example.com/app-v1.0.tar.gz
    dest: /tmp/app.tar.gz

# Request with custom headers and auth
- name: Authenticated request
  uri:
    url: https://api.example.com/private
    headers:
      Authorization: "Bearer {{ token }}"
      Accept: "application/json"
    return_content: true

# Skip if file already exists
- name: Download only if needed
  uri:
    url: https://example.com/file.txt
    dest: /tmp/file.txt
    creates: /tmp/file.txt
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `url` | required | HTTP or HTTPS URL |
| `method` | `GET` | HTTP method (GET, POST, PUT, DELETE, PATCH, etc.) |
| `body` | | Request body content |
| `body_format` | `raw` | Body format: `raw`, `json`, `form-urlencoded`, `form-multipart` |
| `headers` | `{}` | Custom HTTP headers as key-value pairs |
| `status_code` | `[200]` | List of acceptable status codes |
| `timeout` | `30` | Request timeout in seconds |
| `return_content` | `false` | Include response body in output |
| `dest` | | Save response to file path |
| `creates` | | Skip request if this file exists |
| `url_username` | | Username for HTTP authentication |
| `url_password` | | Password for HTTP authentication |
| `force_basic_auth` | `false` | Send auth header immediately |
| `follow_redirects` | `safe` | `all`, `none`, `safe`, `urllib2`; `yes`/`no` are rejected |
| `validate_certs` | `true` | Verify SSL certificates |

**Changed Detection**:
- GET/HEAD requests: `changed: false`
- POST/PUT/PATCH/DELETE: `changed: true`
- Downloads to `dest`: `changed: true`

### cron

Manages cron.d and crontab entries.

```yaml
# Add a cron job
- name: Run backup daily
  cron:
    name: daily backup
    minute: "0"
    hour: "2"
    job: /usr/local/bin/backup.sh

# Use special time shortcut
- name: Run at reboot
  cron:
    name: startup task
    special_time: reboot
    job: /usr/local/bin/startup.sh

# Disable a job (comment it out)
- name: Temporarily disable job
  cron:
    name: daily backup
    minute: "0"
    hour: "2"
    job: /usr/local/bin/backup.sh
    disabled: true

# Set environment variable
- name: Set PATH in crontab
  cron:
    name: PATH
    env: true
    job: /usr/local/bin:/usr/bin

# Create cron.d file
- name: Create system cron job
  cron:
    name: system task
    minute: "30"
    hour: "*/4"
    user: root
    job: /usr/local/bin/system-task.sh
    cron_file: my-system-task

# Remove a job
- name: Remove old job
  cron:
    name: old task
    state: absent
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | required | Description of crontab entry (used as identifier) |
| `job` | | Command to execute (required when state=present) |
| `state` | `present` | `present` or `absent` |
| `minute` | `*` | Minute (0-59, *, */2, etc.) |
| `hour` | `*` | Hour (0-23, *, */2, etc.) |
| `day` | `*` | Day of month (1-31, *, */2, etc.) |
| `month` | `*` | Month (1-12, JAN-DEC, *, etc.) |
| `weekday` | `*` | Day of week (0-6, SUN-SAT, *, etc.) |
| `special_time` | | `reboot`, `yearly`, `annually`, `monthly`, `weekly`, `daily`, `hourly` |
| `disabled` | `false` | Comment out the job |
| `user` | current | User whose crontab to modify |
| `backup` | `false` | Create backup before changes |
| `cron_file` | | Create file in /etc/cron.d/ instead of user crontab |
| `env` | `false` | Manage environment variable instead of job |
| `insertafter` | | Insert env var after another (only with env=true) |
| `insertbefore` | | Insert env var before another (only with env=true) |

**Special Features**:
- Jobs are marked with `#Ansible: <name>` comment for tracking
- `special_time` cannot be combined with minute/hour/day/month/weekday
- When using `cron_file`, user field is included in the job line
- Empty cron.d files are automatically removed

### systemd_service

Manages systemd units (services, timers, etc.). Alias: `systemd`.

```yaml
# Start a service
- name: Start nginx
  systemd_service:
    name: nginx
    state: started

# Stop and disable a service
- name: Stop and disable service
  systemd_service:
    name: nginx
    state: stopped
    enabled: false

# Restart with daemon-reload
- name: Restart after config change
  systemd_service:
    name: myapp
    state: restarted
    daemon_reload: true

# Enable service at boot
- name: Enable nginx on boot
  systemd_service:
    name: nginx
    enabled: true

# Reload service configuration
- name: Reload nginx
  systemd_service:
    name: nginx
    state: reloaded

# Mask a service (prevent starting)
- name: Mask dangerous service
  systemd_service:
    name: insecure-service
    masked: true

# Unmask and enable
- name: Unmask and enable
  systemd_service:
    name: myservice
    masked: false
    enabled: true

# Just daemon-reload (no service name)
- name: Reload systemd daemon
  systemd_service:
    daemon_reload: true

# Timer unit
- name: Start timer
  systemd_service:
    name: backup.timer
    state: started
    enabled: true
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | | Name of the unit (adds `.service` if no extension) |
| `state` | | `started`, `stopped`, `restarted`, `reloaded` |
| `enabled` | | Whether the unit starts on boot (true/false) |
| `masked` | | Whether the unit is masked (true/false) |
| `daemon_reload` | `false` | Run `daemon-reload` before other operations |
| `daemon_reexec` | `false` | Run `daemon-reexec` before other operations |
| `scope` | `system` | `system`, `user`, or `global` |
| `no_block` | `false` | Don't wait for operation to complete |
| `force` | `false` | Force enable even if symlinks exist |

**State Behaviors**:
- `started`: Idempotent - starts only if not running
- `stopped`: Idempotent - stops only if running
- `restarted`: Always restarts (always reports changed)
- `reloaded`: Starts if stopped, then reloads (always reports changed)

**Order of Operations**: enable/disable → mask/unmask → state change

**Idempotency**: `started`/`stopped`/`enabled`/`masked` are idempotent.

### service

Generic service management module. Works across different init systems (systemd, sysvinit). Provides a simpler interface than `systemd_service` for common operations.

```yaml
# Start a service
- name: Start nginx
  service:
    name: nginx
    state: started

# Stop a service
- name: Stop nginx
  service:
    name: nginx
    state: stopped

# Restart a service
- name: Restart nginx
  service:
    name: nginx
    state: restarted

# Enable service at boot
- name: Enable nginx on boot
  service:
    name: nginx
    enabled: true

# Start and enable in one task
- name: Start and enable nginx
  service:
    name: nginx
    state: started
    enabled: true

# Restart with sleep between stop/start
- name: Restart with delay
  service:
    name: myapp
    state: restarted
    sleep: 2

# Use pattern to find service by process name
- name: Start service matching pattern
  service:
    name: myapp
    pattern: myapp_daemon
    state: started

# Force specific init system
- name: Start using systemd
  service:
    name: nginx
    state: started
    use: systemd
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | required | Name of the service |
| `state` | | `started`, `stopped`, `restarted`, `reloaded` |
| `enabled` | | Whether the service starts on boot (true/false) |
| `arguments` | `""` | Additional arguments for init scripts (ignored on systemd) |
| `pattern` | | Process name pattern to check if service is running |
| `sleep` | `0` | Seconds to wait between stop/start during restart |
| `use` | `auto` | Force specific service manager: `systemd`, `sysvinit`, or `auto` |

**State Behaviors**:
- `started`: Idempotent - starts only if not running
- `stopped`: Idempotent - stops only if running
- `restarted`: Always restarts (always reports changed)
- `reloaded`: Starts if stopped, then reloads (always reports changed)

**Idempotency**: `started`/`stopped`/`enabled` are idempotent.

**Differences from systemd_service**:
- Simpler interface without daemon_reload, masked, scope options
- Supports `pattern` for finding services by process name
- Supports `sleep` between stop/start during restart
- Supports `arguments` for sysvinit scripts
- Auto-detects init system (defaults to systemd if available)

### service_facts

Gathers and returns service state information as facts. Takes no parameters.

```yaml
# Gather all service facts
- name: Gather service facts
  service_facts:
```

**Returns**: A `services` map where each key is the service name and value contains:

| Field | Description |
|-------|-------------|
| `name` | Name of the service (e.g., `ssh.service`) |
| `state` | Current state: `running`, `stopped`, `failed`, or `unknown` |
| `status` | Boot status: `enabled`, `disabled`, `static`, `indirect`, `masked`, or `unknown` |
| `source` | Init system: `systemd` or `sysv` |

**Example Response**:
```json
{
  "changed": false,
  "services": {
    "ssh.service": {
      "name": "ssh.service",
      "state": "running",
      "status": "enabled",
      "source": "systemd"
    },
    "cron.service": {
      "name": "cron.service",
      "state": "running",
      "status": "enabled",
      "source": "systemd"
    }
  }
}
```

**Features**:
- Detects systemd services via `systemctl list-units` and `systemctl list-unit-files`
- Detects sysvinit services via `service --status-all`
- Always returns `changed: false` (read-only operation)
- Idempotent by design

### gather_facts

Collects system facts and exposes them as `ansible_facts` plus top-level `ansible_*` variables for use in later tasks.

```yaml
# Gather default facts (min + platform + env + date/time + network + hardware, etc.)
- name: Gather facts
  gather_facts:

# Gather only network facts
- name: Gather network facts only
  gather_facts:
    gather_subset:
      - "!all"
      - network

# Limit to date/time facts
- name: Gather date_time facts
  gather_facts:
    filter: ansible_date_time

# Read local facts from a custom directory
- name: Gather local facts
  gather_facts:
    gather_subset: ["!all", "local"]
    fact_path: /tmp/custom_facts.d
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `gather_subset` | `all` | Subset list or comma-separated string. Supports `min`, `network`, `hardware`, `virtual`, `service_mgr`, `pkg_mgr`, `env`, `date_time`, `local`, plus negation (e.g. `!all`, `!hardware`). |
| `filter` | | Shell-style glob or list of globs used to filter fact keys (`ansible_date_time`, `*env*`). |
| `fact_path` | `/etc/ansible/facts.d` | Directory to read `.fact` files when `local` subset is enabled. |

**Returns**:
- `ansible_facts`: map containing gathered facts keyed without the `ansible_` prefix (for example `ansible_facts.date_time`).
- Top-level `ansible_*` variables injected into runtime vars for use in templates (`ansible_user_id`, `ansible_hostname`, `ansible_date_time`, etc.).

**Example Response**:
```json
{
  "changed": false,
  "ansible_facts": {
    "user_id": "root",
    "hostname": "testhost",
    "date_time": {
      "year": 2026,
      "month": 3,
      "day": 1,
      "hour": 12,
      "minute": 30,
      "second": 10,
      "epoch": 1762029010,
      "iso8601": "2026-03-01T12:30:10Z",
      "timezone": "+0000"
    }
  }
}
```

**Notes**:
- Returns `changed: false` (read-only).
- Invalid subsets fail the task with a clear error.

### group

Manages groups on the system.

```yaml
# Create a group
- name: Create developers group
  group:
    name: developers
    state: present

# Create group with specific GID
- name: Create group with GID
  group:
    name: mygroup
    gid: 5000
    state: present

# Create system group (GID < 1000)
- name: Create system group
  group:
    name: myservice
    system: true
    state: present

# Create group with non-unique GID
- name: Create group sharing GID
  group:
    name: altgroup
    gid: 5000
    non_unique: true
    state: present

# Remove a group
- name: Remove old group
  group:
    name: oldgroup
    state: absent

# Force remove group (even if primary for a user)
- name: Force remove group
  group:
    name: testgroup
    state: absent
    force: true
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | required | Name of the group to manage. |
| `state` | `present` | `present` to create/modify, `absent` to remove. |
| `gid` | | Optional group ID to assign. |
| `system` | `false` | Create a system group (GID < 1000). |
| `local` | `false` | Use local group commands (lgroupadd/lgroupmod/lgroupdel). |
| `non_unique` | `false` | Allow non-unique GID. Requires `gid` to be set. |
| `force` | `false` | Force deletion even if group is a user's primary group. |

**Returns**:
```json
{
  "changed": true,
  "name": "developers",
  "gid": 5000,
  "state": "present",
  "system": false
}
```

**Idempotency**:
- Creation: Checks if group exists via `getent group` before creating.
- Modification: Only modifies if requested GID differs from current.
- Deletion: Returns `changed: false` if group doesn't exist.

**Notes**:
- `force` and `local` are mutually exclusive.
- `non_unique` requires `gid` to be specified.
- System groups typically have GID < 1000.

### iptables

Manages iptables firewall rules. Supports IPv4 (iptables) and IPv6 (ip6tables), all tables (filter, nat, mangle, raw, security), chain management, policies, and rule CRUD operations with idempotency via the `-C` (check) flag.

```yaml
# Basic rule - allow SSH
- name: Allow SSH
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "22"
    jump: ACCEPT

# Drop traffic from malicious IP
- name: Block bad IP
  iptables:
    chain: INPUT
    source: 192.168.100.50
    jump: DROP

# Rule with comment
- name: Allow HTTP with comment
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "80"
    jump: ACCEPT
    comment: "Allow HTTP traffic"

# Connection tracking (stateful firewall)
- name: Allow established connections
  iptables:
    chain: INPUT
    ctstate:
      - ESTABLISHED
      - RELATED
    jump: ACCEPT

# Multiple destination ports (multiport)
- name: Allow web ports
  iptables:
    chain: INPUT
    protocol: tcp
    destination_ports:
      - "80"
      - "443"
      - "8080"
    jump: ACCEPT

# NAT - DNAT port forwarding
- name: Forward port 8080 to internal server
  iptables:
    table: nat
    chain: PREROUTING
    protocol: tcp
    destination_port: "8080"
    jump: DNAT
    to_destination: "192.168.1.100:80"

# NAT - Masquerade (source NAT)
- name: Masquerade outgoing traffic
  iptables:
    table: nat
    chain: POSTROUTING
    source: 192.168.0.0/24
    out_interface: eth0
    jump: MASQUERADE

# Set chain policy
- name: Set INPUT policy to DROP
  iptables:
    chain: INPUT
    policy: DROP

# Flush chain
- name: Flush INPUT chain
  iptables:
    chain: INPUT
    flush: true

# Create custom chain
- name: Create LOGDROP chain
  iptables:
    chain: LOGDROP
    chain_management: true
    state: present

# Delete custom chain
- name: Delete custom chain
  iptables:
    chain: LOGDROP
    chain_management: true
    state: absent

# Insert rule at specific position
- name: Insert SSH rule at top
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "22"
    jump: ACCEPT
    action: insert
    rule_num: 1

# Remove a rule
- name: Remove old rule
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "8080"
    jump: ACCEPT
    state: absent

# Rate limiting
- name: Rate limit SSH connections
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "22"
    limit: 3/minute
    limit_burst: "5"
    jump: ACCEPT

# Logging
- name: Log dropped packets
  iptables:
    chain: INPUT
    jump: LOG
    log_prefix: "DROPPED: "
    log_level: warning

# REJECT with ICMP message
- name: Reject with port unreachable
  iptables:
    chain: INPUT
    protocol: tcp
    destination_port: "23"
    jump: REJECT
    reject_with: icmp-port-unreachable

# ICMP rules
- name: Allow ping
  iptables:
    chain: INPUT
    protocol: icmp
    icmp_type: echo-request
    jump: ACCEPT

# Interface matching
- name: Allow all traffic on loopback
  iptables:
    chain: INPUT
    in_interface: lo
    jump: ACCEPT

# Source negation
- name: Allow from all except specific IP
  iptables:
    chain: INPUT
    source: "!192.168.1.100"
    protocol: tcp
    destination_port: "80"
    jump: ACCEPT

# Goto custom chain
- name: Goto LOGDROP chain
  iptables:
    chain: INPUT
    source: 10.0.0.0/8
    goto: LOGDROP
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `table` | `filter` | Table to operate on: `filter`, `nat`, `mangle`, `raw`, `security`. |
| `chain` | required | Chain to operate on (INPUT, OUTPUT, FORWARD, PREROUTING, POSTROUTING, or custom). |
| `state` | `present` | `present` to add rule, `absent` to remove. |
| `action` | `append` | `append` or `insert`. |
| `rule_num` | | Insert position (only with `action=insert`). |
| `protocol` | | Protocol: `tcp`, `udp`, `icmp`, `all`, etc. |
| `source` | | Source address/network. Prefix with `!` to negate. |
| `destination` | | Destination address/network. Prefix with `!` to negate. |
| `source_port` | | Source port or range (e.g., `1024:65535`). |
| `destination_port` | | Destination port or range. |
| `destination_ports` | | Multiple ports (uses multiport extension). |
| `in_interface` | | Input interface (e.g., `eth0`, `lo`). |
| `out_interface` | | Output interface. |
| `jump` | | Target: `ACCEPT`, `DROP`, `REJECT`, `LOG`, `DNAT`, `SNAT`, `MASQUERADE`, etc. |
| `goto` | | Goto user-defined chain (instead of jump). |
| `ctstate` | | Connection tracking states: `NEW`, `ESTABLISHED`, `RELATED`, `INVALID`. |
| `comment` | | Comment for the rule (uses comment extension). |
| `match` | | Explicit match extensions to load. |
| `icmp_type` | | ICMP type (e.g., `echo-request`). |
| `limit` | | Rate limit (e.g., `5/second`, `100/minute`). |
| `limit_burst` | | Burst limit for rate limiting. |
| `log_prefix` | | Prefix for LOG messages. |
| `log_level` | | Log level (e.g., `warning`, `info`). |
| `reject_with` | | ICMP type for REJECT (e.g., `icmp-port-unreachable`). |
| `to_destination` | | DNAT destination (e.g., `192.168.1.1:80`). |
| `to_source` | | SNAT source address. |
| `to_ports` | | Port translation for NAT. |
| `flush` | `false` | Flush all rules from chain (or entire table if no chain). |
| `policy` | | Set chain policy: `ACCEPT`, `DROP`, `QUEUE`, `RETURN`. |
| `chain_management` | `false` | Create/delete custom chains. |
| `ip_version` | `ipv4` | `ipv4`, `ipv6`, or `both`. |
| `wait` | `0` | Seconds to wait for xtables lock. |

**Idempotency**: Uses `iptables -C` (check) to verify if rule exists before adding/removing.

**Safe Deployment Pattern**:
1. Keep default policy as ACCEPT (prevents lockout on flush)
2. Add permissive rules (SSH, established connections) first
3. Add restrictive rules (DROP) last
4. Use explicit DROP rule at end instead of DROP policy

### iptables_state

Saves iptables state to a file or restores from a file. Uses `iptables-save` and `iptables-restore` internally. Supports saving/restoring specific tables and idempotent operations.

```yaml
# Save current iptables state to a file
- name: Save iptables rules
  iptables_state:
    path: /etc/iptables/rules.v4
    state: saved

# Save only filter table
- name: Save filter table
  iptables_state:
    path: /etc/iptables/filter.rules
    state: saved
    table: filter

# Restore iptables state from a file
- name: Restore iptables rules
  iptables_state:
    path: /etc/iptables/rules.v4
    state: restored

# Restore without flushing existing rules
- name: Append rules from file
  iptables_state:
    path: /etc/iptables/extra.rules
    state: restored
    noflush: true

# Restore specific table from file
- name: Restore only nat table
  iptables_state:
    path: /etc/iptables/rules.v4
    state: restored
    table: nat

# Save with counters (non-idempotent)
- name: Save with packet counters
  iptables_state:
    path: /tmp/iptables-counters.save
    state: saved
    counters: true

# IPv6 rules
- name: Save IPv6 rules
  iptables_state:
    path: /etc/iptables/rules.v6
    state: saved
    ip_version: ipv6

# Get current state without saving (check_mode pattern)
- name: Get current state
  iptables_state:
    path: /tmp/current.rules
    state: saved
  register: iptables_state
  check_mode: true
  changed_when: false
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `path` | required | Path to save or restore iptables state file. |
| `state` | required | `saved` to save state to file, `restored` to restore from file. |
| `table` | | Specific table to save/restore: `filter`, `nat`, `mangle`, `raw`, `security`. If not specified, all tables are used. |
| `counters` | `false` | Save/restore packet and byte counters. When true, module is not idempotent. |
| `noflush` | `false` | For `state=restored`: don't flush existing rules before restoring. Policies are still updated. |
| `ip_version` | `ipv4` | `ipv4` or `ipv6`. |
| `wait` | `0` | Seconds to wait for xtables lock. |
| `modprobe` | | Path to modprobe program for loading kernel modules. |

**Returns**:
```json
{
  "changed": true,
  "applied": true,
  "initial_state": ["*filter", ":INPUT ACCEPT [0:0]", ...],
  "saved": ["*filter", ":INPUT ACCEPT [0:0]", ...],
  "restored": ["*filter", ":INPUT DROP [0:0]", ...],
  "tables": {
    "filter": [":INPUT ACCEPT", ":FORWARD ACCEPT", "-A INPUT -j ACCEPT"],
    "nat": [":PREROUTING ACCEPT", ":POSTROUTING ACCEPT"]
  }
}
```

**Idempotency**:
- For `state=saved`: Compares file content with current state (excluding timestamps/counters)
- For `state=restored`: Compares current state with file content for specified tables
- Returns `changed: false` if states already match

**Notes**:
- Requires `iptables-save` and `iptables-restore` (or `ip6tables-save`/`ip6tables-restore` for IPv6)
- Saved files have mode 0600 for security
- When restoring a partial file (only some tables), only those tables are compared/restored
- Use `table` parameter to work with specific tables without affecting others

### reboot

Reboots a machine, waits for it to go down, come back up, and respond to commands. The reboot operation is handled by the controller (not the agent) since it requires reconnecting after the reboot.

```yaml
# Unconditionally reboot with all defaults
- name: Reboot the machine
  reboot:

# Reboot a slow machine that might have lots of updates to apply
- name: Reboot with extended timeout
  reboot:
    reboot_timeout: 3600

# Reboot with custom message
- name: Reboot with message
  reboot:
    msg: "Rebooting for kernel update"

# Reboot with delays
- name: Reboot with delays
  reboot:
    pre_reboot_delay: 5
    post_reboot_delay: 30

# Reboot with custom test command
- name: Reboot and verify with uptime
  reboot:
    test_command: uptime

# Reboot with custom boot time command
- name: Reboot with custom boot time check
  reboot:
    boot_time_command: "uptime | cut -d ' ' -f 5"

# Reboot with custom search paths for shutdown command
- name: Reboot with molly-guard
  reboot:
    search_paths:
      - /lib/molly-guard
      - /sbin
      - /usr/sbin

# Reboot using custom command
- name: Reboot with systemctl
  reboot:
    reboot_command: "systemctl reboot"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `pre_reboot_delay` | `0` | Seconds to wait before reboot. On Linux, this is passed to the shutdown command. |
| `post_reboot_delay` | `0` | Seconds to wait after the reboot command before attempting to validate the system rebooted. |
| `reboot_timeout` | `600` | Maximum seconds to wait for machine to reboot and respond to a test command. |
| `connect_timeout` | `30` | Maximum seconds to wait for a successful SSH connection before retrying. |
| `test_command` | `whoami` | Command to run on the rebooted host to determine the machine is ready for further tasks. |
| `msg` | `Reboot initiated by dibra` | Message to display to users before reboot. |
| `search_paths` | `["/sbin", "/bin", "/usr/sbin", "/usr/bin", "/usr/local/sbin"]` | Paths to search on the remote machine for the `shutdown` command. |
| `boot_time_command` | `cat /proc/sys/kernel/random/boot_id` | Command to run that returns a unique string indicating the last time the system was booted. |
| `reboot_command` | `[determined based on target OS]` | Custom command to run that reboots the system. If set, ignores `pre_reboot_delay`, `msg`, and `search_paths`. |

**Returns**:
```json
{
  "changed": true,
  "rebooted": true,
  "elapsed": 45,
  "msg": "system rebooted successfully (elapsed: 45s)"
}
```

**Reboot Flow**:
1. Controller gets current boot time from target using `boot_time_command`
2. If `pre_reboot_delay` > 0, waits before issuing reboot
3. Controller searches `search_paths` for `shutdown` or `reboot` command (unless `reboot_command` is specified)
4. Controller issues reboot command via SSH
5. SSH connection is closed (will be dropped by reboot anyway)
6. If `post_reboot_delay` > 0, waits before checking
7. Controller retries SSH connection until successful or timeout
8. Controller verifies boot time has changed (new boot_id)
9. Controller runs `test_command` to verify system is ready

**Idempotency**: This module is NOT idempotent - it always reboots the system.

**Notes**:
- Cannot be used with containers (containers cannot be rebooted like VMs)
- The boot_time_command must return a consistent value that changes on reboot
- Use `reboot_command` for non-standard systems (e.g., `launchctl reboot userspace` on macOS)
- Connection timeout retries with exponential backoff

## Playbook Format

```yaml
vars:
  app_name: "myapp"
vars_files:
  - defaults.yml
vars_merge: replace   # or "merge"

hosts:
  - name: webserver1
    host: 192.168.1.100
    port: 22
    user: deploy
    password: "ssh-password"      # Or use ssh_key_path
    # ssh_key_path: ~/.ssh/id_rsa
    become: true                  # Use sudo
    become_password: "sudo-password"
    groups:
      - web
      - prod

tasks:
  - name: Task description
    vars:
      task_key: value
    <module>:
      <param>: "{{ app_name }}"
```

## External Inventory

Dibra supports Ansible-compatible YAML inventory files, allowing you to define hosts, groups, and variables separately from the playbook. Use the `-i` / `--inventory` flag or the `inventory:` key in the playbook.

### Usage

```bash
# CLI flag
dibra -config playbook.yaml -i inventory.yaml

# Or reference in playbook
# inventory: inventory.yaml
```

### YAML Inventory Format

```yaml
all:
  vars:
    env: production
  hosts:
    standalone_host:
      host: 10.0.0.99
  children:
    webservers:
      hosts:
        web1:
          host: 192.168.1.10
          port: 22
          user: deploy
          ssh_pass: secret
          become: true
          become_password: sudo_pass
        web2:
          host: 192.168.1.20
          ssh_private_key_file: ~/.ssh/id_rsa
      vars:
        http_port: 80
    dbservers:
      hosts:
        db1:
          host: 192.168.2.10
      vars:
        db_port: 5432
    production:
      children:
        webservers:
        dbservers:
      vars:
        deploy_env: prod
```

### Top-level groups without `all:` wrapper

Groups can be defined at the top level without wrapping in `all:`. An implicit `all` group is created automatically:

```yaml
webservers:
  hosts:
    web1:
      host: 10.0.0.1
  vars:
    role: web
dbservers:
  hosts:
    db1:
      host: 10.0.1.1
```

### Connection Variable Mapping

| Ansible Variable | Dibra Host Field |
|-----------------|-----------------|
| `host` | `host` (default: hostname) |
| `port` | `port` (default: 22) |
| `user` | `user` |
| `ssh_pass` | `password` |
| `ssh_private_key_file` | `ssh_key_path` |
| `become` | `become` |
| `become_password` | `become_password` |

### Variable Precedence (Low → High)

1. `all` group vars
2. Parent group vars (alphabetical for same depth)
3. Child group vars (alphabetical for same depth)
4. Host inline vars

### Implicit Groups

- **`all`**: Contains every host (created automatically if not defined)
- **`ungrouped`**: Contains hosts not in any named group

### External var files

`group_vars/` and `host_vars/` directories are resolved relative to the **inventory file** location (not the playbook).

### Playbook with inventory reference

```yaml
inventory: inventory.yaml

tasks:
  - name: Deploy
    copy:
      content: "hello"
      dest: /tmp/hello.txt
```

### Error behavior

- If both `-i` flag and playbook `hosts:` are present, dibra errors with a clear message
- If inventory file is not found, dibra errors
- Circular group references are detected and reported

## Handlers

Handlers are tasks that run only after a changed task notifies them. Define
handlers at play level and use `notify` with one name/topic or a list:

```yaml
vars:
  service_name: caddy

tasks:
  - name: Install Caddy configuration
    copy:
      src: Caddyfile
      dest: /etc/caddy/Caddyfile
    notify:
      - restart {{ service_name }}
      - reload web configuration

  - name: Apply pending restarts before health checks
    meta: flush_handlers

handlers:
  - name: restart {{ service_name }}
    systemd_service:
      name: "{{ service_name }}"
      state: restarted

  - name: reload caddy
    listen: reload web configuration
    systemd_service:
      name: caddy
      state: reloaded
```

A typical web-server layout keeps start/enable as tasks and restart/reload as handlers:

```yaml
tasks:
  - name: Install nginx
    apt:
      name: nginx
      state: present

  - name: Start and enable nginx
    systemd_service:
      name: nginx
      state: started
      enabled: true

  - name: Deploy nginx configuration
    template:
      src: nginx.conf.j2
      dest: /etc/nginx/nginx.conf
    notify:
      - Validate nginx config
      - Reload nginx

  - name: Apply config before health checks
    meta: flush_handlers

  - name: Verify nginx is serving
    uri:
      url: http://127.0.0.1/
      status_code:
        - 200

handlers:
  - name: Validate nginx config
    command:
      cmd: nginx -t
    changed_when: false

  - name: Reload nginx
    systemd_service:
      name: nginx
      state: reloaded
```

Handler behavior:

- Notifications are queued only when the task's effective result is changed.
  `changed_when` overrides the module's `changed` value. Notify names are
  case-sensitive. For a loop, if **any** iteration is changed, **every**
  loop item's notify targets are queued, not only the changed items.
- A handler runs once per flush cycle even when notified repeatedly. Handlers
  run in definition order, not notification order. A changed handler may
  `notify` another handler; that target runs in the same flush if it has not
  already run.
- `listen` accepts one or more non-templated topics. A topic runs every
  listening handler. Duplicate handler names use the last definition.
  Handlers may also use `when`; a false condition skips that handler without
  failing the flush.
- Pending handlers run automatically after normal tasks. `meta:
  flush_handlers` runs them immediately; a later change can notify them again.
  `meta: end_host` and `meta: end_play` skip remaining tasks and do **not**
  flush pending handlers. See `meta` under Modules for the full action list.
- Handler arguments render from the current per-host variable context.
  Handler names are templated before normal tasks start and therefore cannot
  use variables registered by those tasks. `listen` topics are never
  templated.
- Failed hosts do not flush handlers by default. Set play-level
  `force_handlers: true` or pass `--force-handlers` to run already-notified
  handlers after a failure.
- Handlers may run controller primitives (`debug`, `fail`, `assert`,
  `set_fact`, `include_vars`, `pause`, `meta: noop`). `meta: flush_handlers`
  cannot be used as a handler; other `meta` actions from a handler fail with
  `meta actions cannot be used from a handler`.
- Keep `state: started` / `enabled: true` as regular tasks so a later run
  still heals a crashed service. Use handlers for `restarted` / `reloaded`
  after a package or config change.
- Static `import_tasks` entries under `handlers` expand into individually
  notifiable handlers. A named dynamic `include_tasks` handler loads and runs
  its tasks when notified.
- Unknown notification targets produce a warning. Roles and handler insertion
  through roles are not supported yet.

## when

Conditionally executes a task. The condition is evaluated against the same variable context used for templates (`vars`, `hostvars`, `inventory_hostname`, `group_names`, registered results, etc.). If the condition resolves to false, the task is skipped. If the condition cannot be evaluated, the task fails.

**Syntax**:
- String expression using Jinja-style syntax (`==`, `!=`, `>`, `>=`, `<`, `<=`, `and`, `or`, `not`, `in`, `is defined`, filters like `default`, `length`)
- Boolean literal (`true`/`false`)
- Number (`0` is false, non-zero is true)
- List of conditions (all must be true)

```yaml
- name: Run only on web hosts
  copy:
    content: "web"
    dest: /tmp/web.txt
  when: '"web" in group_names'

- name: Multiple conditions (AND)
  copy:
    content: "ready"
    dest: /tmp/ready.txt
  when:
    - app.enabled
    - (app.port | int) > 1024

- name: Use filters
  copy:
    content: "fallback"
    dest: /tmp/fallback.txt
  when: (missing_value | default("fallback")) == "fallback"

- name: Boolean literal
  ping:
  when: true
```

**Behavior**:
- Skipped tasks report `skipped: true`, `changed: false`, and `msg: "when condition false"` in register results.
- `include_tasks`: if the `when` condition is false, no tasks are included.
- `import_tasks`: parent `when` conditions are merged with imported task conditions (logical AND).

## import_tasks

Imports a list of tasks from another YAML file, inserting them into the current playbook at parse time (static include). This is a controller-side directive, not a remote module.

```yaml
# Free-form syntax
- name: Import common tasks
  import_tasks: common/setup.yaml

# Explicit file parameter
- name: Import with file param
  import_tasks:
    file: common/setup.yaml

# With vars (inherited by imported tasks as defaults)
- name: Import with variables
  vars:
    app_port: 8080
  import_tasks: app/deploy.yaml

# Templated path (play vars + extra vars available)
- name: Import dynamic path
  import_tasks: "{{ tasks_dir }}/setup.yaml"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `file` | required | Path to the YAML file containing tasks. Relative paths resolve relative to the file containing the directive. |

**Imported file format**: The file must contain a YAML list of tasks at the root level:
```yaml
- name: First task
  copy:
    content: "hello"
    dest: /tmp/hello.txt

- name: Second task
  ping:
```

**Key behaviors**:
- **Static include**: Tasks are expanded at parse time, before execution begins. The `import_tasks` directive is replaced by the imported tasks in the flat task list.
- **Relative paths**: Resolved relative to the directory of the file containing the `import_tasks` directive (not the top-level playbook). This enables nested imports to reference sibling files correctly.
- **Nested imports**: Imported files can themselves contain `import_tasks` directives (up to 50 levels deep).
- **Circular detection**: Circular imports are detected and reported with the full import chain.
- **Vars inheritance**: `vars` on the `import_tasks` directive are inherited by all imported tasks as defaults. The imported task's own `vars` take precedence over inherited vars.
- **Templated paths**: File paths support `{{ }}` template interpolation using play-level vars, vars_files, and extra vars. Host-specific variables are NOT available (expansion happens once, before the host loop).
- **Same file twice**: The same file can be imported multiple times in different places.
- **Absolute paths**: Absolute file paths are also supported.

**Error cases**:
- Missing file path → `file path is required`
- File not found → `failed to load`
- Circular import → `circular import detected: a.yaml -> b.yaml -> a.yaml`
- Max depth exceeded → `maximum nesting depth (50) exceeded`

## include_tasks

Includes a list of tasks from another YAML file at runtime (dynamic include). Unlike `import_tasks` which expands at parse time, `include_tasks` expands during execution, giving it access to the full variable context including host vars and task vars.

```yaml
# Free-form syntax
- name: Include common tasks
  include_tasks: common/setup.yaml

# Explicit file parameter
- name: Include with file param
  include_tasks:
    file: common/setup.yaml

# With vars (inherited by included tasks as defaults)
- name: Include with variables
  vars:
    app_port: 8080
  include_tasks: app/deploy.yaml

# Templated path (all vars available including host vars)
- name: Include dynamic path
  include_tasks: "{{ task_file }}"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `file` | required | Path to the YAML file containing tasks. Relative paths resolve relative to the file containing the directive. |

**Key behaviors**:
- **Dynamic include**: Tasks are expanded at runtime during the task execution loop. This means all variable sources (host vars, task vars, extra vars) are available for path templating.
- **Relative paths**: Resolved relative to the directory of the file containing the `include_tasks` directive. Nested includes correctly resolve relative to their own file's directory.
- **Nested includes**: Included files can contain both `include_tasks` and `import_tasks` directives.
- **Vars inheritance**: `vars` on the `include_tasks` directive are inherited by all included tasks as defaults. The included task's own `vars` take precedence.
- **Templated paths**: File paths support `{{ }}` template interpolation using the full variable context (host vars, play vars, task vars, extra vars).
- **Same file twice**: The same file can be included multiple times in different places.
- **Absolute paths**: Absolute file paths are also supported.
- **Execution order**: Included tasks are inserted immediately after the include directive and executed before subsequent tasks.

**Differences from import_tasks**:

| Feature | `import_tasks` | `include_tasks` |
|---------|---------------|-----------------|
| Expansion time | Parse time (static) | Runtime (dynamic) |
| Variable context for path | Play vars + extra vars only | Full context (host vars, task vars, etc.) |
| Circular detection | Yes (with import chain) | No (handled naturally by execution) |

**Error cases**:
- Missing file path → `file path is required`
- File not found → `failed to read`
- Parse error → `failed to parse`

## Loops

Dibra supports task loops using `loop`, `with_items`, `with_list`, `with_dict`, and `with_sequence`. Looping runs the task once per item and can be combined with `when`, `register`, and `include_tasks`.

### Basic loop forms

```yaml
- name: Loop over a list
  copy:
    content: "{{ item }}"
    dest: "/tmp/dibra-loop-{{ item }}.txt"
  loop:
    - alpha
    - bravo

- name: Loop over a list variable
  command:
    cmd: "echo {{ item }}"
  loop: "{{ my_items }}"
```

### with_items

`with_items` behaves like `loop`, but also flattens one level of nested lists.

```yaml
- name: Flatten one level
  copy:
    content: "{{ item }}"
    dest: "/tmp/dibra-loop-{{ item }}.txt"
  with_items: "{{ [["red", "green"], "blue"] }}"
```

### with_list

`with_list` behaves like `loop` without flattening.

```yaml
- name: With list
  copy:
    content: "{{ item }}"
    dest: "/tmp/dibra-loop-{{ item }}.txt"
  with_list: "{{ list_items }}"
```

### with_dict

`with_dict` expects a map and exposes `item.key` and `item.value`.

```yaml
- name: With dict
  copy:
    content: "{{ item.value }}"
    dest: "/tmp/dibra-loop-{{ item.key }}.txt"
  with_dict:
    first: one
    second: two
```

### with_sequence

`with_sequence` builds an integer sequence. It accepts either a string of `key=value` pairs or a map.

```yaml
- name: With sequence string
  command:
    cmd: "echo {{ item }}"
  with_sequence: "start=1 count=3 stride=1"

- name: With sequence map and format
  command:
    cmd: "echo {{ item }}"
  with_sequence:
    start: 0
    end: 4
    stride: 2
    format: "user%02d"
```

### Loop control metadata

Loop metadata is configured under `loop_control`.

| Field | Description |
|-------|-------------|
| `loop_var` | Custom item variable name (default `item`) |
| `index_var` | Variable name for the loop index (0-based) |
| `pause` | Seconds to sleep between iterations (float supported) |
| `extended` | Adds `ansible_loop` metadata (index, first, last, etc.) |
| `label` | Label string for logging (currently informational) |

Extended metadata fields include `ansible_loop.index`, `ansible_loop.index0`, `ansible_loop.first`, `ansible_loop.last`, `ansible_loop.length`, `ansible_loop.previtem`, and `ansible_loop.nextitem`.

```yaml
- name: Custom loop vars
  copy:
    content: "{{ fruit }}-{{ fruit_idx }}"
    dest: "/tmp/dibra-loop-{{ fruit_idx }}.txt"
  loop: "{{ fruits }}"
  loop_control:
    loop_var: fruit
    index_var: fruit_idx

- name: Extended loop info
  copy:
    content: "index={{ ansible_loop.index }} first={{ ansible_loop.first }}"
    dest: "/tmp/dibra-loop-extended-{{ item }}.txt"
  loop: "{{ fruits }}"
  loop_control:
    extended: true
```

### Register results in loops

When `register` is used with a loop, the registered variable contains `results`, an array of per-iteration result objects. Each item includes the module response plus `item`, `ansible_loop_var`, and any `index_var`.

```yaml
- name: Loop register
  command:
    cmd: "echo {{ item }}"
  loop: "{{ fruits }}"
  register: loop_cmd

- name: Use loop register
  copy:
    content: "{{ loop_cmd.results[0].stdout }}|{{ loop_cmd.results[1].stdout }}"
    dest: /tmp/dibra-loop-register.txt
```

### Loop-aware include_tasks

`include_tasks` can be combined with loops to expand the include once per item. Each included task gets the loop variables merged into its `vars` and can use `loop_control.loop_var`.

```yaml
- name: Include tasks per item
  include_tasks: include_tasks.yaml
  loop: "{{ include_items }}"
  loop_control:
    loop_var: include_item
```

Included task file example:

```yaml
- name: Included loop copy
  copy:
    content: "{{ include_item.value }}"
    dest: "/tmp/dibra-loop-{{ include_item.name }}.txt"
```

### Behavior notes

- `loop`, `with_items`, `with_list`, `with_dict`, and `with_sequence` are mutually exclusive on a task.
- Empty loop lists skip the task and produce a `skipped` register result with `results: []`.
- Loop values can be templated expressions; when the template is the full value, Dibra resolves the underlying list before iterating.

## Variable System

Dibra implements a variable system with six precedence layers, template interpolation, and magic variables. For full documentation see `docs/Variables.md`.

### Precedence (Low → High)

1. `group_vars/<group>.{yml,yaml,json}` — auto-loaded by group membership
2. `host_vars/<host>.{yml,yaml,json}` — auto-loaded by host name
3. Playbook `vars` + `vars_files` — merged, then applied as one layer
4. Runtime vars — `register` results, `set_fact`, `include_vars`, and gathered `ansible_*` facts for the current host
5. Task `vars` — scoped to a single task
6. Extra vars (`-e` / `--extra-vars`) — CLI flags, always win

### Merge Strategy

Set `vars_merge` at playbook root:

- `replace` (default): higher precedence replaces entire key
- `merge`: recursively merges maps; scalars and lists still replace

### Template Interpolation

String values in module arguments are rendered with `{{ expression }}` syntax:

```yaml
vars:
  app_name: myapp
  deploy_dir: "/opt/{{ app_name }}"

tasks:
  - name: Create dir
    file:
      path: "{{ deploy_dir }}"     # resolves to /opt/myapp
      state: directory
```

Supports dot notation (`{{ app.port }}`), bracket notation (`{{ items[0] }}`), and nested references. Templates are resolved iteratively — if a resolved value contains more `{{ }}`, they are resolved too (up to 10 passes).

### Namespaces

All layers are accessible under `vars.*` even after flattening:

- `vars.group` — group vars
- `vars.host` — host vars
- `vars.play` — play + vars_files
- `vars.runtime` — register, `set_fact`, `include_vars`, and gathered facts
- `vars.task` — task vars
- `vars.extra` — extra vars

```yaml
content: "play_port={{ vars.play.app.port }}, effective={{ app.port }}"
```

### Magic Variables

| Variable | Type | Description |
|----------|------|-------------|
| `inventory_hostname` | string | Current host name |
| `group_names` | list | Groups the current host belongs to |
| `groups` | map | Group name → list of host names |
| `hostvars` | map | Host name → resolved vars for that host |

```yaml
content: "host={{ inventory_hostname }}, group={{ group_names[0] }}"
content: "web0={{ groups.web[0] }}, self_port={{ hostvars[inventory_hostname].app.port }}"
```

### Inventory Vars Directory Layout

```
playbook.yaml
group_vars/
  web.yml
  prod.yml
host_vars/
  web1.yml
```

Files are auto-loaded relative to the playbook directory. Missing files are silently skipped.

### Extra Vars

```bash
dibra -config playbook.yaml -e app_port=9090
dibra -config playbook.yaml -e "port=9090,env=prod"
dibra -config playbook.yaml -e @production.yml
```

### Error Handling

- Missing variables in `{{ }}` fail the task immediately with a clear error
- `renderArgs` errors are surfaced and the task is skipped (errors are never silently swallowed)

## Example: Install Caddy from Third-Party Repo

```yaml
hosts:
  - name: webserver
    host: 192.168.1.100
    user: root
    password: "secret"
    become: true

tasks:
  - name: Add Caddy GPG key
    apt_key:
      url: https://dl.cloudsmith.io/public/caddy/stable/gpg.key
      keyring: /usr/share/keyrings/caddy-stable-archive-keyring.gpg

  - name: Add Caddy repository
    apt_repository:
      repo: "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
      filename: caddy-stable
      update_cache: true

  - name: Install Caddy
    apt:
      name: caddy
      state: present
```

## Example: Deploy Application Files

```yaml
tasks:
  - name: Create app directory
    file:
      path: /opt/myapp
      state: directory
      mode: "0755"

  - name: Copy application binary
    copy:
      src: ./build/myapp
      dest: /opt/myapp/myapp
      mode: "0755"

  - name: Deploy configuration
    copy:
      src: ./config/production.yaml
      dest: /opt/myapp/config.yaml
      mode: "0644"
      backup: true

  - name: Create symlink in PATH
    file:
      path: /usr/local/bin/myapp
      src: /opt/myapp/myapp
      state: link

## Testing

### Integration Tests
NEVER RUN THE FULL SUITE OF INTEGRATION TESTS BUT RUN ONLY THE SPECIFIC ONES WE ARE WORKING ON.

Integration tests run against a Docker container with Ubuntu 22.04 + systemd + SSH.
All integration invocations must use `-count=1`; these tests depend on external
container state and must never reuse Go's successful-result cache.

The default `full` profile and the explicit `docker` profile require Docker
Engine 29.7.2, Compose 5.4.0, and buildx 0.30.0 on the managed host. Ansible-core
certification uses a separate Docker-free host; see
[`docs/CoreTestLanes.md`](docs/CoreTestLanes.md).

```bash
# Full test cycle: start container → run tests → stop container
make test-integration

# Or manage container manually
make test-integration-up      # Start container (SSH on port 2222)
make test-integration-only    # Run tests (container must be running)
make test-integration-down    # Stop and remove container
```

To run specific tests make sure we have docker up and pass the specific module test suite
```bash
make test-integration-up      # Start container (SSH on port 2222)
go test -tags=integration -count=1 -v -timeout 20m ./test/integration/... -run TestPlaybook_Service # for the service module
make test-integration-down    # Stop and remove container
```

### Test Container

The test container runs **systemd as PID 1**, enabling:
- Full `systemctl` support for service management
- Realistic testing environment matching production servers
- SSH access on port 2222 (root:rootpass, testuser:testpass)

```bash
# Connect to test container
ssh -p 2222 root@localhost  # password: rootpass

# Verify systemd
systemctl status ssh
systemctl list-units --type=service
```

### Core Certification Host

`test/core/docker-compose.yaml` is an Ubuntu 22.04 systemd/SSH host **without**
Docker Engine, Compose, or buildx on the managed node. SSH is on port 2223
(`root:rootpass`, `testuser` with sudo). A sidecar `httpbin` serves URI tests.
See [`docs/CoreTestLanes.md`](docs/CoreTestLanes.md).

```bash
make test-core-integration-up
make test-core-integration-only
make test-core-execution-integration-only
make test-core-files-content-integration-only
make test-core-system-packages-integration-only
make test-core-network-vcs-integration-only
make test-core-integration-down
```

Opt-in transport/execution smokes (not certification) are Fedora systemd on
port 2224 and Alpine without systemd on port 2225:

```bash
make test-platform-fedora-systemd-smoke
make test-platform-alpine-nonsystemd-smoke
```

### Test Coverage

DO NOT ADD EACH INTEGRATION TEST HERE, JUST THE MAIN LEVEL

| Test Suite | What it verifies |
|------|------------------|
| `TestPlaybook_Ping` | Ping Module SUITE, SSH connectivity test |
| `TestPlaybook_Command` | Comamnd Module |
| `TestPlaybook_Shell` | Module shell command execution |
| `TestPlaybook_Apt` | Apt Module Package install + idempotency via `dpkg -s` |
| `TestPlaybook_File` | File Module |
| `TestPlaybook_Copy` | Copy Module Inline content copy via `cat` |
| `TestPlaybook_Fetch` | Fetch module |
| `TestPlaybook_Git` | Git module |
| `TestPlaybook_URI` | URI module |
| `TestPlaybook_Cron` | Cron Module |
| `TestPlaybook_Systemd` | SystemD module Start service + idempotency |
| `TestPlaybook_Service` | Service Module Start service + idempotency |
| `TestPlaybook_Group` | Group Module |
| `TestPlaybook_Lineinfile` | Lineinfile Module: line add, replace, remove, backrefs, firstmatch |
| `TestPlaybook_*CoreParity` | First ansible-core certification slice for `command`, `shell`, `file`, `copy`, `template`, and `lineinfile` on the Docker-free core host |
| `TestPlaybook_Blockinfile` | Blockinfile Module: block insert, update, remove, markers, insertafter/before |
| `TestPlaybook_Replace` | Replace Module: regex replace, before/after, backrefs, backup, validate |
| `TestPlaybook_Iptables` | Iptables Module: rules, chains, tables, NAT, policies, flush |
| `TestPlaybook_IptablesState` | Iptables State Module: save, restore, tables, idempotency |
| `TestPlaybook_FullDeployWorkflow` | Full app deployment: dirs, config, symlinks |
| `TestPlaybook_Unarchive` | Unarchive module Basic tar extraction + idempotency |
| `TestPlaybook_Tempfile` | Tempfile module: file/directory creation, prefix/suffix, custom path, permissions |
| `TestPlaybook_Slurp` | Slurp module: text/binary/empty/unicode files, path alias, proc files, symlinks, error handling (not found, directory, unreadable, invalid path), idempotency, template variables |
| `TestPlaybook_Reboot` | Reboot module: boot time commands, search paths, test commands, shutdown detection |
| `TestPlaybook_Variables` | Variables: precedence, namespaces, vars_files, extra vars, hostvars/groups |
| `TestPlaybook_ImportTasks` | import_tasks: basic, free-form/file syntax, subdirectory, nested, circular detection, vars inheritance/override, multiple imports, mixed modules, templated paths, execution order, idempotency, absolute paths, extra vars |
| `TestPlaybook_IncludeTasks` | include_tasks: basic, free-form/file syntax, subdirectory, nested includes, vars inheritance/override, multiple includes, mixed modules, templated paths (play vars, host vars, extra vars), nested include+import interaction, execution order, idempotency, absolute paths, extra vars, deeply nested 3 levels |
| `TestPlaybook_DockerSwarmServiceHealthcheck` | Swarm service healthcheck configuration (Phase 6.3.1) |
| `TestPlaybook_DockerSwarmServiceDNS` | Swarm service DNS configuration (Phase 6.3.2) |
| `TestPlaybook_DockerSwarmServiceMounts` | Swarm service mounts configuration (Phase 6.3.4) |
| `TestPlaybook_DockerSwarmServiceUpdateConfig` | Swarm service update/rollback configuration (Phase 6.2) |
| `TestPlaybook_DockerSwarmServiceIdempotency` | Swarm service improved idempotency (Phase 6.4) |
| `TestPlaybook_DockerNodeLabelsToRemove` | Node label removal support (Phase 6.5.3) |
| `TestPlaybook_DockerSwarmServiceInfo` | Swarm service info module (Phase 6.6) |
| `TestPlaybook_DockerSwarmServiceInfoParity` | Swarm service info parity: manager check, exists/service, ID lookup, inspect equality, check mode, idempotency |
| `TestPlaybook_DockerNodeInfo` | Node info module (Phase 6.7) |
| `TestPlaybook_DockerHostInfo` | Host info and shared explicit Docker connection options |
| `TestPlaybook_DockerVolume` | Volume deep compare, driver options, metadata, recreate (Phase 7.4) |
| `TestPlaybook_DockerSecretHashIdempotency` | Secret hash-based idempotency, data change detection (Phase 7.3) |
| `TestPlaybook_DockerSecretParity` | Swarm secret parity: data/data_src/base64, ansible_key, rolling versions, versions_to_keep, check mode, labels, force, in-use rotation |
| `TestPlaybook_DockerConfigHashIdempotency` | Config hash-based idempotency, label-only updates (Phase 7.3) |
| `TestPlaybook_DockerConfigParity` | Swarm config parity: data/data_src/base64, ansible_key, rolling versions, versions_to_keep, template_driver, check mode, labels, force, in-use rotation |
| `TestPlaybook_DockerStackParity` | Swarm stack parity: compose paths/dicts, absent retries, prune, detach, docker_cli, stack_spec_diff, check-mode skip |
| `TestPlaybook_DockerStackInfoParity` | Swarm stack info parity: off-swarm failure, empty list, Name/Services, multiple stacks, docker_cli, check/diff, idempotency |
| `TestPlaybook_DockerStackTaskInfoParity` | Swarm stack task info parity: required name, off-swarm failure, missing stack, DesiredState/Image/Name, two services, docker_cli, check/diff, idempotency |
| `TestPlaybook_DockerVolumePrune` | Prune filter improvements (Phase 7.2) |
| `TestPlaybook_Find` | Find module: recursive/non-recursive search, glob/regex patterns, excludes, file_type (file/directory/link/any), age/size filters, hidden files, symlinks, depth limit, mode filtering, checksum algorithms, contains content matching, multiple paths, limit, path/pattern/exclude aliases, template variables, idempotency |
| `TestPlaybook_Register` | Register keyword: basic shell register, register on failure, overwrite, command module, ping module-specific fields, stdout_lines access, chained registers, file/copy/tempfile module fields, multiple modules, idempotency tracking, template expressions with registered vars, include_tasks/import_tasks boundary, invalid variable names (numeric, hyphen, space), underscore prefix, no side effects without register, rerun idempotency |
| `TestPlaybook_Handlers` | Handlers: changed notifications, changed_when, loops (any change notifies every templated handler), deduplication, definition order, listen topics, duplicate names, explicit/automatic flushing, re-notification, variables, handler imports/includes, handler-to-handler notify, handler `when`, case-sensitive names, failure skipping, play-level and CLI `force_handlers`, illegal `meta: flush_handlers` handlers, and idempotent reruns |
| `TestPlaybook_HandlersNotifyServiceRestarts` | Article samples: nginx template restart, multi-file reload-once, definition-order validate/reload/verify, `listen` fan-out, `flush_handlers` before a health check, start-as-task/restart-as-handler, Apache-style index+lineinfile reload, and looped configs with one service restart |
| `TestPlaybook_TemplateModule` | Template module: basic render, dest directory, custom delimiters, trim blocks, idempotency, force flag, validation, newline sequences, register, nested includes, builtin filters (default/upper/lower/replace/join/length/title/trim/tojson/int/float), custom Ansible filters (split, regex_replace/search/findall, basename/dirname, to_yaml/from_json/to_nice_json, quote, ternary, b64encode/b64decode, hash, comment, combine, dict2items/items2dict, flatten, type_debug, splitext), filter chaining, for-loops (loop variables, dict iteration, conditional filtering), complex conditionals (if/elif/else, and/or/not, in, is defined/is not defined), set statements, macros, raw blocks, whitespace control, magic variables (dibra_managed, template_host, template_destpath), complex nested data structures, template inheritance (extends/block) |
| `TestPlaybook_Inventory` | External YAML inventory: basic inventory loading, idempotency, host output, groups with vars, children group hierarchy, implicit all group, ungrouped hosts, group_vars/host_vars files relative to inventory, deep hierarchy (4 levels), multi-parent groups, host vars override group vars, magic variables (inventory_hostname, group_names), playbook inventory reference, error on both hosts and inventory, play vars + inventory, extra vars + inventory, task vars + inventory, inventory not found error, register with inventory, import_tasks with inventory, SSH key path, port as string coercion, become as string coercion, groups in context |

Each test:
1. Runs a playbook via `go run ./cmd/controller`
2. Verifies changes on remote via SSH commands
3. Runs playbook again to verify idempotency
