//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerImagePullParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine  = "alpine:latest"
		busybox = "busybox:latest"
		base    = "/tmp/dibra-image-pull"
	)
	mustRemote(t, client, "rm -rf "+base+" && mkdir -p "+base+" && rm -f /tmp/.dibra-agent")
	mustRemote(t, client, "docker image rm -f "+alpine+" "+busybox+" >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "rm -rf "+base)

	templatePath := writeResultTemplate(t, "image_pull_result")
	runPull := func(name, arguments, taskOptions string) map[string]any {
		t.Helper()
		remotePath := base + "/" + name + ".json"
		playbook := playbookHeader + `
  - name: Pull image
    community.docker.docker_image_pull:
` + arguments + `
    register: image_pull_result
` + taskOptions + `

  - name: Persist pull result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s pull failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	architecture := normalizeDockerArchitecture(strings.TrimSpace(mustRemote(t, client, "docker info --format '{{.Architecture}}'")))

	t.Run("missing image check mode predicts exact diff without pulling", func(t *testing.T) {
		mustRemote(t, client, "docker image rm -f "+alpine+" >/dev/null 2>&1 || true")
		result := runPull("missing-check", `      name: alpine
      platform: `+architecture+`
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
      timeout: 60
      debug: true
`, "    check_mode: true\n    diff: true\n")
		if result["changed"] != true || imageExists(t, client, alpine) {
			t.Fatalf("result = %#v; image exists = %v", result, imageExists(t, client, alpine))
		}
		assertActions(t, result, "Pulled image alpine:latest")
		before, after := resultDiff(t, result)
		if before["exists"] != false || after["id"] != "unknown" {
			t.Fatalf("diff = %#v", result["diff"])
		}
	})

	t.Run("pull with architecture shorthand returns raw inspection", func(t *testing.T) {
		result := runPull("first", `      name: alpine
      platform: `+architecture+`
`, "    diff: true\n")
		if result["changed"] != true {
			t.Fatalf("result = %#v", result)
		}
		assertActions(t, result, "Pulled image alpine:latest")
		before, after := resultDiff(t, result)
		if before["exists"] != false || after["id"] == nil {
			t.Fatalf("diff = %#v", result["diff"])
		}
		assertRawImageInspection(t, result["image"], dockerInspectImage(t, client, alpine))
	})

	t.Run("always predicts unknown but real repeated pull is idempotent", func(t *testing.T) {
		check := runPull("always-check", `      name: alpine:latest
      pull: always
      platform: linux/`+architecture+`
`, "    check_mode: true\n    diff: true\n")
		if check["changed"] != true {
			t.Fatalf("check result = %#v", check)
		}
		before, after := resultDiff(t, check)
		if before["id"] == nil || after["id"] != "unknown" {
			t.Fatalf("check diff = %#v", check["diff"])
		}

		actual := runPull("always-existing", `      name: alpine
      pull: always
      platform: linux/`+architecture+`
`, "    diff: true\n")
		if actual["changed"] != false {
			t.Fatalf("actual result = %#v", actual)
		}
		assertActions(t, actual, "Pulled image alpine:latest")
		actualBefore, actualAfter := resultDiff(t, actual)
		if actualBefore["id"] != actualAfter["id"] {
			t.Fatalf("actual diff = %#v", actual["diff"])
		}
	})

	t.Run("not_present skips matching platform with and without check mode", func(t *testing.T) {
		for _, test := range []struct {
			name        string
			taskOptions string
			platform    string
		}{
			{"platform-check", "    check_mode: true\n    diff: true\n", "      platform: " + architecture + "\n"},
			{"platform-real", "    diff: true\n", "      platform: linux/" + architecture + "\n"},
			{"without-platform", "    diff: true\n", ""},
		} {
			result := runPull("not-present-"+test.name, `      name: alpine
      pull: not_present
`+test.platform, test.taskOptions)
			if result["changed"] != false || len(resultStrings(t, result, "actions")) != 0 {
				t.Fatalf("%s result = %#v", test.name, result)
			}
			before, after := resultDiff(t, result)
			if before["id"] == nil || before["id"] != after["id"] {
				t.Fatalf("%s diff = %#v", test.name, result["diff"])
			}
		}
	})

	t.Run("embedded tag overrides tag option", func(t *testing.T) {
		mustRemote(t, client, "docker image rm -f "+busybox+" >/dev/null 2>&1 || true")
		result := runPull("tag-precedence", `      name: busybox:latest
      tag: ignored
`, "    diff: true\n")
		if result["changed"] != true {
			t.Fatalf("result = %#v", result)
		}
		assertActions(t, result, "Pulled image busybox:latest")
		assertRawImageInspection(t, result["image"], dockerInspectImage(t, client, busybox))
	})

	t.Run("digest references support always and not_present", func(t *testing.T) {
		digestReference := strings.TrimSpace(mustRemote(t, client,
			"docker image inspect --format '{{index .RepoDigests 0}}' "+busybox))
		if !strings.Contains(digestReference, "@sha256:") {
			t.Fatalf("unexpected RepoDigest %q", digestReference)
		}
		mustRemote(t, client, "docker image rm -f "+busybox+" >/dev/null 2>&1 || true")

		first := runPull("digest-first", "      name: "+digestReference+"\n", "    diff: true\n")
		if first["changed"] != true {
			t.Fatalf("first digest result = %#v", first)
		}
		repository, digest, _ := strings.Cut(digestReference, "@")
		assertActions(t, first, "Pulled image "+repository+":"+digest)
		assertRawImageInspection(t, first["image"], dockerInspectImage(t, client, digestReference))

		always := runPull("digest-always", "      name: "+digestReference+"\n      pull: always\n", "    diff: true\n")
		if always["changed"] != false {
			t.Fatalf("always digest result = %#v", always)
		}
		assertActions(t, always, "Pulled image "+repository+":"+digest)

		notPresent := runPull("digest-not-present", "      name: "+digestReference+"\n      pull: not_present\n", "    diff: true\n")
		if notPresent["changed"] != false || len(resultStrings(t, notPresent, "actions")) != 0 {
			t.Fatalf("not-present digest result = %#v", notPresent)
		}
	})

	t.Run("validation and platform API gate match upstream", func(t *testing.T) {
		imageID := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{.Id}}' "+alpine))
		cases := []struct {
			name      string
			arguments string
			message   string
		}{
			{"id", "      name: " + imageID + "\n", "Cannot pull an image by ID"},
			{"tag", "      name: alpine\n      tag: foo/bar\n", `"foo/bar" is not a valid docker tag!`},
			{"api", "      name: alpine\n      platform: " + architecture + "\n      api_version: '1.31'\n", "requires Docker API version 1.32"},
		}
		for _, test := range cases {
			playbook := playbookHeader + `
  - name: Invalid pull
    docker_image_pull:
` + test.arguments
			output := runPlaybook(t, playbook)
			if !strings.Contains(output, "FAILED") || !strings.Contains(output, test.message) {
				t.Fatalf("%s validation output = %s", test.name, output)
			}
		}
	})
}

func TestPlaybook_DockerImagePushParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		openRegistry = "127.0.0.1:5000"
		authRegistry = "127.0.0.1:5001"
		base         = "/tmp/dibra-image-push"
	)
	openImage := openRegistry + "/dibra/parity:latest"
	authImage := authRegistry + "/dibra/parity:latest"
	checkImage := openRegistry + "/dibra/check-mode:latest"
	authSource := "dibra-auth-source:latest"

	mustRemote(t, client, "docker rm -f dibra-registry-open dibra-registry-auth >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -rf "+base+" && mkdir -p "+base+"/auth && rm -f /tmp/.dibra-agent")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null && docker pull busybox:latest >/dev/null")
	mustRemote(t, client, "docker pull registry:2 >/dev/null && docker run -d --name dibra-registry-open -p 5000:5000 registry:2 >/dev/null")
	mustRemote(t, client, "for i in $(seq 1 30); do curl -fsS http://127.0.0.1:5000/v2/ >/dev/null && exit 0; sleep 1; done; exit 1")
	defer func() {
		mustRemote(t, client, "docker logout "+authRegistry+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker rm -f dibra-registry-open dibra-registry-auth >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker image rm -f "+openImage+" "+authImage+" "+checkImage+" "+authSource+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "rm -rf "+base)
	}()

	templatePath := writeResultTemplate(t, "image_push_result")
	runPush := func(name, arguments string, commandArgs ...string) map[string]any {
		t.Helper()
		remotePath := base + "/" + name + ".json"
		playbook := playbookHeader + `
  - name: Push image
    community.docker.docker_image_push:
` + arguments + `
    register: image_push_result

  - name: Persist push result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybookWithArgs(t, playbook, commandArgs...)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s push failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("check mode is unsupported and never pushes", func(t *testing.T) {
		mustRemote(t, client, "docker tag alpine:latest "+checkImage)
		playbook := playbookHeader + `
  - name: Push is skipped in check mode
    docker_image_push:
      name: ` + checkImage + `
`
		output := runPlaybookWithArgs(t, playbook, "--check")
		if strings.Contains(output, "FAILED") || !strings.Contains(output, "SKIPPED") {
			t.Fatalf("check-mode output = %s", output)
		}
		status := strings.TrimSpace(mustRemote(t, client,
			"curl -s -o /dev/null -w '%{http_code}' http://"+openRegistry+"/v2/dibra/check-mode/manifests/latest"))
		if status != "404" {
			t.Fatalf("check mode pushed manifest; registry status = %s", status)
		}
	})

	t.Run("first push changes, returns raw image, and emits no diff", func(t *testing.T) {
		mustRemote(t, client, "docker tag alpine:latest "+openImage)
		result := runPush("first", `      name: `+openRegistry+`/dibra/parity
      tag: latest
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
      timeout: 60
      debug: true
`, "--diff")
		if result["changed"] != true {
			t.Fatalf("result = %#v", result)
		}
		assertActions(t, result, "Pushed image "+openImage)
		if _, hasDiff := result["diff"]; hasDiff {
			t.Fatalf("unsupported diff returned: %#v", result["diff"])
		}
		assertRawImageInspection(t, result["image"], dockerInspectImage(t, client, openImage))
	})

	t.Run("repeat push is idempotent", func(t *testing.T) {
		result := runPush("repeat", "      name: "+openImage+"\n")
		if result["changed"] != false {
			t.Fatalf("result = %#v", result)
		}
		assertActions(t, result, "Pushed image "+openImage)
	})

	t.Run("same repository with different local image changes", func(t *testing.T) {
		mustRemote(t, client, "docker tag busybox:latest "+openImage)
		result := runPush("replacement", "      name: "+openImage+"\n      tag: ignored\n")
		if result["changed"] != true {
			t.Fatalf("result = %#v", result)
		}
		assertActions(t, result, "Pushed image "+openImage)
		assertRawImageInspection(t, result["image"], dockerInspectImage(t, client, openImage))
	})

	t.Run("upstream validation failures are stable", func(t *testing.T) {
		imageID := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{.Id}}' alpine:latest"))
		digest := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{index .RepoDigests 0}}' alpine:latest"))
		cases := []struct {
			name      string
			arguments string
			message   string
		}{
			{"missing", "      name: " + openRegistry + "/dibra/missing\n", "Cannot find image " + openRegistry + "/dibra/missing:latest"},
			{"id", "      name: " + imageID + "\n", "Cannot push an image by ID"},
			{"digest", "      name: " + digest + "\n", "Cannot push an image by digest"},
			{"tag", "      name: alpine\n      tag: foo/bar\n", `"foo/bar" is not a valid docker tag!`},
			{"embedded-tag", "      name: 'alpine:foo bar'\n", `"foo bar" is not a valid docker tag!`},
		}
		for _, test := range cases {
			playbook := playbookHeader + `
  - name: Invalid push
    docker_image_push:
` + test.arguments
			output := runPlaybook(t, playbook)
			if !strings.Contains(output, "FAILED") || !strings.Contains(output, test.message) {
				t.Fatalf("%s validation output = %s", test.name, output)
			}
		}
	})

	t.Run("authenticated registry requires config credentials", func(t *testing.T) {
		mustRemote(t, client, "docker pull httpd:2-alpine >/dev/null")
		mustRemote(t, client, "docker run --rm --entrypoint htpasswd httpd:2-alpine -Bbn testuser hunter2 > "+base+"/auth/htpasswd")
		mustRemote(t, client, "docker run -d --name dibra-registry-auth -p 5001:5000 -v "+base+"/auth:/auth -e REGISTRY_AUTH=htpasswd -e REGISTRY_AUTH_HTPASSWD_REALM=Registry -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd registry:2 >/dev/null")
		mustRemote(t, client, "for i in $(seq 1 30); do code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:5001/v2/); test \"$code\" = 401 && exit 0; sleep 1; done; exit 1")
		mustRemote(t, client, "docker logout "+authRegistry+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "mkdir -p "+base+"/rootfs && printf auth-parity > "+base+"/rootfs/content && tar -C "+base+"/rootfs -cf "+base+"/rootfs.tar .")
		mustRemote(t, client, "docker import "+base+"/rootfs.tar "+authSource+" >/dev/null && docker tag "+authSource+" "+authImage)

		unauthenticated := playbookHeader + `
  - name: Push without registry credentials
    docker_image_push:
      name: ` + authImage + `
`
		output := runPlaybook(t, unauthenticated)
		if !strings.Contains(output, "FAILED") ||
			!(strings.Contains(strings.ToLower(output), "unauthorized") || strings.Contains(strings.ToLower(output), "no basic auth credentials")) {
			t.Fatalf("unauthenticated push output = %s", output)
		}

		mustRemote(t, client, "printf hunter2 | docker login "+authRegistry+" -u testuser --password-stdin >/dev/null")
		first := runPush("auth-first", "      name: "+authImage+"\n")
		if first["changed"] != true {
			t.Fatalf("authenticated first push = %#v", first)
		}
		second := runPush("auth-repeat", "      name: "+authImage+"\n")
		if second["changed"] != false {
			t.Fatalf("authenticated repeat push = %#v", second)
		}
	})

	t.Run("authenticated registry pull uses config and preserves idempotency", func(t *testing.T) {
		mustRemote(t, client, "docker image rm -f "+authImage+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker logout "+authRegistry+" >/dev/null 2>&1 || true")
		unauthenticated := playbookHeader + `
  - name: Pull without registry credentials
    docker_image_pull:
      name: ` + authImage + `
`
		output := runPlaybook(t, unauthenticated)
		if !strings.Contains(output, "FAILED") ||
			!(strings.Contains(strings.ToLower(output), "unauthorized") || strings.Contains(strings.ToLower(output), "no basic auth credentials")) {
			t.Fatalf("unauthenticated pull output = %s", output)
		}

		mustRemote(t, client, "printf hunter2 | docker login "+authRegistry+" -u testuser --password-stdin >/dev/null")
		pullTemplate := writeResultTemplate(t, "authenticated_pull_result")
		pullPath := base + "/authenticated-pull.json"
		playbook := playbookHeader + `
  - name: Pull with registry config credentials
    community.docker.docker_image_pull:
      name: ` + authImage + `
      pull: not_present
    register: authenticated_pull_result
    diff: true

  - name: Persist authenticated pull
    template:
      src: ` + pullTemplate + `
      dest: ` + pullPath + `
`
		output = runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("authenticated pull failed: %s", output)
		}
		first := readRemoteJSONMap(t, client, pullPath)
		if first["changed"] != true {
			t.Fatalf("first pull = %#v", first)
		}
		assertRawImageInspection(t, first["image"], dockerInspectImage(t, client, authImage))

		output = runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("repeated authenticated pull failed: %s", output)
		}
		second := readRemoteJSONMap(t, client, pullPath)
		if second["changed"] != false || len(resultStrings(t, second, "actions")) != 0 {
			t.Fatalf("repeated pull = %#v", second)
		}
	})
}

func mustRemote(t *testing.T, client *ssh.Client, command string) string {
	t.Helper()
	stdout, stderr, err := client.Run(command)
	if err != nil {
		t.Fatalf("remote command failed: %s\n%v\nstdout: %s\nstderr: %s", command, err, stdout, stderr)
	}
	return strings.TrimSpace(stdout)
}

func normalizeDockerArchitecture(value string) string {
	switch value {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return value
	}
}

func assertActions(t *testing.T, result map[string]any, want ...string) {
	t.Helper()
	got := resultStrings(t, result, "actions")
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
}

func resultDiff(t *testing.T, result map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	diff, ok := result["diff"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %T in %#v", result["diff"], result)
	}
	before, beforeOK := diff["before"].(map[string]any)
	after, afterOK := diff["after"].(map[string]any)
	if !beforeOK || !afterOK {
		t.Fatalf("diff before/after = %T/%T in %#v", diff["before"], diff["after"], diff)
	}
	return before, after
}
