# CUE Schema Usage

Dibra ships CUE schema definitions in the binary under the import path
`dibra.dev/schema`. These can be used to validate playbooks and inventory
files in your own CUE projects.

For editor autocomplete without relying on the CUE CLI, run:

```bash
dibra schema install
```

This writes the embedded schema files to `cue.mod/pkg/dibra.dev/schema` so the
CUE language server can load them.

To upgrade the vendored schema to match your installed `dibra` version, run:

```bash
dibra schema upgrade
```

## Quick Start

1. Import the schema from `dibra.dev/schema`.
2. Compose the schema types with `&` to validate values.

If you used `dibra init`, the schema is installed automatically (disable with
`dibra init -schema=false`).

Use `dibra schema status` to check the installed schema version and whether it
matches the running dibra binary.

## Playbook Example

```cue
package deploy

import "dibra.dev/schema"

hosts: [
    schema.#Host & {
        name: "web1"
        host: "192.168.1.10"
        user: "root"
    },
]

tasks: [
    schema.#Task & {
        name: "Ping"
        ping: {}
    },
]
```

## Inventory Example

```cue
package inventory

import "dibra.dev/schema"

inventory: schema.#Inventory & {
    all: {
        hosts: {
            web1: {
                host: "192.168.1.10"
                user: "root"
            }
        }
    }
}

all: inventory.all
```

The dibra binary ships the schema under the import path `dibra.dev/schema`.
