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
│       ├── copy/             # File copy (local→remote, content, remote_src)
│       ├── fetch/            # File fetch (remote→local)
│       ├── file/             # File/directory/symlink management
│       ├── ping/             # Connectivity test module
│       ├── stat/             # File stat (used internally by fetch)
│       ├── uri/              # HTTP/HTTPS requests
│       ├── cron/             # Crontab management
│       ├── service/          # Generic service management
│       └── user/             # User account management
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

Integration tests run against a Docker container with Ubuntu 22.04 + systemd + SSH.

```bash
# Full test cycle: start container → run tests → stop container
make test-integration

# Or manage container manually
make test-integration-up      # Start container (SSH on port 2222)
make test-integration-only    # Run tests (container must be running)
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

| Test | What it verifies |
|------|------------------|
| `TestPlaybook_PingBasic` | Basic SSH connectivity test |
| `TestPlaybook_PingReturnsPong` | Default return value is "pong" |
| `TestPlaybook_PingCustomData` | Custom data parameter via playbook |
| `TestPlaybook_PingCustomDataReturned` | Custom data returned correctly |
| `TestPlaybook_PingCrash` | Failure when data=crash |
| `TestPlaybook_PingCrashResponse` | Response when data=crash |
| `TestPlaybook_PingIdempotent` | Multiple runs return same result |
| `TestPlaybook_PingNeverChanges` | Always returns changed=false |
| `TestPlaybook_PingEmptyData` | Empty data defaults to pong |
| `TestPlaybook_PingSpecialChars` | Special characters in data |
| `TestPlaybook_PingSSHConnectivity` | Verifies SSH connection works |
| `TestPlaybook_PingMultipleRuns` | Multiple playbook runs succeed |
| `TestPlaybook_AptInstall` | Package install + idempotency via `dpkg -s` |
| `TestPlaybook_AptRemove` | Package removal + idempotency |
| `TestPlaybook_FileDirectory` | Directory creation, permissions via `stat` |
| `TestPlaybook_FileTouch` | File creation, permissions |
| `TestPlaybook_FileSymlink` | Symlink creation via `readlink` |
| `TestPlaybook_FileAbsent` | File deletion + idempotency |
| `TestPlaybook_CopyContent` | Inline content copy via `cat` |
| `TestPlaybook_CopyRemoteSrc` | Remote-to-remote copy |
| `TestPlaybook_CopyWithBackup` | Backup file creation |
| `TestPlaybook_AptKeyAndRepo` | GPG key + repository management |
| `TestPlaybook_FetchBasic` | Basic fetch with hostname/path structure + idempotency |
| `TestPlaybook_FetchFlat` | Flat mode fetch to specific file |
| `TestPlaybook_FetchFlatToDir` | Flat mode fetch to directory (uses filename) |
| `TestPlaybook_FetchMissingFile` | Fails on missing remote file |
| `TestPlaybook_FetchMissingFileNoFail` | Skips missing file with fail_on_missing=false |
| `TestPlaybook_FetchDirectory` | Fails on directory (not supported) |
| `TestPlaybook_URIGet` | Simple GET request |
| `TestPlaybook_URIPost` | POST with JSON body, changed detection |
| `TestPlaybook_URIStatusCode` | Custom status code acceptance (404) |
| `TestPlaybook_URIStatusCodeFail` | Fail on unexpected status (500) |
| `TestPlaybook_URIHeaders` | Custom headers |
| `TestPlaybook_URIDownload` | Download file to dest |
| `TestPlaybook_URICreates` | Skip request if file exists |
| `TestPlaybook_URITimeout` | Request timeout handling |
| `TestPlaybook_URIFollowRedirects` | Follow redirects |
| `TestPlaybook_URINoFollowRedirects` | Don't follow redirects |
| `TestPlaybook_CronAddJob` | Add cron job + idempotency |
| `TestPlaybook_CronRemoveJob` | Remove cron job + idempotency |
| `TestPlaybook_CronSpecialTime` | @reboot and other special times |
| `TestPlaybook_CronDisabled` | Commented-out (disabled) jobs |
| `TestPlaybook_CronEnv` | Environment variable management |
| `TestPlaybook_CronEnvRemove` | Remove environment variable |
| `TestPlaybook_CronFile` | /etc/cron.d file with user field |
| `TestPlaybook_CronFileRemove` | Auto-remove empty cron.d file |
| `TestPlaybook_CronUpdateJob` | Update existing job |
| `TestPlaybook_SystemdServiceStart` | Start service + idempotency |
| `TestPlaybook_SystemdServiceStop` | Stop service + idempotency |
| `TestPlaybook_SystemdServiceRestart` | Restart service (always changed) |
| `TestPlaybook_SystemdServiceEnable` | Enable at boot + idempotency |
| `TestPlaybook_SystemdServiceDisable` | Disable from boot + idempotency |
| `TestPlaybook_SystemdServiceStartAndEnable` | Combined start+enable + idempotency |
| `TestPlaybook_SystemdServiceDaemonReload` | daemon-reload without service name |
| `TestPlaybook_SystemdServiceDaemonReloadWithRestart` | daemon-reload before restart |
| `TestPlaybook_SystemdServiceMask` | Mask service + idempotency |
| `TestPlaybook_SystemdServiceUnmask` | Unmask service + idempotency |
| `TestPlaybook_SystemdServiceUnmaskAndEnable` | Unmask then enable + idempotency |
| `TestPlaybook_SystemdServiceImplicitServiceExtension` | Auto-add .service extension |
| `TestPlaybook_SystemdServiceExplicitServiceExtension` | Explicit .service extension |
| `TestPlaybook_SystemdServiceNonExistent` | Fails on non-existent service |
| `TestPlaybook_SystemdServiceGlobPattern` | Rejects glob patterns |
| `TestPlaybook_SystemdServiceNoAction` | Fails when no action specified |
| `TestPlaybook_SystemdAlias` | Uses `systemd` as alias |
| `TestPlaybook_SystemdServiceStopAndDisable` | Combined stop+disable + idempotency |
| `TestPlaybook_SystemdServiceReloaded` | Reload service (always changed) |
| `TestPlaybook_SystemdServiceFullWorkflow` | Full workflow: start+enable+daemon_reload |
| `TestPlaybook_SystemdServiceTimerUnit` | Timer unit (.timer) management |
| `TestPlaybook_SystemdServiceNoBlock` | no_block async operation |
| `TestPlaybook_SystemdServiceInvalidState` | Fails on invalid state |
| `TestPlaybook_SystemdServiceDaemonReexec` | daemon-reexec |
| `TestPlaybook_SystemdServiceDaemonReexecAndReload` | Combined daemon-reexec+reload |
| `TestPlaybook_SystemdServiceForceEnable` | Force enable with --force |
| `TestPlaybook_SystemdServiceNameRequired` | Fails when name missing for state |
| `TestPlaybook_SystemdServiceEnabledWithoutName` | Fails when name missing for enabled |
| `TestPlaybook_SystemdServiceMaskedWithoutName` | Fails when name missing for masked |
| `TestPlaybook_SystemdServiceStaticService` | Handles static services |
| `TestPlaybook_ServiceStart` | Start service + idempotency |
| `TestPlaybook_ServiceStop` | Stop service + idempotency |
| `TestPlaybook_ServiceRestart` | Restart service (always changed) |
| `TestPlaybook_ServiceEnable` | Enable at boot + idempotency |
| `TestPlaybook_ServiceDisable` | Disable from boot + idempotency |
| `TestPlaybook_ServiceStartAndEnable` | Combined start+enable + idempotency |
| `TestPlaybook_ServiceStopAndDisable` | Combined stop+disable + idempotency |
| `TestPlaybook_ServiceReloaded` | Reload service (always changed) |
| `TestPlaybook_ServiceNonExistent` | Fails on non-existent service |
| `TestPlaybook_ServiceNameRequired` | Fails when name is missing |
| `TestPlaybook_ServiceStateOrEnabledRequired` | Fails when neither state nor enabled |
| `TestPlaybook_ServiceInvalidState` | Fails on invalid state |
| `TestPlaybook_ServiceWithPattern` | Pattern-based service detection |
| `TestPlaybook_ServiceRestartWithSleep` | Restart with sleep between stop/start |
| `TestPlaybook_ServiceImplicitServiceExtension` | Auto-add .service extension |
| `TestPlaybook_ServiceExplicitServiceExtension` | Explicit .service extension |
| `TestPlaybook_ServiceFullWorkflow` | Full workflow: start+enable + idempotency |
| `TestPlaybook_ServiceUseSystemd` | Force systemd service manager |
| `TestPlaybook_ServiceTimerUnit` | Timer unit (.timer) management |
| `TestPlaybook_ServiceEnableOnlyNoState` | Enable without changing running state |
| `TestPlaybook_ServiceReloadStartsIfStopped` | Reload starts service if stopped |
| `TestPlaybook_ServiceFactsGather` | Basic service facts gathering |
| `TestPlaybook_ServiceFactsContainsSSH` | SSH service included in results |
| `TestPlaybook_ServiceFactsIdempotent` | Always returns changed=false |
| `TestPlaybook_ServiceFactsSystemdSource` | Systemd services have source=systemd |
| `TestPlaybook_ServiceFactsWithRunningService` | Detects running services |
| `TestPlaybook_ServiceFactsWithStoppedService` | Detects stopped services |
| `TestPlaybook_ServiceFactsMultipleRuns` | Consistent results across runs |
| `TestPlaybook_ServiceFactsAgentDirect` | Direct agent execution |
| `TestPlaybook_ServiceFactsServiceStates` | Valid state values |
| `TestPlaybook_ServiceFactsEnabledStatus` | Valid status values |
| `TestPlaybook_ServiceFactsServiceNames` | Map keys match service names |
| `TestPlaybook_ServiceFactsDetectsEnabledDisabled` | Detects enabled/disabled status |
| `TestPlaybook_ServiceFactsDetectsRunningState` | Detects running state correctly |
| `TestPlaybook_FullDeployWorkflow` | Full app deployment: dirs, config, symlinks |

Each test:
1. Runs a playbook via `go run ./cmd/controller`
2. Verifies changes on remote via SSH commands
3. Runs playbook again to verify idempotency
