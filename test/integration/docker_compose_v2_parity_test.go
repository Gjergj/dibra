//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerComposeV2Parity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	root := "/tmp/dibra-compose-v2-parity"
	mustRemote(t, client, "rm -rf "+root+" /tmp/dibra-compose-v2-*.json /tmp/.dibra-agent")
	mustRemote(t, client, "mkdir -p "+root)
	defer mustRemote(t, client, "rm -rf "+root)

	t.Run("start-stop", func(t *testing.T) {
		project := root + "/start-stop"
		mustRemote(t, client, "mkdir -p "+project)
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
`)

		check := runComposeV2(t, client, "present-check", project, "      state: present\n", "    check_mode: true\n")
		if check["changed"] != true {
			t.Fatalf("present check = %#v", check)
		}
		if strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker compose ps -q")) != "" {
			t.Fatal("check mode created containers")
		}

		first := runComposeV2(t, client, "present-1", project, "      state: present\n", "")
		if first["changed"] != true {
			t.Fatalf("present = %#v", first)
		}
		if len(asMaps(first["containers"])) != 1 || len(asMaps(first["images"])) != 1 {
			t.Fatalf("present resources = %#v", first)
		}

		secondCheck := runComposeV2(t, client, "present-2-check", project, "      state: present\n", "    check_mode: true\n")
		if secondCheck["changed"] != false && secondCheck["changed"] != nil {
			t.Fatalf("present idempotent check = %#v", secondCheck)
		}
		second := runComposeV2(t, client, "present-2", project, "      state: present\n", "")
		if second["changed"] != false && second["changed"] != nil {
			t.Fatalf("present idempotent = %#v", second)
		}

		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "900"]
    stop_grace_period: 1s
`)
		changed := runComposeV2(t, client, "present-changed", project, "      state: present\n", "")
		if changed["changed"] != true {
			t.Fatalf("recreate = %#v", changed)
		}

		if result := runComposeV2(t, client, "assume-yes", project, "      state: present\n      assume_yes: true\n", ""); result == nil {
			t.Fatal("assume_yes failed")
		}

		absentCheck := runComposeV2(t, client, "absent-check", project, "      state: absent\n", "    check_mode: true\n")
		if absentCheck["changed"] != true {
			t.Fatalf("absent check = %#v", absentCheck)
		}
		if strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker compose ps -q --all")) == "" {
			t.Fatal("absent check removed containers")
		}

		absent := runComposeV2(t, client, "absent-1", project, "      state: absent\n", "")
		if absent["changed"] != true {
			t.Fatalf("absent = %#v", absent)
		}
		absentAgainCheck := runComposeV2(t, client, "absent-2-check", project, "      state: absent\n", "    check_mode: true\n")
		if absentAgainCheck["changed"] != false && absentAgainCheck["changed"] != nil {
			t.Fatalf("absent idempotent check = %#v", absentAgainCheck)
		}
		absentAgain := runComposeV2(t, client, "absent-2", project, "      state: absent\n", "")
		if absentAgain["changed"] != false && absentAgain["changed"] != nil {
			t.Fatalf("absent idempotent = %#v", absentAgain)
		}

		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
