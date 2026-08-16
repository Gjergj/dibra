//go:build integration

package integration

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerContainerCopyIntoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "dibra-copy-into-parity"
	root := "/tmp/dibra-copy-into-parity"
	mustRemote(t, client, "docker rm -f "+containerName+" >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-copy-into-*.json /tmp/.dibra-agent")
	mustRemote(t, client, "mkdir -p "+root)
	mustRemote(t, client, "printf 'Content 1\\n' > "+root+"/file_1")
	mustRemote(t, client, "printf 'Content 2\\nExtra line' > "+root+"/file_2")
	mustRemote(t, client, "chmod 0644 "+root+"/file_1 "+root+"/file_2")
	defer mustRemote(t, client, "docker rm -f "+containerName+" >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-copy-into-*.json")

	startContainer := func() {
		t.Helper()
		mustRemote(t, client, "docker rm -f "+containerName+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker run -d --name "+containerName+" alpine:latest sh -c 'mkdir /dir; ln -s file /lnk; ln -s lnk3 /lnk2; ln -s lnk2 /lnk1; sleep 3600'")
	}
	startContainer()

	templatePath := writeResultTemplate(t, "copy_result")
	runCopy := func(testName, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-copy-into-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Copy into container
    community.docker.docker_container_copy_into:
` + arguments + `
    register: copy_result
` + taskOptions + `

  - name: Persist copy result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			return nil, output
		}
		return readRemoteJSONMap(t, client, remotePath), output
	}
	success := func(testName, arguments, taskOptions string) map[string]any {
		t.Helper()
		result, output := runCopy(testName, arguments, taskOptions)
		if result == nil {
			t.Fatalf("%s copy playbook failed: %s", testName, output)
		}
		return result
	}
	fileArgs := func(source, destination string) string {
		return "      container: " + containerName + "\n" +
			"      path: " + source + "\n" +
			"      container_path: " + destination + "\n"
	}
	contentArgs := func(content, destination, mode string) string {
		return "      container: " + containerName + "\n" +
			"      content: " + content + "\n" +
			"      container_path: " + destination + "\n" +
			"      mode: \"" + mode + "\"\n" +
			"      mode_parse: modern\n"
	}

	t.Run("upstream file check diff idempotency and force", func(t *testing.T) {
		args := fileArgs(root+"/file_1", "/file")
		predicted := success("file-check-create", args, "    check_mode: true\n    diff: true\n")
		assertCopyChanged(t, predicted, true)
		assertCopyDiff(t, predicted, "", "Content 1\n")
		if containerPathExists(t, client, containerName, "/file") {
			t.Fatal("check mode created /file")
		}

		created := success("file-create", args, "    diff: true\n")
		assertCopyChanged(t, created, true)
		unchanged := success("file-idempotent", args, "    diff: true\n")
		assertCopyChanged(t, unchanged, false)
		assertCopyDiff(t, unchanged, "Content 1\n", "Content 1\n")
		assertContainerFile(t, client, containerName, "/file", "Content 1\n", "644", "0", "0", "regular file")

		forceArgs := args + "      force: true\n"
		assertCopyChanged(t, success("file-force-check", forceArgs, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("file-force", forceArgs, ""), true)

		forceFalse := fileArgs(root+"/file_2", "/file") +
			"      mode: \"0777\"\n      mode_parse: modern\n" +
			"      owner_id: 123\n      group_id: 321\n      force: false\n"
		skipped := success("file-force-false", forceFalse, "    diff: true\n")
		assertCopyChanged(t, skipped, false)
		assertCopyDiff(t, skipped, "Content 1\n", "Content 1\n")
		if copyDiff(t, skipped)["after_header"] != "/file" {
			t.Fatalf("force=false diff = %#v", copyDiff(t, skipped))
		}
		assertContainerFile(t, client, containerName, "/file", "Content 1\n", "644", "0", "0", "regular file")

		modified := success("file-modify", fileArgs(root+"/file_2", "/file"), "    diff: true\n")
		assertCopyChanged(t, modified, true)
		assertCopyDiff(t, modified, "Content 1\n", "Content 2\nExtra line")

		metadataArgs := fileArgs(root+"/file_2", "/file") +
			"      mode: \"0707\"\n      mode_parse: modern\n" +
			"      owner_id: 12\n      group_id: 910\n"
		assertCopyChanged(t, success("file-metadata-check", metadataArgs, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("file-metadata", metadataArgs, ""), true)
		assertCopyChanged(t, success("file-metadata-idempotent", metadataArgs, "    diff: true\n"), false)
		assertContainerFile(t, client, containerName, "/file", "Content 2\nExtra line", "707", "12", "910", "regular file")
		secondMetadataArgs := fileArgs(root+"/file_2", "/file") +
			"      mode: \"0707\"\n      mode_parse: modern\n" +
			"      owner_id: 13\n      group_id: 13\n"
		assertCopyChanged(t, success("file-metadata-second", secondMetadataArgs, ""), true)
		assertCopyChanged(t, success("file-metadata-second-idempotent", secondMetadataArgs, "    diff: true\n"), false)
		assertContainerFile(t, client, containerName, "/file", "Content 2\nExtra line", "707", "13", "13", "regular file")
		noDiff := success("file-diff-disabled", secondMetadataArgs, "")
		if _, found := noDiff["diff"]; found {
			t.Fatalf("diff disabled result = %#v", noDiff)
		}

		mustRemote(t, client, "docker exec "+containerName+" mkdir -p /replace-dir")
		directory := success("file-directory", fileArgs(root+"/file_1", "/replace-dir"), "    check_mode: true\n    diff: true\n")
		assertCopyChanged(t, directory, true)
		diff := copyDiff(t, directory)
		if diff["before"] != "(directory)" || diff["after"] != "Content 1\n" {
			t.Fatalf("directory diff = %#v", diff)
		}
		assertCopyChanged(t, success("file-directory-real", fileArgs(root+"/file_1", "/replace-dir"), ""), true)
		assertContainerFile(t, client, containerName, "/replace-dir", "Content 1\n", "644", "0", "0", "regular file")
	})

	t.Run("upstream content base64 and mode parsing", func(t *testing.T) {
		startContainer()
		_, output := runCopy("content-mode-required",
			"      container: "+containerName+"\n      content: value\n      container_path: /file\n", "")
		if !strings.Contains(output, "missing parameter(s) required by 'content': mode") {
			t.Fatalf("content without mode output = %s", output)
		}

		args := contentArgs(`"Content 1\n"`, "/file", "0644")
		predicted := success("content-check", args, "    check_mode: true\n    diff: true\n")
		assertCopyChanged(t, predicted, true)
		assertCopyDiff(t, predicted, "", "Content 1\n")
		assertCopyChanged(t, success("content-create", args, ""), true)
		assertCopyChanged(t, success("content-idempotent", args, "    diff: true\n"), false)

		encoded := base64.StdEncoding.EncodeToString([]byte("Content 1\n"))
		base64Args := contentArgs(encoded, "/file", "0644") + "      content_is_b64: true\n"
		assertCopyChanged(t, success("content-base64", base64Args, "    check_mode: true\n    diff: true\n"), false)

		forceFalse := contentArgs(`"Some other content\n"`, "/file", "0777") +
			"      owner_id: 123\n      group_id: 321\n      force: false\n"
		assertCopyChanged(t, success("content-force-false", forceFalse, "    diff: true\n"), false)
		assertContainerFile(t, client, containerName, "/file", "Content 1\n", "644", "0", "0", "regular file")

		modeCases := []struct {
			name, mode, parser, want string
		}{
			{name: "modern-integer", mode: "504", parser: "modern", want: "770"},
			{name: "legacy-integer", mode: "455", parser: "legacy", want: "707"},
			{name: "legacy-string", mode: `"420"`, parser: "legacy", want: "644"},
			{name: "octal-string", mode: `"0600"`, parser: "octal_string_only", want: "600"},
		}
		for _, test := range modeCases {
			arguments := "      container: " + containerName + "\n" +
				"      content: \"Content 1\\n\"\n" +
				"      container_path: /file\n" +
				"      mode: " + test.mode + "\n" +
				"      mode_parse: " + test.parser + "\n" +
				"      owner_id: 0\n      group_id: 0\n"
			assertCopyChanged(t, success("mode-"+test.name, arguments, ""), true)
			if got := strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" stat -c %a /file")); got != test.want {
				t.Fatalf("%s mode = %q, want %q", test.name, got, test.want)
			}
		}

		contentDirectory := contentArgs(`"Content 1\n"`, "/dir", "0644")
		assertCopyChanged(t, success("content-directory-check", contentDirectory, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("content-directory", contentDirectory, ""), true)
		assertCopyChanged(t, success("content-directory-idempotent", contentDirectory, "    check_mode: true\n    diff: true\n"), false)
	})

	t.Run("container and managed-node symlinks", func(t *testing.T) {
		startContainer()
		args := contentArgs(`"Content 1\n"`, "/file", "0644")
		assertCopyChanged(t, success("symlink-target", args, ""), true)

		followArgs := contentArgs(`"Content 1\n"`, "/lnk", "0644") + "      follow: true\n"
		followed := success("symlink-follow", followArgs, "    diff: true\n")
		assertCopyChanged(t, followed, false)
		if followed["container_path"] != "/file" {
			t.Fatalf("followed path = %#v", followed)
		}
		pathFollow := fileArgs(root+"/file_1", "/lnk") + "      follow: true\n"
		assertCopyChanged(t, success("path-symlink-follow", pathFollow, "    check_mode: true\n    diff: true\n"), false)
		assertCopyChanged(t, success("path-symlink-follow-force", pathFollow+"      force: true\n", ""), true)
		if got := strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" readlink /lnk")); got != "file" {
			t.Fatalf("force-follow replaced destination link: %q", got)
		}

		noFollow := success("symlink-no-follow-check", contentArgs(`"Content 1\n"`, "/lnk", "0644"), "    check_mode: true\n    diff: true\n")
		assertCopyChanged(t, noFollow, true)
		if before := copyDiff(t, noFollow)["before"]; before != "file" && before != "/file" {
			t.Fatalf("no-follow diff = %#v", copyDiff(t, noFollow))
		}
		assertCopyChanged(t, success("symlink-no-follow", contentArgs(`"Content 1\n"`, "/lnk", "0644"), ""), true)
		assertContainerFile(t, client, containerName, "/lnk", "Content 1\n", "644", "0", "0", "regular file")

		mustRemote(t, client, "docker exec "+containerName+" ln -s file /path-link")
		pathNoFollow := fileArgs(root+"/file_1", "/path-link")
		assertCopyChanged(t, success("path-symlink-no-follow-check", pathNoFollow, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("path-symlink-no-follow", pathNoFollow, ""), true)
		assertCopyChanged(t, success("path-symlink-no-follow-idempotent", pathNoFollow, "    check_mode: true\n    diff: true\n"), false)

		mustRemote(t, client, "ln -sf file_1 "+root+"/link_1; ln -sf dead "+root+"/link_2")
		localLink := fileArgs(root+"/link_1", "/local-link") +
			"      local_follow: false\n      owner_id: 0\n      group_id: 0\n"
		assertCopyChanged(t, success("local-link", localLink, ""), true)
		if got := strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" readlink /local-link")); got != "file_1" {
			t.Fatalf("copied local symlink target = %q", got)
		}
		assertCopyChanged(t, success("local-link-idempotent", localLink, "    diff: true\n"), false)
		mustRemote(t, client, "docker exec "+containerName+" chown -h 123:321 /local-link")
		assertCopyChanged(t, success("local-link-metadata-ignored", localLink, "    check_mode: true\n    diff: true\n"), false)

		localFollow := fileArgs(root+"/link_1", "/local-target") +
			"      local_follow: true\n      owner_id: 0\n      group_id: 0\n"
		assertCopyChanged(t, success("local-follow", localFollow, ""), true)
		assertContainerFile(t, client, containerName, "/local-target", "Content 1\n", "644", "0", "0", "regular file")

		dangling := fileArgs(root+"/link_2", "/dangling") +
			"      local_follow: false\n      owner_id: 0\n      group_id: 0\n"
		assertCopyChanged(t, success("local-dangling", dangling, ""), true)
		if got := strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" readlink /dangling")); got != "dead" {
			t.Fatalf("copied dangling symlink target = %q", got)
		}

		mustRemote(t, client, "docker exec "+containerName+" sh -c 'ln -s cycle2 /cycle1; ln -s cycle1 /cycle2'")
		_, output := runCopy("symlink-cycle", contentArgs("value", "/cycle1", "0644")+"      follow: true\n", "")
		if !strings.Contains(output, "infinite symbolic link loop") &&
			!strings.Contains(output, "An unexpected Docker error occurred") {
			t.Fatalf("cycle output = %s", output)
		}
	})

	t.Run("stopped and minimal containers", func(t *testing.T) {
		startContainer()
		mustRemote(t, client, "docker stop -t 1 "+containerName)
		args := contentArgs(`"Stopped content\n"`, "/stopped", "0707") +
			"      owner_id: 12\n      group_id: 910\n"
		assertCopyChanged(t, success("stopped-check", args, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("stopped-copy", args, ""), true)
		assertCopyChanged(t, success("stopped-idempotent", args, "    diff: true\n"), false)
		pathArgs := fileArgs(root+"/file_1", "/stopped-path") +
			"      mode: \"0707\"\n      mode_parse: modern\n" +
			"      owner_id: 12\n      group_id: 910\n"
		assertCopyChanged(t, success("stopped-path-check", pathArgs, "    check_mode: true\n    diff: true\n"), true)
		assertCopyChanged(t, success("stopped-path", pathArgs, ""), true)
		assertCopyChanged(t, success("stopped-path-idempotent", pathArgs, "    check_mode: true\n    diff: true\n"), false)

		_, output := runCopy("stopped-auto-owner", contentArgs(`"fail\n"`, "/failure", "0644"), "")
		if !strings.Contains(output, "Cannot execute command in paused container") {
			t.Fatalf("stopped auto-owner output = %s", output)
		}
		mustRemote(t, client, "docker start "+containerName)
		assertContainerFile(t, client, containerName, "/stopped", "Stopped content\n", "707", "12", "910", "regular file")
		assertContainerFile(t, client, containerName, "/stopped-path", "Content 1\n", "707", "12", "910", "regular file")

		mustRemote(t, client, "docker exec "+containerName+" rm /bin/sh")
		minimal := contentArgs("minimal", "/minimal", "0600") + "      owner_id: 0\n      group_id: 0\n"
		assertCopyChanged(t, success("minimal-explicit", minimal, ""), true)
	})

	t.Run("binary large normalization and errors", func(t *testing.T) {
		startContainer()
		binary := []byte{'a', 0, 'b', '\n'}
		encoded := base64.StdEncoding.EncodeToString(binary)
		args := contentArgs(encoded, "/binary", "0600") + "      content_is_b64: true\n"
		assertCopyChanged(t, success("binary-create", args, ""), true)
		binaryResult := success("binary-diff", args, "    diff: true\n")
		assertCopyChanged(t, binaryResult, false)
		diff := copyDiff(t, binaryResult)
		if numberValue(diff["src_binary"]) != 1 || numberValue(diff["dst_binary"]) != 1 {
			t.Fatalf("binary diff = %#v", diff)
		}

		mustRemote(t, client, "dd if=/dev/zero of="+root+"/large bs=110000 count=1 status=none")
		large := success("large-diff", fileArgs(root+"/large", "/large"), "    check_mode: true\n    diff: true\n")
		if numberValue(copyDiff(t, large)["src_larger"]) != 104448 {
			t.Fatalf("large diff = %#v", copyDiff(t, large))
		}

		relative := success("relative-path", contentArgs("relative", "var/lib/../data", "0644")+"      owner_id: 0\n      group_id: 0\n", "")
		if relative["container_path"] != "/var/data" {
			t.Fatalf("relative result = %#v", relative)
		}

		errorCases := []struct {
			name, arguments, message string
		}{
			{name: "invalid-base64", arguments: contentArgs(`"%%%"`, "/bad", "0644") + "      content_is_b64: true\n", message: "Cannot Base64 decode"},
			{name: "missing-source", arguments: fileArgs(root+"/missing", "/bad") + "      owner_id: 0\n      group_id: 0\n", message: "Cannot find local file"},
			{name: "directory-source", arguments: fileArgs(root, "/bad") + "      owner_id: 0\n      group_id: 0\n", message: "is not a symbolic link or file"},
			{name: "missing-container", arguments: strings.ReplaceAll(contentArgs("value", "/bad", "0644")+"      owner_id: 0\n      group_id: 0\n", containerName, "missing-copy-container"), message: "Could not find container"},
		}
		for _, test := range errorCases {
			_, output := runCopy("error-"+test.name, test.arguments, "")
			if !strings.Contains(output, test.message) {
				t.Fatalf("%s output = %s", test.name, output)
			}
		}
	})
}

