//go:build integration

package integration

import (
	"strings"
	"testing"
)

func resetUFW(t *testing.T, client interface{ Run(string) (string, string, error) }) {
	client.Run("ufw --force reset")
	client.Run("ufw --force disable")
}

func TestPlaybook_UFWEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH before enabling
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for enabling UFW")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "Status: active") {
		t.Errorf("UFW should be active, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 1 {
		t.Error("Expected idempotent on second run for enable")
	}
}

func TestPlaybook_UFWDisable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	// Must allow SSH before enabling UFW, otherwise SSH connections get blocked
	client.Run("ufw allow 22/tcp")
	client.Run("ufw --force enable")

	playbook := playbookHeader + `
  - name: Disable UFW
    ufw:
      state: disabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for disabling UFW")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "inactive") {
		t.Errorf("UFW should be inactive, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWReload(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	client.Run("ufw allow 22/tcp")
	client.Run("ufw --force enable")

	playbook := playbookHeader + `
  - name: Reload UFW
    ufw:
      state: reloaded
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for reloading UFW")
	}
}

func TestPlaybook_UFWReset(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	client.Run("ufw allow 8080/tcp")

	playbook := playbookHeader + `
  - name: Reset UFW
    ufw:
      state: reset
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for resetting UFW")
	}
}

func TestPlaybook_UFWDefaultIncoming(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	// Need to enable UFW to see defaults in status output
	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Set default deny incoming
    ufw:
      default: deny
      direction: incoming

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "deny (incoming)") {
		t.Errorf("Expected default deny incoming, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 0 {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWDefaultOutgoing(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	// Need to enable UFW to see defaults in status output
	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Set default allow outgoing
    ufw:
      default: allow
      direction: outgoing

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "allow (outgoing)") {
		t.Errorf("Expected default allow outgoing, got: %s", status)
	}
}

func TestPlaybook_UFWAllowPort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for new rule")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "22/tcp") && !strings.Contains(status, "22") {
		t.Errorf("Expected SSH rule, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	changeCount := strings.Count(output, "CHANGED")
	if changeCount > 0 {
		t.Errorf("Expected no changes on second run (idempotent), got %d changes", changeCount)
	}
}

func TestPlaybook_UFWAllowHTTPHTTPS(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow HTTP
    ufw:
      rule: allow
      port: "80"
      proto: tcp

  - name: Allow HTTPS
    ufw:
      rule: allow
      port: "443"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "80") {
		t.Errorf("Expected HTTP rule, got: %s", status)
	}
	if !strings.Contains(status, "443") {
		t.Errorf("Expected HTTPS rule, got: %s", status)
	}
}

func TestPlaybook_UFWDenyPort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Deny telnet
    ufw:
      rule: deny
      port: "23"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for deny rule")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "23") || !strings.Contains(status, "DENY") {
		t.Errorf("Expected deny rule for port 23, got: %s", status)
	}
}

func TestPlaybook_UFWRejectPort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Reject auth port
    ufw:
      rule: reject
      port: "113"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "113") || !strings.Contains(status, "REJECT") {
		t.Errorf("Expected reject rule for port 113, got: %s", status)
	}
}

func TestPlaybook_UFWLimitSSH(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Rate limit SSH
    ufw:
      rule: limit
      port: "22"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for limit rule")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "LIMIT") {
		t.Errorf("Expected LIMIT rule, got: %s", status)
	}
}

func TestPlaybook_UFWFromIP(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow from specific IP
    ufw:
      rule: allow
      from_ip: 192.168.1.100

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for from_ip rule")
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "192.168.1.100") {
		t.Errorf("Expected rule with source IP, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 0 {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWFromSubnet(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow from subnet
    ufw:
      rule: allow
      from_ip: 10.0.0.0/8
      port: "3306"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "10.0.0.0/8") {
		t.Errorf("Expected rule with subnet, got: %s", status)
	}
}

func TestPlaybook_UFWToIP(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow to specific destination
    ufw:
      rule: allow
      to_ip: 192.168.1.50
      port: "8080"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "192.168.1.50") || !strings.Contains(status, "8080") {
		t.Errorf("Expected rule with destination IP and port, got: %s", status)
	}
}

func TestPlaybook_UFWDeleteRule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	setupPlaybook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Add rule to delete
    ufw:
      rule: allow
      port: "9999"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	runPlaybook(t, setupPlaybook)

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "9999") {
		t.Fatalf("Setup failed - rule not created: %s", status)
	}

	deletePlaybook := playbookHeader + `
  - name: Delete rule
    ufw:
      rule: allow
      port: "9999"
      proto: tcp
      delete: true
`
	output := runPlaybook(t, deletePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for delete")
	}

	status = remoteExec(t, client, "ufw status")
	if strings.Contains(status, "9999") {
		t.Errorf("Rule should be deleted, got: %s", status)
	}

	output = runPlaybook(t, deletePlaybook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second delete (idempotent)")
	}
}

func TestPlaybook_UFWLogging(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	// Must enable UFW first, then set logging - logging state can only be
	// verified when UFW is active
	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled

  - name: Enable logging
    ufw:
      logging: "on"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "Logging: on") {
		t.Errorf("Expected logging on, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 0 {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWLoggingLevel(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Set logging to medium
    ufw:
      logging: medium

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "medium") {
		t.Errorf("Expected logging medium, got: %s", status)
	}
}

func TestPlaybook_UFWLoggingOff(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	// Enable UFW with SSH allowed, and enable logging
	client.Run("ufw allow 22/tcp")
	client.Run("ufw logging on")
	client.Run("ufw --force enable")

	playbook := playbookHeader + `
  - name: Disable logging
    ufw:
      logging: "off"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for disabling logging")
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "Logging: off") {
		t.Errorf("Expected logging off, got: %s", status)
	}
}

