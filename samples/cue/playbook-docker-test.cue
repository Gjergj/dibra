package playbook

hosts: [{
	name:            "testhost"
	host:            "mytesthost"
	port:            22
	user:            "testuser"
	password:        "testpass"
	become:          true
	become_password: "testpass"
}]

tasks: [
	{name: "Ping", ping: {}},
	{
		name: "Pull and Start alpine container"
		docker_container: {
			name:    "test-alpine"
			image:   "alpine:latest"
			state:   "started"
			command: ["sleep", "300"]
			pull:    true
		}
	},
	{
		name: "Verify container is running (using shell)"
		shell: {cmd: "docker ps | grep test-alpine"}
	},
	{
		name: "Remove alpine container"
		docker_container: {
			name:       "test-alpine"
			state:      "absent"
			force_kill: true
		}
	},
	{
		name: "Verify container is gone"
		shell: {cmd: "docker ps -a | grep test-alpine || echo 'Gone'"}
	},
]
