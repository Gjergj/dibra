# CUE Inventory

Dibra can load inventory data from `.cue` files or directories. This unlocks
type-safe host definitions, reuse via `let` bindings, and CUE-native composition
for group hierarchies.

Run with:

```bash
dibra -config playbook.cue -i inventory.cue
dibra validate -config playbook.cue -i inventory.cue
dibra init ./deploy
```

You can also point to a directory containing multiple `.cue` files that share
the same `package` name.

## Format

Inventory uses the same structure as YAML inventory: `all`, `children`, `hosts`,
and `vars`. Top-level groups without an `all` wrapper are treated as children
of `all` automatically.

```cue
package inventory

all: {
    vars: {
        env: "production"
    }
    children: {
        webservers: {
            hosts: {
                web1: {
                    host: "192.168.1.10"
                    user: "deploy"
                    port: 22
                    become: true
                }
                web2: {
                    host: "192.168.1.20"
                    user: "deploy"
                }
            }
            vars: {
                http_port: 80
            }
        }
        dbservers: {
            hosts: {
                db1: {
                    host: "192.168.2.10"
                }
            }
            vars: {
                db_port: 5432
            }
        }
    }
}
```

## Notes

- The `-i` / `--inventory` flag accepts `.cue` files or directories.
- The playbook `inventory:` field can also reference `.cue` paths.
- Variable resolution, templates, and secret handling work the same as YAML.
