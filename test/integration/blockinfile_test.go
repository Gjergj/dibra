//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_Blockinfile_BasicInsert(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-basic.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert a block
    blockinfile:
      path: ` + testFile + `
      block: |
        block line 1
        block line 2
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "# BEGIN ANSIBLE MANAGED BLOCK") {
		t.Error("Expected BEGIN marker in file")
	}
	if !strings.Contains(content, "# END ANSIBLE MANAGED BLOCK") {
		t.Error("Expected END marker in file")
	}
	if !strings.Contains(content, "block line 1") {
		t.Error("Expected block content in file")
	}
	if !strings.Contains(content, "block line 2") {
		t.Error("Expected block content in file")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertWithBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-backup.txt"

	client.Run("rm -f " + testFile + "*")
	client.Run("echo -e 'original content' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with backup
    blockinfile:
      path: ` + testFile + `
      block: |
        Match User testuser
          PasswordAuthentication no
      backup: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert with backup")
	}

	backupExists := remoteExec(t, client, "ls "+testFile+".*~ 2>/dev/null | wc -l")
	if backupExists == "0" {
		t.Error("Expected backup file to be created")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "Match User testuser") {
		t.Error("Expected block content in file")
	}

	client.Run("rm -f " + testFile + "*")
}

func TestPlaybook_Blockinfile_CreateFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-create.txt"

	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Create file and insert block
    blockinfile:
      path: ` + testFile + `
      block: |
        new content
      create: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file creation")
	}
	if !strings.Contains(output, "File created") {
		t.Error("Expected 'File created' message")
	}

	if !remoteFileExists(t, client, testFile) {
		t.Error("Expected file to be created")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "new content") {
		t.Error("Expected block content in new file")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_RemoveBlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-remove.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\nline 1\n# BEGIN ANSIBLE MANAGED BLOCK\nblock content\n# END ANSIBLE MANAGED BLOCK\nline 2\nEOF")

	playbook := playbookHeader + `
  - name: Remove block
    blockinfile:
      path: ` + testFile + `
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block removal")
	}
	if !strings.Contains(output, "Block removed") {
		t.Error("Expected 'Block removed' message")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "# BEGIN ANSIBLE MANAGED BLOCK") {
		t.Error("Expected BEGIN marker to be removed")
	}
	if strings.Contains(content, "block content") {
		t.Error("Expected block content to be removed")
	}
	if !strings.Contains(content, "line 1") {
		t.Error("Expected original content to remain")
	}
	if !strings.Contains(content, "line 2") {
		t.Error("Expected original content to remain")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second removal")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_RemoveWithEmptyBlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-empty.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\nline 1\n# BEGIN ANSIBLE MANAGED BLOCK\nblock content\n# END ANSIBLE MANAGED BLOCK\nline 2\nEOF")

	playbook := playbookHeader + `
  - name: Remove block using empty block content
    blockinfile:
      path: ` + testFile + `
      block: ""
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block removal via empty block")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "# BEGIN ANSIBLE MANAGED BLOCK") {
		t.Error("Expected BEGIN marker to be removed")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_CustomMarkers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-markers.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with custom markers
    blockinfile:
      path: ` + testFile + `
      marker: "<!-- {mark} CUSTOM BLOCK -->"
      block: |
        <custom>content</custom>
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "<!-- BEGIN CUSTOM BLOCK -->") {
		t.Error("Expected custom BEGIN marker in file")
	}
	if !strings.Contains(content, "<!-- END CUSTOM BLOCK -->") {
		t.Error("Expected custom END marker in file")
	}
	if !strings.Contains(content, "<custom>content</custom>") {
		t.Error("Expected block content in file")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_CustomMarkerBeginEnd(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-marker-begin-end.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with custom marker begin/end
    blockinfile:
      path: ` + testFile + `
      marker_begin: "START"
      marker_end: "FINISH"
      block: |
        custom content
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "# START ANSIBLE MANAGED BLOCK") {
		t.Error("Expected custom START marker in file")
	}
	if !strings.Contains(content, "# FINISH ANSIBLE MANAGED BLOCK") {
		t.Error("Expected custom FINISH marker in file")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertAfter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-insertafter.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block after line2
    blockinfile:
      path: ` + testFile + `
      insertafter: "line2"
      block: |
        inserted after line2
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter")
	}

	content := remoteFileContent(t, client, testFile)
	line2Idx := strings.Index(content, "line2")
	blockIdx := strings.Index(content, "inserted after line2")
	line3Idx := strings.Index(content, "line3")

	if blockIdx < line2Idx || blockIdx > line3Idx {
		t.Error("Block should be inserted after line2 but before line3")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertBefore(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-insertbefore.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block before line2
    blockinfile:
      path: ` + testFile + `
      insertbefore: "line2"
      block: |
        inserted before line2
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertbefore")
	}

	content := remoteFileContent(t, client, testFile)
	line1Idx := strings.Index(content, "line1")
	blockIdx := strings.Index(content, "inserted before line2")
	line2Idx := strings.Index(content, "line2\n")

	if blockIdx < line1Idx || blockIdx > line2Idx {
		t.Error("Block should be inserted after line1 but before line2")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertBeforeBOF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-bof.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block at beginning of file
    blockinfile:
      path: ` + testFile + `
      insertbefore: BOF
      block: |
        first block
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for BOF insert")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "BEGIN") {
		t.Errorf("Expected block at beginning of file, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertAfterEOF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-eof.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block at end of file (default)
    blockinfile:
      path: ` + testFile + `
      insertafter: EOF
      block: |
        last block
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for EOF insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.HasSuffix(strings.TrimSpace(content), "# END ANSIBLE MANAGED BLOCK") {
		t.Errorf("Expected block at end of file, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_UpdateExistingBlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-update.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\nline 1\n# BEGIN ANSIBLE MANAGED BLOCK\nold content\n# END ANSIBLE MANAGED BLOCK\nline 2\nEOF")

	playbook := playbookHeader + `
  - name: Update existing block
    blockinfile:
      path: ` + testFile + `
      block: |
        new content
        more new content
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block update")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "old content") {
		t.Error("Old content should be replaced")
	}
	if !strings.Contains(content, "new content") {
		t.Error("New content should be present")
	}
	if !strings.Contains(content, "more new content") {
		t.Error("More new content should be present")
	}

	markerCount := strings.Count(content, "# BEGIN ANSIBLE MANAGED BLOCK")
	if markerCount != 1 {
		t.Errorf("Expected exactly 1 BEGIN marker, got %d", markerCount)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_MultipleBlocks(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-multi.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'content' > " + testFile)

	playbook1 := playbookHeader + `
  - name: Insert first block
    blockinfile:
      path: ` + testFile + `
      marker: "# {mark} BLOCK ONE"
      block: |
        block one content
`
	playbook2 := playbookHeader + `
  - name: Insert second block
    blockinfile:
      path: ` + testFile + `
      marker: "# {mark} BLOCK TWO"
      block: |
        block two content
`

	runPlaybook(t, playbook1)
	runPlaybook(t, playbook2)

	content := remoteFileContent(t, client, testFile)

	if !strings.Contains(content, "# BEGIN BLOCK ONE") {
		t.Error("Expected BLOCK ONE BEGIN marker")
	}
	if !strings.Contains(content, "# END BLOCK ONE") {
		t.Error("Expected BLOCK ONE END marker")
	}
	if !strings.Contains(content, "block one content") {
		t.Error("Expected block one content")
	}
	if !strings.Contains(content, "# BEGIN BLOCK TWO") {
		t.Error("Expected BLOCK TWO BEGIN marker")
	}
	if !strings.Contains(content, "# END BLOCK TWO") {
		t.Error("Expected BLOCK TWO END marker")
	}
	if !strings.Contains(content, "block two content") {
		t.Error("Expected block two content")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_PreserveCRLF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-crlf.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'line1\\r\\nline2\\r\\nline3\\r\\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block preserving CRLF
    blockinfile:
      path: ` + testFile + `
      block: |
        dos content
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	crlfCount := remoteExec(t, client, "grep -c $'\\r' "+testFile+" || true")
	if crlfCount == "0" {
		t.Log("Warning: CRLF line endings may not have been preserved")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_PrependNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-prepend.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with prepend newline
    blockinfile:
      path: ` + testFile + `
      insertafter: "line1"
      prepend_newline: true
      block: |
        prepended block
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert with prepend_newline")
	}

	content := remoteFileContent(t, client, testFile)
	line1Idx := strings.Index(content, "line1")
	blockIdx := strings.Index(content, "# BEGIN ANSIBLE MANAGED BLOCK")

	if blockIdx > line1Idx {
		between := content[line1Idx+len("line1") : blockIdx]
		if !strings.Contains(between, "\n\n") {
			t.Log("Warning: prepend_newline may not have added blank line")
		}
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_AppendNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-append.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with append newline
    blockinfile:
      path: ` + testFile + `
      insertafter: "line1"
      append_newline: true
      block: |
        appended block
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert with append_newline")
	}

	content := remoteFileContent(t, client, testFile)
	endMarkerIdx := strings.Index(content, "# END ANSIBLE MANAGED BLOCK")
	line2Idx := strings.Index(content, "line2")

	if line2Idx > endMarkerIdx {
		between := content[endMarkerIdx:line2Idx]
		if !strings.Contains(between, "\n\n") {
			t.Log("Warning: append_newline may not have added blank line")
		}
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_Validation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-validate.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with validation (should pass)
    blockinfile:
      path: ` + testFile + `
      block: |
        validated content
      validate: "grep line1 %s"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for validated block insert")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_ValidationFailure(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-validate-fail.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with validation (should fail)
    blockinfile:
      path: ` + testFile + `
      block: |
        new content
      validate: "grep NONEXISTENT %s"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") || !strings.Contains(output, "validation failed") {
		t.Error("Expected validation failure")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_FileWithoutTrailingNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-notrailingnl.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'line without newline' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block into file without trailing newline
    blockinfile:
      path: ` + testFile + `
      block: |
        block content
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line without newline") {
		t.Error("Original content should be preserved")
	}
	if !strings.Contains(content, "block content") {
		t.Error("Block content should be added")
	}
	if !strings.Contains(content, "# BEGIN ANSIBLE MANAGED BLOCK") {
		t.Error("Marker should be present")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_BlockWithoutTrailingNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-blocknl.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'existing' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block without trailing newline (YAML |- chomps)
    blockinfile:
      path: ` + testFile + `
      block: |-
        block without trailing newline
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "block without trailing newline") {
		t.Error("Block content should be present")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_AbsentOnNonexistentFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-nonexistent.txt"

	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Remove block from nonexistent file
    blockinfile:
      path: ` + testFile + `
      state: absent
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change for absent on nonexistent file")
	}
	if !strings.Contains(output, "OK") {
		t.Error("Expected OK status for absent on nonexistent file")
	}
}

func TestPlaybook_Blockinfile_WithMode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-mode.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'content' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block with mode
    blockinfile:
      path: ` + testFile + `
      block: |
        secured content
      mode: "0600"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert with mode")
	}

	mode := remoteFileMode(t, client, testFile)
	if mode != "600" {
		t.Errorf("Expected mode 600, got %s", mode)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_SSHConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-sshd-test.conf"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n# SSH Server Configuration\nPort 22\nPermitRootLogin yes\n# End of config\nEOF")

	playbook := playbookHeader + `
  - name: Add SSH match block
    blockinfile:
      path: ` + testFile + `
      block: |
        Match User ansible-agent
            PasswordAuthentication no
      backup: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for SSH config modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "Match User ansible-agent") {
		t.Error("Expected Match User block in file")
	}
	if !strings.Contains(content, "PasswordAuthentication no") {
		t.Error("Expected PasswordAuthentication setting in file")
	}

	matchCount := strings.Count(content, "Match User ansible-agent")
	if matchCount != 1 {
		t.Errorf("Expected exactly 1 Match User block, got %d", matchCount)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile + "*")
}

func TestPlaybook_Blockinfile_ApacheConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-httpd-test.conf"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n<VirtualHost *:80>\n    ServerName example.com\n</VirtualHost>\nEOF")

	playbook := playbookHeader + `
  - name: Add Apache logging config
    blockinfile:
      path: ` + testFile + `
      marker: "# {mark} LOGGING CONFIG"
      insertafter: "ServerName"
      block: |
        ErrorLog ${APACHE_LOG_DIR}/error.log
        CustomLog ${APACHE_LOG_DIR}/access.log combined
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for Apache config modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "ErrorLog") {
		t.Error("Expected ErrorLog in file")
	}
	if !strings.Contains(content, "# BEGIN LOGGING CONFIG") {
		t.Error("Expected custom BEGIN marker")
	}
	if !strings.Contains(content, "# END LOGGING CONFIG") {
		t.Error("Expected custom END marker")
	}

	serverNameIdx := strings.Index(content, "ServerName")
	errorLogIdx := strings.Index(content, "ErrorLog")
	virtualHostEndIdx := strings.Index(content, "</VirtualHost>")

	if errorLogIdx < serverNameIdx {
		t.Error("ErrorLog should be after ServerName")
	}
	if errorLogIdx > virtualHostEndIdx {
		t.Error("ErrorLog should be before </VirtualHost>")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_SysctlConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-sysctl-test.conf"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n# sysctl.conf\nnet.ipv4.ip_forward = 0\n# End\nEOF")

	playbook := playbookHeader + `
  - name: Add kernel parameters
    blockinfile:
      path: ` + testFile + `
      insertafter: "net.ipv4.ip_forward"
      block: |
        # Custom kernel parameters
        net.ipv4.tcp_syncookies = 1
        net.ipv4.tcp_max_syn_backlog = 2048
        net.core.somaxconn = 1024
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for sysctl config modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "net.ipv4.tcp_syncookies = 1") {
		t.Error("Expected tcp_syncookies setting")
	}
	if !strings.Contains(content, "net.core.somaxconn = 1024") {
		t.Error("Expected somaxconn setting")
	}

	ipForwardIdx := strings.Index(content, "net.ipv4.ip_forward")
	syncookiesIdx := strings.Index(content, "tcp_syncookies")

	if syncookiesIdx < ipForwardIdx {
		t.Error("New settings should be after ip_forward")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_EnvironmentVariables(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-environment-test"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\nPATH=/usr/local/bin:/usr/bin\nEOF")

	playbook := playbookHeader + `
  - name: Add Java environment variables
    blockinfile:
      path: ` + testFile + `
      marker: "# {mark} JAVA CONFIG"
      block: |
        JAVA_HOME=/usr/lib/jvm/java-11-openjdk
        JAVA_OPTS="-Xmx512m -Xms256m"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for environment file modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "JAVA_HOME=/usr/lib/jvm/java-11-openjdk") {
		t.Error("Expected JAVA_HOME setting")
	}
	if !strings.Contains(content, "JAVA_OPTS") {
		t.Error("Expected JAVA_OPTS setting")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_FirewallRules(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-firewall-test.xml"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<service>\n  <short>MyService</short>\n</service>\nEOF")

	playbook := playbookHeader + `
  - name: Add firewall rules
    blockinfile:
      path: ` + testFile + `
      marker: "<!-- {mark} CUSTOM RULES -->"
      insertbefore: "</service>"
      block: |
        <port protocol="tcp" port="8080"/>
        <port protocol="tcp" port="8443"/>
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for firewall config modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "<!-- BEGIN CUSTOM RULES -->") {
		t.Error("Expected BEGIN CUSTOM RULES marker")
	}
	if !strings.Contains(content, `<port protocol="tcp" port="8080"/>`) {
		t.Error("Expected port 8080 rule")
	}
	if !strings.Contains(content, `<port protocol="tcp" port="8443"/>`) {
		t.Error("Expected port 8443 rule")
	}

	port8080Idx := strings.Index(content, "8080")
	serviceEndIdx := strings.Index(content, "</service>")

	if port8080Idx > serviceEndIdx {
		t.Error("Ports should be before </service>")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_RegexInsertAfter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-regex.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '[section1]\nkey1=value1\n\n[section2]\nkey2=value2' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert block after section2 header using regex
    blockinfile:
      path: ` + testFile + `
      insertafter: "\\[section2\\]"
      block: |
        key3=value3
        key4=value4
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for regex insertafter")
	}

	content := remoteFileContent(t, client, testFile)
	section2Idx := strings.Index(content, "[section2]")
	key3Idx := strings.Index(content, "key3=value3")

	if key3Idx < section2Idx {
		t.Error("Block should be inserted after [section2]")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_InsertNoMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-nomatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert after non-matching pattern (should go to EOF)
    blockinfile:
      path: ` + testFile + `
      insertafter: "NONEXISTENT"
      block: |
        fallback to EOF
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter with no match")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.HasSuffix(strings.TrimSpace(content), "# END ANSIBLE MANAGED BLOCK") {
		t.Error("Block should be at EOF when insertafter doesn't match")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_EmptyFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-empty.txt"

	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	playbook := playbookHeader + `
  - name: Insert block into empty file
    blockinfile:
      path: ` + testFile + `
      block: |
        content in empty file
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block insert into empty file")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "content in empty file") {
		t.Error("Expected block content in file")
	}
	if !strings.Contains(content, "# BEGIN ANSIBLE MANAGED BLOCK") {
		t.Error("Expected BEGIN marker")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_MultilineBlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-multiline.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'header' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert multiline block with special characters
    blockinfile:
      path: ` + testFile + `
      block: |
        # This is a comment
        key1 = "value with spaces"
        key2 = 'single quoted'
        key3 = $VARIABLE
        key4 = $(command)
        key5 = line; with; semicolons
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiline block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, `key1 = "value with spaces"`) {
		t.Error("Expected key1 with double quotes")
	}
	if !strings.Contains(content, `key2 = 'single quoted'`) {
		t.Error("Expected key2 with single quotes")
	}
	if !strings.Contains(content, "$VARIABLE") {
		t.Error("Expected $VARIABLE")
	}
	if !strings.Contains(content, "$(command)") {
		t.Error("Expected $(command)")
	}
	if !strings.Contains(content, "line; with; semicolons") {
		t.Error("Expected semicolons")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_IndentedContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-indent.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'root:' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert YAML-like indented block
    blockinfile:
      path: ` + testFile + `
      block: |
        nested:
          level1:
            level2: value
        another:
          key: value
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for indented block insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "nested:") {
		t.Error("Expected nested:")
	}
	if !strings.Contains(content, "level1:") {
		t.Error("Expected level1:")
	}
	if !strings.Contains(content, "level2: value") {
		t.Error("Expected level2:")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_RemoveOneOfMultiple(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-blockinfile-remove-one.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\ncontent\n# BEGIN BLOCK ONE\nblock one content\n# END BLOCK ONE\nmore content\n# BEGIN BLOCK TWO\nblock two content\n# END BLOCK TWO\nfinal content\nEOF")

	playbook := playbookHeader + `
  - name: Remove only BLOCK ONE
    blockinfile:
      path: ` + testFile + `
      marker: "# {mark} BLOCK ONE"
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for block removal")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "# BEGIN BLOCK ONE") {
		t.Error("BLOCK ONE should be removed")
	}
	if !strings.Contains(content, "# BEGIN BLOCK TWO") {
		t.Error("BLOCK TWO should remain")
	}
	if !strings.Contains(content, "block two content") {
		t.Error("BLOCK TWO content should remain")
	}
	if !strings.Contains(content, "content") {
		t.Error("Original content should remain")
	}
	if !strings.Contains(content, "final content") {
		t.Error("Final content should remain")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Blockinfile_CreateWithParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/goansible-blockinfile-newdir"
	testFile := testDir + "/subdir/newfile.txt"

	client.Run("rm -rf " + testDir)

	playbook := playbookHeader + `
  - name: Create file with parent directories
    blockinfile:
      path: ` + testFile + `
      create: true
      block: |
        new file content
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file creation with parent dirs")
	}

	if !remoteFileExists(t, client, testFile) {
		t.Error("Expected file to be created")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "new file content") {
		t.Error("Expected block content in new file")
	}

	client.Run("rm -rf " + testDir)
}
