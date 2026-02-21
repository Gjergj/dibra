// Composed Playbook — demonstrates CUE-only features
//
// This playbook uses composed types to deploy Caddy and a custom API service.
// The #InstallCaddy types encapsulate multiple
// low-level tasks behind a simplified interface.
//
// Usage:
//   dibra -config samples/cue/playbook-composed.cue

package playbook

import (
	"list"
)

// ─── Composed type definitions ───

#Task: {
	name: string
	{[string]: _}
}

// ─── Instances ───



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
	// Post-deploy verification
	[{name: "Verify API health", shell: {cmd: "curl -sf http://localhost:8080/health || echo 'not yet'"}}],
])
