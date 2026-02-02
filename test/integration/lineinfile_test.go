//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_Lineinfile_BasicInsert(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-test.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert a line at EOF
    lineinfile:
      path: ` + testFile + `
      line: "line 4"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for line insert")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line 4") {
		t.Errorf("Expected 'line 4' in file, got: %s", content)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertAfterBOF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-bof.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert line at beginning of file
    lineinfile:
      path: ` + testFile + `
      line: "line 0"
      insertbefore: BOF
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for BOF insert")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || lines[0] != "line 0" {
		t.Errorf("Expected 'line 0' as first line, got: %s", content)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertAfterEOF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-eof.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert line at end of file
    lineinfile:
      path: ` + testFile + `
      line: "line 3"
      insertafter: EOF
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for EOF insert")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "line 3" {
		t.Errorf("Expected 'line 3' as last line, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertAfterRegex(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-after.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '[section_one]\n\n[section_two]\n\n[section_three]' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert after section_two
    lineinfile:
      path: ` + testFile + `
      line: "option = value"
      insertafter: "\\[section_two\\]"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "option = value") {
		t.Errorf("Expected 'option = value' in file, got: %s", content)
	}

	idx := strings.Index(content, "[section_two]")
	optionIdx := strings.Index(content, "option = value")
	section3Idx := strings.Index(content, "[section_three]")

	if optionIdx < idx || optionIdx > section3Idx {
		t.Error("Line should be inserted after [section_two] but before [section_three]")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertBeforeRegex(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-before.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '[section_one]\n\n[section_two]\n\n[section_three]' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert before section_three
    lineinfile:
      path: ` + testFile + `
      line: "before_option = value"
      insertbefore: "\\[section_three\\]"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertbefore")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "before_option = value") {
		t.Errorf("Expected 'before_option = value' in file, got: %s", content)
	}

	optionIdx := strings.Index(content, "before_option = value")
	section3Idx := strings.Index(content, "[section_three]")

	if optionIdx > section3Idx {
		t.Error("Line should be inserted before [section_three]")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_ReplaceWithRegexp(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-replace.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'This is line 1\nThis is line 2\nThis is line 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace line matching regexp
    lineinfile:
      path: ` + testFile + `
      regexp: "^This is line 2$"
      line: "This is the new line 2"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for line replacement")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "This is the new line 2") {
		t.Errorf("Expected replaced line, got: %s", content)
	}
	if strings.Contains(content, "This is line 2") && !strings.Contains(content, "This is the new line 2") {
		t.Error("Original line should be replaced")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_Backrefs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-backrefs.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'This is line 1\nREF this is line for backrefs REF\nThis is line 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with backreferences
    lineinfile:
      path: ` + testFile + `
      regexp: "^(REF) .* (REF)$"
      line: "\\1 modified content \\2"
      backrefs: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for backref replacement")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "REF modified content REF") {
		t.Errorf("Expected backrefs expanded, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_BackrefsNoMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-backrefs-nomatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	originalContent := remoteFileContent(t, client, testFile)

	playbook := playbookHeader + `
  - name: Backrefs with no match should not change file
    lineinfile:
      path: ` + testFile + `
      regexp: "^NOMATCH.*$"
      line: "\\1 new content \\2"
      backrefs: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change when backrefs pattern doesn't match")
	}

	content := remoteFileContent(t, client, testFile)
	if content != originalContent {
		t.Errorf("File should remain unchanged, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_SearchString(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-search.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'This is line 1\nThis is line 2\nThis is line 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace line by search_string
    lineinfile:
      path: ` + testFile + `
      search_string: "line 2"
      line: "This is the replaced line"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for search_string replacement")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "This is the replaced line") {
		t.Errorf("Expected replaced line, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_StateAbsent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-absent.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove line matching regexp
    lineinfile:
      path: ` + testFile + `
      regexp: "^line 2$"
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for line removal")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line 2") {
		t.Errorf("Line should be removed, got: %s", content)
	}
	if !strings.Contains(content, "line 1") || !strings.Contains(content, "line 3") {
		t.Error("Other lines should remain")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_StateAbsentSearchString(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-absent-search.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove line by search_string
    lineinfile:
      path: ` + testFile + `
      search_string: "line 2"
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for line removal")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line 2") {
		t.Errorf("Line should be removed, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_StateAbsentExactLine(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-absent-line.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove exact line
    lineinfile:
      path: ` + testFile + `
      line: "line 2"
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for exact line removal")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line 2") {
		t.Errorf("Line should be removed, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_CreateFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-create.txt"

	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Create file with line
    lineinfile:
      path: ` + testFile + `
      line: "new line in new file"
      create: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file creation")
	}

	if !remoteFileExists(t, client, testFile) {
		t.Error("File should be created")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "new line in new file") {
		t.Errorf("Expected line in new file, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_CreateFileFail(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-nocreate.txt"

	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Should fail without create
    lineinfile:
      path: ` + testFile + `
      line: "new line"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "does not exist") {
		t.Error("Expected failure when file doesn't exist and create=false")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_Backup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-backup.txt"

	client.Run("rm -f " + testFile + "*")
	client.Run("echo -e 'original line 1\noriginal line 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Modify with backup
    lineinfile:
      path: ` + testFile + `
      regexp: "^original line 1$"
      line: "modified line 1"
      backup: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for modification")
	}

	backupCheck := remoteExec(t, client, "ls "+testFile+".*~ 2>/dev/null | wc -l")
	if backupCheck == "0" {
		t.Error("Backup file should exist")
	}

	backupContent := remoteExec(t, client, "cat "+testFile+".*~")
	if !strings.Contains(backupContent, "original line 1") {
		t.Error("Backup should contain original content")
	}

	client.Run("rm -f " + testFile + "*")
}

func TestPlaybook_Lineinfile_FirstMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-firstmatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline1\nline1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace only first match
    lineinfile:
      path: ` + testFile + `
      regexp: "^line1$"
      line: "modified_line1"
      firstmatch: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for first match replacement")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if lines[0] != "modified_line1" {
		t.Errorf("First line should be modified, got: %s", lines[0])
	}

	line1Count := 0
	for _, line := range lines {
		if line == "line1" {
			line1Count++
		}
	}
	if line1Count != 2 {
		t.Errorf("Expected 2 remaining 'line1' entries, got %d", line1Count)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertAfterFirstMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-insertafter-fm.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline1\nline1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert after first match only
    lineinfile:
      path: ` + testFile + `
      line: "inserted"
      insertafter: "^line1$"
      firstmatch: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter with firstmatch")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) < 2 || lines[1] != "inserted" {
		t.Errorf("Line should be inserted after first line1, got: %v", lines)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertBeforeFirstMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-insertbefore-fm.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline1\nline1\nline2\nline3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert before first match only
    lineinfile:
      path: ` + testFile + `
      line: "inserted_before"
      insertbefore: "^line1$"
      firstmatch: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertbefore with firstmatch")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) == 0 || lines[0] != "inserted_before" {
		t.Errorf("Line should be inserted before first line1, got: %v", lines)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_ModeOwnerGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-perms.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Modify with permissions
    lineinfile:
      path: ` + testFile + `
      line: "line 3"
      mode: "0640"
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for modification with permissions")
	}

	mode := remoteFileMode(t, client, testFile)
	if mode != "640" {
		t.Errorf("Expected mode 640, got %s", mode)
	}

	owner := remoteFileOwner(t, client, testFile)
	if owner != "root" {
		t.Errorf("Expected owner root, got %s", owner)
	}

	group := remoteFileGroup(t, client, testFile)
	if group != "root" {
		t.Errorf("Expected group root, got %s", group)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_ValidateSuccess(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-validate.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1' > " + testFile)

	playbook := playbookHeader + `
  - name: Modify with successful validation
    lineinfile:
      path: ` + testFile + `
      line: "line 2"
      validate: "true %s"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for modification with successful validation")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line 2") {
		t.Error("Line should be added after successful validation")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_ValidateFail(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-validate-fail.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1' > " + testFile)

	playbook := playbookHeader + `
  - name: Modify with failed validation
    lineinfile:
      path: ` + testFile + `
      line: "line 2"
      validate: "/bin/false %s"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "validation") {
		t.Error("Expected failure for failed validation")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line 2") {
		t.Error("Line should NOT be added after failed validation")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_EmptyFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-empty.txt"

	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	playbook := playbookHeader + `
  - name: Add line to empty file
    lineinfile:
      path: ` + testFile + `
      line: "first line"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for adding to empty file")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "first line") {
		t.Errorf("Expected line in empty file, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_FileWithoutNewlineAtEOF(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-noeof.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'line 1\nline 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Add line to file without trailing newline
    lineinfile:
      path: ` + testFile + `
      line: "line 3"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for adding to file without EOF newline")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line 3") {
		t.Errorf("Expected line 3, got: %s", content)
	}

	lineCount := remoteExec(t, client, "wc -l < "+testFile)
	if lineCount != "3" {
		t.Errorf("Expected 3 lines, got %s", lineCount)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_SingleQuotes(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-quotes.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'original' > " + testFile)

	playbook := playbookHeader + `
  - name: Add line with single quotes
    lineinfile:
      path: ` + testFile + `
      line: "require('dotenv').config();"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for adding line with quotes")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "require('dotenv').config();") {
		t.Errorf("Expected line with quotes, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_SpecialRegexChars(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-special.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'test.value=old\nother.value=x' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace line with dots in regexp
    lineinfile:
      path: ` + testFile + `
      regexp: "^test\\.value=.*$"
      line: "test.value=new"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for regexp with special chars")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "test.value=new") {
		t.Errorf("Expected replaced line, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RegexpNoMatchInsertAfter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-nomatch-insert.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '[section_one]\nkey1=value1\n\n[section_two]\nkey2=value2' > " + testFile)

	playbook := playbookHeader + `
  - name: Add new key if not found after section
    lineinfile:
      path: ` + testFile + `
      regexp: "^newkey=.*$"
      line: "newkey=newvalue"
      insertafter: "\\[section_one\\]"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter when regexp doesn't match")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "newkey=newvalue") {
		t.Errorf("Expected newkey line, got: %s", content)
	}

	section1Idx := strings.Index(content, "[section_one]")
	newkeyIdx := strings.Index(content, "newkey=newvalue")
	section2Idx := strings.Index(content, "[section_two]")

	if newkeyIdx < section1Idx || newkeyIdx > section2Idx {
		t.Error("Line should be inserted after [section_one] but before [section_two]")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_MultipleRemoval(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-multiremove.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line1\nline2\nline1\nline3\nline1' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove all matching lines
    lineinfile:
      path: ` + testFile + `
      regexp: "^line1$"
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiple line removal")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line1") {
		t.Errorf("All line1 entries should be removed, got: %s", content)
	}

	lineCount := remoteExec(t, client, "wc -l < "+testFile)
	if lineCount != "2" {
		t.Errorf("Expected 2 lines remaining, got %s", lineCount)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_CreateWithParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-parent/subdir/test.txt"

	client.Run("rm -rf /tmp/goansible-lineinfile-parent")

	playbook := playbookHeader + `
  - name: Create file with parent dirs
    lineinfile:
      path: ` + testFile + `
      line: "line in nested file"
      create: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file creation with parent dirs")
	}

	if !remoteFileExists(t, client, testFile) {
		t.Error("File should be created in nested directory")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line in nested file") {
		t.Errorf("Expected line in new file, got: %s", content)
	}

	client.Run("rm -rf /tmp/goansible-lineinfile-parent")
}

func TestPlaybook_Lineinfile_RealWorldSSHDConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-sshd_config"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '#Port 22\n#PermitRootLogin yes\n#PasswordAuthentication yes' > " + testFile)

	playbook := playbookHeader + `
  - name: Disable root login
    lineinfile:
      path: ` + testFile + `
      regexp: "^#?PermitRootLogin"
      line: "PermitRootLogin no"

  - name: Disable password auth
    lineinfile:
      path: ` + testFile + `
      regexp: "^#?PasswordAuthentication"
      line: "PasswordAuthentication no"

  - name: Change SSH port
    lineinfile:
      path: ` + testFile + `
      regexp: "^#?Port"
      line: "Port 2222"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for sshd config modifications")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "PermitRootLogin no") {
		t.Error("Expected PermitRootLogin to be set to no")
	}
	if !strings.Contains(content, "PasswordAuthentication no") {
		t.Error("Expected PasswordAuthentication to be set to no")
	}
	if !strings.Contains(content, "Port 2222") {
		t.Error("Expected Port to be set to 2222")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RealWorldApacheConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-apache.conf"

	client.Run("rm -f " + testFile)
	client.Run(`echo -e '<VirtualHost *:80>\n    ServerName example.com\n    DocumentRoot /var/www/html\n</VirtualHost>' > ` + testFile)

	playbook := playbookHeader + `
  - name: Change DocumentRoot
    lineinfile:
      path: ` + testFile + `
      regexp: "^\\s*DocumentRoot"
      line: "    DocumentRoot /var/www/myapp"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for Apache config modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "DocumentRoot /var/www/myapp") {
		t.Errorf("Expected DocumentRoot to be changed, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RealWorldEnvironmentFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-env"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'PATH=/usr/bin\nHOME=/root\nLANG=en_US.UTF-8' > " + testFile)

	playbook := playbookHeader + `
  - name: Add custom path
    lineinfile:
      path: ` + testFile + `
      regexp: "^PATH="
      line: "PATH=/usr/local/bin:/usr/bin:/bin"

  - name: Add new variable
    lineinfile:
      path: ` + testFile + `
      line: "CUSTOM_VAR=myvalue"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for environment file modifications")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "PATH=/usr/local/bin:/usr/bin:/bin") {
		t.Error("Expected PATH to be updated")
	}
	if !strings.Contains(content, "CUSTOM_VAR=myvalue") {
		t.Error("Expected CUSTOM_VAR to be added")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RealWorldHostsFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-hosts"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '127.0.0.1 localhost\n::1 localhost' > " + testFile)

	playbook := playbookHeader + `
  - name: Add custom host entry
    lineinfile:
      path: ` + testFile + `
      line: "192.168.1.100 myserver.local"
      insertafter: EOF
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for hosts file modification")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "192.168.1.100 myserver.local") {
		t.Error("Expected host entry to be added")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RegexpWithInsertBefore(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-regexp-insertbefore.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Add line using regexp and insertbefore
    lineinfile:
      path: ` + testFile + `
      regexp: "^newline$"
      line: "newline"
      insertbefore: "^line 3$"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for regexp with insertbefore")
	}

	content := remoteFileContent(t, client, testFile)
	newlineIdx := strings.Index(content, "newline")
	line3Idx := strings.Index(content, "line 3")

	if newlineIdx == -1 || newlineIdx > line3Idx {
		t.Error("newline should be inserted before line 3")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_RegexpWithInsertAfter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-regexp-insertafter.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Add line using regexp and insertafter
    lineinfile:
      path: ` + testFile + `
      regexp: "^newline$"
      line: "newline"
      insertafter: "^line 1$"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for regexp with insertafter")
	}

	content := remoteFileContent(t, client, testFile)
	line1Idx := strings.Index(content, "line 1")
	newlineIdx := strings.Index(content, "newline")
	line2Idx := strings.Index(content, "line 2")

	if newlineIdx < line1Idx || newlineIdx > line2Idx {
		t.Error("newline should be inserted after line 1 but before line 2")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_InsertNoMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-insert-nomatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3' > " + testFile)

	playbook := playbookHeader + `
  - name: Insert after non-matching pattern (should go to EOF)
    lineinfile:
      path: ` + testFile + `
      line: "new line"
      insertafter: "^NOMATCH$"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for insertafter with no match")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) == 0 || lines[len(lines)-1] != "new line" {
		t.Error("Line should be inserted at EOF when insertafter doesn't match")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_DuplicatePrevention(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-duplicate.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nexisting line\nline 2' > " + testFile)

	playbook := playbookHeader + `
  - name: Add line that already exists
    lineinfile:
      path: ` + testFile + `
      line: "existing line"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change when line already exists")
	}

	lineCount := remoteExec(t, client, "grep -c 'existing line' "+testFile)
	if lineCount != "1" {
		t.Errorf("Expected exactly 1 occurrence of the line, got %s", lineCount)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_ReplaceLastMatchOnly(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-lastmatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'match_line\nother line\nmatch_line\nfinal line\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace last matching line (default behavior)
    lineinfile:
      path: ` + testFile + `
      regexp: "^match_line$"
      line: "modified_match_line"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for last match replacement")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	matchCount := 0
	modifiedIdx := -1
	originalIdx := -1
	for i, line := range lines {
		if line == "match_line" {
			matchCount++
			originalIdx = i
		}
		if line == "modified_match_line" {
			modifiedIdx = i
		}
	}

	if matchCount != 1 {
		t.Errorf("Expected 1 original match_line remaining, got %d", matchCount)
	}

	if modifiedIdx < originalIdx {
		t.Error("Modified line should be after original (last match)")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Lineinfile_MultilineSupport(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-lineinfile-multiline.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'initial' > " + testFile)

	playbook := playbookHeader + `
  - name: Add single line (multiline in YAML)
    lineinfile:
      path: ` + testFile + `
      line: "This is a complex line with special chars: $HOME; echo test"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for complex line addition")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "This is a complex line with special chars: $HOME; echo test") {
		t.Errorf("Expected complex line, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}
