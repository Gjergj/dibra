//go:build integration

package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type coreParityClient interface {
	Run(string) (string, string, error)
}

func ensureCoreParityAgent(t *testing.T) {
	t.Helper()
	output := runPlaybook(t, playbookHeader+`
  - name: Prepare agent for core parity
    ping:
`)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("failed to prepare parity agent: %s", output)
	}
}

func runCoreParityModule(t *testing.T, client coreParityClient, module string, args map[string]any) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{"module": module, "args": args})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(request)
	stdout, stderr, err := client.Run("printf '%s' '" + encoded + "' | base64 -d | /tmp/.dibra-agent")
	if err != nil {
		t.Fatalf("%s agent invocation failed: %v\nstderr: %s\nstdout: %s", module, err, stderr, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("decode %s response: %v\nstdout: %s\nstderr: %s", module, err, stdout, stderr)
	}
	return result
}

func coreParityBool(t *testing.T, result map[string]any, key string) bool {
	t.Helper()
	value, ok := result[key].(bool)
	if !ok {
		t.Fatalf("%s = %#v, want bool in result %#v", key, result[key], result)
	}
	return value
}

func coreParityString(t *testing.T, result map[string]any, key string) string {
	t.Helper()
	value, ok := result[key].(string)
	if !ok {
		t.Fatalf("%s = %#v, want string in result %#v", key, result[key], result)
	}
	return value
}

func coreParityInt(t *testing.T, result map[string]any, key string) int {
	t.Helper()
	value, ok := result[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v, want number in result %#v", key, result[key], result)
	}
	return int(value)
}

func assertCoreParitySuccess(t *testing.T, result map[string]any) {
	t.Helper()
	if failed, _ := result["failed"].(bool); failed {
		t.Fatalf("module unexpectedly failed: %#v", result)
	}
}

func TestPlaybook_CommandCoreParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	ensureCoreParityAgent(t)

	const base = "/tmp/dibra-command-core-parity"
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base+"/work")
	t.Cleanup(func() { _, _, _ = client.Run("rm -rf " + base) })

	missing := runCoreParityModule(t, client, "command", map[string]any{"chdir": "/"})
	if !coreParityBool(t, missing, "failed") || !strings.Contains(coreParityString(t, missing, "msg"), "required") {
		t.Fatalf("missing command validation = %#v", missing)
	}

	argv := runCoreParityModule(t, client, "command", map[string]any{
		"argv":  []string{"/bin/pwd"},
		"chdir": base + "/work",
	})
	assertCoreParitySuccess(t, argv)
	if !coreParityBool(t, argv, "changed") ||
		coreParityInt(t, argv, "rc") != 0 ||
		coreParityString(t, argv, "stdout") != base+"/work" ||
		coreParityString(t, argv, "stderr") != "" {
		t.Fatalf("argv/chdir result = %#v", argv)
	}
	command, ok := argv["cmd"].([]any)
	if !ok || len(command) != 1 || command[0] != "/bin/pwd" {
		t.Fatalf("cmd result = %#v", argv["cmd"])
	}

	invalidChdir := runCoreParityModule(t, client, "command", map[string]any{
		"argv":  []string{"/bin/true"},
		"chdir": base + "/missing",
	})
	if !coreParityBool(t, invalidChdir, "failed") ||
		!strings.Contains(coreParityString(t, invalidChdir, "msg"), "Unable to change directory") {
		t.Fatalf("invalid chdir result = %#v", invalidChdir)
	}

	stdin := runCoreParityModule(t, client, "command", map[string]any{
		"argv":  []string{"/bin/cat"},
		"stdin": "foobar",
	})
	assertCoreParitySuccess(t, stdin)
	if coreParityString(t, stdin, "stdout") != "foobar" || coreParityInt(t, stdin, "rc") != 0 {
		t.Fatalf("stdin result = %#v", stdin)
	}

	nonzero := runCoreParityModule(t, client, "command", map[string]any{
		"argv": []string{"/bin/sh", "-c", "printf command-out; printf command-err >&2; exit 7"},
	})
	if !coreParityBool(t, nonzero, "failed") ||
		coreParityInt(t, nonzero, "rc") != 7 ||
		coreParityString(t, nonzero, "stdout") != "command-out" ||
		coreParityString(t, nonzero, "stderr") != "command-err" {
		t.Fatalf("nonzero result = %#v", nonzero)
	}

	remoteExec(t, client, "touch "+base+"/created")
	creates := runCoreParityModule(t, client, "command", map[string]any{
		"argv":    []string{"/bin/false"},
		"creates": base + "/creat*",
	})
	assertCoreParitySuccess(t, creates)
	if coreParityBool(t, creates, "changed") {
		t.Fatalf("creates result = %#v", creates)
	}

	removes := runCoreParityModule(t, client, "command", map[string]any{
		"argv":    []string{"/bin/false"},
		"removes": base + "/absent*",
	})
	assertCoreParitySuccess(t, removes)
	if coreParityBool(t, removes, "changed") {
		t.Fatalf("removes result = %#v", removes)
	}
}

