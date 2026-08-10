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
  https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-deploy.sh \
  | sh
sudo systemctl enable --now dibra-deploy.service
```

The installer verifies the release checksum and does not start the service by
default. Pass `--enable` to install and start in one command. See
[`docs/DibraDeploy.md`](docs/DibraDeploy.md) for pinned releases, manual
installation, the ZIP contract, and service operation.



## Documentation
Unfortunately for the time being the best documentation is the code and AGENTS.md

The [Ansible parity and upstream tracking program](docs/ansible-parity-program.md)
defines how we track Ansible and collection changes and port them to Dibra.

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
```

For pull-based local execution, `dibra-deploy` periodically fetches a ZIP
project from a local task server and applies it without SSH.

## Limitations
 * Currently supports only Debian/Ubuntu systems.
