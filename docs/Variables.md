# Dibra Variable System

Dibra implements a variable system inspired by Ansible with a smaller, more explicit precedence model. Variables let you parameterize playbooks so the same tasks can be reused across different environments, hosts, and configurations.

## Quick Reference

```yaml
# Playbook root
vars:
  app_name: "myapp"
  app_port: 8080
vars_files:
  - defaults.yml
vars_merge: merge  # or "replace" (default)

# Host definition
hosts:
  - name: web1
    groups: [web, prod]

# Task
tasks:
  - name: Deploy config
    vars:
      task_key: "value"
    copy:
      content: "name={{ app_name }}, port={{ app_port }}"
      dest: /etc/myapp.conf
```

```bash
# CLI extra vars
dibra -config playbook.yaml -e app_port=9090
dibra -config playbook.yaml --extra-vars @production.yml
```

## Variable Sources and Precedence

Variables come from five sources, listed from **lowest to highest** precedence:

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (lowest) | `group_vars/<group>.yml` | Auto-loaded for each group a host belongs to |
| 2 | `host_vars/<host>.yml` | Auto-loaded for each host by name |
| 3 | Play `vars` + `vars_files` | Defined at playbook root |
| 4 | Task `vars` | Defined per-task, scoped to that task only |
| 5 (highest) | Extra vars (`-e`) | CLI flags, always win |

When the same key exists at multiple levels, the higher-precedence source wins.

### Group Vars

Files in `group_vars/` are auto-loaded based on the `groups` field of each host. The directory must be relative to the playbook.

```
group_vars/
  web.yml       # loaded for hosts in "web" group
  prod.yml      # loaded for hosts in "prod" group
```

Inventory can be defined in YAML.

Supported extensions: `.yml`, `.yaml`, `.json`.

When a host belongs to multiple groups, group vars are merged in alphabetical order by group name. For example, `prod.yml` is merged before `web.yml`.

### Host Vars

Files in `host_vars/` are auto-loaded based on the host's `name` field.

```
host_vars/
  web1.yml      # loaded for host named "web1"
```

Host vars override group vars for the same keys.

### Play Vars and vars_files

Defined at the playbook root:

```yaml
vars:
  greeting: "hello"
  app:
    port: 8080

vars_files:
  - common.yml
  - secrets.yml
```

Play vars and vars_files are merged together (vars_files values overlay play vars), then applied as a single layer. Paths in `vars_files` are resolved relative to the playbook directory.

### Task Vars

Scoped to a single task:

```yaml
tasks:
  - name: Debug task
    vars:
      debug_mode: true
    copy:
      content: "debug={{ debug_mode }}"
      dest: /tmp/debug.txt
```

Task vars override play vars for that task only.

### Extra Vars

Passed via CLI, always have the highest precedence:

```bash
# Inline key=value (comma-separated for multiple)
dibra -config playbook.yaml -e app_port=9090
dibra -config playbook.yaml -e "app_port=9090,env=prod"

# From YAML file
dibra -config playbook.yaml -e @vars.yml
dibra -config playbook.yaml --extra-vars @production.yml
```

## Merge Strategy

Set `vars_merge` at the playbook root to control how overlapping keys are combined:

```yaml
vars_merge: replace   # default
```

| Strategy | Behavior |
|----------|----------|
| `replace` (default) | Higher-precedence value completely replaces lower-precedence value for the same key |
| `merge` | Maps are recursively merged (only conflicting sub-keys are overwritten); scalars and lists still replace |

### Example: replace vs merge

Given group vars `{app: {port: 8080, log: "info"}}` and play vars `{app: {port: 9090}}`:

