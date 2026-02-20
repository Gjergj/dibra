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
#### MacOS
`brew install gjergj/tap/dibra`



## Documentation
Unfortunately for the time being the best documentation is the code and AGENTS.md

## Development

### Git Hooks (Lint + Unit Tests)

Enable the pre-commit hook to run linting and unit tests before each commit:

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
```

## Limitations
 * Currently supports only Debian/Ubuntu systems.
