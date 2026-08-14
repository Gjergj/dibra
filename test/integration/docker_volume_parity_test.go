//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerVolumeParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	name := "dibra-volume-parity-basic"
	mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-volume-parity-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "volume_result")
	runVolume := func(testName, arguments, taskOptions string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-volume-parity-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Manage volume
    community.docker.docker_volume:
` + arguments + `
    register: volume_result
` + taskOptions + `

  - name: Persist volume result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s volume playbook failed: %s", testName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}
	volumeExists := func() bool {
		t.Helper()
		output := remoteExec(t, client, "docker volume inspect "+name+" >/dev/null 2>&1; echo $?")
		return strings.TrimSpace(output) == "0"
	}

	t.Run("create idempotent recreate and absent", func(t *testing.T) {
		created := runVolume("create", "      name: "+name+"\n", "")
		if created["changed"] != true || !volumeExists() {
			t.Fatalf("create = %#v exists=%t", created, volumeExists())
		}
		if volume, _ := created["volume"].(map[string]any); volume["Name"] != name {
			t.Fatalf("create volume = %#v", created["volume"])
		}

		idempotent := runVolume("idempotent", "      name: "+name+"\n", "")
		if idempotent["changed"] != false {
			t.Fatalf("idempotent = %#v", idempotent)
		}

		optionsChanged := runVolume("options-changed-same", "      name: "+name+"\n      recreate: options-changed\n", "")
		if optionsChanged["changed"] != false {
			t.Fatalf("options-changed same = %#v", optionsChanged)
		}

		always := runVolume("always", "      name: "+name+"\n      recreate: always\n", "")
		if always["changed"] != true || !volumeExists() {
			t.Fatalf("always = %#v exists=%t", always, volumeExists())
		}

		absent := runVolume("absent", "      name: "+name+"\n      state: absent\n", "")
		if absent["changed"] != true || volumeExists() {
			t.Fatalf("absent = %#v exists=%t", absent, volumeExists())
		}

		absentAgain := runVolume("absent-idempotent", "      name: "+name+"\n      state: absent\n", "")
		if absentAgain["changed"] != false || volumeExists() {
			t.Fatalf("absent idempotent = %#v exists=%t", absentAgain, volumeExists())
		}
	})

	t.Run("driver_options", func(t *testing.T) {
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
		opts := `      name: ` + name + `
      driver: local
      driver_options:
        type: tmpfs
        device: tmpfs
        o: size=100m,uid=1000
`
		created := runVolume("opts-create", opts, "")
		if created["changed"] != true {
			t.Fatalf("opts create = %#v", created)
		}
		idempotent := runVolume("opts-idempotent", opts, "")
		if idempotent["changed"] != false {
			t.Fatalf("opts idempotent = %#v", idempotent)
		}

		changedOpts := `      name: ` + name + `
      driver: local
      driver_options:
        type: tmpfs
        device: tmpfs
        o: size=200m,uid=1000
`
		unchanged := runVolume("opts-never", changedOpts, "")
		if unchanged["changed"] != false {
			t.Fatalf("opts never should leave mismatch unchanged = %#v", unchanged)
		}
		if got := strings.TrimSpace(remoteExec(t, client, "docker volume inspect --format '{{index .Options \"o\"}}' "+name)); got != "size=100m,uid=1000" {
			t.Fatalf("options after never = %q", got)
		}

		recreated := runVolume("opts-changed", changedOpts+"      recreate: options-changed\n", "")
		if recreated["changed"] != true {
			t.Fatalf("opts options-changed = %#v", recreated)
		}
		if got := strings.TrimSpace(remoteExec(t, client, "docker volume inspect --format '{{index .Options \"o\"}}' "+name)); got != "size=200m,uid=1000" {
			t.Fatalf("options after recreate = %q", got)
		}
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
	})

	t.Run("labels", func(t *testing.T) {
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
		created := runVolume("labels-create", `      name: `+name+`
      labels:
        ansible.test.1: hello
        ansible.test.2: world
`, "")
		if created["changed"] != true {
			t.Fatalf("labels create = %#v", created)
		}

		idempotent := runVolume("labels-idempotent", `      name: `+name+`
      labels:
        ansible.test.2: world
        ansible.test.1: hello
`, "")
		if idempotent["changed"] != false {
			t.Fatalf("labels idempotent = %#v", idempotent)
		}

		less := runVolume("labels-less", `      name: `+name+`
      labels:
        ansible.test.1: hello
`, "")
		if less["changed"] != false {
			t.Fatalf("labels less never = %#v", less)
		}

		lessChanged := runVolume("labels-less-options", `      name: `+name+`
      labels:
        ansible.test.1: hello
      recreate: options-changed
`, "")
		if lessChanged["changed"] != false {
			t.Fatalf("labels less options-changed = %#v", lessChanged)
		}

		more := runVolume("labels-more", `      name: `+name+`
      labels:
        ansible.test.1: hello
        ansible.test.3: ansible
`, "")
		if more["changed"] != false {
			t.Fatalf("labels more never = %#v", more)
		}

		moreChanged := runVolume("labels-more-options", `      name: `+name+`
      labels:
        ansible.test.1: hello
        ansible.test.3: ansible
      recreate: options-changed
`, "")
		if moreChanged["changed"] != true {
			t.Fatalf("labels more options-changed = %#v", moreChanged)
		}
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
	})

	t.Run("check and diff", func(t *testing.T) {
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
		predicted := runVolume("check-create", "      name: "+name+"\n", "    check_mode: true\n    diff: true\n")
		if predicted["changed"] != true || volumeExists() {
			t.Fatalf("check create = %#v exists=%t", predicted, volumeExists())
		}
		diff, _ := predicted["diff"].(map[string]any)
		before, _ := diff["before"].(map[string]any)
		after, _ := diff["after"].(map[string]any)
		if before["exists"] != false || after["exists"] != true {
			t.Fatalf("check create diff = %#v", predicted["diff"])
		}

		created := runVolume("real-create", "      name: "+name+"\n", "    diff: true\n")
		if created["changed"] != true || !volumeExists() {
			t.Fatalf("real create = %#v exists=%t", created, volumeExists())
		}

		checkAbsent := runVolume("check-absent", "      name: "+name+"\n      state: absent\n", "    check_mode: true\n    diff: true\n")
		if checkAbsent["changed"] != true || !volumeExists() {
			t.Fatalf("check absent = %#v exists=%t", checkAbsent, volumeExists())
		}
		mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
	})
}
