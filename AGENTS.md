# GoAnsible

A minimal Ansible-like configuration management tool written in Go.

## Quick Reference

```bash
# Build
go build ./...

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
│       ├── ping/             # Connectivity test module
│       ├── stat/             # File stat (used internally by fetch)
│       ├── uri/              # HTTP/HTTPS requests
│       ├── cron/             # Crontab management
│       ├── service/          # Generic service management
│       ├── user/             # User account management
│       └── group/            # Group management
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
| `TestPlaybook_FullDeployWorkflow` | Full app deployment: dirs, config, symlinks |
| `TestPlaybook_Unarchive` | Unarchive module Basic tar extraction + idempotency |

Each test:
1. Runs a playbook via `go run ./cmd/controller`
2. Verifies changes on remote via SSH commands
3. Runs playbook again to verify idempotency
