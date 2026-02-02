# GoAnsible

A minimal Ansible-like tool written in Go, supporting the `apt` module for Debian/Ubuntu systems.

## Architecture

```
┌─────────────────────┐         SSH          ┌─────────────────────┐
│   Controller (CLI)  │ ───────────────────► │   Agent Binary      │
│                     │  1. Upload agent     │                     │
│  - Parse YAML       │  2. Execute with     │  - Receive JSON     │
│  - SSH connection   │     JSON args        │  - Run apt-get      │
│  - Orchestrate      │  3. Get JSON result  │  - Return JSON      │
└─────────────────────┘                      └─────────────────────┘
```

## Features

- **apt module** with support for:
  - `state`: present, absent, latest
  - `update_cache`: run apt-get update
  - `cache_valid_time`: skip update if cache is fresh
  - `purge`: remove config files when removing packages
  - `autoremove`: remove unused dependencies
  - `upgrade`: safe, full, dist

- **Agent-based execution**: Cross-compiles a Go agent for linux/amd64, uploads it once, reuses on subsequent runs
- **Ansible-style privilege escalation**: Uses `sudo -S` at the SSH layer
- **Idempotent**: Reports "changed" only when actual changes occur

## Usage

```bash
# Build and run
cd goansible
go run ./cmd/controller -config playbook.yaml

# With verbose output
go run ./cmd/controller -config playbook.yaml -v

# Force re-upload of agent
go run ./cmd/controller -config playbook.yaml --force-agent-upload
```

## Playbook Format

```yaml
hosts:
  - name: webserver1
    host: 192.168.1.100
    port: 22
    user: deploy
    password: "your-ssh-password"
    become: true
    become_password: "your-sudo-password"

tasks:
  - name: Update apt cache
    apt:
      update_cache: true
      cache_valid_time: 3600

  - name: Install nginx and curl
    apt:
      name:
        - nginx
        - curl
      state: present

  - name: Ensure htop is at latest version
    apt:
      name: htop
      state: latest

  - name: Remove vim
    apt:
      name: vim
      state: absent
      purge: true

  - name: Remove unused packages
    apt:
      autoremove: true
```

## Building

```bash
# Build controller
go build -o goansible ./cmd/controller

# Build agent manually (for testing)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o goansible-agent ./cmd/agent
```

## How It Works

1. **Controller** reads the playbook YAML
2. **Cross-compiles** the agent for linux/amd64 (cached by source hash)
3. **Connects** to each host via SSH
4. **Uploads** agent to `/tmp/.goansible-agent` (if not present)
5. **Executes** agent with `sudo -S` wrapper, passing JSON request via stdin
6. **Parses** JSON response from agent stdout
7. **Reports** changed/ok/failed status for each task
