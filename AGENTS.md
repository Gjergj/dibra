# GoAnsible

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
./bin/goansible --version

# Run playbook
go run ./cmd/controller -config playbook.yaml

# Verbose output
go run ./cmd/controller -config playbook.yaml -v

# Force re-upload agent
go run ./cmd/controller -config playbook.yaml --force-agent-upload

# Run integration tests
make test-integration

# Run integration tests (container must be running)
make test-integration-only

# Start/stop test container
make test-integration-up
make test-integration-down
```

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
| **macOS** | `brew install Gjergj/tap/goansible` |
| **macOS/Linux** | `curl -fsSL https://raw.githubusercontent.com/Gjergj/goansible/main/scripts/install.sh \| sh` |
| **Linux (deb)** | Download `.deb` from releases, `sudo dpkg -i goansible_*.deb` |
| **Linux (rpm)** | Download `.rpm` from releases, `sudo rpm -i goansible_*.rpm` |
| **Windows** | `scoop bucket add goansible https://github.com/Gjergj/scoop-bucket && scoop install goansible` |
| **Windows** | `choco install goansible` |
| **Any** | Download binary from [GitHub Releases](https://github.com/Gjergj/goansible/releases) |

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
│                     │  1. Cross-compile    │                     │
│  - Parse YAML       │  2. Upload agent     │  - Receive JSON     │
│  - SSH connection   │  3. Execute with     │  - Execute modules  │
│  - Orchestrate      │     JSON stdin       │  - Return JSON      │
│  - File transfer    │  4. Parse response   │  - Syscalls only    │
└─────────────────────┘                      └─────────────────────┘
```

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Agent Delivery** | Build on demand | Cross-compiles `GOOS=linux GOARCH=amd64` when source changes; caches by source hash |
| **Agent Caching** | Upload once, reuse | Agent stored at `/tmp/.goansible-agent`; use `--force-agent-upload` to update |
| **Privilege Escalation** | Controller wraps with `sudo -S` | Follows Ansible pattern; agent runs as root, doesn't handle sudo itself |
| **Communication** | JSON over stdin/stdout | Agent reads JSON request from stdin, writes JSON response to stdout |
| **Idempotency** | Check before change | All modules check current state before making changes |
| **File Transfer** | SCP protocol | Controller uploads files to `/tmp/.goansible-copy-<hash>` before copy module runs |
| **Checksum Validation** | SHA1 | Matches Ansible; computed locally, verified on remote after transfer |

### Privilege Escalation Flow

When connecting as non-root user with `become: true`:

```
Controller executes via SSH:
  echo "$PASSWORD" | sudo -S -p '' /tmp/.goansible-agent

Agent stdin receives:
  password\n{"module":"apt","args":{...}}

Agent skips to first '{' character before parsing JSON.
```

When connecting as root: sudo wrapper is skipped entirely.

## Project Structure

```
goansible/
├── cmd/
│   ├── controller/main.go    # CLI orchestrator
│   └── agent/main.go         # Remote agent binary
├── internal/
│   ├── builder/builder.go    # Cross-compiles agent on demand
│   ├── config/config.go      # YAML playbook parsing
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
       └── docker_container/ # Docker container management
├── test/
│   ├── Dockerfile            # Ubuntu 22.04 + systemd + SSH
│   ├── docker-compose.yaml   # Test container orchestration
│   └── integration/
│       └── integration_test.go  # Playbook-based integration tests
├── Makefile                  # Build and test commands
├── playbook.yaml             # Example playbook
└── AGENTS.md                 # This file
```

## Modules

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

Manages the lifecycle of Docker containers.

```yaml
- name: Start a container
  docker_container:
    name: my-container
    image: alpine:latest
    state: started
    command: ["sleep", "infinity"]

- name: Create a container with port bindings
  docker_container:
    name: web
    image: nginx
    state: started
    ports:
      - "8080:80"

- name: Remove a container
  docker_container:
    name: old-container
    state: absent
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | required | Name of the container. |
| `image` | | Image to use for the container. |
| `state` | `started` | `started`, `stopped`, `present`, `absent`. |
| `command` | | Command to execute at startup. |
| `ports` | | List of port bindings (`host:container`). |
| `volumes` | | List of volume bindings (`host:container:ro`). |
| `env` | | Dictionary of environment variables. |
| `network_mode` | | Network mode (e.g., `host`, `bridge`). |
| `networks` | | List of networks to connect to. |
| `pull` | `false` | Pull image if missing (currently simplified). |

### docker_image

Manages Docker images.

```yaml
- name: Pull an image
  docker_image:
    name: alpine
    tag: latest
    source: pull

- name: Remove an image
  docker_image:
    name: alpine
    tag: 3.14
    state: absent
```

### docker_network

Manages Docker networks.

```yaml
- name: Create a network
  docker_network:
    name: my-network
    driver: bridge

- name: Remove a network
  docker_network:
    name: my-network
    state: absent
```

### docker_volume

Manages Docker volumes.

```yaml
- name: Create a volume
  docker_volume:
    name: my-data
    driver: local

- name: Remove a volume
  docker_volume:
    name: my-data
    state: absent
```

### docker_prune

Prunes unused Docker resources (system prune, or specific types).

```yaml
- name: Prune everything
  docker_prune:
    containers: true
    images: true
    networks: true
    volumes: true
    builder: true

- name: Prune only dangling images
  docker_prune:
    images: true
    images_filters:
      dangling: "true"
```

### docker_login

Log into a Docker registry.

```yaml
- name: Login to Docker Hub
  docker_login:
    username: myuser
    password: mypassword

- name: Login to private registry
  docker_login:
    registry: https://myregistry.com
    username: myuser
    password: mypassword
```

### docker_swarm

Initialize or leave a Swarm.

```yaml
- name: Init Swarm
  docker_swarm:
    state: present
    advertise_addr: 192.168.1.10

- name: Join Swarm
  docker_swarm:
    state: join
    remote_addrs: [192.168.1.10:2377]
    join_token: SWMTKN-...

- name: Leave Swarm
  docker_swarm:
    state: absent
    force: true
```

### docker_swarm_service

Manage Swarm Services.

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

- name: Remove service
  docker_swarm_service:
    name: my-web
    state: absent
```

### docker_node

Manage Swarm Nodes.

```yaml
- name: Add Label to Node
  docker_node:
    hostname: my-worker-1
    labels:
      tier: frontend

- name: Drain Node
  docker_node:
    self: true
    availability: drain

- name: Promote Node
  docker_node:
    hostname: my-worker-1
    role: manager
```

### docker_compose

Manage Docker Compose projects.

```yaml
- name: Deploy Stack
  docker_compose:
    project_src: /opt/my-project
    state: present
    build: true
    pull: true
    env:
      DB_PASSWORD: secret

- name: Scale Service
  docker_compose:
    project_src: /opt/my-project
    scale:
      web: 3

- name: Remove Stack
  docker_compose:
    project_src: /opt/my-project
    state: absent
```

### docker_secret

Manage Docker Swarm secrets.

```yaml
- name: Create Secret
  docker_secret:
    name: my-secret
    data: "supersecretpassword"
    state: present
    labels:
      env: prod

- name: Remove Secret
  docker_secret:
    name: my-secret
    state: absent
```

### docker_config

Manage Docker Swarm configs.

```yaml
- name: Create Config
  docker_config:
    name: my-config
    data: "server { listen 80; }"
    state: present

- name: Remove Config
  docker_config:
    name: my-config
    state: absent
```

### docker_stack

Deploy Docker Swarm stacks.

```yaml
- name: Deploy Stack
  docker_stack:
    name: my-app
    compose_file: /path/to/docker-compose.yml
    state: present

- name: Remove Stack
  docker_stack:
    name: my-app
    state: absent
```

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

### docker_container_copy_into

Copy files or content into a Docker container.

```yaml
- name: Copy content into container
  docker_container_copy_into:
    container: my-container
    content: "Hello World"
    container_path: /tmp/hello.txt
    mode: "0644"

- name: Copy local file into container
  docker_container_copy_into:
    container: my-container
    path: /local/path/file.txt
    container_path: /app/file.txt
    owner_id: 1000
    group_id: 1000

### docker_image_build

Build Docker images using Docker's buildx plugin (BuildKit).

```yaml
- name: Build image
  docker_image_build:
    name: my-app
    tag: v1.0.0
    path: /path/to/app
    dockerfile: Dockerfile.prod
    args:
        VERSION: "1.0.0"
```

### docker_image_load

Load Docker image(s) from archives.

```yaml
- name: Load image from tar
  docker_image_load:
    path: /tmp/image.tar

### docker_image_export

Export (archive) one or more Docker images to a tarball.

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
```
```
```

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
      Match User ansible-agent
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
2. Controller uploads to `/tmp/.goansible-copy-<hash>`
3. Agent verifies checksum matches
4. Agent atomically moves to destination
5. Agent applies mode/owner/group

**Idempotency**: Compares SHA1 checksums; skips if destination matches.

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
2. Controller uploads to `/tmp/.goansible-unarchive-<hash>`
3. Agent verifies checksum matches
4. Agent extracts archive to destination
5. Agent applies mode/owner/group if specified

**Idempotency**:
- For tar: Uses `tar --diff` to compare archive contents with filesystem
- For zip: Checks if all files in archive exist in destination
- Use `creates` parameter for faster idempotency checks

### stat

Internal module used by fetch. Gets file metadata and checksum.

```yaml
# Used internally - not typically called directly
- stat:
    path: /etc/myapp/config.yaml
    follow: true  # Follow symlinks
```

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
| `body_format` | `raw` | Body format: `raw`, `json`, `form-urlencoded` |
| `headers` | `{}` | Custom HTTP headers as key-value pairs |
| `status_code` | `[200]` | List of acceptable status codes |
| `timeout` | `30` | Request timeout in seconds |
| `return_content` | `false` | Include response body in output |
| `dest` | | Save response to file path |
| `creates` | | Skip request if this file exists |
| `url_username` | | Username for HTTP authentication |
| `url_password` | | Password for HTTP authentication |
| `force_basic_auth` | `false` | Send auth header immediately |
| `follow_redirects` | `safe` | `all`, `none`, `safe` (GET/HEAD only) |
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
| `msg` | `Reboot initiated by GoAnsible` | Message to display to users before reboot. |
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
hosts:
  - name: webserver1
    host: 192.168.1.100
    port: 22
    user: deploy
    password: "ssh-password"      # Or use ssh_key_path
    # ssh_key_path: ~/.ssh/id_rsa
    become: true                  # Use sudo
    become_password: "sudo-password"

tasks:
  - name: Task description
    <module>:
      <param>: <value>
```

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
NEVER RUN THE FULL SUITE OF INTEGRATION TESTS BUT RUN ONLT THE SPECIFIC ONES WE ARE WORKING ON.

Integration tests run against a Docker container with Ubuntu 22.04 + systemd + SSH.

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
go test -tags=integration -v -timeout 20m ./test/integration/... -run TestPlaybook_Service # for the service module
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
| `TestPlaybook_Blockinfile` | Blockinfile Module: block insert, update, remove, markers, insertafter/before |
| `TestPlaybook_Replace` | Replace Module: regex replace, before/after, backrefs, backup, validate |
| `TestPlaybook_Iptables` | Iptables Module: rules, chains, tables, NAT, policies, flush |
| `TestPlaybook_IptablesState` | Iptables State Module: save, restore, tables, idempotency |
| `TestPlaybook_FullDeployWorkflow` | Full app deployment: dirs, config, symlinks |
| `TestPlaybook_Unarchive` | Unarchive module Basic tar extraction + idempotency |
| `TestPlaybook_Tempfile` | Tempfile module: file/directory creation, prefix/suffix, custom path, permissions |
| `TestPlaybook_Reboot` | Reboot module: boot time commands, search paths, test commands, shutdown detection |

Each test:
1. Runs a playbook via `go run ./cmd/controller`
2. Verifies changes on remote via SSH commands
3. Runs playbook again to verify idempotency
