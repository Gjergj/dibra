//go:build integration

package integration

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerExecParity independently ports the pinned
// community.docker docker_container_exec integration target
// (tests/integration/targets/docker_container_exec/tasks/main.yml), the
// 5.2.2 module documentation examples, and documented error/option
// contracts that the upstream target does not run.
func TestPlaybook_DockerContainerExecParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		container = "dibra-container-exec-parity"
		image     = "alpine:latest"
	)
	remoteExec(t, client, "docker pull "+image)
	remoteExec(t, client, "docker rm -f "+container+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent /tmp/dibra-container-exec-*.json")
	defer remoteExec(t, client, "docker rm -f "+container+" || true")

	t.Run("missing container", func(t *testing.T) {
		output := runContainerExecOutput(t, "missing", `
      container: dibra-container-exec-does-not-exist
      command: /bin/sh -c "ls -a"
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, `Could not find container`) {
			t.Fatalf("missing container: %s", output)
		}
	})

	remoteExec(t, client, "docker run -d --name "+container+" "+image+" /bin/sh -c 'sleep 10m'")
	defer remoteExec(t, client, "docker rm -f "+container+" || true")

	t.Run("command result shape", func(t *testing.T) {
		result := runContainerExec(t, client, "command", `
      container: `+container+`
      command: /bin/sh -c "ls -a"
`)
		assertSynchronousResultShape(t, result)
		if numberValue(result["rc"]) != 0 {
			t.Fatalf("rc = %#v", result["rc"])
		}
		if !strings.Contains(resultString(result, "stdout"), ".") {
			t.Fatalf("stdout = %#v", result["stdout"])
		}
	})

	t.Run("argv matches command stdout", func(t *testing.T) {
		command := runContainerExec(t, client, "command-eq", `
      container: `+container+`
      command: /bin/sh -c "ls -a"
`)
		argv := runContainerExec(t, client, "argv-eq", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - ls -a
`)
		assertSynchronousResultShape(t, argv)
		if numberValue(argv["rc"]) != 0 || argv["stdout"] != command["stdout"] {
			t.Fatalf("command stdout=%#v argv=%#v", command["stdout"], argv)
		}
	})

	t.Run("cat without stdin EOF", func(t *testing.T) {
		result := runContainerExec(t, client, "cat-empty", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat
`)
		assertSynchronousResultShape(t, result)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "" || result["stderr"] != "" {
			t.Fatalf("cat without stdin = %#v", result)
		}
		assertStringLines(t, result, "stdout_lines")
		assertStringLines(t, result, "stderr_lines")
	})

	t.Run("cat with stdin newline preserved", func(t *testing.T) {
		result := runContainerExec(t, client, "cat-newline", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat
      stdin: Hello world!
      strip_empty_ends: false
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "Hello world!\n" || result["stderr"] != "" {
			t.Fatalf("cat with stdin = %#v", result)
		}
		assertStringLines(t, result, "stdout_lines", "Hello world!")
		assertStringLines(t, result, "stderr_lines")
	})

	t.Run("cat with stdin no added newline", func(t *testing.T) {
		result := runContainerExec(t, client, "cat-no-newline", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat
      stdin: Hello world!
      stdin_add_newline: false
      strip_empty_ends: false
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "Hello world!" || result["stderr"] != "" {
			t.Fatalf("cat without added newline = %#v", result)
		}
		assertStringLines(t, result, "stdout_lines", "Hello world!")
	})

	t.Run("cat with stdin newline then strip", func(t *testing.T) {
		result := runContainerExec(t, client, "cat-strip", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat
      stdin: Hello world!
      stdin_add_newline: true
      strip_empty_ends: true
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "Hello world!" || result["stderr"] != "" {
			t.Fatalf("cat with strip = %#v", result)
		}
		assertStringLines(t, result, "stdout_lines", "Hello world!")
	})

	t.Run("long stdin", func(t *testing.T) {
		long1 := strings.Repeat("something long ", 10000)
		long2 := strings.Repeat("something else ", 5000)
		want := long1 + "\n" + long2
		result := runContainerExec(t, client, "cat-long", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat
      stdin: `+strconv.Quote(want)+`
`)
		assertSynchronousResultShape(t, result)
		if result["changed"] != true || numberValue(result["rc"]) != 0 {
			t.Fatalf("long stdin status = %#v", result)
		}
		if result["stdout"] != want || result["stderr"] != "" {
			t.Fatalf("long stdin stdout len=%d want=%d stderr=%#v", len(resultString(result, "stdout")), len(want), result["stderr"])
		}
		assertStringLines(t, result, "stdout_lines", long1, long2)
		assertStringLines(t, result, "stderr_lines")
	})

	t.Run("detach returns exec id and omits rc stdout stderr", func(t *testing.T) {
		result := runContainerExec(t, client, "detach", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - echo "Detach worked." > /result.txt
      detach: true
`)
		if result["changed"] != true {
			t.Fatalf("detach unchanged: %#v", result)
		}
		execID, _ := result["exec_id"].(string)
		if execID == "" {
			t.Fatalf("missing exec_id: %#v", result)
		}
		for _, field := range []string{"rc", "stdout", "stderr", "stdout_lines", "stderr_lines"} {
			if _, found := result[field]; found {
				t.Fatalf("detached result returned %s: %#v", field, result)
			}
		}

		env := runContainerExec(t, client, "env", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - 'echo "$FOO" ; echo $FOO > /dev/stderr'
      env:
        FOO: |-
          bar
          baz
`)
		assertSynchronousResultShape(t, env)
		if numberValue(env["rc"]) != 0 || env["stdout"] != "bar\nbaz" || env["stderr"] != "bar baz" {
			t.Fatalf("env result = %#v", env)
		}
		assertStringLines(t, env, "stdout_lines", "bar", "baz")
		assertStringLines(t, env, "stderr_lines", "bar baz")

		remoteExec(t, client, "for i in $(seq 1 30); do docker exec "+container+" test -f /result.txt && break; sleep 0.1; done")
		check := runContainerExec(t, client, "detach-check", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - cat /result.txt
      strip_empty_ends: false
`)
		if numberValue(check["rc"]) != 0 || check["stdout"] != "Detach worked.\n" || check["stderr"] != "" {
			t.Fatalf("detach check = %#v", check)
		}
		assertStringLines(t, check, "stdout_lines", "Detach worked.")
		assertStringLines(t, check, "stderr_lines")
	})

	t.Run("docs example command with chdir", func(t *testing.T) {
		result := runContainerExec(t, client, "docs-command", `
      container: `+container+`
      command: /bin/sh -c "pwd"
      chdir: /root
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "/root" {
			t.Fatalf("docs command = %#v", result)
		}
	})

	t.Run("docs example argv stderr redirect", func(t *testing.T) {
		result := runContainerExec(t, client, "docs-argv-stderr", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - ls -lah > /dev/stderr
      chdir: /root
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "" {
			t.Fatalf("docs argv stdout = %#v", result)
		}
		if !strings.Contains(resultString(result, "stderr"), ".") {
			t.Fatalf("docs argv stderr = %#v", result["stderr"])
		}
		if _, ok := result["stderr_lines"].([]any); !ok {
			t.Fatalf("stderr_lines missing: %#v", result)
		}
	})

	t.Run("shell is required to expand env vars", func(t *testing.T) {
		literal := runContainerExec(t, client, "env-literal", `
      container: `+container+`
      argv:
        - echo
        - $FOO
      env:
        FOO: bar
`)
		if numberValue(literal["rc"]) != 0 || literal["stdout"] != "$FOO" {
			t.Fatalf("literal argv = %#v", literal)
		}
		expanded := runContainerExec(t, client, "env-shell", `
      container: `+container+`
      command: /bin/sh -c "echo $FOO"
      env:
        FOO: bar
`)
		if numberValue(expanded["rc"]) != 0 || expanded["stdout"] != "bar" {
			t.Fatalf("shell command = %#v", expanded)
		}
	})

	t.Run("user chdir tty and docker_host", func(t *testing.T) {
		result := runContainerExec(t, client, "options", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - 'printf "%s" "$FOO"; echo; pwd; id -u'
      chdir: /tmp
      user: "65534"
      tty: false
      docker_host: unix:///var/run/docker.sock
      env:
        FOO: option-value
`)
		if numberValue(result["rc"]) != 0 {
			t.Fatalf("options rc = %#v", result)
		}
		stdout := resultString(result, "stdout")
		if !strings.Contains(stdout, "option-value") || !strings.Contains(stdout, "/tmp") || !strings.Contains(stdout, "65534") {
			t.Fatalf("options stdout = %q", stdout)
		}
	})

	t.Run("tty allocates a terminal", func(t *testing.T) {
		result := runContainerExec(t, client, "tty", `
      container: `+container+`
      argv: [echo, hello]
      tty: true
`)
		if result["changed"] != true || numberValue(result["rc"]) != 0 {
			t.Fatalf("tty result = %#v", result)
		}
		if !strings.Contains(resultString(result, "stdout"), "hello") {
			t.Fatalf("tty stdout = %#v", result["stdout"])
		}
	})

	t.Run("quoted boolean-looking env stays a string", func(t *testing.T) {
		result := runContainerExec(t, client, "env-quoted", `
      container: `+container+`
      command: /bin/sh -c "echo $FLAG"
      env:
        FLAG: "true"
`)
		if numberValue(result["rc"]) != 0 || result["stdout"] != "true" {
			t.Fatalf("quoted env = %#v", result)
		}
	})

	t.Run("globs require a shell", func(t *testing.T) {
		result := runContainerExec(t, client, "glob", `
      container: `+container+`
      argv: [ls, "*"]
`)
		if result["failed"] == true {
			t.Fatalf("glob argv failed the module: %#v", result)
		}
		if numberValue(result["rc"]) == 0 {
			t.Fatalf("literal glob unexpectedly succeeded: %#v", result)
		}
	})

	t.Run("docker_url alias reaches the daemon", func(t *testing.T) {
		result := runContainerExec(t, client, "docker-url", `
      container: `+container+`
      argv: [/bin/true]
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
`)
		if result["changed"] != true || numberValue(result["rc"]) != 0 {
			t.Fatalf("docker_url alias = %#v", result)
		}
	})

	t.Run("nonzero rc is data not failure", func(t *testing.T) {
		result := runContainerExec(t, client, "nonzero", `
      container: `+container+`
      argv:
        - /bin/sh
        - "-c"
        - exit 7
`)
		if result["failed"] == true || result["changed"] != true || numberValue(result["rc"]) != 7 {
			t.Fatalf("nonzero rc = %#v", result)
		}
	})

	t.Run("always reports changed", func(t *testing.T) {
		args := `
      container: ` + container + `
      argv: [/bin/true]
`
		first := runContainerExec(t, client, "changed-1", args)
		second := runContainerExec(t, client, "changed-2", args)
		if first["changed"] != true || second["changed"] != true {
			t.Fatalf("idempotency N/A: first=%#v second=%#v", first, second)
		}
	})

	t.Run("paused container", func(t *testing.T) {
		remoteExec(t, client, "docker pause "+container)
		defer remoteExec(t, client, "docker unpause "+container+" || true")
		output := runContainerExecOutput(t, "paused", `
      container: `+container+`
      argv: [/bin/true]
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "has been paused") {
			t.Fatalf("paused container: %s", output)
		}
	})

	t.Run("check mode skips execution", func(t *testing.T) {
		run := runContainerExecResult(t, client, "check", `
      container: `+container+`
      argv: [touch, /tmp/check-mode-must-not-exist]
`, "--check")
		if run.failed {
			t.Fatalf("check mode failed: %s", run.output)
		}
		if run.result["skipped"] != true {
			t.Fatalf("check mode result = %#v", run.result)
		}
		if got := remoteExec(t, client, "docker exec "+container+" test ! -e /tmp/check-mode-must-not-exist && echo absent"); got != "absent" {
			t.Fatalf("check mode executed the command: %q", got)
		}
	})

	t.Run("diff mode executes without diff output", func(t *testing.T) {
		run := runContainerExecResult(t, client, "diff-mode", `
      container: `+container+`
      argv: [/bin/true]
`, "--diff")
		if run.failed || run.result["changed"] != true {
			t.Fatalf("diff mode execution failed: %s\n%#v", run.output, run.result)
		}
		if _, found := run.result["diff"]; found {
			t.Fatalf("unsupported diff mode returned a diff: %#v", run.result["diff"])
		}
	})

	t.Run("validation failures", func(t *testing.T) {
		both := playbookHeader + `
  - name: Both command and argv
    docker_container_exec:
      container: ` + container + `
      command: "true"
      argv: ["true"]
`
		if output := runPlaybook(t, both); !strings.Contains(output, "FAILED") ||
			!strings.Contains(strings.ToLower(output), "exactly one") {
			t.Fatalf("command+argv: %s", output)
		}

		neither := playbookHeader + `
  - name: Neither command nor argv
    docker_container_exec:
      container: ` + container + `
`
		if output := runPlaybook(t, neither); !strings.Contains(output, "FAILED") ||
			!strings.Contains(strings.ToLower(output), "exactly one") {
			t.Fatalf("neither: %s", output)
		}

		detachStdin := playbookHeader + `
  - name: Detach with stdin
    docker_container_exec:
      container: ` + container + `
      argv: [cat]
      detach: true
      stdin: nope
`
		if output := runPlaybook(t, detachStdin); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "stdin cannot") {
			t.Fatalf("detach+stdin: %s", output)
		}

		envType := playbookHeader + `
  - name: Non-string env
    docker_container_exec:
      container: ` + container + `
      argv: ["true"]
      env:
        COUNT: 3
`
		if output := runPlaybook(t, envType); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "non-string value") {
			t.Fatalf("env type: %s", output)
		}

		chdir := playbookHeader + `
  - name: Old API chdir
    docker_container_exec:
      container: ` + container + `
      argv: [pwd]
      chdir: /tmp
      api_version: "1.34"
`
		if output := runPlaybook(t, chdir); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "1.35") {
			t.Fatalf("chdir api: %s", output)
		}
	})
}

