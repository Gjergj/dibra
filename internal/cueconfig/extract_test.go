package cueconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue"
)

func TestExtract_BasicPlaybook(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "192.168.1.10"
	port: 22
	user: "deploy"
	become: true
}]

tasks: [{
	name: "Test connectivity"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(cfg.Hosts))
	}
	h := cfg.Hosts[0]
	if h.Name != "web1" {
		t.Errorf("host name = %q, want %q", h.Name, "web1")
	}
	if h.Host != "192.168.1.10" {
		t.Errorf("host host = %q, want %q", h.Host, "192.168.1.10")
	}
	if h.Port != 22 {
		t.Errorf("host port = %d, want 22", h.Port)
	}
	if h.User != "deploy" {
		t.Errorf("host user = %q, want %q", h.User, "deploy")
	}
	if !h.Become {
		t.Error("host become = false, want true")
	}

	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(cfg.Tasks))
	}
	task := cfg.Tasks[0]
	if task.Name != "Test connectivity" {
		t.Errorf("task name = %q, want %q", task.Name, "Test connectivity")
	}
	if task.Ping == nil {
		t.Fatal("task.Ping is nil")
	}
}

func TestExtract_CopyModule(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Deploy config"
	copy: {
		content: "hello world"
		dest:    "/tmp/hello.txt"
		mode:    "0644"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(cfg.Tasks))
	}
	task := cfg.Tasks[0]
	if task.Copy == nil {
		t.Fatal("task.Copy is nil")
	}
	if task.Copy.Content != "hello world" {
		t.Errorf("copy content = %q, want %q", task.Copy.Content, "hello world")
	}
	if task.Copy.Dest != "/tmp/hello.txt" {
		t.Errorf("copy dest = %q, want %q", task.Copy.Dest, "/tmp/hello.txt")
	}
	if task.Copy.Mode != "0644" {
		t.Errorf("copy mode = %q, want %q", task.Copy.Mode, "0644")
	}
}

func TestExtract_ShellModule(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Run command"
	shell: {
		cmd: "echo hello"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Shell == nil {
		t.Fatal("task.Shell is nil")
	}
	if task.Shell.Cmd != "echo hello" {
		t.Errorf("shell cmd = %q, want %q", task.Shell.Cmd, "echo hello")
	}
}

func TestExtract_SystemdServiceModule(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Start nginx"
	systemd_service: {
		name:          "nginx"
		state:         "started"
		enabled:       true
		daemon_reload: true
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.SystemdService == nil {
		t.Fatal("task.SystemdService is nil")
	}
	if task.SystemdService.Name != "nginx" {
		t.Errorf("name = %q, want %q", task.SystemdService.Name, "nginx")
	}
	if task.SystemdService.State != "started" {
		t.Errorf("state = %q, want %q", task.SystemdService.State, "started")
	}
	if task.SystemdService.Enabled == nil || !*task.SystemdService.Enabled {
		t.Error("enabled should be true")
	}
	if !task.SystemdService.DaemonReload {
		t.Error("daemon_reload should be true")
	}
}

func TestExtract_MultipleTasks(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [
	{
		name: "Install packages"
		apt: {
			name: ["nginx", "curl"]
			state: "present"
			update_cache: true
		}
	},
	{
		name: "Create directory"
		file: {
			path:  "/opt/myapp"
			state: "directory"
			mode:  "0755"
		}
	},
	{
		name: "Deploy config"
		copy: {
			content: "hello"
			dest:    "/opt/myapp/config.txt"
		}
	},
]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(cfg.Tasks))
	}

	if cfg.Tasks[0].Apt == nil {
		t.Error("task 0: apt is nil")
	}
	if cfg.Tasks[1].File == nil {
		t.Error("task 1: file is nil")
	}
	if cfg.Tasks[1].File.Path != "/opt/myapp" {
		t.Errorf("task 1: file path = %q, want %q", cfg.Tasks[1].File.Path, "/opt/myapp")
	}
	if cfg.Tasks[1].File.State != "directory" {
		t.Errorf("task 1: file state = %q, want %q", cfg.Tasks[1].File.State, "directory")
	}
	if cfg.Tasks[2].Copy == nil {
		t.Error("task 2: copy is nil")
	}
}

