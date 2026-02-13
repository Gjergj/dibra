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
	if !strings.Contains(crontab, "#Dibra: test job") {
		t.Error("Crontab should contain Dibra marker")
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

	cronFile := "/etc/cron.d/dibra-test"
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
      cron_file: dibra-test
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

	cronFile := "/etc/cron.d/dibra-test-remove"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	setupPlaybook := playbookHeader + `
  - name: Create cron.d file
    cron:
      name: temp job
      user: root
      job: /usr/bin/echo temp
      cron_file: dibra-test-remove
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove cron.d job
    cron:
      name: temp job
      cron_file: dibra-test-remove
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

func TestPlaybook_CronFullSchedule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Test all time fields: minute, hour, day, month, weekday
	playbook := playbookHeader + `
  - name: Add job with full schedule
    cron:
      name: full schedule job
      minute: "30"
      hour: "2"
      day: "15"
      month: "6"
      weekday: "0"
      job: /usr/bin/echo full-schedule
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for full schedule job")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "30 2 15 6 0 /usr/bin/echo full-schedule") {
		t.Errorf("Crontab should contain full schedule, got: %s", crontab)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronStepValue(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Test step value: every 15 minutes
	playbook := playbookHeader + `
  - name: Run every 15 minutes
    cron:
      name: step job
      minute: "*/15"
      job: /usr/bin/echo every-15-min
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "*/15 * * * * /usr/bin/echo every-15-min") {
		t.Errorf("Crontab should contain step value, got: %s", crontab)
	}
}

func TestPlaybook_CronRangeValue(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Test range: business hours (9-17)
	playbook := playbookHeader + `
  - name: Run during business hours
    cron:
      name: business hours job
      minute: "0"
      hour: "9-17"
      job: /usr/bin/echo business-hours
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "0 9-17 * * * /usr/bin/echo business-hours") {
		t.Errorf("Crontab should contain range value, got: %s", crontab)
	}
}

func TestPlaybook_CronListValue(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Test list: Mon, Wed, Fri (1,3,5)
	playbook := playbookHeader + `
  - name: Run on specific weekdays
    cron:
      name: weekday list job
      minute: "0"
      hour: "9"
      weekday: "1,3,5"
      job: /usr/bin/echo mon-wed-fri
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "0 9 * * 1,3,5 /usr/bin/echo mon-wed-fri") {
		t.Errorf("Crontab should contain list value, got: %s", crontab)
	}
}

func TestPlaybook_CronSpecialTimeDaily(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add daily job
    cron:
      name: daily job
      special_time: daily
      job: /usr/bin/echo daily-task
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@daily /usr/bin/echo daily-task") {
		t.Errorf("Crontab should contain @daily job, got: %s", crontab)
	}
}

func TestPlaybook_CronSpecialTimeWeekly(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add weekly job
    cron:
      name: weekly job
      special_time: weekly
      job: /usr/bin/echo weekly-task
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@weekly /usr/bin/echo weekly-task") {
		t.Errorf("Crontab should contain @weekly job, got: %s", crontab)
	}
}

func TestPlaybook_CronSpecialTimeHourly(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add hourly job
    cron:
      name: hourly job
      special_time: hourly
      job: /usr/bin/echo hourly-task
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@hourly /usr/bin/echo hourly-task") {
		t.Errorf("Crontab should contain @hourly job, got: %s", crontab)
	}
}

func TestPlaybook_CronBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("rm -f /tmp/crontab-backup-*")

	// First add a job
	setupPlaybook := playbookHeader + `
  - name: Add initial job
    cron:
      name: backup test job
      job: /usr/bin/echo initial
`
	runPlaybook(t, setupPlaybook)

	// Now update with backup
	playbook := playbookHeader + `
  - name: Update job with backup
    cron:
      name: backup test job
      job: /usr/bin/echo updated
      backup: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for job update")
	}

	// Verify backup was created
	backupCheck := remoteExec(t, client, "ls /tmp/crontab-backup-* 2>/dev/null | wc -l")
	if backupCheck == "0" {
		t.Error("Backup file should exist")
	}
}

func TestPlaybook_CronReenableJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// First add a disabled job
	disabledPlaybook := playbookHeader + `
  - name: Add disabled job
    cron:
      name: toggle job
      job: /usr/bin/echo toggle
      disabled: true
`
	runPlaybook(t, disabledPlaybook)

	// Verify it's disabled
	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "#* * * * * /usr/bin/echo toggle") {
		t.Errorf("Job should be disabled (commented), got: %s", crontab)
	}

	// Re-enable the job
	enabledPlaybook := playbookHeader + `
  - name: Re-enable job
    cron:
      name: toggle job
      job: /usr/bin/echo toggle
      disabled: false
`
	output := runPlaybook(t, enabledPlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for re-enabling job")
	}

	// Verify it's enabled
	crontab = remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "* * * * * /usr/bin/echo toggle") {
		t.Errorf("Job should be enabled, got: %s", crontab)
	}
	if strings.Contains(crontab, "#* * * * * /usr/bin/echo toggle") {
		t.Error("Job should not be commented out")
	}
}

