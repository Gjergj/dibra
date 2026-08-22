//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
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

	t.Run("loop change notifies every templated handler", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/loop-events "+remoteRoot+"/memcached.conf "+remoteRoot+"/apache.conf")
		remoteExec(t, client, "printf apache > "+remoteRoot+"/apache.conf")
		playbook := handlerPlaybook(`
tasks:
  - name: Template services
    copy:
      content: "{{ item }}"
      dest: ` + remoteRoot + `/{{ item }}.conf
    notify: Restart {{ item }}
    loop:
      - memcached
      - apache
handlers:
  - name: Restart memcached
    shell:
      cmd: echo memcached >> ` + remoteRoot + `/loop-events
  - name: Restart apache
    shell:
      cmd: echo apache >> ` + remoteRoot + `/loop-events
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("loop notify playbook failed: %s", output)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/loop-events"); got != "memcached\napache" {
			t.Fatalf("loop handler events = %q, want both handlers after a single item change", got)
		}
	})

	t.Run("conditional handlers honor when", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/when-events")
		playbook := handlerPlaybook(`
vars:
  web_family: Debian
tasks:
  - name: Deploy web config
    copy:
      content: site
      dest: ` + remoteRoot + `/web.conf
    notify:
      - Restart nginx
      - Restart httpd
handlers:
  - name: Restart nginx
    when: web_family == "Debian"
    shell:
      cmd: echo nginx >> ` + remoteRoot + `/when-events
  - name: Restart httpd
    when: web_family == "RedHat"
    shell:
      cmd: echo httpd >> ` + remoteRoot + `/when-events
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("conditional handler playbook failed: %s", output)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/when-events"); got != "nginx" {
			t.Fatalf("conditional handler events = %q, want nginx only", got)
		}
	})

	t.Run("handler can notify another handler", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/chain-events")
		playbook := handlerPlaybook(`
tasks:
  - name: Deploy app config
    copy:
      content: app
      dest: ` + remoteRoot + `/app.conf
    notify: first
handlers:
  - name: first
    shell:
      cmd: echo first >> ` + remoteRoot + `/chain-events
    notify: second
  - name: second
    shell:
      cmd: echo second >> ` + remoteRoot + `/chain-events
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("handler chain playbook failed: %s", output)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/chain-events"); got != "first\nsecond" {
			t.Fatalf("chained handler events = %q", got)
		}
	})

	t.Run("notify names are case sensitive", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/case-events")
		playbook := handlerPlaybook(`
tasks:
  - name: Deploy nginx config
    copy:
      content: nginx
      dest: ` + remoteRoot + `/case.conf
    notify: restart nginx
handlers:
  - name: Restart nginx
    shell:
      cmd: echo restarted >> ` + remoteRoot + `/case-events
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("case-sensitive notify playbook failed: %s", output)
		}
		if !strings.Contains(output, `requested handler "restart nginx" was not found`) {
			t.Fatalf("expected missing-handler warning, got: %s", output)
		}
		if remoteFileExists(t, client, remoteRoot+"/case-events") {
			t.Fatal("mismatched notify name unexpectedly ran the handler")
		}
	})

	t.Run("play-level force_handlers runs after a later failure", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+remoteRoot+"/play-forced-events")
		playbook := handlerPlaybook(`
force_handlers: true
tasks:
  - name: queue play-level forced handler
    shell:
      cmd: "true"
    notify: play forced handler
  - name: fail host
    command:
      argv: ["/bin/false"]
handlers:
  - name: play forced handler
    shell:
      cmd: echo play-forced >> ` + remoteRoot + `/play-forced-events
`)
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") && !strings.Contains(output, "failed") {
			t.Fatalf("force_handlers playbook did not fail: %s", output)
		}
		if got := remoteFileContent(t, client, remoteRoot+"/play-forced-events"); got != "play-forced" {
			t.Fatalf("play-level force_handlers output = %q", got)
		}
	})

	t.Run("flush_handlers cannot be used as a handler", func(t *testing.T) {
		playbook := handlerPlaybook(`
tasks:
  - name: notify illegal meta handler
    copy:
      content: meta
      dest: ` + remoteRoot + `/meta.conf
    notify: bad flush
handlers:
  - name: bad flush
    meta: flush_handlers
`)
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") && !strings.Contains(output, "failed") {
			t.Fatalf("flush_handlers-as-handler did not fail: %s", output)
		}
		if !strings.Contains(output, "flush_handlers cannot be used as a handler") {
			t.Fatalf("expected flush_handlers-as-handler error, got: %s", output)
		}
	})
}

