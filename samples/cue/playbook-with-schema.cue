package playbook

import "dibra.dev/schema"

vars: {
    ssh_password: "!bw:tmpemail.xyz_secrets/password"
    ssh_host:     "!bw:tmpemail.xyz_secrets/host"
    ssh_user:     "!bw:tmpemail.xyz_secrets/user"
}

hosts: [
    schema.#Host & {
        name:            "webserver1"
        host:            "{{ ssh_host }}"
        port:            22
        user:            "{{ ssh_user }}"
        password:        "{{ ssh_password }}"
        become:          true
        become_password: "{{ ssh_password }}"
    },
]

tasks: [
    schema.#Task & {name: "Update apt cache", apt: {update_cache: true, cache_valid_time: 3600}},
    schema.#Task & {name: "Install curl", apt: {name: ["curl"], state: "present"}},
]