func TestPlaybook_ShellCoreParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	ensureCoreParityAgent(t)

	const base = "/tmp/dibra-shell-core-parity"
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base+"/work")
	t.Cleanup(func() { _, _, _ = client.Run("rm -rf " + base) })

	missing := runCoreParityModule(t, client, "shell", map[string]any{})
	if !coreParityBool(t, missing, "failed") || !strings.Contains(coreParityString(t, missing, "msg"), "no command") {
		t.Fatalf("missing shell command validation = %#v", missing)
	}

	const command = "printf 'left\\n' | tr a-z A-Z; printf warning >&2"
	executed := runCoreParityModule(t, client, "shell", map[string]any{
		"cmd":   command,
		"chdir": base + "/work",
	})
	assertCoreParitySuccess(t, executed)
	if !coreParityBool(t, executed, "changed") ||
		coreParityString(t, executed, "cmd") != command ||
		coreParityInt(t, executed, "rc") != 0 ||
		coreParityString(t, executed, "stdout") != "LEFT" ||
		coreParityString(t, executed, "stderr") != "warning" {
		t.Fatalf("shell execution result = %#v", executed)
	}

	withNewline := runCoreParityModule(t, client, "shell", map[string]any{
		"cmd":   "python3 -c 'import sys; print(sys.stdin.buffer.read().hex())'",
		"stdin": "test",
	})
	assertCoreParitySuccess(t, withNewline)
	if coreParityString(t, withNewline, "stdout") != "746573740a" {
		t.Fatalf("default stdin newline result = %#v", withNewline)
	}
	withoutNewline := runCoreParityModule(t, client, "shell", map[string]any{
		"cmd":               "python3 -c 'import sys; print(sys.stdin.buffer.read().hex())'",
		"stdin":             "test",
		"stdin_add_newline": false,
	})
	assertCoreParitySuccess(t, withoutNewline)
	if coreParityString(t, withoutNewline, "stdout") != "74657374" {
		t.Fatalf("disabled stdin newline result = %#v", withoutNewline)
	}

	nonzero := runCoreParityModule(t, client, "shell", map[string]any{
		"cmd": "printf shell-out; printf shell-err >&2; exit 5",
	})
	if !coreParityBool(t, nonzero, "failed") ||
		coreParityInt(t, nonzero, "rc") != 5 ||
		coreParityString(t, nonzero, "stdout") != "shell-out" ||
		coreParityString(t, nonzero, "stderr") != "shell-err" {
		t.Fatalf("nonzero shell result = %#v", nonzero)
	}

	remoteExec(t, client, "touch "+base+"/created")
	for name, args := range map[string]map[string]any{
		"creates": {"cmd": "exit 9", "creates": base + "/creat*"},
		"removes": {"cmd": "exit 9", "removes": base + "/absent*"},
	} {
		t.Run(name, func(t *testing.T) {
			result := runCoreParityModule(t, client, "shell", args)
			assertCoreParitySuccess(t, result)
			if coreParityBool(t, result, "changed") {
				t.Fatalf("%s result = %#v", name, result)
			}
		})
	}
}

