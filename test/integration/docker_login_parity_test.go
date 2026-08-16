//go:build integration

package integration

import (
	"encoding/base64"
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

	t.Run("credential helper stores reuses and erases credentials", func(t *testing.T) {
		const helperPath = "/usr/local/bin/docker-credential-dibra-test"
		const helperState = "/tmp/dibra-credential-helper-state"
		script := `#!/bin/sh
set -eu
state=/tmp/dibra-credential-helper-state
case "$1" in
  get)
    server="$(cat)"
    if [ ! -f "$state" ]; then
      echo "credentials not found" >&2
      exit 1
    fi
    IFS="$(printf '\t')" read -r stored_server username secret < "$state"
    if [ "$server" != "$stored_server" ]; then
      echo "credentials not found" >&2
      exit 1
    fi
    printf '{"Username":"%s","Secret":"%s"}\n' "$username" "$secret"
    ;;
  store)
    payload="$(cat)"
    server="$(printf '%s' "$payload" | sed -n 's/.*"ServerURL":"\([^"]*\)".*/\1/p')"
    username="$(printf '%s' "$payload" | sed -n 's/.*"Username":"\([^"]*\)".*/\1/p')"
    secret="$(printf '%s' "$payload" | sed -n 's/.*"Secret":"\([^"]*\)".*/\1/p')"
    printf '%s\t%s\t%s\n' "$server" "$username" "$secret" > "$state"
    chmod 600 "$state"
    ;;
  erase)
    cat >/dev/null
    rm -f "$state"
    ;;
  list)
    if [ ! -f "$state" ]; then
      printf '{}\n'
      exit 0
    fi
    IFS="$(printf '\t')" read -r server username secret < "$state"
    printf '{"%s":"%s"}\n' "$server" "$username"
    ;;
  *)
    exit 2
    ;;
esac
`
		encoded := base64.StdEncoding.EncodeToString([]byte(script))
		mustRemote(t, client, "printf '%s' '"+encoded+"' | base64 -d > "+helperPath+" && chmod 0755 "+helperPath+" && rm -f "+helperState)
		defer mustRemote(t, client, "rm -f "+helperPath+" "+helperState)
		mustRemote(t, client, `printf '%s\n' '{"credHelpers":{"localhost:5000":"dibra-test"}}' > `+configPath)

		arguments := "      registry_url: localhost:5000\n      username: testuser\n      password: testpass\n      config_path: " + configPath + "\n"
		created, output := runLogin("helper-create", arguments, "")
		if created == nil || created["changed"] != true {
			t.Fatalf("helper create failed: result=%#v output=%s", created, output)
		}
		if !remoteFileExists(t, client, helperState) || remoteFileMode(t, client, helperState) != "600" {
			t.Fatalf("helper state missing or insecure")
		}

		again, output := runLogin("helper-idempotent", arguments, "")
		if again == nil || again["changed"] != false {
			t.Fatalf("helper idempotency failed: result=%#v output=%s", again, output)
		}

		loggedOut, output := runLogin("helper-logout", "      state: absent\n      registry_url: localhost:5000\n      config_path: "+configPath+"\n", "")
		if loggedOut == nil || loggedOut["changed"] != true || remoteFileExists(t, client, helperState) {
			t.Fatalf("helper logout failed: result=%#v output=%s", loggedOut, output)
		}
	})

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