func TestPlaybook_HandlersNotifyServiceRestarts(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		confd      = "/etc/nginx/conf.d/dibra-handlers.conf"
		siteConf   = "/etc/nginx/conf.d/dibra-handlers-site.conf"
		sslConf    = "/etc/nginx/conf.d/dibra-handlers-ssl.conf"
		indexFile  = "/var/www/html/dibra-handlers.html"
		appUnit    = "/etc/systemd/system/dibra-handlers-app.service"
		appName    = "dibra-handlers-app"
		markerRoot = "/tmp/dibra-handlers-services"
	)
	cleanup := func() {
		remoteExec(t, client, "systemctl stop "+appName+".service >/dev/null 2>&1 || true")
		remoteExec(t, client, "rm -f "+confd+" "+siteConf+" "+sslConf+" "+indexFile+" "+appUnit)
		remoteExec(t, client, "rm -rf "+markerRoot)
		remoteExec(t, client, "systemctl daemon-reload >/dev/null 2>&1 || true")
		remoteExec(t, client, "systemctl start nginx >/dev/null 2>&1 || true")
	}
	cleanup()
	defer cleanup()
	remoteExec(t, client, "mkdir -p "+markerRoot+" /var/www/html /etc/nginx/conf.d")
	if start := remoteExec(t, client, "systemctl start nginx && systemctl is-active nginx"); start != "active" {
		t.Fatalf("nginx is not active: %s", start)
	}

	t.Run("template notifies a single nginx restart", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "nginx.conf.j2"), []byte("# marker={{ page_title }}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		playbookPath := filepath.Join(directory, "playbook.yaml")
		playbook := handlerPlaybook(`
vars:
  page_title: handlers-one
tasks:
  - name: Install nginx
    apt:
      name: nginx
      state: present
  - name: Deploy nginx configuration
    template:
      src: nginx.conf.j2
      dest: ` + confd + `
      owner: root
      group: root
      mode: "0644"
    notify: Restart nginx
handlers:
  - name: Restart nginx
    systemd_service:
      name: nginx
      state: restarted
`)
		if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
			t.Fatal(err)
		}
		pidBefore := nginxMainPID(t, client)
		first := runPlaybookFromFile(t, playbookPath)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("nginx restart playbook failed: %s", first)
		}
		if !strings.Contains(first, "Handler: Restart nginx") {
			t.Fatalf("expected nginx restart handler, got: %s", first)
		}
		pidAfter := nginxMainPID(t, client)
		if pidAfter == "" || pidAfter == pidBefore {
			t.Fatalf("nginx MainPID did not change after restart: before=%q after=%q", pidBefore, pidAfter)
		}
		if got := strings.TrimSpace(remoteFileContent(t, client, confd)); got != "# marker=handlers-one" {
			t.Fatalf("nginx conf = %q", got)
		}

		pidBefore = nginxMainPID(t, client)
		second := runPlaybookFromFile(t, playbookPath)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent nginx playbook failed: %s", second)
		}
		if strings.Contains(second, "Handler: Restart nginx") {
			t.Fatalf("unchanged template restarted nginx: %s", second)
		}
		if pidAfter := nginxMainPID(t, client); pidAfter != pidBefore {
			t.Fatalf("idempotent run changed nginx MainPID: before=%q after=%q", pidBefore, pidAfter)
		}
	})

	t.Run("multiple tasks notify one reload once", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+markerRoot+"/reloads")
		playbook := handlerPlaybook(`
tasks:
  - name: Update SSL configuration
    copy:
      content: "# ssl v1"
      dest: ` + sslConf + `
    notify: Reload nginx
  - name: Deploy site configuration
    copy:
      content: "# site v1"
      dest: ` + siteConf + `
    notify: Reload nginx
  - name: Enable site marker
    copy:
      content: enabled
      dest: ` + markerRoot + `/site-enabled
    notify: Reload nginx
handlers:
  - name: Reload nginx
    systemd_service:
      name: nginx
      state: reloaded
    notify: Record nginx reload
  - name: Record nginx reload
    shell:
      cmd: echo reloaded >> ` + markerRoot + `/reloads
`)
		first := runPlaybook(t, playbook)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("multi-notify reload failed: %s", first)
		}
		if strings.Count(first, "Handler: Reload nginx") != 1 {
			t.Fatalf("expected one reload handler, got: %s", first)
		}
		if got := remoteFileContent(t, client, markerRoot+"/reloads"); got != "reloaded" {
			t.Fatalf("reload count marker = %q, want a single reload", got)
		}
		if remoteExec(t, client, "systemctl is-active nginx") != "active" {
			t.Fatal("nginx stopped after reload")
		}

		second := runPlaybook(t, playbook)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent multi-notify failed: %s", second)
		}
		if strings.Contains(second, "Handler: Reload nginx") {
			t.Fatalf("unchanged files reloaded nginx: %s", second)
		}
		if got := remoteFileContent(t, client, markerRoot+"/reloads"); got != "reloaded" {
			t.Fatalf("second run reloaded again: %q", got)
		}
	})

	t.Run("one task notifies multiple handlers in definition order", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+markerRoot+"/order")
		playbook := handlerPlaybook(`
tasks:
  - name: Update application config
    copy:
      content: app-config
      dest: ` + markerRoot + `/app.yml
    notify:
      - Clear application cache
      - Restart application
      - Reload nginx
handlers:
  - name: Restart application
    shell:
      cmd: echo restart-app >> ` + markerRoot + `/order
  - name: Reload nginx
    systemd_service:
      name: nginx
      state: reloaded
  - name: Clear application cache
    shell:
      cmd: echo clear-cache >> ` + markerRoot + `/order
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("multi-handler notify failed: %s", output)
		}
		if got := remoteFileContent(t, client, markerRoot+"/order"); got != "restart-app\nclear-cache" {
			t.Fatalf("handler order = %q, want definition order", got)
		}
		restartAt := strings.Index(output, "Handler: Restart application")
		reloadAt := strings.Index(output, "Handler: Reload nginx")
		clearAt := strings.Index(output, "Handler: Clear application cache")
		if restartAt < 0 || reloadAt < 0 || clearAt < 0 || !(restartAt < reloadAt && reloadAt < clearAt) {
			t.Fatalf("handler log order was not definition order: %s", output)
		}
	})

	t.Run("validate then reload then verify", func(t *testing.T) {
		playbook := handlerPlaybook(`
tasks:
  - name: Update server block
    copy:
      content: "# validate-reload"
      dest: ` + confd + `
    notify:
      - Verify nginx is serving
      - Reload nginx
      - Validate nginx config
handlers:
  - name: Validate nginx config
    command:
      cmd: nginx -t
    changed_when: false
  - name: Reload nginx
    systemd_service:
      name: nginx
      state: reloaded
  - name: Verify nginx is serving
    uri:
      url: http://127.0.0.1/
      status_code:
        - 200
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("validate/reload/verify failed: %s", output)
		}
		validateAt := strings.Index(output, "Handler: Validate nginx config")
		reloadAt := strings.Index(output, "Handler: Reload nginx")
		verifyAt := strings.Index(output, "Handler: Verify nginx is serving")
		if validateAt < 0 || reloadAt < 0 || verifyAt < 0 || !(validateAt < reloadAt && reloadAt < verifyAt) {
			t.Fatalf("expected validate, reload, verify in definition order: %s", output)
		}
	})

	t.Run("listen topic runs every subscriber", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+markerRoot+"/listen")
		writeAppUnit(t, client, appUnit)
		playbook := handlerPlaybook(`
tasks:
  - name: Start application service
    systemd_service:
      name: ` + appName + `
      state: started
      enabled: true
  - name: Deploy application code
    copy:
      content: release-1
      dest: ` + markerRoot + `/release.txt
    notify: application changed
  - name: Update environment file
    copy:
      content: ENV=prod
      dest: ` + markerRoot + `/env
    notify: application changed
handlers:
  - name: Restart application service
    systemd_service:
      name: ` + appName + `
      state: restarted
    listen: application changed
  - name: Clear application cache
    shell:
      cmd: echo cache >> ` + markerRoot + `/listen
    listen: application changed
  - name: Notify monitoring system
    shell:
      cmd: echo monitor-{{ inventory_hostname }} >> ` + markerRoot + `/listen
    listen: application changed
`)
		first := runPlaybook(t, playbook)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("listen topic playbook failed: %s", first)
		}
		if strings.Count(first, "Handler: Restart application service") != 1 ||
			strings.Count(first, "Handler: Clear application cache") != 1 ||
			strings.Count(first, "Handler: Notify monitoring system") != 1 {
			t.Fatalf("expected each listen handler once, got: %s", first)
		}
		if got := remoteFileContent(t, client, markerRoot+"/listen"); got != "cache\nmonitor-testhost" {
			t.Fatalf("listen markers = %q", got)
		}
		if remoteExec(t, client, "systemctl is-active "+appName) != "active" {
			t.Fatal("application service was not running after listen handlers")
		}

		second := runPlaybook(t, playbook)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent listen playbook failed: %s", second)
		}
		if strings.Contains(second, "Handler: Restart application service") {
			t.Fatalf("unchanged deploy restarted the app: %s", second)
		}
	})

	t.Run("flush handlers before a health check", func(t *testing.T) {
		pidBefore := nginxMainPID(t, client)
		playbook := handlerPlaybook(`
tasks:
  - name: Deploy config
    copy:
      content: "# flushed"
      dest: ` + confd + `
    notify: Restart nginx
  - name: Force handlers to run NOW
    meta: flush_handlers
  - name: Verify service is running after restart
    uri:
      url: http://127.0.0.1/
      status_code:
        - 200
handlers:
  - name: Restart nginx
    systemd_service:
      name: nginx
      state: restarted
`)
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("flush then health check failed: %s", output)
		}
		handlerAt := strings.Index(output, "Handler: Restart nginx")
		flushAt := strings.Index(output, "Force handlers to run NOW")
		healthAt := strings.Index(output, "Verify service is running after restart")
		if handlerAt < 0 || flushAt < 0 || healthAt < 0 || !(flushAt < handlerAt && handlerAt < healthAt) {
			t.Fatalf("handler did not run between flush and health check: %s", output)
		}
		if pidAfter := nginxMainPID(t, client); pidAfter == "" || pidAfter == pidBefore {
			t.Fatalf("flush did not restart nginx: before=%q after=%q", pidBefore, pidAfter)
		}
	})

	t.Run("start as a task and restart as a handler", func(t *testing.T) {
		remoteExec(t, client, "systemctl stop "+appName+" >/dev/null 2>&1 || true; rm -f "+appUnit+"; systemctl daemon-reload >/dev/null 2>&1 || true")
		playbook := handlerPlaybook(`
tasks:
  - name: Install application unit
    copy:
      dest: ` + appUnit + `
      content: |
        [Unit]
        Description=Dibra handler test app
        [Service]
        Type=simple
        ExecStart=/bin/sleep infinity
        [Install]
        WantedBy=multi-user.target
      mode: "0644"
    notify: Restart application
  - name: Reload systemd
    systemd_service:
      daemon_reload: true
  - name: Start and enable application
    systemd_service:
      name: ` + appName + `
      state: started
      enabled: true
handlers:
  - name: Restart application
    systemd_service:
      name: ` + appName + `
      state: restarted
`)
		first := runPlaybook(t, playbook)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("start-task restart-handler failed: %s", first)
		}
		if !strings.Contains(first, "Handler: Restart application") {
			t.Fatalf("package/unit change did not restart the app: %s", first)
		}
		if remoteExec(t, client, "systemctl is-active "+appName) != "active" {
			t.Fatal("application was not left running")
		}
		pidBefore := remoteExec(t, client, "systemctl show -p MainPID --value "+appName)
		second := runPlaybook(t, playbook)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent start/restart playbook failed: %s", second)
		}
		if strings.Contains(second, "Handler: Restart application") {
			t.Fatalf("unchanged unit restarted the app: %s", second)
		}
		if pidAfter := remoteExec(t, client, "systemctl show -p MainPID --value "+appName); pidAfter != pidBefore {
			t.Fatalf("second run restarted the app: before=%q after=%q", pidBefore, pidAfter)
		}
	})

	t.Run("config and index changes reload nginx once", func(t *testing.T) {
		remoteExec(t, client, "rm -f "+markerRoot+"/apache-style")
		playbook := handlerPlaybook(`
tasks:
  - name: Install nginx
    apt:
      name: nginx
      state: present
    notify: Start nginx
  - name: Create custom index
    copy:
      content: "Welcome to {{ inventory_hostname }}!"
      dest: ` + indexFile + `
    notify: Reload nginx
  - name: Configure nginx marker
    lineinfile:
      path: ` + confd + `
      line: "# listen-test"
      create: true
    notify: Reload nginx
handlers:
  - name: Start nginx
    systemd_service:
      name: nginx
      state: started
  - name: Reload nginx
    systemd_service:
      name: nginx
      state: reloaded
    notify: Record reload
  - name: Record reload
    shell:
      cmd: echo reloaded >> ` + markerRoot + `/apache-style
`)
		first := runPlaybook(t, playbook)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("apache-style nginx playbook failed: %s", first)
		}
		if strings.Contains(first, "Handler: Start nginx") {
			t.Fatalf("already-installed nginx notified Start nginx: %s", first)
		}
		if strings.Count(first, "Handler: Reload nginx") != 1 {
			t.Fatalf("expected one reload after index/config change: %s", first)
		}
		if got := remoteFileContent(t, client, indexFile); got != "Welcome to testhost!" {
			t.Fatalf("index = %q", got)
		}

		second := runPlaybook(t, playbook)
		if strings.Contains(second, "FAILED") {
			t.Fatalf("idempotent apache-style playbook failed: %s", second)
		}
		if strings.Contains(second, "Handler: Reload nginx") || strings.Contains(second, "Handler: Start nginx") {
			t.Fatalf("unchanged apache-style run executed handlers: %s", second)
		}
		if got := remoteFileContent(t, client, markerRoot+"/apache-style"); got != "reloaded" {
			t.Fatalf("reload ran more than once: %q", got)
		}
	})

	t.Run("loop of configs restarts the app once", func(t *testing.T) {
		writeAppUnit(t, client, appUnit)
		remoteExec(t, client, "systemctl start "+appName)
		pidBefore := remoteExec(t, client, "systemctl show -p MainPID --value "+appName)
		playbook := handlerPlaybook(`
tasks:
  - name: Deploy main config
    copy:
      content: "{{ item.content }}"
      dest: "{{ item.dest }}"
    loop:
      - {content: app, dest: ` + markerRoot + `/app.conf}
      - {content: db, dest: ` + markerRoot + `/db.conf}
      - {content: cache, dest: ` + markerRoot + `/cache.conf}
    notify: Restart myapp
handlers:
  - name: Restart myapp
    systemd_service:
      name: ` + appName + `
      state: restarted
`)
		first := runPlaybook(t, playbook)
		if strings.Contains(first, "FAILED") {
			t.Fatalf("looped config restart failed: %s", first)
		}
		if strings.Count(first, "Handler: Restart myapp") != 1 {
			t.Fatalf("expected one restart after three config changes: %s", first)
		}
		pidAfter := remoteExec(t, client, "systemctl show -p MainPID --value "+appName)
		if pidAfter == "" || pidAfter == pidBefore {
			t.Fatalf("looped configs did not restart the app: before=%q after=%q", pidBefore, pidAfter)
		}
		second := runPlaybook(t, playbook)
		if strings.Contains(second, "Handler: Restart myapp") {
			t.Fatalf("unchanged loop restarted the app: %s", second)
		}
	})
}

func handlerPlaybook(body string) string {
	return strings.TrimSuffix(integrationTestConfig.playbookHeader(), "tasks:\n") + body
}

func nginxMainPID(t *testing.T, client *ssh.Client) string {
	t.Helper()
	pid := remoteExec(t, client, "systemctl show -p MainPID --value nginx")
	if pid == "" || pid == "0" {
		t.Fatalf("nginx MainPID = %q", pid)
	}
	return pid
}

func writeAppUnit(t *testing.T, client *ssh.Client, path string) {
	t.Helper()
	remoteExec(t, client, "printf '%s\\n' '[Unit]' 'Description=Dibra handler test app' '[Service]' 'Type=simple' 'ExecStart=/bin/sleep infinity' '[Install]' 'WantedBy=multi-user.target' > "+path+" && systemctl daemon-reload")
}