func TestPlaybook_FileCoreParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	ensureCoreParityAgent(t)

	const base = "/tmp/dibra-file-core-parity"
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base+" && printf source > "+base+"/source")
	t.Cleanup(func() { _, _, _ = client.Run("rm -rf " + base) })

	directoryArgs := map[string]any{
		"path":  base + "/directory",
		"state": "directory",
		"mode":  "0750",
		"owner": "nobody",
		"group": "nogroup",
	}
	directory := runCoreParityModule(t, client, "file", directoryArgs)
	assertCoreParitySuccess(t, directory)
	if !coreParityBool(t, directory, "changed") ||
		coreParityString(t, directory, "path") != base+"/directory" ||
		coreParityString(t, directory, "state") != "directory" {
		t.Fatalf("directory result = %#v", directory)
	}
	secondDirectory := runCoreParityModule(t, client, "file", directoryArgs)
	assertCoreParitySuccess(t, secondDirectory)
	if coreParityBool(t, secondDirectory, "changed") {
		t.Fatalf("directory rerun = %#v", secondDirectory)
	}
	if got := remoteFileMode(t, client, base+"/directory"); got != "750" {
		t.Fatalf("directory mode = %s", got)
	}
	if got := remoteFileOwner(t, client, base+"/directory"); got != "nobody" {
		t.Fatalf("directory owner = %s", got)
	}
	if got := remoteFileGroup(t, client, base+"/directory"); got != "nogroup" {
		t.Fatalf("directory group = %s", got)
	}

	for name, args := range map[string]map[string]any{
		"link": {"path": base + "/soft", "src": base + "/source", "state": "link"},
		"hard": {"path": base + "/hard", "src": base + "/source", "state": "hard"},
	} {
		t.Run(name, func(t *testing.T) {
			first := runCoreParityModule(t, client, "file", args)
			assertCoreParitySuccess(t, first)
			if !coreParityBool(t, first, "changed") {
				t.Fatalf("%s creation = %#v", name, first)
			}
			second := runCoreParityModule(t, client, "file", args)
			assertCoreParitySuccess(t, second)
			if coreParityBool(t, second, "changed") {
				t.Fatalf("%s rerun = %#v", name, second)
			}
		})
	}
	if got := remoteSymlinkTarget(t, client, base+"/soft"); got != base+"/source" {
		t.Fatalf("symlink target = %q", got)
	}
	if remoteFileInode(t, client, base+"/hard") != remoteFileInode(t, client, base+"/source") {
		t.Fatal("hard link inode differs from source")
	}

	touchArgs := map[string]any{"path": base + "/touched", "state": "touch", "mode": "0644"}
	for iteration := 1; iteration <= 2; iteration++ {
		touched := runCoreParityModule(t, client, "file", touchArgs)
		assertCoreParitySuccess(t, touched)
		if !coreParityBool(t, touched, "changed") {
			t.Fatalf("touch iteration %d = %#v", iteration, touched)
		}
	}
	if !remoteIsFile(t, client, base+"/touched") {
		t.Fatal("touch did not create a regular file")
	}

	absentArgs := map[string]any{"path": base + "/directory", "state": "absent"}
	absent := runCoreParityModule(t, client, "file", absentArgs)
	assertCoreParitySuccess(t, absent)
	if !coreParityBool(t, absent, "changed") || remoteFileExists(t, client, base+"/directory") {
		t.Fatalf("absent result = %#v", absent)
	}
	absentAgain := runCoreParityModule(t, client, "file", absentArgs)
	assertCoreParitySuccess(t, absentAgain)
	if coreParityBool(t, absentAgain, "changed") {
		t.Fatalf("absent rerun = %#v", absentAgain)
	}
}

