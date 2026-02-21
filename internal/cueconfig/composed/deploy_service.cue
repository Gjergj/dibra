package composed

import (
	"strings"
	"list"
)

#Task: {
	name: string
	{[string]: _}
}

#InstallCaddy: {
	// User-facing fields
	install_method: "apt" | *"apt"
	version:        string | *"2"

	_versions: {
		"2": {
			key_url:       "https://dl.cloudsmith.io/public/caddy/stable/gpg.key"
			keyring:       "/usr/share/keyrings/caddy-stable-archive-keyring.gpg"
			repo:          "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
			repo_filename: "caddy-stable"
		}
	}

	let _config = _versions[version]

	tasks: [...#Task]
	if install_method == "apt" {
		tasks: [
			{
				name: "Install Caddy prerequisites"
				apt: {
					name: [
						"debian-keyring",
						"debian-archive-keyring",
						"apt-transport-https",
						"curl",
					]
					state:        "present"
					update_cache: true
				}
			},
			{
				name: "Add Caddy GPG key"
				apt_key: {
					keyring: _config.keyring
					state:   "present"
					url:     _config.key_url
				}
			},
			{
				name: "Add Caddy repository"
				apt_repository: {
					filename:     _config.repo_filename
					repo:         _config.repo
					update_cache: true
				}
			},
			{
				name: "Ensure Caddy repo file permissions"
				file: {
					path: "/etc/apt/sources.list.d/\(_config.repo_filename).list"
					mode: "0644"
				}
			},
			{
				name: "Ensure Caddy keyring permissions"
				file: {
					path: _config.keyring
					mode: "0644"
				}
			},
			{
				name: "Install Caddy"
				apt: {
					name:  "caddy"
					state: "present"
				}
			},
		]
	}
}