func TestPlaybook_UFWRuleWithLog(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Reject with logging
    ufw:
      rule: reject
      port: "113"
      proto: tcp
      log: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_UFWDirectionIn(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow incoming on port 5000
    ufw:
      rule: allow
      direction: in
      port: "5000"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "5000") {
		t.Errorf("Expected rule for port 5000, got: %s", status)
	}
}

func TestPlaybook_UFWDirectionOut(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow outgoing on port 53
    ufw:
      rule: allow
      direction: out
      port: "53"
      proto: udp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_UFWPortRange(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow port range
    ufw:
      rule: allow
      port: "6000:6100"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "6000:6100") {
		t.Errorf("Expected port range rule, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 0 {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWFromPort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow from specific source port
    ufw:
      rule: allow
      from_port: "1024"
      to_port: "80"
      proto: tcp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_UFWPolicyAlias(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Set policy using alias
    ufw:
      policy: reject
      direction: incoming

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "reject (incoming)") {
		t.Errorf("Expected reject incoming, got: %s", status)
	}
}

func TestPlaybook_UFWFromAlias(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow using 'from' alias
    ufw:
      rule: allow
      from: 172.16.0.0/12

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "172.16.0.0/12") {
		t.Errorf("Expected rule with from alias, got: %s", status)
	}
}

func TestPlaybook_UFWSrcAlias(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH from anywhere
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow using 'src' alias
    ufw:
      rule: allow
      src: 192.168.100.0/24
      port: "3000"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "192.168.100.0/24") {
		t.Errorf("Expected rule with src alias, got: %s", status)
	}
}

func TestPlaybook_UFWDestAlias(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow using 'dest' alias
    ufw:
      rule: allow
      dest: 10.10.10.10
      port: "443"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "10.10.10.10") {
		t.Errorf("Expected rule with dest alias, got: %s", status)
	}
}

func TestPlaybook_UFWCompleteSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Set default deny incoming
    ufw:
      default: deny
      direction: incoming

  - name: Set default allow outgoing
    ufw:
      default: allow
      direction: outgoing

  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow HTTP
    ufw:
      rule: allow
      port: "80"
      proto: tcp

  - name: Allow HTTPS
    ufw:
      rule: allow
      port: "443"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status verbose")
	if !strings.Contains(status, "Status: active") {
		t.Error("UFW should be active")
	}
	if !strings.Contains(status, "deny (incoming)") {
		t.Error("Should have deny incoming policy")
	}
	if !strings.Contains(status, "allow (outgoing)") {
		t.Error("Should have allow outgoing policy")
	}
	if !strings.Contains(status, "22") {
		t.Error("Should have SSH rule")
	}
	if !strings.Contains(status, "80") {
		t.Error("Should have HTTP rule")
	}
	if !strings.Contains(status, "443") {
		t.Error("Should have HTTPS rule")
	}

	output = runPlaybook(t, playbook)
	changeCount := strings.Count(output, "CHANGED")
	if changeCount > 0 {
		t.Errorf("Expected idempotent on second run, got %d changes", changeCount)
	}
}