`)
		stoppedCreate := runComposeV2(t, client, "stopped-1", project, "      state: stopped\n", "")
		if stoppedCreate["changed"] != true {
			t.Fatalf("stopped create = %#v", stoppedCreate)
		}
		assertComposeServiceRunning(t, stoppedCreate, "web", false)
		if result := runComposeV2(t, client, "stopped-2", project, "      state: stopped\n", ""); result["changed"] != false && result["changed"] != nil {
			t.Fatalf("stopped idempotent = %#v", result)
		}
		started := runComposeV2(t, client, "started", project, "      state: present\n", "")
		if started["changed"] != true {
			t.Fatalf("start = %#v", started)
		}
		assertComposeServiceRunning(t, started, "web", true)
		restarted := runComposeV2(t, client, "restarted", project, "      state: restarted\n", "")
		if restarted["changed"] != true {
			t.Fatalf("restart = %#v", restarted)
		}
		assertComposeServiceRunning(t, restarted, "web", true)
		stoppedRunning := runComposeV2(t, client, "stopped-running", project, "      state: stopped\n      timeout: 1\n", "")
		if stoppedRunning["changed"] != true {
			t.Fatalf("stop running = %#v", stoppedRunning)
		}
		assertComposeServiceRunning(t, stoppedRunning, "web", false)
		restartStopped := runComposeV2(t, client, "restart-stopped", project, "      state: restarted\n", "")
		if restartStopped["changed"] != true {
			t.Fatalf("restart from stopped = %#v", restartStopped)
		}
		assertComposeServiceRunning(t, restartStopped, "web", true)
		runComposeV2(t, client, "cleanup", project, "      state: absent\n", "")
	})

	t.Run("definition", func(t *testing.T) {
		defer mustRemote(t, client, "docker compose -p dibra-v2-def down -v --remove-orphans >/dev/null 2>&1 || true")
		first := runComposeDefinition(t, client, "def-1", "dibra-v2-def", "sleep 600", "")
		if first["changed"] != true || len(asMaps(first["containers"])) != 1 {
			t.Fatalf("definition present = %#v", first)
		}
		second := runComposeDefinition(t, client, "def-2", "dibra-v2-def", "sleep 600", "")
		if second["changed"] != false && second["changed"] != nil {
			t.Fatalf("definition idempotent = %#v", second)
		}
		changed := runComposeDefinition(t, client, "def-3", "dibra-v2-def", "sleep 900", "")
		if changed["changed"] != true {
			t.Fatalf("definition changed = %#v", changed)
		}
		absent := runComposeDefinition(t, client, "def-absent", "dibra-v2-def", "sleep 900", "      state: absent\n")
		if absent["changed"] != true {
			t.Fatalf("definition absent = %#v", absent)
		}
	})

	t.Run("build", func(t *testing.T) {
		project := root + "/build"
		mustRemote(t, client, "mkdir -p "+project+"/build")
		defer composeDown(t, client, project, "")
		defer mustRemote(t, client, "docker rmi dibra-compose-v2-build:latest >/dev/null 2>&1 || true")
		writeCompose(t, client, project, `services:
  app:
    build: ./build
    image: dibra-compose-v2-build
    pull_policy: never
    stop_grace_period: 1s
