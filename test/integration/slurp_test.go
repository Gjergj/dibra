//go:build integration

package integration

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPlaybook_Slurp(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-slurp-test"
	remoteExec(t, client, "rm -rf "+testDir)
	remoteExec(t, client, "mkdir -p "+testDir)
	defer remoteExec(t, client, "rm -rf "+testDir)

	t.Run("basic text file", func(t *testing.T) {
		remoteExec(t, client, "echo -n 'We are at the café' > "+testDir+"/foo.txt")

		playbook := playbookHeader + `
  - name: Slurp a text file
    slurp:
      src: ` + testDir + `/foo.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("path alias", func(t *testing.T) {
		remoteExec(t, client, "echo -n 'path alias test' > "+testDir+"/path-alias.txt")

		playbook := playbookHeader + `
  - name: Slurp using path alias
    slurp:
      path: ` + testDir + `/path-alias.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with path alias, got: %s", output)
		}
	})

	t.Run("binary file", func(t *testing.T) {
		remoteExec(t, client, "printf '\\x00\\x01\\x02\\xff\\xfe\\xfd' > "+testDir+"/bar.bin")

		playbook := playbookHeader + `
  - name: Slurp a binary file
    slurp:
      src: ` + testDir + `/bar.bin
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("content verification via command", func(t *testing.T) {
		expectedContent := "Hello, World! 12345"
		remoteExec(t, client, "echo -n '"+expectedContent+"' > "+testDir+"/verify.txt")

		remoteB64 := remoteExec(t, client, "base64 "+testDir+"/verify.txt")
		localB64 := base64.StdEncoding.EncodeToString([]byte(expectedContent))

		if strings.ReplaceAll(remoteB64, "\n", "") != localB64 {
			t.Errorf("Base64 mismatch: remote=%q local=%q", remoteB64, localB64)
		}

		playbook := playbookHeader + `
  - name: Slurp for verification
    slurp:
      src: ` + testDir + `/verify.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("proc file", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Slurp /proc/sys/kernel/hostname
    slurp:
      src: /proc/sys/kernel/hostname
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success reading proc file, got: %s", output)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Slurp nonexistent file
    slurp:
      src: ` + testDir + `/i_do_not_exist
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("Expected failure for missing file, got: %s", output)
		}
		if !strings.Contains(output, "file not found") {
			t.Errorf("Expected 'file not found' error, got: %s", output)
		}
	})

	t.Run("directory instead of file", func(t *testing.T) {
		remoteExec(t, client, "mkdir -p "+testDir+"/subdir")

		playbook := playbookHeader + `
  - name: Slurp a directory
    slurp:
      src: ` + testDir + `/subdir
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("Expected failure for directory, got: %s", output)
		}
		if !strings.Contains(output, "source is a directory") {
			t.Errorf("Expected 'source is a directory' error, got: %s", output)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		remoteExec(t, client, "echo 'secret' > "+testDir+"/unreadable.txt")
		remoteExec(t, client, "chmod 000 "+testDir+"/unreadable.txt")

		playbook := testUserPlaybookHeader(true) + `
  - name: Create unreadable file owned by root
    copy:
      content: "secret data"
      dest: ` + testDir + `/unreadable2.txt
      mode: "0600"
      owner: root
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "Failed to connect") {
			t.Skip("Skipping - testuser SSH not available")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		remoteExec(t, client, "touch "+testDir+"/empty.txt")

		playbook := playbookHeader + `
  - name: Slurp an empty file
    slurp:
      src: ` + testDir + `/empty.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success for empty file, got: %s", output)
		}
	})

	t.Run("file with special characters", func(t *testing.T) {
		remoteExec(t, client, "printf 'line1\\nline2\\ttab\\nend' > "+testDir+"/special.txt")

		playbook := playbookHeader + `
  - name: Slurp file with special chars
    slurp:
      src: ` + testDir + `/special.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("large file", func(t *testing.T) {
		remoteExec(t, client, "dd if=/dev/urandom of="+testDir+"/large.bin bs=1024 count=100 2>/dev/null")

		playbook := playbookHeader + `
  - name: Slurp a 100KB file
    slurp:
      src: ` + testDir + `/large.bin
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success for large file, got: %s", output)
		}
	})

	t.Run("symlink to file", func(t *testing.T) {
		remoteExec(t, client, "echo -n 'symlink target' > "+testDir+"/link-target.txt")
		remoteExec(t, client, "ln -sf "+testDir+"/link-target.txt "+testDir+"/link.txt")

		playbook := playbookHeader + `
  - name: Slurp a symlink
    slurp:
      src: ` + testDir + `/link.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success for symlink, got: %s", output)
		}
	})

	t.Run("symlink to nonexistent", func(t *testing.T) {
		remoteExec(t, client, "ln -sf "+testDir+"/does-not-exist "+testDir+"/broken-link")

		playbook := playbookHeader + `
  - name: Slurp a broken symlink
    slurp:
      src: ` + testDir + `/broken-link
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("Expected failure for broken symlink, got: %s", output)
		}
	})

	t.Run("file as directory in path", func(t *testing.T) {
		remoteExec(t, client, "echo 'data' > "+testDir+"/regular-file.txt")

		playbook := playbookHeader + `
  - name: Slurp path treating file as directory
    slurp:
      src: ` + testDir + `/regular-file.txt/somefile
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("Expected failure, got: %s", output)
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		remoteExec(t, client, "echo -n 'idempotent content' > "+testDir+"/idem.txt")

		playbook := playbookHeader + `
  - name: Slurp for idempotency test
    slurp:
      src: ` + testDir + `/idem.txt
`
		output1 := runPlaybook(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("First run failed: %s", output1)
		}

		output2 := runPlaybook(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}

		if strings.Contains(output1, "CHANGED") || strings.Contains(output2, "CHANGED") {
			t.Error("Slurp should never report changes")
		}
	})

	t.Run("template variable in path", func(t *testing.T) {
		remoteExec(t, client, "echo -n 'templated' > "+testDir+"/template.txt")

		playbook := playbookHeader + `
  - name: Slurp using variable path
    slurp:
      src: "{{ slurp_dir }}/template.txt"
    vars:
      slurp_dir: ` + testDir + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with template variable, got: %s", output)
		}
	})

	t.Run("multiline file", func(t *testing.T) {
		remoteExec(t, client, "printf 'line1\\nline2\\nline3\\n' > "+testDir+"/multiline.txt")

		playbook := playbookHeader + `
  - name: Slurp multiline file
    slurp:
      src: ` + testDir + `/multiline.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("file with unicode", func(t *testing.T) {
		remoteExec(t, client, "printf '日本語テスト' > "+testDir+"/unicode.txt")

		playbook := playbookHeader + `
  - name: Slurp unicode file
    slurp:
      src: ` + testDir + `/unicode.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("no src parameter", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Slurp without src
    slurp:
      src: ""
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("Expected failure without src, got: %s", output)
		}
	})
}