func TestExtract_HostDefaults(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	h := cfg.Hosts[0]
	if h.Port != 22 {
		t.Errorf("default port = %d, want 22", h.Port)
	}
	if h.Become {
		t.Error("default become should be false")
	}
}

func TestExtract_Vars(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

vars: {
	app_name: "myapp"
	app_port: 8080
}

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Vars == nil {
		t.Fatal("vars is nil")
	}
	if cfg.Vars["app_name"] != "myapp" {
		t.Errorf("app_name = %v, want %q", cfg.Vars["app_name"], "myapp")
	}
	// JSON numbers are float64
	if cfg.Vars["app_port"] != float64(8080) {
		t.Errorf("app_port = %v, want 8080", cfg.Vars["app_port"])
	}
}

func TestExtract_VarsMerge(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

vars_merge: "merge"

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.VarsMerge != "merge" {
		t.Errorf("vars_merge = %q, want %q", cfg.VarsMerge, "merge")
	}
}

func TestExtract_DefaultVarsMerge(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.VarsMerge != "replace" {
		t.Errorf("default vars_merge = %q, want %q", cfg.VarsMerge, "replace")
	}
}

func TestExtract_Register(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name:     "Get uptime"
	register: "uptime_result"
	shell: {
		cmd: "uptime"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Register != "uptime_result" {
		t.Errorf("register = %q, want %q", task.Register, "uptime_result")
	}
}

func TestExtract_TaskVars(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Deploy with vars"
	vars: {
		port: "8080"
	}
	shell: {
		cmd: "echo hello"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Vars == nil {
		t.Fatal("task vars is nil")
	}
	if task.Vars["port"] != "8080" {
		t.Errorf("task var port = %v, want %q", task.Vars["port"], "8080")
	}
}

func TestExtract_ImportTasks(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Import setup"
	import_tasks: "setup/tasks.yaml"
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.ImportTasks == nil {
		t.Fatal("import_tasks is nil")
	}
	if task.ImportTasks.File != "setup/tasks.yaml" {
		t.Errorf("import_tasks.file = %q, want %q", task.ImportTasks.File, "setup/tasks.yaml")
	}
}

func TestExtract_IncludeTasks(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Include setup"
	include_tasks: {file: "setup/tasks.yaml"}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.IncludeTasks == nil {
		t.Fatal("include_tasks is nil")
	}
	if task.IncludeTasks.File != "setup/tasks.yaml" {
		t.Errorf("include_tasks.file = %q, want %q", task.IncludeTasks.File, "setup/tasks.yaml")
	}
}

func TestExtract_TaskMultipleModules(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Invalid"
	ping: {}
	shell: {cmd: "echo hi"}
}]
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for task with multiple modules")
	}
	if !strings.Contains(err.Error(), "multiple modules") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadValue_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "inventory.cue", `
package inventory

all: {
	vars: {env: "prod"}
	children: {
		web: {
			hosts: {
				web1: {host: "10.0.0.1"}
			}
		}
	}
}
`)

	v, err := LoadValue(filepath.Join(dir, "inventory.cue"))
	if err != nil {
		t.Fatalf("LoadValue failed: %v", err)
	}

	if !v.LookupPath(cue.ParsePath("all")).Exists() {
		t.Fatal("expected all to exist")
	}
}

func TestExtract_HostWithSSHKey(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name:         "web1"
	host:         "10.0.0.1"
	user:         "deploy"
	ssh_key_path: "~/.ssh/id_rsa"
	become:       true
	become_password: "sudo-pass"
	groups:       ["web", "prod"]
}]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	h := cfg.Hosts[0]
	if h.SSHKeyPath != "~/.ssh/id_rsa" {
		t.Errorf("ssh_key_path = %q, want %q", h.SSHKeyPath, "~/.ssh/id_rsa")
	}
	if h.BecomePassword != "sudo-pass" {
		t.Errorf("become_password = %q, want %q", h.BecomePassword, "sudo-pass")
	}
	if len(h.Groups) != 2 || h.Groups[0] != "web" || h.Groups[1] != "prod" {
		t.Errorf("groups = %v, want [web prod]", h.Groups)
	}
}

