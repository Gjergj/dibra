// Composed Playbook — demonstrates CUE-only features
//
// This playbook uses composed types to deploy Caddy and a custom API service.
// The #InstallFromAptRepo and #DeployService types encapsulate multiple
// low-level tasks behind a simplified interface.
//
// Usage:
//   dibra -config samples/cue/playbook-composed.cue

package playbook

import (
	"strings"
	"list"
)

// ─── Composed type definitions ───

#Task: {
	name: string
	{[string]: _}
}

#InstallFromAptRepo: {
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

#DeployService: {
	service_name: string
	binary_src:   string
	binary_dest:  string | *"/usr/local/bin/\(service_name)"
	description:  string | *"Managed by dibra"
	user:         string | *"root"
	env:          {[string]: string} | *{}
	enabled:      bool | *true

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

	_tasks: [...#Task]
	_tasks: [
		{name: "Copy \(service_name) binary", copy: {src: binary_src, dest: binary_dest, mode: "0755"}},
		{name: "Deploy \(service_name) unit", copy: {content: _unit, dest: "/etc/systemd/system/\(service_name).service", mode: "0644"}},
		{let _enabled = enabled, name: "Start \(service_name)", systemd_service: {name: service_name, state: "started", enabled: _enabled, daemon_reload: true}},
	]
}

// ─── Instances ───

// Install Caddy from apt repo (3 tasks)
caddy: #InstallFromAptRepo & {
	key_url:       "https://dl.cloudsmith.io/public/caddy/stable/gpg.key"
	keyring:       "/usr/share/keyrings/caddy-stable-archive-keyring.gpg"
	repo:          "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
	repo_filename: "caddy-stable"
	packages:      ["caddy"]
}

// Deploy a custom API service (3 tasks)
// Cross-references caddy's repo_filename in the description
api: #DeployService & {
	service_name: "my-api"
	binary_src:   "./build/api"
	description:  "My API (reverse-proxied by \(caddy.repo_filename))"
	env: {
		PORT:     "8080"
		APP_ENV:  "production"
		LOG_LEVEL: "info"
	}
}

// ─── Host & tasks ───

hosts: [{
	name:            "webserver1"
	host:            "mytesthost"
	port:            22
	user:            "root"
	password:        "testpass"
	become:          true
	become_password: "testpass"
}]

// Mix one-off tasks with composed tasks
tasks: list.Concat([
	// Pre-requisites
	[{name: "Install prerequisites", apt: {name: ["curl", "jq"], state: "present", update_cache: true}}],
	// Caddy install (3 tasks from composed type)
	caddy._tasks,
	// API deploy (3 tasks from composed type)
	api._tasks,
	// Post-deploy verification
	[{name: "Verify API health", shell: {cmd: "curl -sf http://localhost:8080/health || echo 'not yet'"}}],
])
