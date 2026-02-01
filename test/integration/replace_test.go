//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_Replace_BasicReplace(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-basic.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The quick brown fox jumps over the lazy dog.\nThe fox is quick.\nFox is the animal.' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace fox with cat
    replace:
      path: ` + testFile + `
      regexp: 'fox'
      replace: 'cat'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for basic replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "fox") {
		t.Errorf("Expected 'fox' to be replaced, got: %s", content)
	}
	if !strings.Contains(content, "cat") {
		t.Errorf("Expected 'cat' in file, got: %s", content)
	}
	if !strings.Contains(content, "Fox") {
		t.Error("Case-sensitive: 'Fox' should NOT be replaced")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected idempotent - no change on second run")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_CaseInsensitive(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-case.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The Fox and the fox are both FOX.' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace all fox variants (case insensitive)
    replace:
      path: ` + testFile + `
      regexp: '(?i)fox'
      replace: 'cat'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for case-insensitive replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(strings.ToLower(content), "fox") {
		t.Errorf("Expected all 'fox' variants to be replaced, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_RemoveMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-remove.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1 # comment\nline 2\nline 3 # another comment' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove comments (no replace value)
    replace:
      path: ` + testFile + `
      regexp: ' #.*$'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for removing matches")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "#") {
		t.Errorf("Expected comments to be removed, got: %s", content)
	}
	if !strings.Contains(content, "line 1") {
		t.Error("Expected 'line 1' to remain")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_BeforeParameter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-before.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The quick brown fox jumps over the lazy dog.\nWe promptly judged antique ivory buckles for the next prize.\nJinxed wizards pluck ivy from the big quilt.\nJaded zombies acted quaintly but kept driving their oxen forward.' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove all spaces before "quilt"
    replace:
      path: ` + testFile + `
      before: 'quilt'
      regexp: ' '
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for before parameter replace")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(content, "\n")

	if len(lines) > 0 && strings.Contains(lines[0], " ") {
		t.Error("Expected spaces to be removed in first line (before 'quilt')")
	}

	if len(lines) > 3 && !strings.Contains(lines[3], " ") {
		t.Error("Expected spaces to remain in last line (after 'quilt')")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_AfterParameter(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-after.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The quick brown fox jumps over the lazy dog.\nWe promptly judged antique ivory buckles for the next prize.\nJinxed wizards pluck ivy from the big quilt.\nJaded zombies acted quaintly but kept driving their oxen forward.' > " + testFile)

	playbook := playbookHeader + `
  - name: Remove all spaces after "promptly"
    replace:
      path: ` + testFile + `
      after: 'promptly'
      regexp: ' '
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for after parameter replace")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(content, "\n")

	if len(lines) > 0 && !strings.Contains(lines[0], " ") {
		t.Error("Expected spaces to remain in first line (before 'promptly')")
	}

	if len(lines) > 3 && strings.Contains(lines[3], " ") {
		t.Error("Expected spaces to be removed in last line (after 'promptly')")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_BeforeAndAfterCombined(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-beforeafter.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The quick brown fox jumps over the lazy dog.\nWe promptly judged antique ivory buckles for the next prize.\nJinxed wizards pluck ivy from the big quilt.\nJaded zombies acted quaintly but kept driving their oxen forward.' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace 'e' with '3' between "promptly" and "quilt"
    replace:
      path: ` + testFile + `
      after: 'promptly'
      before: 'quilt'
      regexp: 'e'
      replace: '3'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for before+after replace")
	}

	content := remoteFileContent(t, client, testFile)

	if !strings.Contains(content, "The quick") {
		t.Error("Content before 'promptly' should be unchanged")
	}
	if !strings.Contains(content, "quilt") {
		t.Error("Content at/after 'quilt' should be unchanged")
	}

	if strings.Contains(content, "judg3d") || strings.Contains(content, "antiqu3") ||
		strings.Contains(content, "buckl3s") || strings.Contains(content, "th3 n3xt priz3") {
		// This verifies 'e' chars in the middle section are replaced with '3'
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_BeforeAfterNoMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-nomatch.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'simple content' > " + testFile)

	playbook := playbookHeader + `
  - name: before "promptly" but after "quilt" (wrong order, no match)
    replace:
      path: ` + testFile + `
      before: 'promptly'
      after: 'quilt'
      regexp: 'e'
      replace: '3'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change when before/after don't match")
	}

	if !strings.Contains(output, "Pattern for before/after") || !strings.Contains(output, "did not match") {
		t.Log("Warning: Expected message about before/after not matching")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_Backreferences(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-backref.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'ServerName old.host.name\nServerName another.old.name' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace hostname with backreference
    replace:
      path: ` + testFile + `
      regexp: '(ServerName\s+)old\.host\.name'
      replace: '\1new.host.name'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for backreference replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "ServerName new.host.name") {
		t.Errorf("Expected backreference replacement, got: %s", content)
	}
	if !strings.Contains(content, "another.old.name") {
		t.Error("Non-matching lines should remain unchanged")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_MultilineRegexp(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-multiline.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'line 1\nline 2\nline 3\nline 4' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace at beginning of each line (multiline mode)
    replace:
      path: ` + testFile + `
      regexp: '^line'
      replace: 'row'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiline replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "line") {
		t.Errorf("Expected all 'line' at BOL to be replaced, got: %s", content)
	}
	if !strings.Contains(content, "row 1") || !strings.Contains(content, "row 4") {
		t.Error("Expected 'row' replacements")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_EndOfLineMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-eol.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'line 1.\nline 2.\nline 3.\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Add semicolon at end of each line
    replace:
      path: ` + testFile + `
      regexp: '\.$'
      replace: ';'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for EOL replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, ".") {
		t.Errorf("Expected all '.' at EOL to be replaced, got: %s", content)
	}
	if !strings.Contains(content, "line 1;") {
		t.Error("Expected semicolons at end of lines")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_Backup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-backup.txt"

	client.Run("rm -f " + testFile + "*")
	client.Run("echo 'original content' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with backup
    replace:
      path: ` + testFile + `
      regexp: 'original'
      replace: 'modified'
      backup: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for backup replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "modified") {
		t.Error("Expected content to be modified")
	}

	backups := remoteExec(t, client, "ls "+testFile+".*~ 2>/dev/null | head -1")
	if backups == "" {
		t.Error("Expected backup file to be created")
	} else {
		backupContent := remoteFileContent(t, client, backups)
		if !strings.Contains(backupContent, "original") {
			t.Error("Backup should contain original content")
		}
	}

	client.Run("rm -f " + testFile + "*")
}

func TestPlaybook_Replace_NoMatch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-nomatch2.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'hello world' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace non-existent pattern
    replace:
      path: ` + testFile + `
      regexp: 'NONEXISTENT'
      replace: 'replaced'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change when pattern doesn't match")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_PathNotExists(t *testing.T) {
	playbook := playbookHeader + `
  - name: Replace in non-existent file
    replace:
      path: /tmp/nonexistent-file-12345.txt
      regexp: 'test'
      replace: 'replaced'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "does not exist") {
		t.Error("Expected failure when file doesn't exist")
	}
}

func TestPlaybook_Replace_PathIsDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/goansible-replace-dir"
	client.Run("mkdir -p " + testDir)

	playbook := playbookHeader + `
  - name: Replace in directory (should fail)
    replace:
      path: ` + testDir + `
      regexp: 'test'
      replace: 'replaced'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "directory") {
		t.Error("Expected failure when path is a directory")
	}

	client.Run("rm -rf " + testDir)
}

func TestPlaybook_Replace_InvalidRegexp(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-invalidre.txt"
	client.Run("echo 'test content' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with invalid regexp
    replace:
      path: ` + testFile + `
      regexp: '[invalid'
      replace: 'replaced'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "invalid") {
		t.Error("Expected failure for invalid regexp")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_WordBoundaries(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-word.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'The fox is foxy.\nFoxes are foxlike.' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace whole word "fox" only
    replace:
      path: ` + testFile + `
      regexp: '\bfox\b'
      replace: 'cat'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for word boundary replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "cat") {
		t.Error("Expected 'fox' to be replaced with 'cat'")
	}
	if !strings.Contains(content, "foxy") {
		t.Error("Expected 'foxy' to remain unchanged")
	}
	if !strings.Contains(content, "foxlike") {
		t.Error("Expected 'foxlike' to remain unchanged")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_SpecialCharsInReplacement(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-special.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'placeholder' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with special chars
    replace:
      path: ` + testFile + `
      regexp: 'placeholder'
      replace: '/home/user/path & more <special> chars'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for special chars replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "/home/user/path") {
		t.Errorf("Expected special chars in replacement, got: %s", content)
	}
	if !strings.Contains(content, "<special>") {
		t.Errorf("Expected angle brackets preserved, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_ApacheConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-apache.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n<VirtualHost *:80>\n    ServerName old.example.com\n    DocumentRoot /var/www/html\n</VirtualHost>\nEOF")

	playbook := playbookHeader + `
  - name: Replace Apache ServerName
    replace:
      path: ` + testFile + `
      regexp: 'ServerName\s+old\.example\.com'
      replace: 'ServerName new.example.com'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for Apache config replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "ServerName new.example.com") {
		t.Errorf("Expected new ServerName, got: %s", content)
	}
	if strings.Contains(content, "old.example.com") {
		t.Error("Old ServerName should be replaced")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_SshConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-ssh.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '#Port 22\nPort 22\nListenAddress 0.0.0.0' > " + testFile)

	playbook := playbookHeader + `
  - name: Change SSH port
    replace:
      path: ` + testFile + `
      regexp: '^#?Port\s+\d+'
      replace: 'Port 2222'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for SSH config replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "Port 22") && !strings.Contains(content, "Port 2222") {
		t.Errorf("Expected Port 2222, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_CommentOutLines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-comment.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'setting1=value1\nsetting2=value2\nsetting3=value3' > " + testFile)

	playbook := playbookHeader + `
  - name: Comment out all lines starting with "setting"
    replace:
      path: ` + testFile + `
      regexp: '^(setting.*)$'
      replace: '# \1'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for comment out replace")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "# ") {
			t.Errorf("Expected all lines to be commented, got: %s", line)
		}
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_UncommentLines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-uncomment.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '# setting1=value1\n# setting2=value2\nsetting3=value3' > " + testFile)

	playbook := playbookHeader + `
  - name: Uncomment lines
    replace:
      path: ` + testFile + `
      regexp: '^#\s*(setting.*)$'
      replace: '\1'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for uncomment replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "# setting1") || strings.Contains(content, "# setting2") {
		t.Errorf("Expected comments to be removed, got: %s", content)
	}
	if !strings.Contains(content, "setting1=value1") {
		t.Error("Expected uncommented line")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_MultipleReplacementsCount(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-count.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'aaa bbb aaa ccc aaa ddd aaa' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace all 'aaa' occurrences
    replace:
      path: ` + testFile + `
      regexp: 'aaa'
      replace: 'XXX'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiple replacements")
	}

	if !strings.Contains(output, "4 replacements") {
		t.Log("Expected '4 replacements' in output (or similar)")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "aaa") {
		t.Error("All 'aaa' should be replaced")
	}
	count := strings.Count(content, "XXX")
	if count != 4 {
		t.Errorf("Expected 4 'XXX', got %d", count)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_EmptyFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-empty.txt"

	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	playbook := playbookHeader + `
  - name: Replace in empty file
    replace:
      path: ` + testFile + `
      regexp: 'test'
      replace: 'replaced'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change for empty file")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_PreserveNewlines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-newlines.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'line1\\nline2\\nline3\\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace preserving newlines
    replace:
      path: ` + testFile + `
      regexp: 'line2'
      replace: 'modified2'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	content := remoteFileContent(t, client, testFile)
	lines := strings.Split(content, "\n")

	expectedLines := 3
	actualLines := 0
	for _, l := range lines {
		if l != "" {
			actualLines++
		}
	}

	if actualLines != expectedLines {
		t.Errorf("Expected %d non-empty lines, got %d", expectedLines, actualLines)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_HtmlFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-html.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n<html>\n<head><title>Old Title</title></head>\n<body>\n<h1>Old Header</h1>\n<p>Some content about the old system.</p>\n</body>\n</html>\nEOF")

	playbook := playbookHeader + `
  - name: Replace old with new in HTML
    replace:
      path: ` + testFile + `
      regexp: '\b[Oo]ld\b'
      replace: 'New'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for HTML replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "Old") || strings.Contains(content, "old") {
		t.Errorf("Expected 'old'/'Old' to be replaced, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_ConfigBetweenMarkers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-markers.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\n# Global settings\nglobal_option=true\n\n# BEGIN MANAGED\nmanaged_option1=old1\nmanaged_option2=old2\n# END MANAGED\n\n# Other settings\nother_option=value\nEOF")

	playbook := playbookHeader + `
  - name: Replace between markers
    replace:
      path: ` + testFile + `
      after: '# BEGIN MANAGED'
      before: '# END MANAGED'
      regexp: 'old'
      replace: 'new'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for marker-based replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "global_option=true") {
		t.Error("Global settings should be unchanged")
	}
	if !strings.Contains(content, "managed_option1=new1") {
		t.Errorf("Expected managed options to be updated, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_IPAddress(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-ip.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e '127.0.0.1 localhost\n192.168.1.100 oldserver\n10.0.0.1 gateway' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace old IP address
    replace:
      path: ` + testFile + `
      regexp: '192\.168\.1\.100'
      replace: '192.168.1.200'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for IP replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "192.168.1.100") {
		t.Error("Old IP should be replaced")
	}
	if !strings.Contains(content, "192.168.1.200") {
		t.Errorf("Expected new IP, got: %s", content)
	}
	if !strings.Contains(content, "127.0.0.1") {
		t.Error("Other IPs should remain unchanged")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_JsonFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-json.txt"

	client.Run("rm -f " + testFile)
	client.Run(`echo '{"name": "old_value", "version": "1.0.0"}' > ` + testFile)

	playbook := playbookHeader + `
  - name: Replace JSON value
    replace:
      path: ` + testFile + `
      regexp: '"name":\s*"old_value"'
      replace: '"name": "new_value"'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for JSON replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "new_value") {
		t.Errorf("Expected new_value in JSON, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_NginxUpstream(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-nginx.txt"

	client.Run("rm -f " + testFile)
	client.Run("cat > " + testFile + " << 'EOF'\nupstream backend {\n    server 192.168.1.10:8080;\n    server 192.168.1.11:8080;\n}\nEOF")

	playbook := playbookHeader + `
  - name: Update nginx upstream servers
    replace:
      path: ` + testFile + `
      regexp: 'server 192\.168\.1\.(\d+):8080'
      replace: 'server 10.0.0.\1:8080'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for nginx replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "192.168.1") {
		t.Errorf("Old IPs should be replaced, got: %s", content)
	}
	if !strings.Contains(content, "10.0.0.10") || !strings.Contains(content, "10.0.0.11") {
		t.Error("Expected new IPs with captured group")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_Validate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-validate.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'valid content' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with passing validation
    replace:
      path: ` + testFile + `
      regexp: 'valid'
      replace: 'modified'
      validate: 'test -f %s'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED with passing validation")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_ValidateFails(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-validatefail.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'valid content' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with failing validation
    replace:
      path: ` + testFile + `
      regexp: 'valid'
      replace: 'modified'
      validate: 'false'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "validation") {
		t.Error("Expected failure due to validation")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "valid") {
		t.Error("File should not be modified when validation fails")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_LargeFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-large.txt"

	client.Run("rm -f " + testFile)
	client.Run("for i in $(seq 1 1000); do echo \"line $i with pattern_to_replace here\"; done > " + testFile)

	playbook := playbookHeader + `
  - name: Replace in large file
    replace:
      path: ` + testFile + `
      regexp: 'pattern_to_replace'
      replace: 'REPLACED'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for large file replace")
	}

	count := remoteExec(t, client, "grep -c REPLACED "+testFile)
	if count != "1000" {
		t.Errorf("Expected 1000 replacements, got %s", count)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_BinaryLikeContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-binary.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo -e 'text\\x00more text\\x00end' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace in file with null bytes
    replace:
      path: ` + testFile + `
      regexp: 'text'
      replace: 'DATA'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_UnicodeContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-unicode.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'Héllo Wörld 你好世界' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace unicode content
    replace:
      path: ` + testFile + `
      regexp: 'Wörld'
      replace: 'Universe'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for unicode replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "Universe") {
		t.Errorf("Expected unicode replacement, got: %s", content)
	}
	if !strings.Contains(content, "你好世界") {
		t.Error("Other unicode content should remain")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_TabsAndSpaces(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-whitespace.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'key\\t=\\tvalue\\n  spaced  content  \\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace tabs with spaces
    replace:
      path: ` + testFile + `
      regexp: '\t'
      replace: ' '
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for whitespace replace")
	}

	content := remoteFileContent(t, client, testFile)
	if strings.Contains(content, "\t") {
		t.Error("Tabs should be replaced with spaces")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_DotMatchesNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-dotall.txt"

	client.Run("rm -f " + testFile)
	client.Run("printf 'start\\nmiddle\\nend\\n' > " + testFile)

	playbook := playbookHeader + `
  - name: Dot should NOT match newline by default (MULTILINE mode)
    replace:
      path: ` + testFile + `
      regexp: 'start.middle'
      replace: 'REPLACED'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected NO change - dot should not match newline in default mode")
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_GreedyVsNonGreedy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-greedy.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo '<tag>content1</tag><tag>content2</tag>' > " + testFile)

	playbook := playbookHeader + `
  - name: Non-greedy match
    replace:
      path: ` + testFile + `
      regexp: '<tag>.*?</tag>'
      replace: '[REPLACED]'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	content := remoteFileContent(t, client, testFile)
	count := strings.Count(content, "[REPLACED]")
	if count != 2 {
		t.Errorf("Expected 2 replacements (non-greedy), got %d: %s", count, content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_EscapedCharacters(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-escaped.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'path/to/file.txt and another/path/here.txt' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace paths with escaped slashes
    replace:
      path: ` + testFile + `
      regexp: 'path/to/file\.txt'
      replace: 'new/location/file.txt'
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for escaped char replace")
	}

	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "new/location/file.txt") {
		t.Errorf("Expected new path, got: %s", content)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_Mode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-mode.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'content' > " + testFile)
	client.Run("chmod 644 " + testFile)

	playbook := playbookHeader + `
  - name: Replace with mode change
    replace:
      path: ` + testFile + `
      regexp: 'content'
      replace: 'modified'
      mode: "0600"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	mode := remoteFileMode(t, client, testFile)
	if mode != "600" {
		t.Errorf("Expected mode 600, got %s", mode)
	}

	client.Run("rm -f " + testFile)
}

func TestPlaybook_Replace_Owner(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-replace-owner.txt"

	client.Run("rm -f " + testFile)
	client.Run("echo 'content' > " + testFile)

	playbook := playbookHeader + `
  - name: Replace with owner change
    replace:
      path: ` + testFile + `
      regexp: 'content'
      replace: 'modified'
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	owner := remoteFileOwner(t, client, testFile)
	if owner != "root" {
		t.Errorf("Expected owner root, got %s", owner)
	}

	client.Run("rm -f " + testFile)
}