func TestExtract_MultipleHosts(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [
	{name: "web1", host: "10.0.0.1", user: "root"},
	{name: "web2", host: "10.0.0.2", user: "root", port: 2222},
	{name: "db1",  host: "10.0.1.1", user: "admin", become: true},
]

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(cfg.Hosts))
	}
	if cfg.Hosts[1].Port != 2222 {
		t.Errorf("web2 port = %d, want 2222", cfg.Hosts[1].Port)
	}
	if !cfg.Hosts[2].Become {
		t.Error("db1 become should be true")
	}
}

func TestExtract_RuntimeTemplateVars(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Show hostname"
	shell: {
		cmd: "echo {{ inventory_hostname }}"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Shell.Cmd != "echo {{ inventory_hostname }}" {
		t.Errorf("shell cmd = %q, want runtime template passthrough", task.Shell.Cmd)
	}
}

func TestExtract_LineinfileModule(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Add line to hosts"
	lineinfile: {
		path: "/etc/hosts"
		line: "192.168.1.100 myserver.local"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Lineinfile == nil {
		t.Fatal("task.Lineinfile is nil")
	}
	if task.Lineinfile.Path != "/etc/hosts" {
		t.Errorf("path = %q, want %q", task.Lineinfile.Path, "/etc/hosts")
	}
	if task.Lineinfile.Line != "192.168.1.100 myserver.local" {
		t.Errorf("line = %q", task.Lineinfile.Line)
	}
}

func TestExtract_DockerContainerModule(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Start container"
	docker_container: {
		name:  "my-app"
		image: "nginx:latest"
		state: "started"
		ports: ["8080:80"]
		env: {
			NODE_ENV: "production"
		}
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.DockerContainer == nil {
		t.Fatal("task.DockerContainer is nil")
	}
	dc := task.DockerContainer
	if dc.Name != "my-app" {
		t.Errorf("name = %q, want %q", dc.Name, "my-app")
	}
	if dc.Image != "nginx:latest" {
		t.Errorf("image = %q, want %q", dc.Image, "nginx:latest")
	}
	if len(dc.Ports) != 1 || dc.Ports[0] != "8080:80" {
		t.Errorf("ports = %v, want [8080:80]", dc.Ports)
	}
	if dc.Env["NODE_ENV"] != "production" {
		t.Errorf("env NODE_ENV = %q, want %q", dc.Env["NODE_ENV"], "production")
	}
}

func TestExtract_MultiFileCUE(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "hosts.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]
`)
	writeCUE(t, dir, "tasks.cue", `
package deploy

tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(cfg.Hosts))
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(cfg.Tasks))
	}
}

