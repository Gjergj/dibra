//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_CronAddJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add cron job
    cron:
      name: test job
      minute: "0"
      hour: "5"
      job: /usr/bin/echo hello
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for new cron job")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "#Ansible: test job") {
		t.Error("Crontab should contain Ansible marker")
	}
	if !strings.Contains(crontab, "0 5 * * * /usr/bin/echo hello") {
		t.Errorf("Crontab should contain job, got: %s", crontab)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronRemoveJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add cron job to remove
    cron:
      name: job to remove
      job: /usr/bin/echo remove
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove cron job
    cron:
      name: job to remove
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for job removal")
	}

	crontab := remoteExec(t, client, "crontab -l 2>/dev/null || echo empty")
	if strings.Contains(crontab, "job to remove") {
		t.Error("Crontab should not contain removed job")
	}

	output = runPlaybook(t, removePlaybook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronSpecialTime(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add reboot job
    cron:
      name: reboot job
      special_time: reboot
      job: /usr/bin/echo rebooted
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for special_time job")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@reboot /usr/bin/echo rebooted") {
		t.Errorf("Crontab should contain @reboot job, got: %s", crontab)
	}
}

func TestPlaybook_CronDisabled(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add disabled job
    cron:
      name: disabled job
      job: /usr/bin/echo disabled
      disabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "#* * * * * /usr/bin/echo disabled") {
		t.Errorf("Crontab should contain commented job, got: %s", crontab)
	}
}

func TestPlaybook_CronEnv(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add environment variable
    cron:
      name: PATH
      env: true
      job: /opt/bin
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for env variable")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, `PATH="/opt/bin"`) {
		t.Errorf("Crontab should contain PATH env, got: %s", crontab)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronEnvRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add env var
    cron:
      name: MYVAR
      env: true
      job: myvalue
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove env var
    cron:
      name: MYVAR
      env: true
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for env removal")
	}

	crontab := remoteExec(t, client, "crontab -l 2>/dev/null || echo empty")
	if strings.Contains(crontab, "MYVAR") {
		t.Error("Crontab should not contain removed env var")
	}
}

func TestPlaybook_CronFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	cronFile := "/etc/cron.d/goansible-test"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	playbook := playbookHeader + `
  - name: Create cron.d file
    cron:
      name: cron.d job
      minute: "30"
      hour: "2"
      user: root
      job: /usr/bin/echo crond
      cron_file: goansible-test
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for cron.d file")
	}

	if !remoteFileExists(t, client, cronFile) {
		t.Error("Cron.d file should exist")
	}

	content := remoteFileContent(t, client, cronFile)
	if !strings.Contains(content, "30 2 * * * root /usr/bin/echo crond") {
		t.Errorf("Cron.d file should contain job with user, got: %s", content)
	}
}

func TestPlaybook_CronFileRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	cronFile := "/etc/cron.d/goansible-test-remove"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	setupPlaybook := playbookHeader + `
  - name: Create cron.d file
    cron:
      name: temp job
      user: root
      job: /usr/bin/echo temp
      cron_file: goansible-test-remove
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove cron.d job
    cron:
      name: temp job
      cron_file: goansible-test-remove
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if remoteFileExists(t, client, cronFile) {
		t.Error("Cron.d file should be removed when empty")
	}
}

func TestPlaybook_CronUpdateJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add initial job
    cron:
      name: updatable job
      minute: "0"
      job: /usr/bin/echo original
`
	runPlaybook(t, setupPlaybook)

	updatePlaybook := playbookHeader + `
  - name: Update job
    cron:
      name: updatable job
      minute: "30"
      job: /usr/bin/echo updated
`
	output := runPlaybook(t, updatePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for job update")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "30 * * * * /usr/bin/echo updated") {
		t.Errorf("Crontab should contain updated job, got: %s", crontab)
	}
	if strings.Contains(crontab, "original") {
		t.Error("Crontab should not contain old job")
	}
}
