//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerComposeV2ExecParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	root := "/tmp/dibra-compose-v2-exec"
	mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-compose-exec-*.json /tmp/.dibra-agent")
	mustRemote(t, client, "mkdir -p "+root)
	defer mustRemote(t, client, "rm -rf "+root)
	defer mustRemote(t, client, "docker rm -f dibra-exec-definition-web-1 >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker network rm dibra-exec-definition_default >/dev/null 2>&1 || true")

	t.Run("upstream-basic", func(t *testing.T) {
		project := root + "/upstream"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: alpine:3.18
    command: ["/bin/sh", "-c", "sleep 600"]
    stop_grace_period: 1s
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "exec-basic-up", project, "      state: present\n", "")

		command := runComposeExec(t, client, "command", project, `
      service: web
      command: /bin/sh -c "ls /"
`, "")
		if numberValue(command["rc"]) != 0 || !strings.Contains(command["stdout"].(string), "usr") || !strings.Contains(command["stdout"].(string), "etc") || command["stderr"] != "" {
			t.Fatalf("command result = %#v", command)
		}

		argv := runComposeExec(t, client, "argv-nonzero", project, `
      service: web
      argv:
        - /bin/sh
        - -c
        - whoami
      user: "1234"
`, "")
		if numberValue(argv["rc"]) != 1 || argv["stdout"] != "" || !strings.Contains(argv["stderr"].(string), "unknown uid 1234") {
			t.Fatalf("argv result = %#v", argv)
		}

		detached := runComposeExec(t, client, "detached", project, `
      service: web
      command: /bin/sh -c "sleep 1"
      detach: true
`, "")
		for _, field := range []string{"rc", "stdout", "stderr"} {
			if _, found := detached[field]; found {
				t.Fatalf("detached result returned %s: %#v", field, detached)
			}
		}

		input := runComposeExec(t, client, "stdin", project, `
      service: web
      command: /bin/sh -c "cat"
      stdin: This is a test
`, "")
		if numberValue(input["rc"]) != 0 || input["stdout"] != "This is a test" || input["stderr"] != "" {
			t.Fatalf("stdin result = %#v", input)
		}
	})

	t.Run("documented-command-and-argv", func(t *testing.T) {
		project := root + "/documented"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  foo:
    image: alpine:3.18
    command: ["sleep", "600"]
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "exec-docs-up", project, "      state: present\n", "")

		command := runComposeExec(t, client, "docs-command", project, `
      service: foo
      command: /bin/sh -c "pwd; ls -la"
      chdir: /root
`, "")
		if numberValue(command["rc"]) != 0 || !strings.HasPrefix(command["stdout"].(string), "/root") {
			t.Fatalf("documented command = %#v", command)
		}
		argv := runComposeExec(t, client, "docs-argv", project, `
      service: foo
      argv:
        - /bin/sh
        - -c
        - echo redirected >&2
      chdir: /root
`, "")
		if numberValue(argv["rc"]) != 0 || !strings.Contains(argv["stderr"].(string), "redirected") {
			t.Fatalf("documented argv = %#v", argv)
		}
	})

	t.Run("env-tty-privileged-and-stdin-controls", func(t *testing.T) {
		project := root + "/options"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  tool:
    image: alpine:3.18
    command: ["sleep", "600"]
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "exec-options-up", project, "      state: present\n", "")

		environment := runComposeExec(t, client, "env", project, `
      service: tool
      argv: ["/bin/sh", "-c", "printf '%s|%s' \"$ALPHA\" \"$BOOL_STRING\""]
      env:
        ALPHA: first
        BOOL_STRING: "true"
      tty: false
      privileged: true
`, "")
		if environment["stdout"] != "first|true" || numberValue(environment["rc"]) != 0 {
			t.Fatalf("env result = %#v", environment)
		}

		withoutNewline := runComposeExec(t, client, "stdin-no-newline", project, `
      service: tool
      argv: ["/bin/sh", "-c", "wc -c"]
      stdin: abc
      stdin_add_newline: false
      strip_empty_ends: false
      tty: false
`, "")
		if strings.TrimSpace(withoutNewline["stdout"].(string)) != "3" {
			t.Fatalf("stdin controls = %#v", withoutNewline)
		}
	})

	t.Run("scaled-service-index", func(t *testing.T) {
		project := root + "/scaled"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  worker:
    image: alpine:3.18
    command: ["sleep", "600"]
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "exec-scaled-up", project, "      state: present\n      scale:\n        worker: 2\n", "")
		secondID := strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker compose ps -q worker | sed -n '2p' | cut -c1-12"))
		if secondID == "" {
			t.Fatal("second service replica was not created")
		}
		result := runComposeExec(t, client, "index", project, `
      service: worker
      index: 2
      command: hostname
`, "")
		if strings.TrimSpace(result["stdout"].(string)) != secondID {
			t.Fatalf("index=2 selected %q, want %q", result["stdout"], secondID)
		}
	})

	t.Run("definition", func(t *testing.T) {
		runComposeDefinition(t, client, "exec-definition-up", "dibra-exec-definition", "sleep 600", "")
		result := runComposeExecDefinition(t, client, "definition", "dibra-exec-definition", `
      service: web
      command: /bin/sh -c "echo inline-exec"
`)
		if result["stdout"] != "inline-exec" || numberValue(result["rc"]) != 0 {
			t.Fatalf("definition result = %#v", result)
		}
		runComposeDefinition(t, client, "exec-definition-down", "dibra-exec-definition", "sleep 600", "      state: absent\n")
	})

	t.Run("files-env-files-profiles", func(t *testing.T) {
		project := root + "/project-options"
		mustRemote(t, client, "mkdir -p "+project)
		mustRemote(t, client, "printf 'EXEC_IMAGE=alpine:3.18\\n' > "+project+"/exec.env")
		mustRemote(t, client, "printf 'services:\\n  tool:\\n    image: ${EXEC_IMAGE}\\n    command: [sleep, \"600\"]\\n    profiles: [tools]\\n' > "+project+"/custom.yaml")
		defer composeDown(t, client, project, "exec-options")
		runComposeV2(t, client, "exec-project-options-up", project, `
      state: present
      project_name: exec-options
      files: [custom.yaml]
      env_files: [exec.env]
      profiles: [tools]
`, "")
		result := runComposeExec(t, client, "project-options", project, `
      project_name: exec-options
      files: [custom.yaml]
      env_files: [exec.env]
      profiles: [tools]
      service: tool
      command: /bin/sh -c "echo project-options"
`, "")
		if result["stdout"] != "project-options" {
			t.Fatalf("project options = %#v", result)
		}
	})

	t.Run("real-world-read-config", func(t *testing.T) {
		project := root + "/ghost"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  ghost:
    image: alpine:3.18
    command: ["/bin/sh", "-c", "mkdir -p /var/lib/ghost/current/core/shared/config; echo version=5 > /var/lib/ghost/current/core/shared/config/defaults.json; sleep 600"]
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "exec-ghost-up", project, "      state: present\n", "")
		result := runComposeExec(t, client, "ghost-config", project, `
      service: ghost
      command: cat /var/lib/ghost/current/core/shared/config/defaults.json
`, "")
		if !strings.Contains(result["stdout"].(string), "version=5") {
			t.Fatalf("real-world config read = %#v", result)
		}
	})

	t.Run("validation-and-runtime-errors", func(t *testing.T) {
		project := root + "/validation"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, "services:\n  web:\n    image: alpine:3.18\n")
		for name, arguments := range map[string]string{
			"missing-command": `
      service: web
`,
			"command-argv": `
      service: web
      command: echo command
      argv: [echo, argv]
`,
			"detach-stdin": `
      service: web
      command: cat
      detach: true
      stdin: ""
`,
			"non-string-env": `
      service: web
      command: env
      env:
        COUNT: 2
`,
		} {
			output := runComposeExecOutput(t, name, project, arguments)
			if !strings.Contains(output, "FAILED") {
				t.Fatalf("%s unexpectedly succeeded: %s", name, output)
			}
		}

		stopped := runComposeExec(t, client, "stopped-service", project, `
      service: web
      command: /bin/true
`, "")
		if stopped["failed"] == true || numberValue(stopped["rc"]) == 0 || !strings.Contains(stopped["stderr"].(string), "not running") {
			t.Fatalf("stopped service result = %#v", stopped)
		}
	})
}

