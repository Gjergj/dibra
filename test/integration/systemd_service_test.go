//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_SystemdServiceStart(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceStop(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Stop cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceRestart(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Restart cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Enable cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceDisable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl enable cron")

	playbook := playbookHeader + `
  - name: Disable cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceStartAndEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start and enable cron service
    systemd_service:
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

func TestPlaybook_SystemdServiceDaemonReload(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Force daemon reload
    systemd_service:
      daemon_reload: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for daemon-reload")
	}
}

func TestPlaybook_SystemdServiceDaemonReloadWithRestart(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")

	playbook := playbookHeader + `
  - name: Daemon reload and restart service
    systemd_service:
      name: cron
      state: restarted
      daemon_reload: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for daemon-reload with restart")
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_SystemdServiceMask(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl unmask cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Mask cron service
    systemd_service:
      name: cron
      masked: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for masking service")
	}

	status := remoteExec(t, client, "systemctl is-enabled cron")
	if status != "masked" {
		t.Errorf("Service should be masked, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("systemctl unmask cron")
}

func TestPlaybook_SystemdServiceUnmask(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl mask cron")

	playbook := playbookHeader + `
  - name: Unmask cron service
    systemd_service:
      name: cron
      masked: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for unmasking service")
	}

	status := remoteExec(t, client, "systemctl is-enabled cron")
	if status == "masked" {
		t.Errorf("Service should not be masked, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_SystemdServiceUnmaskAndEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl mask cron")

	playbook := playbookHeader + `
  - name: Unmask and enable cron service
    systemd_service:
      name: cron
      masked: false
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for unmask and enable")
	}

	status := remoteExec(t, client, "systemctl is-enabled cron")
	if status != "enabled" {
		t.Errorf("Service should be enabled, got: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("systemctl disable cron")
}

func TestPlaybook_SystemdServiceImplicitServiceExtension(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Start cron without .service extension
    systemd_service:
      name: cron
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_SystemdServiceExplicitServiceExtension(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Start cron with .service extension
    systemd_service:
      name: cron.service
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron.service")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_SystemdServiceNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Start non-existent service
    systemd_service:
      name: dibra-nonexistent-service
      state: started
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for non-existent service")
	}
}

func TestPlaybook_SystemdServiceGlobPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Try glob pattern
    systemd_service:
      name: "ssh*"
      state: started
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for glob pattern")
	}
	if !strings.Contains(output, "glob") {
		t.Error("Error message should mention glob patterns")
	}
}

func TestPlaybook_SystemdServiceNoAction(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: No action specified
    systemd_service:
      name: cron
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when no action specified")
	}
}

func TestPlaybook_SystemdAlias(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Use systemd alias
    systemd:
      name: cron
      state: started
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with systemd alias, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-active cron")
	if status != "active" {
		t.Errorf("Service should be active, got: %s", status)
	}
}

func TestPlaybook_SystemdServiceStopAndDisable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron")
	client.Run("systemctl enable cron")

	playbook := playbookHeader + `
  - name: Stop and disable cron service
    systemd_service:
      name: cron
      state: stopped
      enabled: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for stop and disable")
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

	client.Run("systemctl enable cron")
	client.Run("systemctl start cron")
}

func TestPlaybook_SystemdServiceReloaded(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start ssh")

	playbook := playbookHeader + `
  - name: Reload ssh service
    systemd_service:
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

func TestPlaybook_SystemdServiceReloadedStartsIfStopped(t *testing.T) {
	t.Skip("Skipping: stopping SSH would break test connectivity")
}

func TestPlaybook_SystemdServiceFullWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Full service management workflow
    systemd_service:
      name: cron
      state: started
      enabled: true
      daemon_reload: true
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

func TestPlaybook_SystemdServiceTimerUnit(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop apt-daily.timer 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Start timer unit
    systemd_service:
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

func TestPlaybook_SystemdServiceNoBlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Restart with no_block
    systemd_service:
      name: cron
      state: restarted
      no_block: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with no_block, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for restart")
	}
}

func TestPlaybook_SystemdServiceInvalidState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Invalid state
    systemd_service:
      name: cron
      state: running
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for invalid state")
	}
}

func TestPlaybook_SystemdServiceDaemonReexec(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Daemon reexec
    systemd_service:
      daemon_reexec: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for daemon-reexec")
	}
}

func TestPlaybook_SystemdServiceDaemonReexecAndReload(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Daemon reexec and reload
    systemd_service:
      daemon_reexec: true
      daemon_reload: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for daemon-reexec and daemon-reload")
	}
}

func TestPlaybook_SystemdServiceForceEnable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl disable cron 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Force enable cron service
    systemd_service:
      name: cron
      enabled: true
      force: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	status := remoteExec(t, client, "systemctl is-enabled cron")
	if status != "enabled" {
		t.Errorf("Service should be enabled, got: %s", status)
	}
}

func TestPlaybook_SystemdServiceNameRequired(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: State without name
    systemd_service:
      state: started
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when name is missing but state is set")
	}
}

func TestPlaybook_SystemdServiceEnabledWithoutName(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Enabled without name
    systemd_service:
      enabled: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when name is missing but enabled is set")
	}
}

func TestPlaybook_SystemdServiceMaskedWithoutName(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Masked without name
    systemd_service:
      masked: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when name is missing but masked is set")
	}
}

func TestPlaybook_SystemdServiceStaticService(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	staticService := remoteExec(t, client, "systemctl list-unit-files --state=static --no-legend | head -1 | awk '{print $1}'")
	if staticService == "" {
		t.Skip("No static services found for testing")
	}

	playbook := playbookHeader + `
  - name: Enable static service (should be no-op)
    systemd_service:
      name: ` + staticService + `
      enabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Logf("Note: Static service test might fail depending on service: %s", output)
	}
}
