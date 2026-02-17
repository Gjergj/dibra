//go:build integration

package integration

import (
	"strings"
	"testing"
)

const cuePlaybookHeader = `package deploy

hosts: [{
	name: "testhost"
	host: "localhost"
	port: 2222
	user: "root"
	password: "rootpass"
	become: true
}]

tasks: `

func TestCUE_Ping(t *testing.T) {
	playbook := cuePlaybookHeader + `[
	{
		name: "Ping test"
		ping: {}
	},
]`

	output := runPlaybookCUE(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes for ping")
	}
}

func TestCUE_CopyContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("rm -f /tmp/dibra-cue-copy")
	playbook := cuePlaybookHeader + `[
	{
		name: "Copy content"
		copy: {
			content: "hello from cue"
			dest: "/tmp/dibra-cue-copy"
			mode: "0644"
		}
	},
]`

	output := runPlaybookCUE(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Fatalf("Expected CHANGED, got: %s", output)
	}
	content := remoteFileContent(t, client, "/tmp/dibra-cue-copy")
	if content != "hello from cue" {
		t.Fatalf("Expected content, got: %q", content)
	}

	output = runPlaybookCUE(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

func TestCUE_FileDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("rm -rf /tmp/dibra-cue-dir")
	playbook := cuePlaybookHeader + `[
	{
		name: "Create directory"
		file: {
			path: "/tmp/dibra-cue-dir"
			state: "directory"
			mode: "0755"
		}
	},
]`

	output := runPlaybookCUE(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Fatalf("Expected CHANGED, got: %s", output)
	}
	if !remoteDirExists(t, client, "/tmp/dibra-cue-dir") {
		t.Fatal("Expected directory to exist")
	}

	output = runPlaybookCUE(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

func TestCUE_SystemdService(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop dibra-cue-test.service || true")
	client.Run("rm -f /etc/systemd/system/dibra-cue-test.service")

	playbook := cuePlaybookHeader + `[
	{
		name: "Install unit"
		copy: {
			content: "[Unit]\nDescription=Dibra CUE test\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=/bin/sleep infinity\n\n[Install]\nWantedBy=multi-user.target\n"
			dest: "/etc/systemd/system/dibra-cue-test.service"
			mode: "0644"
		}
	},
	{
		name: "Start service"
		systemd_service: {
			name: "dibra-cue-test"
			state: "started"
			enabled: true
			daemon_reload: true
		}
	},
]`

	output := runPlaybookCUE(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Fatalf("Expected CHANGED, got: %s", output)
	}
	if strings.Contains(remoteExec(t, client, "systemctl is-active dibra-cue-test"), "inactive") {
		t.Fatal("Expected service to be active")
	}

	output = runPlaybookCUE(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}
