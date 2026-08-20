# DISCLAIMER:
This project is 99% written by AI agents. If you're allergic to that then this is not the place for you.

# dibra

A minimal Ansible-like tool written in Go.
It is completely inspired by Ansible and born our of my dislike managing python deps.
It's still under heavy development and largely untested in real world.

## Architecture

```
┌─────────────────────┐         SSH          ┌─────────────────────┐
│   Controller (CLI)  │ ───────────────────► │   Agent Binary      │
│                     │  1. Install agent    │                     │
│  - Parse YAML       │  2. Execute with     │  - Receive JSON     │
│  - SSH connection   │     JSON args        │  - Run apt-get      │
│  - Orchestrate      │  3. Get JSON result  │  - Return JSON      │
└─────────────────────┘                      └─────────────────────┘
```

## How It Works

1. **Controller** reads the playbook YAML
2. **Connects** to each host via SSH
3. **Insatalls** agent to `/tmp/.dibra-agent` (if not present)
4. **Executes** agent with `sudo -S` wrapper, passing JSON request via stdin
5. **Parses** JSON response from agent stdout
6. **Reports** changed/ok/failed status for each task

## Installation

### Controller on macOS

```bash
brew install gjergj/tap/dibra
```

### Local pull runner on Linux

Install the latest `dibra-deploy` release and its systemd unit:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh \
  | sh -s -- 'PROJECT_JWT' --endpoint 'https://orchestrator.example/gettasks'
sudo systemctl enable --now dibra-deploy.service
```

The CustomerAdmin receives the project JWT and endpoint in the installation
command returned by the orchestrator project API. The installer stores them in
a root-only environment file, verifies the release checksum, and does not start
the service by default. Pass `--enable` to install and start in one command. See
[`docs/DibraDeploy.md`](docs/DibraDeploy.md) for pinned releases, manual
installation, the ZIP contract, and service operation.



## Documentation
Unfortunately for the time being the best documentation is the code and AGENTS.md

The [Ansible parity and upstream tracking program](docs/ansible-parity-program.md)
defines how we track Ansible and collection changes and port them to Dibra.

The [module registry guide](docs/ModuleRegistry.md) documents canonical Docker
module names, typed decoding and dispatch, capability, sensitivity and
deprecation metadata, the check/diff invocation state, and the process for
registering additional modules.

## Development

### Git Hooks (Lint + Unit Tests)

Enable the pre-commit hook to run linting and unit tests before each commit:

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
```

### Shell Completions

Generate shell completions:

```bash
dibra completion bash > /usr/local/etc/bash_completion.d/dibra
dibra completion zsh > /usr/local/share/zsh/site-functions/_dibra
dibra completion fish > ~/.config/fish/completions/dibra.fish
dibra completion powershell > dibra.ps1
```

Use `dibra -config playbook.yaml --check` for a non-mutating check-mode run and
`--diff` to request structured differences from modules that implement them.
Modules that have not implemented Dibra check mode are safely skipped rather
than executed.

### Core integration certification

Run the non-Docker Ubuntu 22.04 certification lane without installing Docker
Engine, Compose, or buildx in the managed host:

```bash
make test-core-integration
```

Family targets are available for execution, files/content, system/packages,
and network/VCS, with matching `-only` forms for an already-running host. The
Fedora systemd and Alpine non-systemd definitions are currently smoke profiles,
not parity claims. See [`docs/CoreTestLanes.md`](docs/CoreTestLanes.md) for the
commands, supported tests, ports, and integration-profile environment.

### Handlers

Changed tasks can notify play-level handlers. Notifications are deduplicated
and run in handler definition order at the end of the play or at an explicit
`meta: flush_handlers` task:

```yaml
tasks:
  - name: Deploy Caddy configuration
    copy:
      src: Caddyfile
      dest: /etc/caddy/Caddyfile
    notify: restart caddy

handlers:
  - name: restart caddy
    systemd_service:
      name: caddy
      state: restarted
```

Handlers support `listen` topics, `changed_when`, loops, static
`import_tasks`, and dynamic `include_tasks`. Failed hosts skip pending handlers
unless the play sets `force_handlers: true` or the CLI uses
`--force-handlers`. See `AGENTS.md` for the complete behavior.

For pull-based local execution, `dibra-deploy` periodically fetches a ZIP
project from a local task server and applies it without SSH.

## Limitations
 * Currently supports only Debian/Ubuntu systems.