func runComposeExec(t *testing.T, client *ssh.Client, name, project, arguments, taskOptions string) map[string]any {
	t.Helper()
	output := runComposeExecNamed(t, name, project, arguments, taskOptions)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, "/tmp/dibra-compose-exec-"+name+".json")
}

func runComposeExecOutput(t *testing.T, name, project, arguments string) string {
	t.Helper()
	return runComposeExecNamed(t, name, project, arguments, "")
}

func runComposeExecNamed(t *testing.T, name, project, arguments, taskOptions string) string {
	t.Helper()
	templatePath := writeResultTemplate(t, "exec_result")
	remotePath := "/tmp/dibra-compose-exec-" + name + ".json"
	playbook := playbookHeader + `
  - name: Execute command in Compose service
    community.docker.docker_compose_v2_exec:
      project_src: ` + project + `
` + arguments + `
    register: exec_result
` + taskOptions + `

  - name: Persist exec result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	return runPlaybook(t, playbook)
}

func runComposeExecDefinition(t *testing.T, client *ssh.Client, name, projectName, arguments string) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "exec_result")
	remotePath := "/tmp/dibra-compose-exec-" + name + ".json"
	playbook := playbookHeader + `
  - name: Execute command with inline Compose definition
    community.docker.docker_compose_v2_exec:
      project_name: ` + projectName + `
      definition:
        services:
          web:
            image: alpine:latest
            command: ["sh", "-c", "sleep 600"]
            stop_grace_period: 1s
` + arguments + `
    register: exec_result

  - name: Persist exec result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}
