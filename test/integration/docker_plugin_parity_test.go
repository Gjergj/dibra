//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerPluginParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	if strings.TrimSpace(remoteExec(t, client, "test -e /dev/fuse && echo yes || echo no")) != "yes" {
		t.Skip("/dev/fuse is required to enable vieux/sshfs")
	}

	plugin := "vieux/sshfs"
	alias := "dibra-sshfs"
	mustRemote(t, client, "docker plugin disable -f "+alias+" >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker plugin rm -f "+alias+" >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-plugin-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker plugin disable -f "+alias+" >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker plugin rm -f "+alias+" >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "plugin_result")
	runPlugin := func(testName, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-plugin-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Manage plugin
    community.docker.docker_plugin:
` + arguments + `
    register: plugin_result
` + taskOptions + `

  - name: Persist plugin result
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

	t.Run("disable missing fails", func(t *testing.T) {
		_, output := runPlugin("disable-missing", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      state: disable\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Plugin not found: Plugin does not exist.") {
			t.Fatalf("disable missing output = %s", output)
		}
	})

	t.Run("check mode install does not mutate", func(t *testing.T) {
		result, output := runPlugin("check-install", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n", "    check_mode: true\n    diff: true\n")
		if result == nil {
			t.Fatalf("check install failed: %s", output)
		}
		if result["changed"] != true {
			t.Fatalf("check = %#v", result)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker plugin inspect "+alias+" >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatal("check mode installed the plugin")
		}
	})

	t.Run("install enable disable remove", func(t *testing.T) {
		installed, output := runPlugin("install", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      plugin_options:\n        DEBUG: \"0\"\n", "")
		if installed == nil {
			t.Fatalf("install failed: %s", output)
		}
		if installed["changed"] != true {
			t.Fatalf("install = %#v", installed)
		}
		if _, found := installed["actions"]; found {
			t.Fatalf("regular present result must omit actions: %#v", installed)
		}
		pluginInfo, _ := installed["plugin"].(map[string]any)
		name, _ := pluginInfo["Name"].(string)
		if name != alias && name != alias+":latest" && name != plugin && name != plugin+":latest" {
			t.Fatalf("plugin = %#v", pluginInfo)
		}

		idempotent, output := runPlugin("idempotent", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      plugin_options:\n        DEBUG: \"0\"\n", "")
		if idempotent == nil {
			t.Fatalf("idempotent failed: %s", output)
		}
		if idempotent["changed"] != false {
			t.Fatalf("idempotent = %#v", idempotent)
		}

		debugged, output := runPlugin("debug", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      plugin_options:\n        DEBUG: \"0\"\n      debug: true\n", "")
		if debugged == nil {
			t.Fatalf("debug failed: %s", output)
		}
		actions, found := debugged["actions"].([]any)
		if !found || len(actions) != 0 {
			t.Fatalf("debug actions = %#v", debugged)
		}

		enabled, output := runPlugin("enable", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      state: enable\n", "")
		if enabled == nil {
			t.Fatalf("enable failed: %s", output)
		}
		if enabled["changed"] != true {
			t.Fatalf("enable = %#v", enabled)
		}

		disabled, output := runPlugin("disable", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      state: disable\n", "")
		if disabled == nil {
			t.Fatalf("disable failed: %s", output)
		}
		if disabled["changed"] != true {
			t.Fatalf("disable = %#v", disabled)
		}

		removed, output := runPlugin("absent", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      state: absent\n      force_remove: true\n", "")
		if removed == nil {
			t.Fatalf("remove failed: %s", output)
		}
		if removed["changed"] != true {
			t.Fatalf("remove = %#v", removed)
		}
		absentAgain, output := runPlugin("absent-again", "      plugin_name: "+plugin+"\n      alias: "+alias+"\n      state: absent\n", "")
		if absentAgain == nil {
			t.Fatalf("absent again failed: %s", output)
		}
		if absentAgain["changed"] != false {
			t.Fatalf("absent again = %#v", absentAgain)
		}
	})
}
