//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerComposeV2RunParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	root := "/tmp/dibra-compose-v2-run"
	mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-compose-run-*.json /tmp/.dibra-agent")
	mustRemote(t, client, "mkdir -p "+root)
	defer mustRemote(t, client, "rm -rf "+root)
	defer mustRemote(t, client, "docker rm -f dibra-run-meta dibra-run-port-service dibra-run-port-publish >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker network rm documented_default input_default metadata_default no-deps_default project-options_default build_default ports_default dibra-run-definition_default >/dev/null 2>&1 || true")

	t.Run("upstream-basic", func(t *testing.T) {
		project := root + "/basic"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: alpine:3.18
    command: ["/bin/sh", "-c", "sleep 600"]
    stop_grace_period: 1s
`)
		defer composeDown(t, client, project, "")
		runComposeV2(t, client, "run-basic-up", project, "      state: present\n", "")

		command := runComposeRun(t, client, "command", project, `
      service: web
      command: /bin/sh -c "ls /"
      cleanup: true
`, "")
		if numberValue(command["rc"]) != 0 || !strings.Contains(command["stdout"].(string), "usr") || !strings.Contains(command["stdout"].(string), "etc") {
			t.Fatalf("command result = %#v", command)
		}
		if _, found := command["container_id"]; found {
			t.Fatalf("synchronous command returned container_id: %#v", command)
		}

		argv := runComposeRun(t, client, "argv-nonzero", project, `
      service: web
      argv:
        - /bin/sh
        - -c
        - whoami
      user: "1234"
      cleanup: true
`, "")
		if numberValue(argv["rc"]) != 1 || argv["stdout"] != "" || !strings.Contains(argv["stderr"].(string), "unknown uid 1234") {
			t.Fatalf("argv result = %#v", argv)
		}

		detached := runComposeRun(t, client, "detached", project, `
      service: web
      command: /bin/sh -c "sleep 30"
      detach: true
      cleanup: true
`, "")
		containerID, _ := detached["container_id"].(string)
		if containerID == "" || detached["changed"] != false {
			t.Fatalf("detached result = %#v", detached)
		}
		if _, found := detached["rc"]; found {
			t.Fatalf("detached result returned rc: %#v", detached)
		}
		if _, found := detached["stdout"]; found {
			t.Fatalf("detached result returned stdout: %#v", detached)
		}

		input := runComposeRun(t, client, "stdin", project, `
      service: web
      command: /bin/sh -c "cat"
      stdin: This is a test
      cleanup: true
`, "")
		if numberValue(input["rc"]) != 0 || input["stdout"] != "This is a test" {
			t.Fatalf("stdin result = %#v", input)
		}
	})

	t.Run("documented-command-and-argv", func(t *testing.T) {
		project := root + "/documented"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  foo:
    image: alpine:3.18
    working_dir: /tmp
`)
		command := runComposeRun(t, client, "docs-command", project, `
      service: foo
      command: /bin/sh -c "pwd; ls -la"
      chdir: /root
      cleanup: true
`, "")
		if numberValue(command["rc"]) != 0 || !strings.HasPrefix(command["stdout"].(string), "/root") {
			t.Fatalf("documented command = %#v", command)
		}
		argv := runComposeRun(t, client, "docs-argv", project, `
      service: foo
      argv:
        - /bin/sh
        - -c
        - echo redirected >&2
      chdir: /root
      cleanup: true
`, "")
		if numberValue(argv["rc"]) != 0 || !strings.Contains(argv["stderr"].(string), "redirected") {
			t.Fatalf("documented argv = %#v", argv)
		}
	})

	t.Run("stdin-controls-and-env", func(t *testing.T) {
		project := root + "/input"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  tool:
    image: alpine:3.18
`)
		withoutNewline := runComposeRun(t, client, "stdin-no-newline", project, `
      service: tool
      argv: ["/bin/sh", "-c", "wc -c"]
      stdin: abc
      stdin_add_newline: false
      cleanup: true
`, "")
		if strings.TrimSpace(withoutNewline["stdout"].(string)) != "3" {
			t.Fatalf("stdin_add_newline=false = %#v", withoutNewline)
		}
		environment := runComposeRun(t, client, "env", project, `
      service: tool
      argv: ["/bin/sh", "-c", "printf '%s|%s' \"$ALPHA\" \"$BOOL_STRING\""]
      env:
        ALPHA: first
        BOOL_STRING: "true"
      interactive: false
      tty: false
      cleanup: true
`, "")
		if environment["stdout"] != "first|true" {
			t.Fatalf("env result = %#v", environment)
		}
	})

	t.Run("detached-metadata-volume-and-alias", func(t *testing.T) {
		project := root + "/metadata"
		hostDir := root + "/mounted"
		mustRemote(t, client, "mkdir -p "+project+" "+hostDir)
		mustRemote(t, client, "printf mounted > "+hostDir+"/value")
		writeCompose(t, client, project, `services:
  tool:
    image: alpine:3.18
    networks:
      default:
        aliases:
          - tool-alias
`)
		result := runComposeRun(t, client, "metadata", project, `
      service: tool
      argv: ["-c", "test -f /data/value && sleep 60"]
      entrypoint: /bin/sh
      name: dibra-run-meta
      labels:
        - purpose=compose-run-test
      cap_add:
        - CHOWN
      cap_drop:
        - MKNOD
      volumes:
        - `+hostDir+`:/data
      chdir: /data
      user: "0"
      interactive: false
      tty: false
      use_aliases: true
      detach: true
`, "")
		if result["container_id"] == "" {
			t.Fatalf("metadata result = %#v", result)
		}
		if got := strings.TrimSpace(remoteExec(t, client, "docker inspect -f '{{index .Config.Labels \"purpose\"}}|{{.Config.WorkingDir}}|{{.Config.OpenStdin}}|{{.Config.Tty}}' dibra-run-meta")); got != "compose-run-test|/data|false|false" {
			t.Fatalf("metadata inspect = %q", got)
		}
		if got := strings.TrimSpace(remoteExec(t, client, "docker inspect -f '{{json .HostConfig.CapAdd}}|{{json .HostConfig.CapDrop}}' dibra-run-meta")); !strings.Contains(got, "CHOWN") || !strings.Contains(got, "MKNOD") {
			t.Fatalf("capabilities inspect = %q", got)
		}
		network := filepathBase(project) + "_default"
		if got := strings.TrimSpace(remoteExec(t, client, "docker run --rm --network "+network+" alpine:3.18 getent hosts tool-alias")); got == "" {
			t.Fatal("use_aliases did not publish tool-alias")
		}
		mustRemote(t, client, "docker rm -f dibra-run-meta >/dev/null")
	})

	t.Run("no-deps", func(t *testing.T) {
		project := root + "/no-deps"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  db:
    image: alpine:3.18
    command: ["sleep", "600"]
  web:
    image: alpine:3.18
    depends_on:
      - db
`)
		result := runComposeRun(t, client, "no-deps", project, `
      service: web
      argv: ["/bin/sh", "-c", "echo ok"]
      no_deps: true
      cleanup: true
`, "")
		if result["stdout"] != "ok" {
			t.Fatalf("no_deps result = %#v", result)
		}
		if got := strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker compose ps -q db")); got != "" {
			t.Fatalf("dependency was started: %s", got)
		}
	})

	t.Run("definition", func(t *testing.T) {
		result := runComposeRunDefinition(t, client, "definition", "dibra-run-definition", `
      service: tool
      argv: ["/bin/sh", "-c", "echo inline"]
      cleanup: true
`)
		if result["stdout"] != "inline" || numberValue(result["rc"]) != 0 {
			t.Fatalf("definition result = %#v", result)
		}
	})

	t.Run("files-env-files-profiles", func(t *testing.T) {
		project := root + "/project-options"
		mustRemote(t, client, "mkdir -p "+project)
		mustRemote(t, client, "printf 'RUN_IMAGE=alpine:3.18\\n' > "+project+"/run.env")
		mustRemote(t, client, "printf 'services:\\n  tool:\\n    image: ${RUN_IMAGE}\\n    profiles: [tools]\\n' > "+project+"/custom.yaml")
		result := runComposeRun(t, client, "project-options", project, `
      service: tool
      argv: ["/bin/sh", "-c", "echo project-options"]
      files:
        - custom.yaml
      env_files:
        - run.env
      profiles:
        - tools
      cleanup: true
`, "")
		if result["stdout"] != "project-options" {
			t.Fatalf("project options = %#v", result)
		}
	})

	t.Run("build", func(t *testing.T) {
		project := root + "/build"
		mustRemote(t, client, "mkdir -p "+project)
		mustRemote(t, client, "printf 'FROM alpine:3.18\\n' > "+project+"/Dockerfile")
		writeCompose(t, client, project, `services:
  tool:
    build: .
    image: dibra-compose-run-build:latest
`)
		defer mustRemote(t, client, "docker rmi dibra-compose-run-build:latest >/dev/null 2>&1 || true")
		result := runComposeRun(t, client, "build", project, `
      service: tool
      argv: ["/bin/sh", "-c", "echo built"]
      build: true
      quiet_pull: true
      cleanup: true
`, "")
		if !strings.HasSuffix(result["stdout"].(string), "built") || numberValue(result["rc"]) != 0 {
			t.Fatalf("build result = %#v", result)
		}
	})

	t.Run("service-ports-and-publish", func(t *testing.T) {
		project := root + "/ports"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: alpine:3.18
    command: ["/bin/sh", "-c", "mkdir -p /www; echo ok > /www/index.html; httpd -f -p 8080 -h /www"]
    ports:
      - "127.0.0.1:18081:8080"
`)
		servicePorts := runComposeRun(t, client, "service-ports", project, `
      service: web
      name: dibra-run-port-service
      service_ports: true
      detach: true
`, "")
		if servicePorts["container_id"] == "" {
			t.Fatalf("service ports result = %#v", servicePorts)
		}
		if got := remoteExec(t, client, "docker inspect -f '{{json .HostConfig.PortBindings}}' dibra-run-port-service"); !strings.Contains(got, `"HostPort":"18081"`) {
			t.Fatalf("service_ports bindings = %s", got)
		}
		mustRemote(t, client, "docker rm -f dibra-run-port-service >/dev/null")

		published := runComposeRun(t, client, "publish", project, `
      service: web
      name: dibra-run-port-publish
      publish:
        - "127.0.0.1:18082:8080"
      detach: true
`, "")
		if published["container_id"] == "" {
			t.Fatalf("publish result = %#v", published)
		}
		if got := remoteExec(t, client, "docker inspect -f '{{json .HostConfig.PortBindings}}' dibra-run-port-publish"); !strings.Contains(got, `"HostPort":"18082"`) {
			t.Fatalf("publish bindings = %s", got)
		}
		mustRemote(t, client, "docker rm -f dibra-run-port-publish >/dev/null")
	})

	t.Run("validation-errors", func(t *testing.T) {
		project := root + "/validation"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, "services:\n  web:\n    image: alpine:3.18\n")
		for name, arguments := range map[string]string{
			"detach-stdin": `
      service: web
      detach: true
      stdin: ""
`,
			"command-argv": `
      service: web
      command: echo command
      argv: [echo, argv]
`,
			"non-string-env": `
      service: web
      env:
        COUNT: 2
`,
		} {
			output := runComposeRunOutput(t, name, project, arguments)
			if !strings.Contains(output, "FAILED") {
				t.Fatalf("%s unexpectedly succeeded: %s", name, output)
			}
		}
	})
}

func runComposeRun(t *testing.T, client *ssh.Client, name, project, arguments, taskOptions string) map[string]any {
	t.Helper()
	output := runComposeRunNamed(t, name, project, arguments, taskOptions)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, "/tmp/dibra-compose-run-"+name+".json")
}

func runComposeRunOutput(t *testing.T, name, project, arguments string) string {
	t.Helper()
	return runComposeRunNamed(t, name, project, arguments, "")
}

func runComposeRunNamed(t *testing.T, name, project, arguments, taskOptions string) string {
	t.Helper()
	templatePath := writeResultTemplate(t, "run_result")
	remotePath := "/tmp/dibra-compose-run-" + name + ".json"
	playbook := playbookHeader + `
  - name: Run Compose one-off command
    community.docker.docker_compose_v2_run:
      project_src: ` + project + `
` + arguments + `
    register: run_result
` + taskOptions + `

  - name: Persist run result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	return runPlaybook(t, playbook)
}

func runComposeRunDefinition(t *testing.T, client *ssh.Client, name, projectName, arguments string) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "run_result")
	remotePath := "/tmp/dibra-compose-run-" + name + ".json"
	playbook := playbookHeader + `
  - name: Run inline Compose one-off command
    community.docker.docker_compose_v2_run:
      project_name: ` + projectName + `
      definition:
        services:
          tool:
            image: alpine:3.18
` + arguments + `
    register: run_result

  - name: Persist run result
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

func numberValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return -1
	}
}

func filepathBase(path string) string {
	path = strings.TrimRight(path, "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
