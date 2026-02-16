package composed

import (
	"strings"
	"list"
)

#Task: {
	name: string
	{[string]: _}
}

#DeployService: {
	// User-facing fields
	service_name: string
	binary_src:   string
	binary_dest:  string | *"/usr/local/bin/\(service_name)"
	description:  string | *"Managed by dibra"
	user:         string | *"root"
	config_src?:  string
	config_dest?: string
	env:          {[string]: string} | *{}
	enabled:      bool | *true

	// Computed: systemd unit content
	let _env_lines = [ for k, v in env {"Environment=\(k)=\(v)"}]
	let _unit = strings.Join(list.Concat([
		[
			"[Unit]",
			"Description=\(description)",
			"After=network.target",
			"",
			"[Service]",
			"Type=simple",
			"User=\(user)",
			"ExecStart=\(binary_dest)",
			"Restart=on-failure",
		],
		_env_lines,
		[
			"",
			"[Install]",
			"WantedBy=multi-user.target",
		],
	]), "\n")

	// Decomposition: flat task list
	_tasks: [...#Task]
	_tasks: [
		{name: "Copy \(service_name) binary", copy: {src: binary_src, dest: binary_dest, mode: "0755"}},
		if config_src != _|_ if config_dest != _|_ {
			name: "Copy \(service_name) config"
			copy: {src: config_src, dest: config_dest, mode: "0644"}
		},
		{name: "Deploy \(service_name) unit", copy: {content: _unit, dest: "/etc/systemd/system/\(service_name).service", mode: "0644"}},
		{let _enabled = enabled, name: "Start \(service_name)", systemd_service: {name: service_name, state: "started", enabled: _enabled, daemon_reload: true}},
	]
}

#InstallFromAptRepo: {
	// User-facing fields
	key_url:       string
	keyring:       string
	repo:          string
	repo_filename: string
	packages:      [...string]

	_tasks: [...#Task]
	_tasks: [
		{name: "Add GPG key for \(repo_filename)", apt_key: {url: key_url, "keyring": keyring}},
		{name: "Add \(repo_filename) repository", apt_repository: {"repo": repo, filename: repo_filename, update_cache: true}},
		{name: "Install \(repo_filename) packages", apt: {name: packages, state: "present"}},
	]
}

#DeployDockerStack: {
	// User-facing fields
	stack_name:   string
	compose_src:  string
	compose_dest: string | *"/opt/\(stack_name)/docker-compose.yml"
	compose_dir:  string | *"/opt/\(stack_name)"

	_tasks: [...#Task]
	_tasks: [
		{name: "Create \(stack_name) directory", file: {path: compose_dir, state: "directory", mode: "0755"}},
		{name: "Deploy \(stack_name) compose file", copy: {src: compose_src, dest: compose_dest, mode: "0644"}},
		{name: "Deploy \(stack_name) stack", docker_compose: {project_src: compose_dir, state: "present"}},
	]
}

#ConfigureNginxSite: {
	// User-facing fields
	site_name:    string
	config_src:   string
	config_dest:  string | *"/etc/nginx/sites-available/\(site_name)"
	enabled_link: string | *"/etc/nginx/sites-enabled/\(site_name)"

	_tasks: [...#Task]
	_tasks: [
		{name: "Deploy \(site_name) nginx config", copy: {src: config_src, dest: config_dest, mode: "0644"}},
		{name: "Enable \(site_name) site", file: {path: enabled_link, src: config_dest, state: "link"}},
		{name: "Reload nginx for \(site_name)", service: {name: "nginx", state: "reloaded"}},
	]
}