`)
		mustRemote(t, client, "printf '%s\n' 'FROM alpine:latest' 'ENTRYPOINT [\"/bin/sh\", \"-c\", \"sleep 600\"]' > "+project+"/build/Dockerfile")
		first := runComposeV2(t, client, "build-1", project, "      state: present\n", "")
		if first["changed"] != true || len(asMaps(first["images"])) != 1 {
			t.Fatalf("build = %#v", first)
		}
		second := runComposeV2(t, client, "build-2", project, "      state: present\n", "")
		if second["changed"] != false && second["changed"] != nil {
			t.Fatalf("build idempotent = %#v", second)
		}
		buildAlwaysOutput := runComposeV2Output(t, project, "      state: present\n      build: always\n      ignore_build_events: true\n", "")
		if !strings.Contains(buildAlwaysOutput, "FAILED") || !strings.Contains(buildAlwaysOutput, "No such image: sha256:") {
			t.Fatalf("build always image-list failure = %s", buildAlwaysOutput)
		}
		runComposeV2(t, client, "build-cleanup", project, "      state: absent\n      remove_images: local\n", "")
		if strings.TrimSpace(remoteExec(t, client, "docker images -q dibra-compose-v2-build:latest")) != "" {
			t.Fatal("remove_images: local left the built image")
		}
	})

	t.Run("pull", func(t *testing.T) {
		project := root + "/pull"
		mustRemote(t, client, "mkdir -p "+project)
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  missing:
    image: dibra-compose-does-not-exist:latest
`)
		neverOutput := runComposeV2Output(t, project, "      state: present\n      pull: never\n", "")
		if !strings.Contains(neverOutput, "FAILED") || !(strings.Contains(neverOutput, "General error:") || strings.Contains(neverOutput, "Error when processing")) {
			t.Fatalf("pull never missing = %s", neverOutput)
		}

		writeCompose(t, client, project, `services:
  web:
    image: alpine:3.18
    command: ["sleep", "600"]
    stop_grace_period: 1s
`)
		mustRemote(t, client, "docker rmi alpine:3.18 >/dev/null 2>&1 || true")
		missing := runComposeV2(t, client, "pull-missing", project, "      state: present\n      pull: missing\n", "")
		if missing["changed"] != true {
			t.Fatalf("pull missing = %#v", missing)
		}
		if !actionHasStatus(asMaps(missing["actions"]), "Pulling") || !actionHasStatus(asMaps(missing["actions"]), "Creating") {
			t.Fatalf("pull missing actions = %#v", missing["actions"])
		}
		alwaysCheck := runComposeV2(t, client, "pull-always-check", project, "      state: present\n      pull: always\n", "    check_mode: true\n")
		if !actionHasStatus(asMaps(alwaysCheck["actions"]), "Pulling") {
			t.Fatalf("pull always check actions = %#v", alwaysCheck["actions"])
		}
		always := runComposeV2(t, client, "pull-always", project, "      state: present\n      pull: always\n", "")
		if always["changed"] != false && always["changed"] != nil {
			t.Fatalf("pull always after present should be unchanged: %#v", always)
		}
		if !actionHasStatus(asMaps(always["actions"]), "Pulling") {
			t.Fatalf("pull always actions = %#v", always["actions"])
		}
		runComposeV2(t, client, "pull-cleanup", project, "      state: absent\n", "")
	})

	t.Run("container-exit", func(t *testing.T) {
		project := root + "/exit"
		mustRemote(t, client, "mkdir -p "+project)
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sh", "-c", "exit 0"]
    stop_grace_period: 1s
`)
		output := runComposeV2Output(t, project, "      state: present\n      wait: true\n      wait_timeout: 10\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "exited (0)") {
			t.Fatalf("wait exit = %s", output)
		}
		ps := remoteExec(t, client, "cd "+project+" && docker compose ps -a --format '{{.Name}} {{.State}}'")
		if !strings.Contains(ps, "exited") {
			t.Fatalf("exited container missing: %s", ps)
		}
		runComposeV2(t, client, "exit-cleanup", project, "      state: absent\n", "")
	})
}

func TestPlaybook_DockerComposeV2Examples(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	root := "/tmp/dibra-compose-v2-examples"
	mustRemote(t, client, "rm -rf "+root)
	mustRemote(t, client, "mkdir -p "+root)
	defer mustRemote(t, client, "rm -rf "+root)
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")

	t.Run("files env_files scale services profiles", func(t *testing.T) {
		project := root + "/stack"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "stack")
		defer composeDown(t, client, project, "stack")
		mustRemote(t, client, "printf '%s\n' 'services:' '  web:' '    image: alpine:latest' '    command: [\"sleep\", \"600\"]' '    profiles: [\"web\"]' '  worker:' '    image: alpine:latest' '    command: [\"sleep\", \"600\"]' > "+project+"/compose.yaml")
		mustRemote(t, client, "printf '%s\n' 'services:' '  web:' '    environment:' '      FROM_OVERRIDE: \"1\"' > "+project+"/override.yaml")
		mustRemote(t, client, "printf 'FROM_ENVFILE=yes\n' > "+project+"/.env")
		result := runComposeV2(t, client, "examples", project, `      state: present
      files:
        - compose.yaml
        - override.yaml
      env_files:
        - .env
      profiles:
        - web
      services:
        - web
      scale:
        web: 2
      remove_orphans: true
`, "")
		if result["changed"] != true {
			t.Fatalf("examples = %#v", result)
		}
		if len(asMaps(result["containers"])) < 1 {
			t.Fatalf("containers = %#v", result["containers"])
		}
		runComposeV2(t, client, "examples-cleanup", project, "      state: absent\n      remove_volumes: true\n      remove_orphans: true\n", "")
	})

	t.Run("recreate always", func(t *testing.T) {
		project := root + "/recreate"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "")
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
`)
		first := runComposeV2(t, client, "recreate-1", project, "      state: present\n", "")
		if first["changed"] != true {
			t.Fatalf("create = %#v", first)
		}
		again := runComposeV2(t, client, "recreate-2", project, "      state: present\n      recreate: always\n      pull: always\n", "")
		if again["changed"] != true {
			t.Fatalf("recreate always = %#v", again)
		}
		runComposeV2(t, client, "recreate-cleanup", project, "      state: absent\n", "")
	})

	t.Run("remove orphans env file and scale", func(t *testing.T) {
		project := root + "/orphans"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "")
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    environment:
      FROM_ENVFILE: ${FROM_ENVFILE}
  worker:
    image: alpine:latest
    command: ["sleep", "600"]
`)
		mustRemote(t, client, "printf 'FROM_ENVFILE=yes\n' > "+project+"/app.env")
		first := runComposeV2(t, client, "orphan-1", project, `      state: present
      pull: missing
      env_files:
        - app.env
      scale:
        worker: 2
`, "")
		if first["changed"] != true || len(asMaps(first["containers"])) < 3 {
			t.Fatalf("scaled stack = %#v", first)
		}
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    environment:
      FROM_ENVFILE: ${FROM_ENVFILE}
`)
		cleaned := runComposeV2(t, client, "orphan-2", project, `      state: present
      pull: missing
      env_files:
        - app.env
      remove_orphans: true
`, "")
		if cleaned["changed"] != true {
			t.Fatalf("remove orphans = %#v", cleaned)
		}
		if len(asMaps(cleaned["containers"])) != 1 {
			t.Fatalf("expected only web after orphans removed: %#v", cleaned["containers"])
		}
		runComposeV2(t, client, "orphan-cleanup", project, "      state: absent\n      remove_orphans: true\n      remove_volumes: true\n", "")
	})

	t.Run("check_files_existing false", func(t *testing.T) {
		project := root + "/missing-compose"
		mustRemote(t, client, "mkdir -p "+project)
		output := runComposeV2Output(t, project, "      state: present\n      check_files_existing: false\n", "")
		if !strings.Contains(output, "FAILED") || strings.Contains(output, "does not contain compose.yaml") {
			t.Fatalf("check_files_existing=false should reach Compose CLI: %s", output)
		}
	})

	t.Run("flask-style web and db states", func(t *testing.T) {
		project := root + "/flask"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "dibra-docs-flask")
		defer composeDown(t, client, project, "dibra-docs-flask")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
  db:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
`)
		present := runComposeV2(t, client, "flask-present", project, "      project_name: dibra-docs-flask\n      state: present\n", "")
		if present["changed"] != true {
			t.Fatalf("present = %#v", present)
		}
		assertComposeServiceRunning(t, present, "web", true)
		assertComposeServiceRunning(t, present, "db", true)
		if cmd, _ := present["cmd"].(string); !strings.Contains(cmd, "--project-directory /tmp/") {
			t.Fatalf("project_src should be absolutized: %s", cmd)
		}

		stopped := runComposeV2(t, client, "flask-stopped", project, "      project_name: dibra-docs-flask\n      state: stopped\n", "")
		assertComposeServiceRunning(t, stopped, "web", false)
		assertComposeServiceRunning(t, stopped, "db", false)

		restarted := runComposeV2(t, client, "flask-restarted", project, "      project_name: dibra-docs-flask\n      state: restarted\n", "")
		assertComposeServiceRunning(t, restarted, "web", true)
		assertComposeServiceRunning(t, restarted, "db", true)

		runComposeV2(t, client, "flask-cleanup", project, "      project_name: dibra-docs-flask\n      state: absent\n", "")
	})

	t.Run("missing files and project_src", func(t *testing.T) {
		project := root + "/lookup"
		mustRemote(t, client, "mkdir -p "+project)
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
`)
		missingFile := runComposeV2Output(t, project, "      state: present\n      files:\n        - mycustom-compose.yml\n", "")
		if !strings.Contains(missingFile, "FAILED") || !strings.Contains(missingFile, `Cannot find Compose file "mycustom-compose.yml"`) {
			t.Fatalf("missing files = %s", missingFile)
		}

		missingDir := runComposeV2Output(t, root+"/no-such-project", "      state: present\n", "")
		if !strings.Contains(missingDir, "FAILED") || !strings.Contains(missingDir, "is not a directory") {
			t.Fatalf("missing project_src = %s", missingDir)
		}
	})

	t.Run("dependencies false", func(t *testing.T) {
		project := root + "/deps"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "")
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  db:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
    depends_on:
      - db
`)
		noDeps := runComposeV2(t, client, "deps-false", project, `      state: present
      services:
        - web
      dependencies: false
`, "")
		if noDeps["changed"] != true {
			t.Fatalf("no deps = %#v", noDeps)
		}
		if composeContainerByService(asMaps(noDeps["containers"]), "web") == nil {
			t.Fatalf("web missing: %#v", noDeps["containers"])
		}
		if composeContainerByService(asMaps(noDeps["containers"]), "db") != nil {
			t.Fatalf("db should not start with dependencies=false: %#v", noDeps["containers"])
		}

		withDeps := runComposeV2(t, client, "deps-true", project, `      state: present
      services:
        - web
`, "")
		if withDeps["changed"] != true {
			t.Fatalf("with deps = %#v", withDeps)
		}
		assertComposeServiceRunning(t, withDeps, "web", true)
		assertComposeServiceRunning(t, withDeps, "db", true)
		runComposeV2(t, client, "deps-cleanup", project, "      state: absent\n", "")
	})

	t.Run("wait until healthy", func(t *testing.T) {
		project := root + "/healthy"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "")
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    stop_grace_period: 1s
    healthcheck:
      test: ["CMD", "/bin/true"]
      interval: 1s
      timeout: 1s
      retries: 5
      start_period: 1s
`)
		result := runComposeV2(t, client, "wait-healthy", project, "      state: present\n      wait: true\n      wait_timeout: 30\n", "")
		if result["changed"] != true {
			t.Fatalf("wait healthy = %#v", result)
		}
		assertComposeServiceRunning(t, result, "web", true)
		web := composeContainerByService(asMaps(result["containers"]), "web")
		health, _ := web["Health"].(string)
		if health != "" && !strings.EqualFold(health, "healthy") {
			t.Fatalf("health = %q in %#v", health, web)
		}
		runComposeV2(t, client, "wait-healthy-cleanup", project, "      state: absent\n", "")
	})

	t.Run("renew anonymous volumes", func(t *testing.T) {
		project := root + "/anonvol"
		mustRemote(t, client, "mkdir -p "+project)
		composeDown(t, client, project, "")
		defer composeDown(t, client, project, "")
		writeCompose(t, client, project, `services:
  web:
    image: alpine:latest
    command: ["sleep", "600"]
    volumes:
      - /data
`)
		first := runComposeV2(t, client, "anon-1", project, "      state: present\n", "")
		if first["changed"] != true {
			t.Fatalf("create = %#v", first)
		}
		before := strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker inspect -f '{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}{{end}}{{end}}' $(docker compose ps -q web)"))
		if before == "" {
			t.Fatal("expected anonymous volume")
		}
		renewed := runComposeV2(t, client, "anon-2", project, "      state: present\n      recreate: always\n      renew_anon_volumes: true\n", "")
		if renewed["changed"] != true {
			t.Fatalf("renew = %#v", renewed)
		}
		after := strings.TrimSpace(remoteExec(t, client, "cd "+project+" && docker inspect -f '{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}{{end}}{{end}}' $(docker compose ps -q web)"))
		if after == "" || after == before {
			t.Fatalf("anonymous volume was not renewed: before=%q after=%q", before, after)
		}
		runComposeV2(t, client, "anon-cleanup", project, "      state: absent\n      remove_volumes: true\n", "")
	})
}

