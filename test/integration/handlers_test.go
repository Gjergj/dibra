//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_Handlers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const remoteRoot = "/tmp/dibra-handlers"
	remoteExec(t, client, "rm -rf "+remoteRoot+" && mkdir -p "+remoteRoot)
	defer remoteExec(t, client, "rm -rf "+remoteRoot)

	t.Run("notification ordering flushing variables imports and idempotency", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "imported_handlers.yaml"), []byte(`
- name: imported handler
  shell:
    cmd: echo imported >> `+remoteRoot+`/events
`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "dynamic_handler.yaml"), []byte(`
- name: dynamic included task
  shell:
    cmd: echo included >> `+remoteRoot+`/events
`), 0o600); err != nil {
			t.Fatal(err)
		}
		playbookPath := filepath.Join(directory, "playbook.yaml")
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true
vars:
  service_name: caddy
  handler_value: value
tasks:
  - name: create primary config
    copy:
      content: primary
      dest: ` + remoteRoot + `/primary.conf
    notify:
      - duplicate handler
      - handler b
      - restart web
      - restart {{ service_name }}
      - imported handler
  - name: unchanged task does not add a notification
    copy:
      content: primary
      dest: ` + remoteRoot + `/primary.conf
    notify: never handler
  - name: loop changes notify once
    copy:
      content: "{{ item.content }}"
      dest: "` + remoteRoot + `/{{ item.name }}"
    loop:
      - {name: loop-a, content: a}
      - {name: loop-b, content: b}
    notify: handler b
  - name: changed_when suppresses notification
    shell:
      cmd: "true"
    changed_when: false
    notify: never handler
  - name: explicit flush
    meta: flush_handlers
  - name: notify a handler after the first flush
    copy:
      content: post-flush
      dest: ` + remoteRoot + `/post.conf
    notify: handler b
  - name: notify dynamic include for automatic flush
    copy:
      content: dynamic
      dest: ` + remoteRoot + `/dynamic.conf
    notify: dynamic handler
handlers:
  - name: restart {{ service_name }}
    listen: restart web
    shell:
      cmd: echo a-{{ handler_value }} >> ` + remoteRoot + `/events
  - name: handler b
    shell:
      cmd: echo b >> ` + remoteRoot + `/events
  - name: topic listener c
    listen: restart web
    shell:
      cmd: echo c >> ` + remoteRoot + `/events
  - name: import handler definitions
    import_tasks: imported_handlers.yaml
  - name: duplicate handler
    shell:
      cmd: echo obsolete >> ` + remoteRoot + `/events
  - name: duplicate handler
    shell:
      cmd: echo current >> ` + remoteRoot + `/events
  - name: dynamic handler
    include_tasks: dynamic_handler.yaml
`
		if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
			t.Fatal(err)
		}

		first := runPlaybookFromFile(t, playbookPath)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("first handler run failed: %s", first)
		}
		want := "a-value\nb\nc\nimported\ncurrent\nb\nincluded"
		if got := remoteFileContent(t, client, remoteRoot+"/events"); got != want {
			t.Fatalf("handler events = %q, want %q", got, want)
		}

		second := runPlaybookFromFile(t, playbookPath)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent handler run failed: %s", second)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/events"); got != want {
			t.Fatalf("unchanged second run executed handlers: %q", got)
		}
	})

	t.Run("failed host skips handlers unless forced", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/forced-events")
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true
tasks:
  - name: queue forced handler
    shell:
      cmd: "true"
    notify: forced handler
  - name: fail host
    command:
      argv: ["/bin/false"]
handlers:
  - name: forced handler
    shell:
      cmd: echo forced >> ` + remoteRoot + `/forced-events
`
		withoutForce := runPlaybook(t, playbook)
		if !strings.Contains(withoutForce, "FAILED") && !strings.Contains(withoutForce, "failed") {
			t.Fatalf("failure playbook did not fail: %s", withoutForce)
		}
		if remoteFileExists(t, client, remoteRoot+"/forced-events") {
			t.Fatal("handler ran for failed host without force_handlers")
		}

		withForce := runPlaybookWithArgs(t, playbook, "--force-handlers")
		if !strings.Contains(withForce, "FAILED") && !strings.Contains(withForce, "failed") {
			t.Fatalf("forced failure playbook did not retain task failure: %s", withForce)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/forced-events"); got != "forced" {
			t.Fatalf("forced handler output = %q", got)
		}
	})
}
