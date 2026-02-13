//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_TemplateModule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/template")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("failed to create template testdata dir: %v", err)
	}

	writeFile := func(name, content string) string {
		path := filepath.Join(testdataDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
		return path
	}

	basicTemplate := writeFile("basic.j2", "Hello {{ name }}\n")
	customTemplate := writeFile("custom_delims.j2", "Before [[ value ]] After\n")
	childTemplate := writeFile("child.j2", "Child={{ child }}")
	nestedTemplate := writeFile("nested_parent.j2", "Parent={{ parent }} {% include 'child.j2' %}\n")
	newlineTemplate := writeFile("newline.j2", "Hello {{ name }}\nNext\n")
	trimTemplate := writeFile("trim_blocks.j2", "{%- if enabled %}\nvalue={{ value }}\n{%- endif %}\n")

	playbookBasic := playbookHeader + `
  - name: Basic template render
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-basic.txt
    vars:
      name: world
`
	playbookDestDir := playbookHeader + `
  - name: Template dest directory
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-dir/
    vars:
      name: dir
`
	playbookCustom := playbookHeader + `
  - name: Template with custom delimiters
    template:
      src: ` + customTemplate + `
      dest: /tmp/dibra-template-custom.txt
      variable_start_string: "[["
      variable_end_string: "]]"
    vars:
      value: custom
`
	playbookTrim := playbookHeader + `
  - name: Template with trim blocks
    template:
      src: ` + trimTemplate + `
      dest: /tmp/dibra-template-trim.txt
      trim_blocks: true
    vars:
      enabled: true
      value: trimmed
`
	playbookIdempotent := playbookHeader + `
  - name: Template idempotency
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-idempotent.txt
    vars:
      name: once
`
	playbookForceFalse := playbookHeader + `
  - name: Template with force disabled
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-force.txt
      force: false
    vars:
      name: ignored
`
	playbookValidate := playbookHeader + `
  - name: Template with validate
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-validate.txt
      validate: "/bin/sh -c 'test -s %s'"
    vars:
      name: ok
`
	playbookNewlines := playbookHeader + `
  - name: Template with newline sequence
    template:
      src: ` + newlineTemplate + `
      dest: /tmp/dibra-template-crlf.txt
      newline_sequence: "\r\n"
    vars:
      name: win
`
	playbookRegister := playbookHeader + `
  - name: Template with register
    template:
      src: ` + basicTemplate + `
      dest: /tmp/dibra-template-register.txt
    register: template_result
    vars:
      name: register

  - name: Write register fields
    copy:
      content: "changed={{ template_result.changed }} checksum={{ template_result.checksum }} dest={{ template_result.dest }}"
      dest: /tmp/dibra-template-register-meta.txt
      mode: "0644"
`
	playbookNested := playbookHeader + `
  - name: Template with nested includes
    template:
      src: ` + nestedTemplate + `
      dest: /tmp/dibra-template-nested.txt
    vars:
      parent: outer
      child: inner
`

	remoteExec(t, client, "rm -f /tmp/dibra-template-*.txt /tmp/dibra-template-dir/basic.j2")
	remoteExec(t, client, "rm -rf /tmp/dibra-template-dir")

	output := runPlaybook(t, playbookBasic)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("basic template failed: %s", output)
	}
	content := remoteFileContent(t, client, "/tmp/dibra-template-basic.txt")
	if strings.TrimSpace(content) != "Hello world" {
		t.Fatalf("expected rendered content, got: %s", content)
	}

	output = runPlaybook(t, playbookDestDir)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("dest dir template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-dir/basic.j2")
	if strings.TrimSpace(content) != "Hello dir" {
		t.Fatalf("expected dir render, got: %s", content)
	}

	output = runPlaybook(t, playbookCustom)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("custom delimiter template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-custom.txt")
	if strings.TrimSpace(content) != "Before custom After" {
		t.Fatalf("expected custom delimiter render, got: %s", content)
	}

	output = runPlaybook(t, playbookTrim)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("trim blocks template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-trim.txt")
	if strings.TrimSpace(content) != "value=trimmed" {
		t.Fatalf("expected trim blocks render, got: %s", content)
	}

	output = runPlaybook(t, playbookIdempotent)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("idempotent template failed: %s", output)
	}
	output2 := runPlaybook(t, playbookIdempotent)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("idempotent rerun failed: %s", output2)
	}
	if strings.Contains(output2, "CHANGED") {
		t.Fatalf("expected no changes on rerun, got: %s", output2)
	}

	remoteExec(t, client, "echo 'original' > /tmp/dibra-template-force.txt")
	output = runPlaybook(t, playbookForceFalse)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("force=false template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-force.txt")
	if strings.TrimSpace(content) != "original" {
		t.Fatalf("expected original content with force=false, got: %s", content)
	}

	output = runPlaybook(t, playbookValidate)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("validate template failed: %s", output)
	}

	output = runPlaybook(t, playbookNewlines)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("newline sequence template failed: %s", output)
	}
	newlineCheck := remoteExec(t, client, "python3 - <<'PY'\nwith open('/tmp/dibra-template-crlf.txt','rb') as f:\n    data = f.read()\nprint('crlf' if b'\\r\\n' in data else 'no-crlf')\nPY")
	if strings.TrimSpace(newlineCheck) != "crlf" {
		t.Fatalf("expected CRLF newlines, got: %s", newlineCheck)
	}

	output = runPlaybook(t, playbookRegister)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("register template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-register-meta.txt")
	if !strings.Contains(content, "changed=") || !strings.Contains(content, "checksum=") {
		t.Fatalf("expected register metadata, got: %s", content)
	}

	output = runPlaybook(t, playbookNested)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("nested template failed: %s", output)
	}
	content = remoteFileContent(t, client, "/tmp/dibra-template-nested.txt")
	if strings.TrimSpace(content) != "Parent=outer Child=inner" {
		t.Fatalf("expected nested template render, got: %s", content)
	}

	_ = childTemplate
}
