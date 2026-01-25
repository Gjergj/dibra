//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_ServiceStart(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start cron service
    service:
      name: cron
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for starting stopped service")
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_ServiceStop(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Stop cron service
    service:
      name: cron
      state: stopped
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for stopping running service")
	}

	status := remoteExec(t, client, "systemctl is-active cron || echo inactive")
	if !strings.Contains(status, "inactive") {
		t.Errorf("Service should be inactive, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("systemctl start cron")
}

func TestPlaybook_ServiceRestart(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Restart cron service
    service:
      name: cron
      state: restarted
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for restart (always changes)")
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active after restart, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Restart should always report CHANGED")
	}
}

func TestPlaybook_ServiceEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Enable cron service
    service:
      name: cron
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for enabling service")
	}

	status := remoteExec(t, client, "systemctl is-enabled cron")
	if status != "enabled" {
		t.Errorf("Service should be enabled, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_ServiceDisable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl enable cron")

	playbook := playbookHeader + `
  - name: Disable cron service
    service:
      name: cron
      enabled: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for disabling service")
	}

	status := remoteExec(t, client, "systemctl is-enabled cron || echo disabled")
	if !strings.Contains(status, "disabled") {
		t.Errorf("Service should be disabled, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("systemctl enable cron")
}

func TestPlaybook_ServiceStartAndEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start and enable cron service
    service:
      name: cron
      state: started
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for starting and enabling")
	}

	activeStatus := remoteExec(t, client, "systemctl is-active cron")
	if activeStatus != "active" {
		t.Errorf("Service should be active, got: %s", activeStatus)
	}

	enabledStatus := remoteExec(t, client, "systemctl is-enabled cron")
	if enabledStatus != "enabled" {
		t.Errorf("Service should be enabled, got: %s", enabledStatus)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_ServiceStopAndDisable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")
	client.Run("systemctl enable cron")

	playbook := playbookHeader + `
  - name: Stop and disable cron service
    service:
      name: cron
      state: stopped
      enabled: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for stopping and disabling")
	}

	activeStatus := remoteExec(t, client, "systemctl is-active cron || echo inactive")
	if !strings.Contains(activeStatus, "inactive") {
		t.Errorf("Service should be inactive, got: %s", activeStatus)
	}

	enabledStatus := remoteExec(t, client, "systemctl is-enabled cron || echo disabled")
	if !strings.Contains(enabledStatus, "disabled") {
		t.Errorf("Service should be disabled, got: %s", enabledStatus)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("systemctl start cron")
	client.Run("systemctl enable cron")
}

func TestPlaybook_ServiceReloaded(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start ssh")

	playbook := playbookHeader + `
  - name: Reload ssh service
    service:
      name: ssh
      state: reloaded
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for reload (always changes)")
	}

	status := remoteExec(t, client, "systemctl is-active ssh")
	if status != "active" {
		t.Errorf("Service should be active after reload, got: %s", status)
	}
}

func TestPlaybook_ServiceNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Try to start non-existent service
    service:
      name: nonexistent_service_xyz
      state: started
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for non-existent service")
	}
	if !strings.Contains(output, "Could not find") {
		t.Error("Expected 'Could not find' error message")
	}
}

func TestPlaybook_ServiceNameRequired(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Service without name
    service:
      state: started
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when name is missing")
	}
}

func TestPlaybook_ServiceStateOrEnabledRequired(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Service without state or enabled
    service:
      name: cron
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when neither state nor enabled is specified")
	}
}

func TestPlaybook_ServiceInvalidState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Invalid state
    service:
      name: cron
      state: running
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for invalid state")
	}
}

func TestPlaybook_ServiceWithPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Find service with pattern
    service:
      name: cron
      pattern: cron
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when service is already running")
	}
}

func TestPlaybook_ServiceRestartWithSleep(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Restart cron with sleep
    service:
      name: cron
      state: restarted
      sleep: 1
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for restart with sleep")
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active after restart, got: %s", status)
	}
}

func TestPlaybook_ServiceImplicitServiceExtension(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start service without .service extension
    service:
      name: cron
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success without .service extension, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_ServiceExplicitServiceExtension(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron.service 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start service with .service extension
    service:
      name: cron.service
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with .service extension, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron.service")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_ServiceFullWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Full service management workflow
    service:
      name: cron
      state: started
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for full workflow")
	}

	activeStatus := remoteExec(t, client, "systemctl is-active cron")
	if activeStatus != "active" {
		t.Errorf("Service should be active, got: %s", activeStatus)
	}

	enabledStatus := remoteExec(t, client, "systemctl is-enabled cron")
	if enabledStatus != "enabled" {
		t.Errorf("Service should be enabled, got: %s", enabledStatus)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_ServiceUseSystemd(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start service with use=systemd
    service:
      name: cron
      state: started
      use: systemd
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with use=systemd, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_ServiceTimerUnit(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop apt-daily.timer 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start timer unit
    service:
      name: apt-daily.timer
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success for timer unit, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active apt-daily.timer")
	if status != "active" {
		t.Errorf("Timer should be active, got: %s", status)
	}

	client.Run("systemctl stop apt-daily.timer")
}

func TestPlaybook_ServiceEnableOnlyNoState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl disable cron 2>/dev/null || true")
	client.Run("systemctl stop cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Enable without changing state
    service:
      name: cron
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for enabling service")
	}

	activeStatus := remoteExec(t, client, "systemctl is-active cron || echo inactive")
	if !strings.Contains(activeStatus, "inactive") {
		t.Errorf("Service should still be inactive (only enabled, not started), got: %s", activeStatus)
	}

	enabledStatus := remoteExec(t, client, "systemctl is-enabled cron")
	if enabledStatus != "enabled" {
		t.Errorf("Service should be enabled, got: %s", enabledStatus)
	}

	client.Run("systemctl start cron")
}

func TestPlaybook_ServiceReloadStartsIfStopped(t *testing.T) {
	// This test uses a dummy service that supports reload.
	// We can't use ssh (would break our connection if stopped),
	// cron and rsyslog don't support reload.
	// Instead, we test the start-before-reload logic by creating a custom service.
	client := getClient(t)
	defer client.Close()

	// Create a simple service that supports reload
	unitContent := `[Unit]
Description=Test reload service

[Service]
Type=simple
ExecStart=/bin/sleep infinity
ExecReload=/bin/true

[Install]
WantedBy=multi-user.target`

	client.Run("cat > /etc/systemd/system/test-reload.service << 'EOF'\n" + unitContent + "\nEOF")
	client.Run("systemctl daemon-reload")
	client.Run("systemctl stop test-reload 2>/dev/null || true")
	defer client.Run("systemctl stop test-reload; rm -f /etc/systemd/system/test-reload.service; systemctl daemon-reload")

	playbook := playbookHeader + `
  - name: Reload stopped service (should start first)
    service:
      name: test-reload
      state: reloaded
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Reload should succeed: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active test-reload")
	if status != "active" {
		t.Errorf("Service should be active after reload (starts if stopped), got: %s", status)
	}
}