- **replace**: result is `{app: {port: 9090}}` (group's `log` key is lost)
- **merge**: result is `{app: {port: 9090, log: "info"}}` (keys are merged)

## Template Interpolation

Variables are referenced using `{{ expression }}` syntax in string values. Templates are resolved recursively — if a resolved value contains more `{{ }}` expressions, they are resolved too (up to 10 iterations).

### Dot notation

```yaml
{{ app.name }}
{{ db.config.host }}
```

### Bracket notation

```yaml
{{ items[0] }}
{{ groups["web"][0] }}
```

### Mixed notation

```yaml
{{ hostvars.web1.app.port }}
{{ hostvars[inventory_hostname].greeting }}
```

### Template in variables

Variables can reference other variables:

```yaml
vars:
  app_name: "myapp"
  deploy_dir: "/opt/{{ app_name }}"

tasks:
  - name: Create dir
    file:
      path: "{{ deploy_dir }}"    # resolves to /opt/myapp
      state: directory
```

### Limitations

- No filters (e.g., `{{ name | upper }}`)
- No conditionals or loops in templates
- No arithmetic expressions
- Missing variables cause task failure (no silent fallback)

## Variable Namespaces

All variable layers are always visible under `vars.*`, regardless of which layer "won" in the flattened view:

| Namespace | Source |
|-----------|--------|
| `vars.group` | Group vars (merged across all groups) |
| `vars.host` | Host vars |
| `vars.play` | Play vars + vars_files |
| `vars.task` | Task vars |
| `vars.extra` | Extra vars |

This enables debugging and explicit source-specific lookups:

```yaml
# Access the flattened (merged) value
content: "port={{ app.port }}"

# Access a specific layer
content: "group_port={{ vars.group.app.port }}, play_port={{ vars.play.app.port }}"
```

## Magic Variables

These read-only variables are automatically available in every task:

| Variable | Type | Description |
|----------|------|-------------|
| `inventory_hostname` | string | Name of the current host |
| `group_names` | list | Groups the current host belongs to |
| `groups` | map | Map of group name → list of host names |
| `hostvars` | map | Map of host name → resolved variables for that host |

### inventory_hostname

```yaml
content: "Running on {{ inventory_hostname }}"
```

### group_names

```yaml
content: "Primary group: {{ group_names[0] }}"
```

### groups

```yaml
content: "Web servers: {{ groups.web[0] }}"
```

### hostvars

Access another host's variables (or your own):

```yaml
content: "Self port: {{ hostvars[inventory_hostname].app.port }}"
content: "Web1 port: {{ hostvars.web1.app.port }}"
```

`hostvars` contains the fully resolved (flattened) variables plus magic variables for each host.

## Directory Layout

```
playbook.yaml
defaults.yml          # referenced by vars_files
group_vars/
  web.yml             # auto-loaded for "web" group
  prod.yml            # auto-loaded for "prod" group
host_vars/
  web1.yml            # auto-loaded for host "web1"
  db1.yml             # auto-loaded for host "db1"
```

All paths are relative to the playbook directory.

## Complete Example

See `samples/variables/` for a working example with all features demonstrated.

### playbook.yaml

```yaml
vars_merge: merge

vars:
  app:
    name: "myapp"
    port: 8080
  deploy_dir: "/opt/{{ app.name }}"

vars_files:
  - defaults.yml

hosts:
  - name: web1
    host: 192.168.1.10
    user: deploy
    groups: [web, prod]

tasks:
  - name: Deploy config
    copy:
      content: |
        name={{ app.name }}
        port={{ app.port }}
        host={{ inventory_hostname }}
        group={{ group_names[0] }}
      dest: "{{ deploy_dir }}/config.ini"
```

### group_vars/web.yml

```yaml
app:
  port: 8080
max_connections: 200
```

### host_vars/web1.yml

```yaml
app:
  port: 9090
```

### CLI

```bash
# Use defaults
dibra -config playbook.yaml

# Override with extra vars
dibra -config playbook.yaml -e "app.port=3000"

# Override from file
dibra -config playbook.yaml -e @production.yml
```

## Error Handling

- **Missing variables**: Templates referencing undefined variables cause the task to fail immediately with a clear error message showing the unknown variable name.
- **Invalid vars_merge**: Only `replace` and `merge` are accepted; other values fail at load time.
- **Missing vars_files**: Referenced files that don't exist cause a load-time failure.
- **Missing group/host vars files**: These are optional — missing files are silently skipped (this is by design, matching Ansible behavior).

## Resolution Flow

For each host and each task, Dibra resolves variables through this process:

1. Load `group_vars/<group>.yml` for each group (sorted alphabetically, merged)
2. Load `host_vars/<hostname>.yml`
3. Merge play `vars` with `vars_files` content
4. Merge all layers using the configured merge strategy (group → host → play → task → extra)
5. Build magic variables (`inventory_hostname`, `group_names`, `groups`, `hostvars`)
6. Build namespace map (`vars.group`, `vars.host`, `vars.play`, `vars.task`, `vars.extra`)
7. Render all `{{ }}` templates in module arguments against the merged context
8. Execute the module with rendered arguments
