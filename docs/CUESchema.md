# CUE Schema Usage

Dibra ships CUE schema definitions in `cue/schema/`. These can be used to
validate playbooks and inventory files in your own CUE projects.

Because schemas are not embedded yet, you need to vendor the schema directory
into your project and import it using your module path.

## Quick Start

1. Copy `cue/schema/` into your project (or vendor it as `cue/schema/`).
2. Ensure your project has a `cue.mod/module.cue` with a module path.
3. Import the schema using `<module>/cue/schema`.
4. Compose the schema types with `&` to validate values.

Example `cue.mod/module.cue`:

```cue
module: "example.com/project"
```

## Playbook Example

```cue
package deploy

import "example.com/project/cue/schema"

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

import "example.com/project/cue/schema"

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

Replace `example.com/project` with the module path defined in your
`cue.mod/module.cue`.
