package playbook

vars: {
	// Secrets fetched from Bitwarden
	ssh_password: "!bw:tmpemail.xyz_secrets/password"
	ssh_host:     "!bw:tmpemail.xyz_secrets/host"
	ssh_port:     "!bw:tmpemail.xyz_secrets/port"
	ssh_user:     "!bw:tmpemail.xyz_secrets/user"
	// Normal variables
	http_port: 8080
	app_env:   "production"
}

hosts: [{
	name:            "webserver1"
	host:            "{{ ssh_host }}"
	port:            22
	user:            "{{ ssh_user }}"
	password:        "{{ ssh_password }}"
	become:          true
	become_password: "{{ ssh_password }}"
}]

tasks: [
	{name: "Update apt cache", apt: {update_cache: true, cache_valid_time: 3600}},
	{name: "Install curl", apt: {name: ["curl"], state: "present"}},
	{name: "Remove btop", apt: {name: "btop", state: "absent"}},
	{name: "Ensure htop is at latest version", apt: {name: "htop", state: "latest"}},
	{name: "Remove unused packages", apt: {autoremove: true}},
]
