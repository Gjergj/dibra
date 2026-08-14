//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

const composePullImage = "alpine:3.18"

func TestPlaybook_DockerComposeV2PullParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	root := "/tmp/dibra-compose-v2-pull"
	mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-compose-pull-*.json /tmp/.dibra-agent")
	mustRemote(t, client, "mkdir -p "+root)
	defer mustRemote(t, client, "rm -rf "+root)
	defer mustRemote(t, client, "docker rmi "+composePullImage+" alpine:3.19 >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker rmi does-not-exist:latest >/dev/null 2>&1 || true")

	t.Run("missing-image", func(t *testing.T) {
		project := root + "/missing-image"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: does-not-exist:latest
`)
		mustRemote(t, client, "docker rmi does-not-exist:latest >/dev/null 2>&1 || true")

		checkOutput := runComposePullNamed(t, "missing-check", project, "", "    check_mode: true\n")
		if !strings.Contains(checkOutput, "FAILED") && !strings.Contains(checkOutput, "changed") {
			t.Fatalf("missing image check = %s", checkOutput)
		}
		if strings.Contains(checkOutput, "FAILED") && !strings.Contains(checkOutput, "Error when processing") {
			t.Fatalf("missing image check failure = %s", checkOutput)
		}

		realOutput := runComposePullNamed(t, "missing-real", project, "", "")
		if !strings.Contains(realOutput, "FAILED") || !strings.Contains(realOutput, "Error when processing") {
			t.Fatalf("missing image pull = %s", realOutput)
		}
	})

	t.Run("policy-missing-and-always", func(t *testing.T) {
		project := root + "/policies"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: `+composePullImage+`
    command: ["sleep", "600"]
    stop_grace_period: 1s
`)
		mustRemote(t, client, "docker rmi "+composePullImage+" >/dev/null 2>&1 || true")

		missingCheck := runComposePull(t, client, "missing-1-check", project, "      policy: missing\n", "    check_mode: true\n")
		if missingCheck["changed"] != true || !actionHasStatus(asMaps(missingCheck["actions"]), "Pulling") {
			t.Fatalf("policy=missing check = %#v", missingCheck)
		}
		missing := runComposePull(t, client, "missing-1", project, "      policy: missing\n", "")
		if missing["changed"] != true || !actionHasStatus(asMaps(missing["actions"]), "Pulling") {
			t.Fatalf("policy=missing = %#v", missing)
		}

		missingAgainCheck := runComposePull(t, client, "missing-2-check", project, "      policy: missing\n", "    check_mode: true\n")
		if missingAgainCheck["changed"] != false && missingAgainCheck["changed"] != nil {
			t.Fatalf("policy=missing idempotent check = %#v", missingAgainCheck)
		}
		missingAgain := runComposePull(t, client, "missing-2", project, "      policy: missing\n", "")
		if missingAgain["changed"] != false && missingAgain["changed"] != nil {
			t.Fatalf("policy=missing idempotent = %#v", missingAgain)
		}

		mustRemote(t, client, "docker rmi "+composePullImage+" >/dev/null 2>&1 || true")

		alwaysCheck := runComposePull(t, client, "always-1-check", project, "      policy: always\n", "    check_mode: true\n")
		if alwaysCheck["changed"] != true || !actionHasStatus(asMaps(alwaysCheck["actions"]), "Pulling") {
			t.Fatalf("policy=always check = %#v", alwaysCheck)
		}
		always := runComposePull(t, client, "always-1", project, "      policy: always\n", "")
		if !actionHasStatus(asMaps(always["actions"]), "Pulling") || !remoteImageExists(t, client, composePullImage) {
			t.Fatalf("policy=always = %#v", always)
		}

		alwaysAgainCheck := runComposePull(t, client, "always-2-check", project, "      policy: always\n", "    check_mode: true\n")
		if alwaysAgainCheck["changed"] != true || !actionHasStatus(asMaps(alwaysAgainCheck["actions"]), "Pulling") {
			t.Fatalf("policy=always idempotent check = %#v", alwaysAgainCheck)
		}
		alwaysAgain := runComposePull(t, client, "always-2", project, "      policy: always\n", "")
		if alwaysAgain["changed"] != false && alwaysAgain["changed"] != nil {
			t.Fatalf("policy=always idempotent = %#v", alwaysAgain)
		}
		if !actionHasStatus(asMaps(alwaysAgain["actions"]), "Pulling") {
			t.Fatalf("policy=always idempotent actions = %#v", alwaysAgain["actions"])
		}
	})

	t.Run("flask-default-policy", func(t *testing.T) {
		project := root + "/flask"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: `+composePullImage+`
`)
		mustRemote(t, client, "docker rmi "+composePullImage+" >/dev/null 2>&1 || true")
		first := runComposePull(t, client, "flask-1", project, "", "")
		if !actionHasStatus(asMaps(first["actions"]), "Pulling") || !remoteImageExists(t, client, composePullImage) {
			t.Fatalf("default flask pull = %#v", first)
		}
		secondCheck := runComposePull(t, client, "flask-2-check", project, "", "    check_mode: true\n")
		if secondCheck["changed"] != true {
			t.Fatalf("default flask check = %#v", secondCheck)
		}
		second := runComposePull(t, client, "flask-2", project, "", "")
		if second["changed"] != false && second["changed"] != nil {
			t.Fatalf("default flask idempotent = %#v", second)
		}
	})

	t.Run("ignore-pull-failures", func(t *testing.T) {
		project := root + "/ignore-failures"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: `+composePullImage+`
  bad:
    image: does-not-exist:latest
`)
		mustRemote(t, client, "docker rmi "+composePullImage+" does-not-exist:latest >/dev/null 2>&1 || true")

		failed := runComposePullNamed(t, "ignore-fail", project, "", "")
		if !strings.Contains(failed, "FAILED") {
			t.Fatalf("expected mixed pull to fail: %s", failed)
		}

		ignored := runComposePull(t, client, "ignore-ok", project, "      ignore_pull_failures: true\n", "")
		if ignored["failed"] == true {
			t.Fatalf("ignore_pull_failures = %#v", ignored)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker image inspect "+composePullImage+" --format '{{.Id}}'")) == "" {
			t.Fatal("good image was not pulled")
		}
	})

	t.Run("ignore-buildable", func(t *testing.T) {
		project := root + "/ignore-buildable"
		mustRemote(t, client, "mkdir -p "+project+"/src")
		mustRemote(t, client, "printf 'FROM "+composePullImage+"\\nCMD [\"sleep\",\"infinity\"]\\n' > "+project+"/src/Dockerfile")
		writeCompose(t, client, project, `services:
  built:
    build: ./src
  pulled:
    image: `+composePullImage+`
`)
		mustRemote(t, client, "docker rmi "+composePullImage+" >/dev/null 2>&1 || true")
		result := runComposePull(t, client, "ignore-buildable", project, "      ignore_buildable: true\n", "")
		if result["failed"] == true {
			t.Fatalf("ignore_buildable = %#v", result)
		}
	})

	t.Run("services-and-include-deps", func(t *testing.T) {
		project := root + "/deps"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  db:
    image: `+composePullImage+`
  web:
    image: alpine:3.19
    depends_on:
      - db
`)
		mustRemote(t, client, "docker rmi "+composePullImage+" alpine:3.19 >/dev/null 2>&1 || true")
		webOnly := runComposePull(t, client, "services-web", project, "      services:\n        - web\n", "")
		if webOnly["changed"] != true {
			t.Fatalf("services web = %#v", webOnly)
		}
		if !remoteImageExists(t, client, "alpine:3.19") {
			t.Fatal("web image was not pulled")
		}
		if remoteImageExists(t, client, composePullImage) {
			t.Fatal("db dependency was pulled without include_deps")
		}

		withDeps := runComposePull(t, client, "include-deps", project, "      services:\n        - web\n      include_deps: true\n", "")
		if withDeps["failed"] == true {
			t.Fatalf("include_deps = %#v", withDeps)
		}
		if !remoteImageExists(t, client, composePullImage) {
			t.Fatal("db dependency was not pulled with include_deps")
		}
	})

	t.Run("definition", func(t *testing.T) {
		mustRemote(t, client, "docker rmi "+composePullImage+" >/dev/null 2>&1 || true")
		first := runComposePullDefinition(t, client, "def-1", "dibra-pull-def", composePullImage, "")
		if !actionHasStatus(asMaps(first["actions"]), "Pulling") || !remoteImageExists(t, client, composePullImage) {
			t.Fatalf("definition pull = %#v", first)
		}
		second := runComposePullDefinition(t, client, "def-2", "dibra-pull-def", composePullImage, "      policy: missing\n")
		if second["changed"] != false && second["changed"] != nil {
			t.Fatalf("definition missing idempotent = %#v", second)
		}
	})

	t.Run("files-env-profiles", func(t *testing.T) {
		project := root + "/files"
		mustRemote(t, client, "mkdir -p "+project)
		mustRemote(t, client, "printf 'IMAGE="+composePullImage+"\\n' > "+project+"/app.env")
		payload, err := json.Marshal(`services:
  web:
    image: ${IMAGE}
    profiles: ["web"]
  skipped:
    image: alpine:3.19
    profiles: ["other"]
`)
		if err != nil {
			t.Fatal(err)
		}
		mustRemote(t, client, fmt.Sprintf("python3 -c 'import json,pathlib; pathlib.Path(%q).write_text(json.loads(%q))'", project+"/stack.yaml", string(payload)))
		mustRemote(t, client, "docker rmi "+composePullImage+" alpine:3.19 >/dev/null 2>&1 || true")

		result := runComposePull(t, client, "files-env", project, "      files:\n        - stack.yaml\n      env_files:\n        - app.env\n      profiles:\n        - web\n      policy: missing\n", "")
		if result["changed"] != true {
			t.Fatalf("files/env/profiles = %#v", result)
		}
		if !remoteImageExists(t, client, composePullImage) {
			t.Fatal("profile web image was not pulled")
		}
		if remoteImageExists(t, client, "alpine:3.19") {
			t.Fatal("inactive profile image was pulled")
		}
	})
}

func runComposePull(t *testing.T, client *ssh.Client, name, project, arguments, taskOptions string) map[string]any {
	t.Helper()
	output := runComposePullNamed(t, name, project, arguments, taskOptions)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, "/tmp/dibra-compose-pull-"+name+".json")
}

func runComposePullNamed(t *testing.T, name, project, arguments, taskOptions string) string {
	t.Helper()
	templatePath := writeResultTemplate(t, "pull_result")
	remotePath := "/tmp/dibra-compose-pull-" + name + ".json"
	playbook := playbookHeader + `
  - name: Pull compose project
    community.docker.docker_compose_v2_pull:
      project_src: ` + project + `
` + arguments + `
    register: pull_result
` + taskOptions + `

  - name: Persist pull result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	return runPlaybook(t, playbook)
}

func runComposePullDefinition(t *testing.T, client *ssh.Client, name, projectName, image, extra string) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "pull_result")
	remotePath := "/tmp/dibra-compose-pull-" + name + ".json"
	playbook := playbookHeader + `
  - name: Pull inline compose
    community.docker.docker_compose_v2_pull:
      project_name: ` + projectName + `
      definition:
        services:
          web:
            image: ` + image + `
` + extra + `
    register: pull_result

  - name: Persist pull result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}

func remoteImageExists(t *testing.T, client *ssh.Client, reference string) bool {
	t.Helper()
	_, _, err := client.Run("docker image inspect " + reference)
	return err == nil
}
