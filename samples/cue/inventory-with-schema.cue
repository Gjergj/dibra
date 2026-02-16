package inventory

import "example.com/project/cue/schema"

inventory: schema.#Inventory & {
    all: {
        vars: {env: "dev"}
        children: {
            webservers: {
                hosts: {
                    web1: {
                        host: "10.0.0.10"
                        user: "deploy"
                    }
                }
                vars: {http_port: 8080}
            }
        }
    }
}

all: inventory.all
