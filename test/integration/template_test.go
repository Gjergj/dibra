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

	t.Run("BasicRender", func(t *testing.T) {
		output := runPlaybook(t, playbookBasic)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("basic template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-basic.txt")
		if strings.TrimSpace(content) != "Hello world" {
			t.Fatalf("expected rendered content, got: %s", content)
		}
	})

	t.Run("DestDirectory", func(t *testing.T) {
		output := runPlaybook(t, playbookDestDir)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("dest dir template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-dir/basic.j2")
		if strings.TrimSpace(content) != "Hello dir" {
			t.Fatalf("expected dir render, got: %s", content)
		}
	})

	t.Run("CustomDelimiters", func(t *testing.T) {
		output := runPlaybook(t, playbookCustom)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("custom delimiter template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-custom.txt")
		if strings.TrimSpace(content) != "Before custom After" {
			t.Fatalf("expected custom delimiter render, got: %s", content)
		}
	})

	t.Run("TrimBlocks", func(t *testing.T) {
		output := runPlaybook(t, playbookTrim)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("trim blocks template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-trim.txt")
		if strings.TrimSpace(content) != "value=trimmed" {
			t.Fatalf("expected trim blocks render, got: %s", content)
		}
	})

	t.Run("Idempotency", func(t *testing.T) {
		output := runPlaybook(t, playbookIdempotent)
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
	})

	t.Run("ForceFalse", func(t *testing.T) {
		remoteExec(t, client, "echo 'original' > /tmp/dibra-template-force.txt")
		output := runPlaybook(t, playbookForceFalse)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("force=false template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-force.txt")
		if strings.TrimSpace(content) != "original" {
			t.Fatalf("expected original content with force=false, got: %s", content)
		}
	})

	t.Run("Validate", func(t *testing.T) {
		output := runPlaybook(t, playbookValidate)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("validate template failed: %s", output)
		}
	})

	t.Run("NewlineSequence", func(t *testing.T) {
		output := runPlaybook(t, playbookNewlines)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("newline sequence template failed: %s", output)
		}
		newlineCheck := remoteExec(t, client, "python3 - <<'PY'\nwith open('/tmp/dibra-template-crlf.txt','rb') as f:\n    data = f.read()\nprint('crlf' if b'\\r\\n' in data else 'no-crlf')\nPY")
		if strings.TrimSpace(newlineCheck) != "crlf" {
			t.Fatalf("expected CRLF newlines, got: %s", newlineCheck)
		}
	})

	t.Run("Register", func(t *testing.T) {
		output := runPlaybook(t, playbookRegister)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("register template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-register-meta.txt")
		if !strings.Contains(content, "changed=") || !strings.Contains(content, "checksum=") {
			t.Fatalf("expected register metadata, got: %s", content)
		}
	})

	t.Run("NestedIncludes", func(t *testing.T) {
		output := runPlaybook(t, playbookNested)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("nested template failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-template-nested.txt")
		if strings.TrimSpace(content) != "Parent=outer Child=inner" {
			t.Fatalf("expected nested template render, got: %s", content)
		}
	})

	// --- Ansible-compatible filters ---

	t.Run("FilterBuiltinDefaultUpperLower", func(t *testing.T) {
		tpl := writeFile("filter_builtins.j2", `default={{ undef_var | default("fallback") }}
upper={{ name | upper }}
lower={{ name | lower }}
replace={{ name | replace("World", "Go") }}
length={{ items | length }}
join={{ items | join(",") }}
title={{ name | title }}
trim={{ padded | trim }}
`)
		pb := playbookHeader + `
  - name: Builtin filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-builtins.txt
    vars:
      name: "hello World"
      items:
        - a
        - b
        - c
      padded: "  spaced  "
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("builtin filters failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-builtins.txt")
		assertContains(t, content, "default=fallback")
		assertContains(t, content, "upper=HELLO WORLD")
		assertContains(t, content, "lower=hello world")
		assertContains(t, content, "replace=hello Go")
		assertContains(t, content, "length=3")
		assertContains(t, content, "join=a,b,c")
		assertContains(t, content, "title=Hello World")
		assertContains(t, content, "trim=spaced")
	})

	t.Run("FilterSplit", func(t *testing.T) {
		tpl := writeFile("filter_split.j2", `result={{ "a,b,c" | split(",") | join(":") }}
`)
		pb := playbookHeader + `
  - name: Split filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-split.txt
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("split filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-split.txt")
		assertContains(t, content, "result=a:b:c")
	})

	t.Run("FilterRegexReplace", func(t *testing.T) {
		tpl := writeFile("filter_regex.j2", `replaced={{ value | regex_replace("^Hello", "Goodbye") }}
search={{ value | regex_search("[0-9]+") }}
findall={{ nums | regex_findall("[0-9]+") | join(",") }}
`)
		pb := playbookHeader + `
  - name: Regex filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-regex.txt
    vars:
      value: "Hello World 42"
      nums: "abc 123 def 456 ghi"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("regex filters failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-regex.txt")
		assertContains(t, content, "replaced=Goodbye World 42")
		assertContains(t, content, "search=42")
		assertContains(t, content, "findall=123,456")
	})

	t.Run("FilterBasenameAndDirname", func(t *testing.T) {
		tpl := writeFile("filter_path.j2", `base={{ path | basename }}
dir={{ path | dirname }}
`)
		pb := playbookHeader + `
  - name: Path filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-path.txt
    vars:
      path: "/etc/nginx/nginx.conf"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("path filters failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-path.txt")
		assertContains(t, content, "base=nginx.conf")
		assertContains(t, content, "dir=/etc/nginx")
	})

	t.Run("FilterToYamlFromJson", func(t *testing.T) {
		tpl := writeFile("filter_yaml_json.j2", `yaml={{ data | to_yaml }}
parsed_name={{ json_str | from_json | attr("name") }}
nice_json={{ data | to_nice_json }}
`)
		pb := playbookHeader + `
  - name: YAML/JSON filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-yaml-json.txt
    vars:
      data:
        name: test
        port: 8080
      json_str: '{"name":"parsed","value":42}'
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("yaml/json filters failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-yaml-json.txt")
		assertContains(t, content, "parsed_name=parsed")
		// to_yaml produces YAML-formatted output
		assertContains(t, content, "yaml=")
		// to_nice_json produces indented JSON
		assertContains(t, content, "nice_json=")
	})

	t.Run("FilterQuote", func(t *testing.T) {
		tpl := writeFile("filter_quote.j2", `quoted={{ value | quote }}
`)
		pb := playbookHeader + `
  - name: Quote filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-quote.txt
    vars:
      value: "hello world"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("quote filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-quote.txt")
		assertContains(t, content, "quoted='hello world'")
	})

	t.Run("FilterTernary", func(t *testing.T) {
		tpl := writeFile("filter_ternary.j2", `when_true={{ enabled | ternary("yes", "no") }}
when_false={{ disabled | ternary("yes", "no") }}
`)
		pb := playbookHeader + `
  - name: Ternary filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-ternary.txt
    vars:
      enabled: true
      disabled: false
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("ternary filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-ternary.txt")
		assertContains(t, content, "when_true=yes")
		assertContains(t, content, "when_false=no")
	})

	t.Run("FilterB64EncodeAndDecode", func(t *testing.T) {
		tpl := writeFile("filter_b64.j2", `encoded={{ value | b64encode }}
decoded={{ encoded_val | b64decode }}
`)
		pb := playbookHeader + `
  - name: Base64 filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-b64.txt
    vars:
      value: "hello world"
      encoded_val: "aGVsbG8gd29ybGQ="
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("b64 filters failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-b64.txt")
		assertContains(t, content, "encoded=aGVsbG8gd29ybGQ=")
		assertContains(t, content, "decoded=hello world")
	})

	t.Run("FilterHash", func(t *testing.T) {
		tpl := writeFile("filter_hash.j2", `sha256={{ value | hash("sha256") }}
md5={{ value | hash("md5") }}
`)
		pb := playbookHeader + `
  - name: Hash filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-hash.txt
    vars:
      value: "hello"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("hash filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-hash.txt")
		// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
		assertContains(t, content, "sha256=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
		// md5("hello") = 5d41402abc4b2a76b9719d911017c592
		assertContains(t, content, "md5=5d41402abc4b2a76b9719d911017c592")
	})

	t.Run("FilterComment", func(t *testing.T) {
		tpl := writeFile("filter_comment.j2", `{{ text | comment }}
`)
		pb := playbookHeader + `
  - name: Comment filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-comment.txt
    vars:
      text: "line1\nline2\nline3"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("comment filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-comment.txt")
		assertContains(t, content, "# line1")
		assertContains(t, content, "# line2")
		assertContains(t, content, "# line3")
	})

	t.Run("FilterCombine", func(t *testing.T) {
		tpl := writeFile("filter_combine.j2", `{% set merged = dict1 | combine(dict2) %}name={{ merged.name }}
port={{ merged.port }}
env={{ merged.env }}
`)
		pb := playbookHeader + `
  - name: Combine filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-combine.txt
    vars:
      dict1:
        name: app
        port: 8080
      dict2:
        port: 9090
        env: prod
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("combine filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-combine.txt")
		assertContains(t, content, "name=app")
		assertContains(t, content, "port=9090") // dict2 overrides
		assertContains(t, content, "env=prod")
	})

	t.Run("FilterDict2ItemsAndItems2Dict", func(t *testing.T) {
		tpl := writeFile("filter_dict_items.j2", `{% set items = mydict | dict2items %}{% for item in items %}{{ item.key }}={{ item.value }}
{% endfor %}{% set rebuilt = items | items2dict %}name={{ rebuilt.name }}
`)
		pb := playbookHeader + `
  - name: Dict2items and items2dict
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-dict-items.txt
    vars:
      mydict:
        name: app
        port: 8080
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("dict2items/items2dict failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-dict-items.txt")
		assertContains(t, content, "name=app")
	})

	t.Run("FilterFlatten", func(t *testing.T) {
		tpl := writeFile("filter_flatten.j2", `flat={{ nested | flatten | join(",") }}
`)
		pb := playbookHeader + `
  - name: Flatten filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-flatten.txt
    vars:
      nested:
        - - 1
          - 2
        - - 3
          - 4
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("flatten filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-flatten.txt")
		assertContains(t, content, "flat=1,2,3,4")
	})

	t.Run("FilterTypeDebug", func(t *testing.T) {
		tpl := writeFile("filter_type_debug.j2", `type={{ value | type_debug }}
`)
		pb := playbookHeader + `
  - name: Type debug filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-type.txt
    vars:
      value: "hello"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("type_debug filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-type.txt")
		assertContains(t, content, "type=string")
	})

	t.Run("FilterSplitext", func(t *testing.T) {
		tpl := writeFile("filter_splitext.j2", `{% set parts = filename | splitext %}name={{ parts[0] }}
ext={{ parts[1] }}
`)
		pb := playbookHeader + `
  - name: Splitext filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-filter-splitext.txt
    vars:
      filename: "archive.tar.gz"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("splitext filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-filter-splitext.txt")
		assertContains(t, content, "name=archive.tar")
		assertContains(t, content, "ext=.gz")
	})

	// --- For loops ---

	t.Run("ForLoopVariables", func(t *testing.T) {
		tpl := writeFile("for_loop.j2", `{% for item in items %}{{ loop.index }}:{{ loop.index0 }}:{{ item }}:first={{ loop.first }}:last={{ loop.last }}
{% endfor %}`)
		pb := playbookHeader + `
  - name: For loop variables
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-forloop.txt
    vars:
      items:
        - alpha
        - beta
        - gamma
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("for loop failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-forloop.txt")
		assertContains(t, content, "1:0:alpha:first=True:last=False")
		assertContains(t, content, "2:1:beta:first=False:last=False")
		assertContains(t, content, "3:2:gamma:first=False:last=True")
	})

	t.Run("ForLoopOverDict", func(t *testing.T) {
		tpl := writeFile("for_dict.j2", `{% for key, value in config %}{{ key }}={{ value }}
{% endfor %}`)
		pb := playbookHeader + `
  - name: For loop over dict
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-fordict.txt
    vars:
      config:
        host: localhost
        port: 8080
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("for dict loop failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-fordict.txt")
		assertContains(t, content, "host=localhost")
		assertContains(t, content, "port=8080")
	})

	t.Run("ForLoopWithCondition", func(t *testing.T) {
		tpl := writeFile("for_if.j2", `{% for n in numbers if n > 2 %}{{ n }}{% if not loop.last %},{% endif %}{% endfor %}
`)
		pb := playbookHeader + `
  - name: For loop with if
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-forif.txt
    vars:
      numbers:
        - 1
        - 2
        - 3
        - 4
        - 5
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("for loop with condition failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-forif.txt")
		assertContains(t, content, "3,4,5")
	})

	// --- Conditionals ---

	t.Run("ComplexConditionals", func(t *testing.T) {
		tpl := writeFile("conditionals.j2", `{% if enabled and mode == "production" %}prod{% elif enabled and mode == "staging" %}staging{% else %}disabled{% endif %}
{% if "admin" in roles %}has_admin{% endif %}
{% if extra is defined %}extra_defined{% else %}extra_undefined{% endif %}
{% if missing is not defined %}missing_undefined{% endif %}
{% if count > 0 and not disabled %}active{% endif %}
`)
		pb := playbookHeader + `
  - name: Complex conditionals
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-conditionals.txt
    vars:
      enabled: true
      mode: staging
      roles:
        - user
        - admin
      extra: present
      count: 5
      disabled: false
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("complex conditionals failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-conditionals.txt")
		assertContains(t, content, "staging")
		assertContains(t, content, "has_admin")
		assertContains(t, content, "extra_defined")
		assertContains(t, content, "missing_undefined")
		assertContains(t, content, "active")
	})

	// --- Set statement ---

	t.Run("SetStatement", func(t *testing.T) {
		tpl := writeFile("set_stmt.j2", `{% set greeting = "Hello " ~ name %}{{ greeting }}
{% set port_str = port | string %}port={{ port_str }}
`)
		pb := playbookHeader + `
  - name: Set statement
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-set.txt
    vars:
      name: World
      port: 8080
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("set statement failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-set.txt")
		assertContains(t, content, "Hello World")
		assertContains(t, content, "port=8080")
	})

	// --- Macros ---

	t.Run("Macros", func(t *testing.T) {
		tpl := writeFile("macros.j2", `{% macro render_item(name, value) %}{{ name }}={{ value }}{% endmacro %}{{ render_item("host", hostname) }}
{{ render_item("port", port) }}
`)
		pb := playbookHeader + `
  - name: Macros
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-macros.txt
    vars:
      hostname: "web1.example.com"
      port: 443
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("macros failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-macros.txt")
		assertContains(t, content, "host=web1.example.com")
		assertContains(t, content, "port=443")
	})

	// --- Raw blocks ---

	t.Run("RawBlock", func(t *testing.T) {
		tpl := writeFile("raw_block.j2", `before
{% raw %}{{ this_is_not_rendered }}{% endraw %}
after
`)
		pb := playbookHeader + `
  - name: Raw block
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-raw.txt
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("raw block failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-raw.txt")
		assertContains(t, content, "{{ this_is_not_rendered }}")
		assertContains(t, content, "before")
		assertContains(t, content, "after")
	})

	// --- Whitespace control ---

	t.Run("WhitespaceControl", func(t *testing.T) {
		tpl := writeFile("whitespace.j2", `items:
{%- for item in items %}
  - {{ item }}
{%- endfor %}
`)
		pb := playbookHeader + `
  - name: Whitespace control
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-whitespace.txt
    vars:
      items:
        - one
        - two
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("whitespace control failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-whitespace.txt")
		assertContains(t, content, "items:")
		assertContains(t, content, "  - one")
		assertContains(t, content, "  - two")
	})

	// --- Magic variables ---

	t.Run("MagicVariables", func(t *testing.T) {
		tpl := writeFile("magic_vars.j2", `managed={{ ansible_managed }}
host={{ template_host }}
dest={{ template_destpath }}
`)
		pb := playbookHeader + `
  - name: Magic variables
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-magic.txt
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("magic variables failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-magic.txt")
		assertContains(t, content, "managed=Managed by dibra")
		assertContains(t, content, "host=testhost")
		assertContains(t, content, "dest=/tmp/dibra-tpl-magic.txt")
	})

	// --- Complex data structures ---

	t.Run("ComplexData", func(t *testing.T) {
		tpl := writeFile("complex_data.j2", `app_name={{ app.name }}
app_port={{ app.port }}
first_server={{ servers[0].host }}
second_port={{ servers[1].port }}
{% for server in servers %}server={{ server.host }}:{{ server.port }}
{% endfor %}`)
		pb := playbookHeader + `
  - name: Complex data structures
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-complex.txt
    vars:
      app:
        name: myapp
        port: 8080
      servers:
        - host: web1
          port: 80
        - host: web2
          port: 443
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("complex data failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-complex.txt")
		assertContains(t, content, "app_name=myapp")
		assertContains(t, content, "app_port=8080")
		assertContains(t, content, "first_server=web1")
		assertContains(t, content, "second_port=443")
		assertContains(t, content, "server=web1:80")
		assertContains(t, content, "server=web2:443")
	})

	// --- Template inheritance ---

	t.Run("TemplateInheritance", func(t *testing.T) {
		writeFile("base.j2", `header
{% block content %}default content{% endblock %}
footer
`)
		tpl := writeFile("child_extends.j2", `{% extends "base.j2" %}
{% block content %}custom content for {{ name }}{% endblock %}
`)
		pb := playbookHeader + `
  - name: Template inheritance
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-inherit.txt
    vars:
      name: dibra
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("template inheritance failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-inherit.txt")
		assertContains(t, content, "header")
		assertContains(t, content, "custom content for dibra")
		assertContains(t, content, "footer")
		// Should NOT contain "default content"
		if strings.Contains(content, "default content") {
			t.Fatalf("expected block override, but found default content in: %s", content)
		}
	})

	// --- Multiple chained filters ---

	t.Run("FilterChaining", func(t *testing.T) {
		tpl := writeFile("filter_chain.j2", `result={{ value | upper | replace("HELLO", "HI") | split(" ") | first }}
b64round={{ value | b64encode | b64decode }}
`)
		pb := playbookHeader + `
  - name: Filter chaining
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-chain.txt
    vars:
      value: "hello world"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("filter chaining failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-chain.txt")
		assertContains(t, content, "result=HI")
		assertContains(t, content, "b64round=hello world")
	})

	// --- Tojson builtin ---

	t.Run("FilterTojson", func(t *testing.T) {
		tpl := writeFile("filter_tojson.j2", `json={{ data | tojson }}
`)
		pb := playbookHeader + `
  - name: Tojson filter
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-tojson.txt
    vars:
      data:
        name: test
        count: 42
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("tojson filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-tojson.txt")
		// Should contain valid JSON output
		assertContains(t, content, "json=")
		assertContains(t, content, `"name"`)
		assertContains(t, content, `"count"`)
	})

	// --- Int/Float filters ---

	t.Run("FilterIntFloat", func(t *testing.T) {
		tpl := writeFile("filter_int_float.j2", `int_val={{ str_num | int }}
float_val={{ str_float | float }}
math={{ (str_num | int) + 10 }}
`)
		pb := playbookHeader + `
  - name: Int and float filters
    template:
      src: ` + tpl + `
      dest: /tmp/dibra-tpl-intfloat.txt
    vars:
      str_num: "42"
      str_float: "3.14"
`
		output := runPlaybook(t, pb)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("int/float filter failed: %s", output)
		}
		content := remoteFileContent(t, client, "/tmp/dibra-tpl-intfloat.txt")
		assertContains(t, content, "int_val=42")
		assertContains(t, content, "math=52")
	})

	_ = childTemplate
}

func assertContains(t *testing.T, content, expected string) {
	t.Helper()
	if !strings.Contains(content, expected) {
		t.Errorf("expected content to contain %q, got:\n%s", expected, content)
	}
}