func assertCopyChanged(t *testing.T, result map[string]any, want bool) {
	t.Helper()
	if result["changed"] != want || result["failed"] != false {
		t.Fatalf("copy result = %#v, want changed=%t", result, want)
	}
}

func copyDiff(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	diff, ok := result["diff"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %#v", result["diff"])
	}
	return diff
}

func assertCopyDiff(t *testing.T, result map[string]any, before, after string) {
	t.Helper()
	diff := copyDiff(t, result)
	if diff["before"] != before || diff["after"] != after {
		t.Fatalf("diff = %#v, want before=%q after=%q", diff, before, after)
	}
}

func containerPathExists(t *testing.T, client *ssh.Client, containerName, path string) bool {
	t.Helper()
	return strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" sh -c 'test -e "+path+" -o -L "+path+"'; echo $?")) == "0"
}

func assertContainerFile(t *testing.T, client *ssh.Client, containerName, path, content, mode, uid, gid, kind string) {
	t.Helper()
	encoded := strings.TrimSpace(remoteExec(t, client, "docker exec "+containerName+" sh -c 'base64 < "+path+"'"))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode %s content %q: %v", path, encoded, err)
	}
	if string(decoded) != content {
		t.Fatalf("%s content = %q, want %q", path, decoded, content)
	}
	stat := strings.TrimSpace(remoteExec(t, client, fmt.Sprintf(
		"docker exec %s stat -c '%%a|%%u|%%g|%%F' %s", containerName, path,
	)))
	want := mode + "|" + uid + "|" + gid + "|" + kind
	if stat != want {
		t.Fatalf("%s stat = %q, want %q", path, stat, want)
	}
}