func TestExtract_AptDefaultState(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: [{
	name: "Install nginx"
	apt: {
		name: "nginx"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	task := cfg.Tasks[0]
	if task.Apt == nil {
		t.Fatal("task.Apt is nil")
	}
	if task.Apt.State != "present" {
		t.Errorf("apt state = %q, want %q (default)", task.Apt.State, "present")
	}
}

func TestIsCUEConfig(t *testing.T) {
	// Test .cue file extension
	if !IsCUEConfig("deploy.cue") {
		t.Error("deploy.cue should be detected as CUE config")
	}

	// Test .yaml extension
	if IsCUEConfig("playbook.yaml") {
		t.Error("playbook.yaml should not be detected as CUE config")
	}

	// Test directory with .cue files
	dir := t.TempDir()
	writeCUE(t, dir, "test.cue", `package test
x: 1
`)
	if !IsCUEConfig(dir) {
		t.Error("directory with .cue files should be detected")
	}

	// Test empty directory
	emptyDir := t.TempDir()
	if IsCUEConfig(emptyDir) {
		t.Error("empty directory should not be detected as CUE config")
	}

	// Test ambiguous directory (both .cue and .yaml) — IsCUEConfig returns false on error
	ambigDir := t.TempDir()
	writeCUE(t, ambigDir, "deploy.cue", `package deploy
x: 1
`)
	if err := os.WriteFile(filepath.Join(ambigDir, "playbook.yaml"), []byte("---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if IsCUEConfig(ambigDir) {
		t.Error("ambiguous directory should not be detected as CUE config")
	}
}

func TestDetectFormat(t *testing.T) {
	t.Run("CUE file extension", func(t *testing.T) {
		f, err := DetectFormat("deploy.cue")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatCUE {
			t.Errorf("got %v, want FormatCUE", f)
		}
	})

	t.Run("YAML file extension", func(t *testing.T) {
		f, err := DetectFormat("playbook.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatYAML {
			t.Errorf("got %v, want FormatYAML", f)
		}
	})

	t.Run("YML file extension", func(t *testing.T) {
		f, err := DetectFormat("playbook.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatYAML {
			t.Errorf("got %v, want FormatYAML", f)
		}
	})

	t.Run("directory with only CUE files", func(t *testing.T) {
		dir := t.TempDir()
		writeCUE(t, dir, "deploy.cue", `package deploy
x: 1
`)
		f, err := DetectFormat(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatCUE {
			t.Errorf("got %v, want FormatCUE", f)
		}
	})

	t.Run("directory with only YAML files", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "playbook.yaml"), []byte("---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		f, err := DetectFormat(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatYAML {
			t.Errorf("got %v, want FormatYAML", f)
		}
	})

	t.Run("empty directory defaults to YAML", func(t *testing.T) {
		dir := t.TempDir()
		f, err := DetectFormat(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatYAML {
			t.Errorf("got %v, want FormatYAML", f)
		}
	})

	t.Run("ambiguous directory errors", func(t *testing.T) {
		dir := t.TempDir()
		writeCUE(t, dir, "deploy.cue", `package deploy
x: 1
`)
		if err := os.WriteFile(filepath.Join(dir, "playbook.yaml"), []byte("---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := DetectFormat(dir)
		if err == nil {
			t.Fatal("expected error for ambiguous directory, got nil")
		}
		if !strings.Contains(err.Error(), "both .cue and .yaml") {
			t.Errorf("error message = %q, want it to mention 'both .cue and .yaml'", err.Error())
		}
	})

	t.Run("ambiguous directory with .yml", func(t *testing.T) {
		dir := t.TempDir()
		writeCUE(t, dir, "deploy.cue", `package deploy
x: 1
`)
		if err := os.WriteFile(filepath.Join(dir, "playbook.yml"), []byte("---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := DetectFormat(dir)
		if err == nil {
			t.Fatal("expected error for ambiguous directory with .yml, got nil")
		}
	})

	t.Run("non-existent path defaults to YAML", func(t *testing.T) {
		f, err := DetectFormat("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != FormatYAML {
			t.Errorf("got %v, want FormatYAML", f)
		}
	})

	t.Run("unknown extension non-directory defaults to YAML", func(t *testing.T) {
		tmp := t.TempDir()
		f := filepath.Join(tmp, "config.toml")
		if err := os.WriteFile(f, []byte("x = 1\n"), 0644); err != nil {
			t.Fatal(err)
		}
		fmt, err := DetectFormat(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fmt != FormatYAML {
			t.Errorf("got %v, want FormatYAML", fmt)
		}
	})
}

func TestExtract_Inventory(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

inventory: "inventory.yaml"

hosts: []
tasks: [{
	name: "Test"
	ping: {}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Inventory != "inventory.yaml" {
		t.Errorf("inventory = %q, want %q", cfg.Inventory, "inventory.yaml")
	}
}

func TestExtract_CUECrossReferences(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

let _port = 8080
let _host = "10.0.0.1"

hosts: [{
	name: "web1"
	host: _host
	user: "root"
}]

tasks: [{
	name: "Configure"
	copy: {
		content: "port=\(_port)"
		dest:    "/etc/myapp.conf"
	}
}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Hosts[0].Host != "10.0.0.1" {
		t.Errorf("host = %q, want %q", cfg.Hosts[0].Host, "10.0.0.1")
	}
	if cfg.Tasks[0].Copy.Content != "port=8080" {
		t.Errorf("content = %q, want %q", cfg.Tasks[0].Copy.Content, "port=8080")
	}
}

func TestExtract_ComposedDeployService(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

import (
	"strings"
	"list"
)

#Task: {
	name: string
	{[string]: _}
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

	tasks: [...]
	tasks: [
		{name: "Copy \(service_name) binary", copy: {src: binary_src, dest: binary_dest, mode: "0755"}},
		{name: "Deploy \(service_name) unit", copy: {content: _unit, dest: "/etc/systemd/system/\(service_name).service", mode: "0644"}},
		{let _enabled = enabled, name: "Start \(service_name)", systemd_service: {name: service_name, state: "started", enabled: _enabled, daemon_reload: true}},
	]
}

api: #DeployService & {
	service_name: "my-api"
	binary_src:   "./build/api"
	env: {
		PORT: "8080"
	}
}

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: api.tasks
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Tasks) != 3 {
		t.Fatalf("expected 3 tasks from DeployService, got %d", len(cfg.Tasks))
	}

	// Task 0: Copy binary
	if cfg.Tasks[0].Copy == nil {
		t.Fatal("task 0 should be copy")
	}
	if cfg.Tasks[0].Copy.Src != "./build/api" {
		t.Errorf("task 0 copy src = %q", cfg.Tasks[0].Copy.Src)
	}
	if cfg.Tasks[0].Copy.Dest != "/usr/local/bin/my-api" {
		t.Errorf("task 0 copy dest = %q", cfg.Tasks[0].Copy.Dest)
	}

	// Task 1: Deploy unit file
	if cfg.Tasks[1].Copy == nil {
		t.Fatal("task 1 should be copy")
	}
	if cfg.Tasks[1].Copy.Dest != "/etc/systemd/system/my-api.service" {
		t.Errorf("task 1 copy dest = %q", cfg.Tasks[1].Copy.Dest)
	}
	if !strings.Contains(cfg.Tasks[1].Copy.Content, "ExecStart=/usr/local/bin/my-api") {
		t.Errorf("unit content should contain ExecStart, got:\n%s", cfg.Tasks[1].Copy.Content)
	}
	if !strings.Contains(cfg.Tasks[1].Copy.Content, "Environment=PORT=8080") {
		t.Errorf("unit content should contain PORT env var, got:\n%s", cfg.Tasks[1].Copy.Content)
	}

	// Task 2: Start service
	if cfg.Tasks[2].SystemdService == nil {
		t.Fatal("task 2 should be systemd_service")
	}
	if cfg.Tasks[2].SystemdService.Name != "my-api" {
		t.Errorf("task 2 systemd name = %q", cfg.Tasks[2].SystemdService.Name)
	}
}

func TestExtract_ComposedMixedTasks(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

import "list"

#Task: {
	name: string
	{[string]: _}
}

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

// Mix composed + one-off tasks
tasks: list.Concat([
	[{name: "Update cache", apt: {name: ["curl"], state: "present", update_cache: true}}],
	[{name: "Verify caddy", shell: {cmd: "caddy version"}}],
])
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 1 (update cache)  + 1 (verify) = 1
	if len(cfg.Tasks) != 2 {
		t.Fatalf("expected 5 tasks, got %d", len(cfg.Tasks))
	}

	if cfg.Tasks[0].Apt == nil {
		t.Error("task 0 should be apt")
	}
	if cfg.Tasks[1].Shell == nil {
		t.Error("task 1 should be shell")
	}
}

func TestExtract_CrossReferenceBetweenComposed(t *testing.T) {
	dir := t.TempDir()
	writeCUE(t, dir, "deploy.cue", `
package deploy

import (
	"strings"
	"list"
)

#Task: {
	name: string
	{[string]: _}
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

	tasks: [...]
	tasks: [
		{name: "Copy \(service_name) binary", copy: {src: binary_src, dest: binary_dest, mode: "0755"}},
		{name: "Deploy \(service_name) unit", copy: {content: _unit, dest: "/etc/systemd/system/\(service_name).service", mode: "0644"}},
		{let _enabled = enabled, name: "Start \(service_name)", systemd_service: {name: service_name, state: "started", enabled: _enabled, daemon_reload: true}},
	]
}

db_config: {
	host: "db.internal"
	port: 5432
}

api: #DeployService & {
	service_name: "my-api"
	binary_src:   "./build/api"
	env: {
		PORT:   "8080"
		DB_URL: "postgres://\(db_config.host):\(db_config.port)/mydb"
	}
}

frontend: #DeployService & {
	service_name: "my-frontend"
	binary_src:   "./build/frontend"
	env: {
		API_URL: "http://localhost:\(api.env.PORT)"
	}
}

hosts: [{
	name: "web1"
	host: "10.0.0.1"
	user: "root"
}]

tasks: list.Concat([api.tasks, frontend.tasks])
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 3 (api) + 3 (frontend) = 6
	if len(cfg.Tasks) != 6 {
		t.Fatalf("expected 6 tasks, got %d", len(cfg.Tasks))
	}

	// Verify cross-references resolved correctly
	// API unit should contain DB_URL with db_config values
	apiUnit := cfg.Tasks[1].Copy.Content
	if !strings.Contains(apiUnit, "Environment=DB_URL=postgres://db.internal:5432/mydb") {
		t.Errorf("api unit should contain resolved DB_URL, got:\n%s", apiUnit)
	}

	// Frontend unit should contain API_URL referencing api's PORT
	frontendUnit := cfg.Tasks[4].Copy.Content
	if !strings.Contains(frontendUnit, "Environment=API_URL=http://localhost:8080") {
		t.Errorf("frontend unit should contain resolved API_URL, got:\n%s", frontendUnit)
	}
}

func TestLoad_SingleFile(t *testing.T) {
	// Create two .cue files in the same directory with the same package but
	// conflicting fields. Loading a single file should only load that file.
	dir := t.TempDir()
	writeCUE(t, dir, "a.cue", `
package deploy

hosts: [{name: "web1", host: "10.0.0.1", user: "root"}]
tasks: [{name: "Ping", ping: {}}]
`)
	writeCUE(t, dir, "b.cue", `
package deploy

hosts: [{name: "db1", host: "10.0.0.2", user: "root"}]
tasks: [{name: "Shell", shell: {cmd: "echo hi"}}]
`)

	// Loading the directory would fail (conflicting hosts/tasks).
	// Loading a single file should work.
	cfg, err := Load(filepath.Join(dir, "a.cue"))
	if err != nil {
		t.Fatalf("Load single file failed: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Name != "web1" {
		t.Errorf("expected host web1, got %v", cfg.Hosts)
	}
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].Ping == nil {
		t.Errorf("expected ping task, got %v", cfg.Tasks)
	}

	cfg2, err := Load(filepath.Join(dir, "b.cue"))
	if err != nil {
		t.Fatalf("Load single file b.cue failed: %v", err)
	}
	if len(cfg2.Hosts) != 1 || cfg2.Hosts[0].Name != "db1" {
		t.Errorf("expected host db1, got %v", cfg2.Hosts)
	}
}

func TestLoad_SingleFileWithImports(t *testing.T) {
	// Verify that a single .cue file with imports (strings, list) loads correctly.
	dir := t.TempDir()
	writeCUE(t, dir, "composed.cue", `
package deploy

import "list"

_a: [{name: "First", ping: {}}]
_b: [{name: "Second", shell: {cmd: "echo hi"}}]

hosts: [{name: "web1", host: "10.0.0.1", user: "root"}]
tasks: list.Concat([_a, _b])
`)

	cfg, err := Load(filepath.Join(dir, "composed.cue"))
	if err != nil {
		t.Fatalf("Load single file with imports failed: %v", err)
	}
	if len(cfg.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Name != "First" {
		t.Errorf("task 0 name = %q, want %q", cfg.Tasks[0].Name, "First")
	}
	if cfg.Tasks[1].Shell == nil {
		t.Error("task 1 shell is nil")
	}
}

func TestLoad_Samples(t *testing.T) {
	// Validate that all sample CUE files load without errors.
	samples := []struct {
		path     string
		minTasks int
		minHosts int
	}{
		{"../../samples/cue/playbook.cue", 5, 1},
		{"../../samples/cue/playbook-alloy.cue", 3, 1},
		{"../../samples/cue/playbook-caddy.cue", 4, 1},
		{"../../samples/cue/playbook-copy-test.cue", 3, 1},
		{"../../samples/cue/playbook-docker-test.cue", 5, 1},
		{"../../samples/cue/playbook-file-copy-test.cue", 6, 1},
		{"../../samples/cue/playbook-file-test.cue", 5, 1},
		{"../../samples/cue/playbook-composed.cue", 2, 1},
		{"../../samples/cue/variables/playbook.cue", 4, 1},
	}

	for _, s := range samples {
		name := filepath.Base(s.path)
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(s.path)
			if err != nil {
				t.Fatalf("Load(%s) failed: %v", s.path, err)
			}
			if len(cfg.Tasks) < s.minTasks {
				t.Errorf("expected at least %d tasks, got %d", s.minTasks, len(cfg.Tasks))
			}
			if len(cfg.Hosts) < s.minHosts {
				t.Errorf("expected at least %d hosts, got %d", s.minHosts, len(cfg.Hosts))
			}
		})
	}
}

// writeCUE creates a .cue file in the given directory.
func writeCUE(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}