func TestPlaybook_CopyCoreParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const base = "/tmp/dibra-copy-core-parity"
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base+"/destination && printf remote-source > "+base+"/remote-source")
	t.Cleanup(func() { _, _, _ = client.Run("rm -rf " + base) })

	localDir := t.TempDir()
	localSource := filepath.Join(localDir, "local-source.txt")
	if err := os.WriteFile(localSource, []byte("local-source\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	playbook := playbookHeader + fmt.Sprintf(`
  - name: Copy local source for core parity
    copy:
      src: %s
      dest: %s/local-destination
      mode: "0640"
      force: false

  - name: Copy inline content for core parity
    copy:
      content: "inline-content"
      dest: %s/content-destination
      mode: "0600"

  - name: Copy remote source for core parity
    copy:
      src: %s/remote-source
      dest: %s/remote-destination
      remote_src: true
`, localSource, base, base, base, base)

	first := runPlaybook(t, playbook)
	if strings.Contains(first, "FAILED") || !strings.Contains(first, "CHANGED") {
		t.Fatalf("first copy parity run failed: %s", first)
	}
	second := runPlaybook(t, playbook)
	if strings.Contains(second, "FAILED") || strings.Contains(second, "CHANGED") {
		t.Fatalf("second copy parity run was not idempotent: %s", second)
	}
	for path, want := range map[string]string{
		base + "/local-destination":   "local-source",
		base + "/content-destination": "inline-content",
		base + "/remote-destination":  "remote-source",
	} {
		if got := remoteFileContent(t, client, path); got != want {
			t.Fatalf("%s content = %q, want %q", path, got, want)
		}
	}
	if got := remoteFileMode(t, client, base+"/local-destination"); got != "640" {
		t.Fatalf("local copy mode = %s", got)
	}
	if got := remoteFileMode(t, client, base+"/content-destination"); got != "600" {
		t.Fatalf("content copy mode = %s", got)
	}

	remoteResultArgs := map[string]any{
		"src":        base + "/remote-source",
		"dest":       base + "/direct-result-destination",
		"remote_src": true,
	}
	remoteResult := runCoreParityModule(t, client, "copy", remoteResultArgs)
	assertCoreParitySuccess(t, remoteResult)
	if !coreParityBool(t, remoteResult, "changed") ||
		coreParityString(t, remoteResult, "src") != base+"/remote-source" ||
		coreParityString(t, remoteResult, "dest") != base+"/direct-result-destination" ||
		coreParityString(t, remoteResult, "checksum") == "" ||
		coreParityInt(t, remoteResult, "size") != len("remote-source") {
		t.Fatalf("remote source result = %#v", remoteResult)
	}
	remoteResultAgain := runCoreParityModule(t, client, "copy", remoteResultArgs)
	assertCoreParitySuccess(t, remoteResultAgain)
	if coreParityBool(t, remoteResultAgain, "changed") {
		t.Fatalf("remote source rerun = %#v", remoteResultAgain)
	}

	remoteExec(t, client, "printf original > "+base+"/backup-destination")
	backup := runCoreParityModule(t, client, "copy", map[string]any{
		"content": "replacement",
		"dest":    base + "/backup-destination",
		"backup":  true,
	})
	assertCoreParitySuccess(t, backup)
	if !coreParityBool(t, backup, "changed") ||
		coreParityString(t, backup, "dest") != base+"/backup-destination" ||
		coreParityString(t, backup, "checksum") == "" ||
		coreParityInt(t, backup, "size") != len("replacement") {
		t.Fatalf("backup copy result = %#v", backup)
	}
	backupPath := coreParityString(t, backup, "backup_file")
	if backupPath == "" || remoteFileContent(t, client, backupPath) != "original" {
		t.Fatalf("backup path/content = %q", backupPath)
	}

	both := runCoreParityModule(t, client, "copy", map[string]any{
		"src":     base + "/remote-source",
		"content": "conflict",
		"dest":    base + "/invalid",
	})
	if !coreParityBool(t, both, "failed") || !strings.Contains(coreParityString(t, both, "msg"), "mutually exclusive") {
		t.Fatalf("copy argument validation = %#v", both)
	}
}

func TestPlaybook_Lineinfile_CoreParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	ensureCoreParityAgent(t)

	const base = "/tmp/dibra-lineinfile-core-parity"
	const path = base + "/managed.conf"
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base+" && printf 'alpha=1\\nREF old REF\\nomega=9\\n' > "+path)
	t.Cleanup(func() { _, _, _ = client.Run("rm -rf " + base) })

	missing := runCoreParityModule(t, client, "lineinfile", map[string]any{"line": "x"})
	if !coreParityBool(t, missing, "failed") || !strings.Contains(coreParityString(t, missing, "msg"), "path is required") {
		t.Fatalf("lineinfile path validation = %#v", missing)
	}
	exclusive := runCoreParityModule(t, client, "lineinfile", map[string]any{
		"path": path, "line": "x", "regexp": "x", "search_string": "x",
	})
	if !coreParityBool(t, exclusive, "failed") || !strings.Contains(coreParityString(t, exclusive, "msg"), "mutually exclusive") {
		t.Fatalf("lineinfile exclusivity = %#v", exclusive)
	}

	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "insert-before",
			args: map[string]any{"path": path, "line": "header=true", "insertbefore": "BOF"},
		},
		{
			name: "insert-after",
			args: map[string]any{"path": path, "line": "after_alpha=true", "insertafter": "^alpha=1$"},
		},
		{
			name: "regexp",
			args: map[string]any{"path": path, "line": "omega=10", "regexp": "^omega="},
		},
		{
			name: "search-string",
			args: map[string]any{"path": path, "line": "alpha=2", "search_string": "alpha=1"},
		},
		{
			name: "backrefs",
			args: map[string]any{"path": path, "line": "value=\\1", "regexp": "^REF (.*) REF$", "backrefs": true},
		},
		{
			name: "validate",
			args: map[string]any{"path": path, "line": "validated=true", "insertafter": "EOF", "validate": "/bin/sh -c 'test -s %s'"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			first := runCoreParityModule(t, client, "lineinfile", test.args)
			assertCoreParitySuccess(t, first)
			if !coreParityBool(t, first, "changed") ||
				coreParityString(t, first, "path") != path {
				t.Fatalf("%s first result = %#v", test.name, first)
			}
			second := runCoreParityModule(t, client, "lineinfile", test.args)
			assertCoreParitySuccess(t, second)
			if coreParityBool(t, second, "changed") {
				t.Fatalf("%s rerun = %#v", test.name, second)
			}
		})
	}

	beforeFailure := remoteFileContent(t, client, path)
	validateFailure := runCoreParityModule(t, client, "lineinfile", map[string]any{
		"path": path, "line": "must-not-be-written", "insertafter": "EOF", "validate": "/bin/false %s",
	})
	if !coreParityBool(t, validateFailure, "failed") ||
		remoteFileContent(t, client, path) != beforeFailure {
		t.Fatalf("validation failure result = %#v", validateFailure)
	}

	absentArgs := map[string]any{"path": path, "state": "absent", "search_string": "validated=true"}
	removed := runCoreParityModule(t, client, "lineinfile", absentArgs)
	assertCoreParitySuccess(t, removed)
	if !coreParityBool(t, removed, "changed") {
		t.Fatalf("absent result = %#v", removed)
	}
	removedAgain := runCoreParityModule(t, client, "lineinfile", absentArgs)
	assertCoreParitySuccess(t, removedAgain)
	if coreParityBool(t, removedAgain, "changed") {
		t.Fatalf("absent rerun = %#v", removedAgain)
	}
}