func TestPlaybook_UFWDatabaseServerSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow PostgreSQL from app servers
    ufw:
      rule: allow
      from_ip: 10.0.1.0/24
      port: "5432"
      proto: tcp

  - name: Allow MySQL from app servers
    ufw:
      rule: allow
      from_ip: 10.0.1.0/24
      port: "3306"
      proto: tcp

  - name: Set default deny
    ufw:
      default: deny
      direction: incoming

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "5432") {
		t.Error("Should have PostgreSQL rule")
	}
	if !strings.Contains(status, "3306") {
		t.Error("Should have MySQL rule")
	}
	if !strings.Contains(status, "10.0.1.0/24") {
		t.Error("Should restrict to app servers subnet")
	}
}

func TestPlaybook_UFWWebServerSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow HTTP
    ufw:
      rule: allow
      port: "80"
      proto: tcp

  - name: Allow HTTPS
    ufw:
      rule: allow
      port: "443"
      proto: tcp

  - name: Rate limit SSH
    ufw:
      rule: limit
      port: "22"
      proto: tcp
      delete: true

  - name: Apply SSH rate limiting
    ufw:
      rule: limit
      port: "22"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "LIMIT") {
		t.Errorf("Expected LIMIT rule for SSH, got: %s", status)
	}
}

func TestPlaybook_UFWValidationErrors(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	tests := []struct {
		name     string
		playbook string
		errMsg   string
	}{
		{
			name: "Invalid state",
			playbook: playbookHeader + `
  - name: Invalid state
    ufw:
      state: invalid
`,
			errMsg: "state must be one of",
		},
		{
			name: "Invalid logging",
			playbook: playbookHeader + `
  - name: Invalid logging
    ufw:
      logging: invalid
`,
			errMsg: "logging must be one of",
		},
		{
			name: "Invalid default",
			playbook: playbookHeader + `
  - name: Invalid default
    ufw:
      default: invalid
`,
			errMsg: "default/policy must be one of",
		},
		{
			name: "Invalid rule",
			playbook: playbookHeader + `
  - name: Invalid rule
    ufw:
      rule: invalid
`,
			errMsg: "rule must be one of",
		},
		{
			name: "Invalid proto",
			playbook: playbookHeader + `
  - name: Invalid proto
    ufw:
      rule: allow
      port: "80"
      proto: invalid
`,
			errMsg: "proto must be one of",
		},
		{
			name: "No command",
			playbook: playbookHeader + `
  - name: No command
    ufw:
      from_ip: 192.168.1.1
`,
			errMsg: "is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := runPlaybook(t, tt.playbook)
			if !strings.Contains(output, "FAILED") {
				t.Errorf("Expected failure for %s, got: %s", tt.name, output)
			}
		})
	}
}

func TestPlaybook_UFWMultipleRules(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow port 8001
    ufw:
      rule: allow
      port: "8001"
      proto: tcp

  - name: Allow port 8002
    ufw:
      rule: allow
      port: "8002"
      proto: tcp

  - name: Allow port 8003
    ufw:
      rule: allow
      port: "8003"
      proto: tcp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	for _, port := range []string{"8001", "8002", "8003"} {
		if !strings.Contains(status, port) {
			t.Errorf("Expected rule for port %s, got: %s", port, status)
		}
	}

	output = runPlaybook(t, playbook)
	if strings.Count(output, "CHANGED") > 0 {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_UFWUDP(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow DNS UDP
    ufw:
      rule: allow
      port: "53"
      proto: udp

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "53/udp") {
		t.Errorf("Expected UDP rule, got: %s", status)
	}
}

func TestPlaybook_UFWAnyProtocol(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH
    ufw:
      rule: allow
      port: "22"
      proto: tcp

  - name: Allow from trusted host any protocol
    ufw:
      rule: allow
      from_ip: 192.168.50.1

  - name: Enable UFW
    ufw:
      state: enabled
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "ufw status")
	if !strings.Contains(status, "192.168.50.1") {
		t.Errorf("Expected rule for trusted host, got: %s", status)
	}
}

func TestPlaybook_UFWDeleteNonexistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetUFW(t, client)
	defer resetUFW(t, client)

	playbook := playbookHeader + `
  - name: Delete nonexistent rule
    ufw:
      rule: allow
      port: "12345"
      proto: tcp
      delete: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success (no-op), got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when deleting nonexistent rule")
	}
}