func runContainerExec(t *testing.T, client *ssh.Client, suffix, arguments string) map[string]any {
	t.Helper()
	run := runContainerExecResult(t, client, suffix, arguments)
	if run.failed {
		t.Fatalf("exec failed: %s", run.output)
	}
	return run.result
}

func runContainerExecOutput(t *testing.T, suffix, arguments string) string {
	t.Helper()
	return runContainerExecResult(t, nil, suffix, arguments).output
}

type containerExecRun struct {
	output string
	result map[string]any
	failed bool
}

func runContainerExecResult(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) containerExecRun {
	t.Helper()
	remotePath := "/tmp/dibra-container-exec-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "exec_result")
	playbook := playbookHeader + `
  - name: Execute in container
    community.docker.docker_container_exec:
` + arguments + `
    register: exec_result

  - name: Persist exec result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	failed := strings.Contains(output, "FAILED")
	run := containerExecRun{output: output, failed: failed}
	if failed || client == nil {
		return run
	}
	run.result = readRemoteJSONMap(t, client, remotePath)
	return run
}

func assertSynchronousResultShape(t *testing.T, result map[string]any) {
	t.Helper()
	for _, field := range []string{"stdout", "stderr", "stdout_lines", "stderr_lines", "rc"} {
		if _, found := result[field]; !found {
			t.Fatalf("missing %s: %#v", field, result)
		}
	}
	if _, found := result["exec_id"]; found {
		t.Fatalf("synchronous result contains exec_id: %#v", result["exec_id"])
	}
}

func assertStringLines(t *testing.T, result map[string]any, key string, want ...string) {
	t.Helper()
	raw, _ := result[key].([]any)
	got := make([]string, len(raw))
	for index, value := range raw {
		got[index] = fmt.Sprint(value)
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
		return
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func resultString(result map[string]any, key string) string {
	value, _ := result[key].(string)
	return value
}