func TestPlaybook_CronMultipleJobs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add first job
    cron:
      name: multi job 1
      minute: "0"
      job: /usr/bin/echo job1

  - name: Add second job
    cron:
      name: multi job 2
      minute: "15"
      job: /usr/bin/echo job2

  - name: Add third job
    cron:
      name: multi job 3
      minute: "30"
      job: /usr/bin/echo job3
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")

	if !strings.Contains(crontab, "#Dibra: multi job 1") {
		t.Error("Crontab should contain job 1 marker")
	}
	if !strings.Contains(crontab, "#Dibra: multi job 2") {
		t.Error("Crontab should contain job 2 marker")
	}
	if !strings.Contains(crontab, "#Dibra: multi job 3") {
		t.Error("Crontab should contain job 3 marker")
	}
	if !strings.Contains(crontab, "0 * * * * /usr/bin/echo job1") {
		t.Error("Crontab should contain job1")
	}
	if !strings.Contains(crontab, "15 * * * * /usr/bin/echo job2") {
		t.Error("Crontab should contain job2")
	}
	if !strings.Contains(crontab, "30 * * * * /usr/bin/echo job3") {
		t.Error("Crontab should contain job3")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronUpdateEnv(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Add initial env var
	setupPlaybook := playbookHeader + `
  - name: Add env var
    cron:
      name: MYPATH
      env: true
      job: /usr/local/bin
`
	runPlaybook(t, setupPlaybook)

	// Verify initial value
	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, `MYPATH="/usr/local/bin"`) {
		t.Errorf("Crontab should contain initial MYPATH, got: %s", crontab)
	}

	// Update env var value
	updatePlaybook := playbookHeader + `
  - name: Update env var
    cron:
      name: MYPATH
      env: true
      job: /opt/custom/bin
`
	output := runPlaybook(t, updatePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for env update")
	}

	// Verify updated value
	crontab = remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, `MYPATH="/opt/custom/bin"`) {
		t.Errorf("Crontab should contain updated MYPATH, got: %s", crontab)
	}
	if strings.Contains(crontab, "/usr/local/bin") {
		t.Error("Crontab should not contain old value")
	}
}

func TestPlaybook_CronEnvInsertAfter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Add first env var
	setupPlaybook := playbookHeader + `
  - name: Add first env var
    cron:
      name: FIRST_VAR
      env: true
      job: first_value
`
	runPlaybook(t, setupPlaybook)

	// Add second env var after first
	playbook := playbookHeader + `
  - name: Add env var after FIRST_VAR
    cron:
      name: SECOND_VAR
      env: true
      job: second_value
      insertafter: FIRST_VAR
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	// Both vars should exist
	if !strings.Contains(crontab, `FIRST_VAR="first_value"`) {
		t.Error("Crontab should contain FIRST_VAR")
	}
	if !strings.Contains(crontab, `SECOND_VAR="second_value"`) {
		t.Error("Crontab should contain SECOND_VAR")
	}
}

func TestPlaybook_CronFileMultipleJobs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	cronFile := "/etc/cron.d/dibra-multi"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	playbook := playbookHeader + `
  - name: Add first cron.d job
    cron:
      name: crond job 1
      minute: "0"
      hour: "1"
      user: root
      job: /usr/bin/echo crond-job1
      cron_file: dibra-multi

  - name: Add second cron.d job
    cron:
      name: crond job 2
      minute: "30"
      hour: "2"
      user: root
      job: /usr/bin/echo crond-job2
      cron_file: dibra-multi
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if !remoteFileExists(t, client, cronFile) {
		t.Error("Cron.d file should exist")
	}

	content := remoteFileContent(t, client, cronFile)
	if !strings.Contains(content, "0 1 * * * root /usr/bin/echo crond-job1") {
		t.Errorf("Cron.d file should contain job1, got: %s", content)
	}
	if !strings.Contains(content, "30 2 * * * root /usr/bin/echo crond-job2") {
		t.Errorf("Cron.d file should contain job2, got: %s", content)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronUserSpecific(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Use testuser which exists in the container
	client.Run("crontab -u testuser -r 2>/dev/null || true")
	defer client.Run("crontab -u testuser -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add job for testuser
    cron:
      name: testuser job
      minute: "45"
      hour: "3"
      user: testuser
      job: /usr/bin/echo testuser-task
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user-specific job")
	}

	// Verify in testuser's crontab
	crontab := remoteExec(t, client, "crontab -u testuser -l")
	if !strings.Contains(crontab, "#Dibra: testuser job") {
		t.Errorf("Testuser crontab should contain marker, got: %s", crontab)
	}
	if !strings.Contains(crontab, "45 3 * * * /usr/bin/echo testuser-task") {
		t.Errorf("Testuser crontab should contain job, got: %s", crontab)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronDailyLogRotation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Real-world example: daily log rotation
	playbook := playbookHeader + `
  - name: Daily log rotation
    cron:
      name: rotate application logs
      special_time: daily
      job: /usr/sbin/logrotate /etc/logrotate.d/myapp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@daily /usr/sbin/logrotate /etc/logrotate.d/myapp") {
		t.Errorf("Crontab should contain log rotation job, got: %s", crontab)
	}
}

func TestPlaybook_CronDatabaseBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	// Real-world example: database backup at 2 AM
	playbook := playbookHeader + `
  - name: Nightly database backup
    cron:
      name: database backup
      minute: "0"
      hour: "2"
      job: /usr/local/bin/backup-db.sh >> /var/log/backup.log 2>&1
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "0 2 * * * /usr/local/bin/backup-db.sh >> /var/log/backup.log 2>&1") {
		t.Errorf("Crontab should contain backup job with redirection, got: %s", crontab)
	}
}