func runComposeV2(t *testing.T, client *ssh.Client, name, project, arguments, taskOptions string) map[string]any {
	t.Helper()
	output := runComposeV2Named(t, name, project, arguments, taskOptions)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", name, output)
	}
	return readRemoteJSONMap(t, client, "/tmp/dibra-compose-v2-"+name+".json")
}

func runComposeV2Output(t *testing.T, project, arguments, taskOptions string) string {
	t.Helper()
	return runComposeV2Named(t, "output", project, arguments, taskOptions)
}

func runComposeV2Named(t *testing.T, name, project, arguments, taskOptions string) string {
	t.Helper()
	templatePath := writeResultTemplate(t, "compose_result")
	remotePath := "/tmp/dibra-compose-v2-" + name + ".json"
	playbook := playbookHeader + `
  - name: Manage compose project
    community.docker.docker_compose_v2:
      project_src: ` + project + `
` + arguments + `
    register: compose_result
` + taskOptions + `

  - name: Persist compose result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	return runPlaybook(t, playbook)
}

func runComposeDefinition(t *testing.T, client *ssh.Client, name, projectName, command, extra string) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "compose_result")
	remotePath := "/tmp/dibra-compose-v2-" + name + ".json"
	playbook := playbookHeader + `
  - name: Manage inline compose
    community.docker.docker_compose_v2:
      project_name: ` + projectName + `
      definition:
        services:
          web:
            image: alpine:latest
            command: ["sh", "-c", "` + command + `"]
            stop_grace_period: 1s
` + extra + `
    register: compose_result

  - name: Persist compose result
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

func writeCompose(t *testing.T, client *ssh.Client, project, content string) {
	t.Helper()
	payload, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	mustRemote(t, client, fmt.Sprintf("python3 -c 'import json,pathlib; pathlib.Path(%q).write_text(json.loads(%q))'", project+"/docker-compose.yml", string(payload)))
}

func composeDown(t *testing.T, client *ssh.Client, project, name string) {
	t.Helper()
	cmd := "cd " + project + " && docker compose down -v --remove-orphans >/dev/null 2>&1 || true"
	if name != "" {
		cmd = "docker compose -p " + name + " down -v --remove-orphans >/dev/null 2>&1 || true; " + cmd
	}
	mustRemote(t, client, cmd)
}

func asMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	case []map[string]any:
		return typed
	default:
		return nil
	}
}

func actionHasStatus(actions []map[string]any, status string) bool {
	for _, action := range actions {
		if action["status"] == status {
			return true
		}
	}
	return false
}

func composeContainerByService(containers []map[string]any, service string) map[string]any {
	for _, container := range containers {
		if container["Service"] == service {
			return container
		}
	}
	return nil
}

func assertComposeServiceRunning(t *testing.T, result map[string]any, service string, running bool) {
	t.Helper()
	container := composeContainerByService(asMaps(result["containers"]), service)
	if container == nil {
		t.Fatalf("missing service %s in %#v", service, result["containers"])
	}
	state, _ := container["State"].(string)
	isRunning := strings.EqualFold(state, "running")
	if isRunning != running {
		t.Fatalf("service %s state=%q, want running=%v in %#v", service, state, running, container)
	}
}
