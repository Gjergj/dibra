//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerLoginParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "rm -f /tmp/dibra-login-*.json /tmp/dibra-docker-config.json /tmp/.dibra-agent")
	templatePath := writeResultTemplate(t, "login_result")
	configPath := "/tmp/dibra-docker-config.json"

	runLogin := func(testName, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-login-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Manage login
    community.docker.docker_login:
` + arguments + `
    register: login_result
` + taskOptions + `

  - name: Persist login result
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

	t.Run("credential helper is rejected", func(t *testing.T) {
		mustRemote(t, client, `printf '%s\n' '{"credsStore":"osxkeychain"}' > `+configPath)
		_, output := runLogin("helper", "      username: alice\n      password: secret\n      config_path: "+configPath+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "credential helper") {
			t.Fatalf("helper output = %s", output)
		}
	})

	t.Run("logout missing is unchanged", func(t *testing.T) {
		mustRemote(t, client, "rm -f "+configPath)
		result, output := runLogin("logout-missing", "      state: absent\n      config_path: "+configPath+"\n", "")
		if output != "" && result == nil {
			t.Fatalf("logout missing failed: %s", output)
		}
		if result["changed"] != false {
			t.Fatalf("logout missing = %#v", result)
		}
	})

	mustRemote(t, client, "docker rm -f dibra-login-registry >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker pull registry:2")
	mustRemote(t, client, "docker run -d --name dibra-login-registry -p 127.0.0.1:5000:5000 registry:2")
	defer mustRemote(t, client, "docker rm -f dibra-login-registry >/dev/null 2>&1 || true")

	t.Run("check mode authenticates without writing", func(t *testing.T) {
		mustRemote(t, client, "rm -f "+configPath)
		result, output := runLogin("check", "      registry_url: localhost:5000\n      username: testuser\n      password: testpass\n      config_path: "+configPath+"\n", "    check_mode: true\n")
		if result == nil {
			t.Fatalf("check login failed: %s", output)
		}
		if result["changed"] != true {
			t.Fatalf("check = %#v", result)
		}
		if remoteFileExists(t, client, configPath) {
			t.Fatal("check mode wrote config.json")
		}
	})

	t.Run("login writes 0600 config and is idempotent", func(t *testing.T) {
		mustRemote(t, client, "rm -f "+configPath)
		created, output := runLogin("create", "      registry_url: localhost:5000\n      username: testuser\n      password: testpass\n      config_path: "+configPath+"\n", "")
		if created == nil {
			t.Fatalf("login failed: %s", output)
		}
		if created["changed"] != true {
			t.Fatalf("create = %#v", created)
		}
		if loginResult, _ := created["login_result"].(map[string]any); loginResult["username"] != "testuser" {
			t.Fatalf("login_result = %#v", created["login_result"])
		}
		if remoteFileMode(t, client, configPath) != "600" {
			t.Fatalf("mode = %s", remoteFileMode(t, client, configPath))
		}

		again, output := runLogin("idempotent", "      registry: localhost:5000\n      username: testuser\n      password: testpass\n      config_path: "+configPath+"\n", "")
		if again == nil {
			t.Fatalf("idempotent login failed: %s", output)
		}
		if again["changed"] != false {
			t.Fatalf("idempotent = %#v", again)
		}

		reauth, output := runLogin("reauthorize", "      registry_url: localhost:5000\n      username: testuser\n      password: testpass\n      reauthorize: true\n      config_path: "+configPath+"\n", "")
		if reauth == nil {
			t.Fatalf("reauthorize failed: %s", output)
		}
		if reauth["changed"] != true {
			t.Fatalf("reauthorize = %#v", reauth)
		}

		loggedOut, output := runLogin("logout", "      state: absent\n      registry_url: localhost:5000\n      config_path: "+configPath+"\n", "")
		if loggedOut == nil {
			t.Fatalf("logout failed: %s", output)
		}
		if loggedOut["changed"] != true {
			t.Fatalf("logout = %#v", loggedOut)
		}
	})
}
