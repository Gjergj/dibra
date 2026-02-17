# CUE Schema Usage

Dibra ships CUE schema definitions in the binary under the import path
`dibra.dev/schema`. These can be used to validate playbooks and inventory
files in your own CUE projects.

## Quick Start

1. Import the schema from `dibra.dev/schema`.
2. Compose the schema types with `&` to validate values.

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
