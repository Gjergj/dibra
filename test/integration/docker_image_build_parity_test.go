//go:build integration

package integration

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerImageBuildParity independently ports the pinned
// community.docker docker_image_build integration target
// (tasks/tests/options.yml plus its Dockerfile templates), the two
// Examples on the 5.2.2 module documentation, and documented output,
// secret, and connection options that the upstream target does not run.
func TestPlaybook_DockerImageBuildParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		baseImage = "alpine:latest"
		context   = "/tmp/dibra-image-build-parity"
	)
	remoteExec(t, client, "docker pull "+baseImage)
	remoteExec(t, client, "rm -rf "+context+" && mkdir -p "+context+"/files")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent /tmp/dibra-image-build-*.json")
	defer remoteExec(t, client, "rm -rf "+context)

	writeRemoteFile(t, client, context+"/files/Dockerfile", ""+
		"FROM "+baseImage+"\n"+
		"ENV install=/workdir\n"+
		"WORKDIR ${install}\n")
	writeRemoteFile(t, client, context+"/files/ArgsDockerfile", ""+
		"ARG BASE\n"+
		"ARG TEST1\n"+
		"ARG TEST2\n"+
		"ARG TEST3\n"+
		"FROM ${BASE}\n"+
		"ENV install=/opt/app\n"+
		"WORKDIR ${install}\n"+
		"RUN printf '%s - %s - %s\\n' \"${TEST1}\" \"${TEST2}\" \"${TEST3}\"\n")
	writeRemoteFile(t, client, context+"/files/CustomDockerfile", ""+
		"FROM "+baseImage+"\n"+
		"ENV INSTALL_PATH=/newdata\n"+
		"RUN mkdir -p $INSTALL_PATH\n"+
		"WORKDIR $INSTALL_PATH\n")
	writeRemoteFile(t, client, context+"/files/StagedDockerfile", ""+
		"FROM "+baseImage+" AS first\n"+
		"WORKDIR /first\n"+
		"\n"+
		"FROM "+baseImage+" AS second\n"+
		"WORKDIR /second\n")
	writeRemoteFile(t, client, context+"/files/HostsDockerfile", ""+
		"FROM "+baseImage+"\n"+
		"RUN ping -c1 some-custom-host\n")
	writeRemoteFile(t, client, context+"/files/SecretsDockerfile", ""+
		"# syntax=docker/dockerfile:1\n"+
		"FROM "+baseImage+"\n"+
		"RUN --mount=type=secret,id=my-awesome-secret base64 /run/secrets/my-awesome-secret\n")
	writeRemoteFile(t, client, context+"/files/FileSecretDockerfile", ""+
		"# syntax=docker/dockerfile:1\n"+
		"FROM "+baseImage+"\n"+
		"RUN --mount=type=secret,id=file-token cat /run/secrets/file-token\n")
	writeRemoteFile(t, client, context+"/files/EnvSecretDockerfile", ""+
		"# syntax=docker/dockerfile:1\n"+
		"FROM "+baseImage+"\n"+
		"RUN --mount=type=secret,id=env-token cat /run/secrets/env-token\n")
	writeRemoteFile(t, client, context+"/files/nested/Dockerfile", ""+
		"FROM "+baseImage+"\n"+
		"WORKDIR /nested\n")
	writeRemoteFile(t, client, context+"/files/FailDockerfile", ""+
		"FROM "+baseImage+"\n"+
		"RUN false\n")
	dockerPath := strings.TrimSpace(remoteExec(t, client, "command -v docker"))
	wrapper := context + "/docker-with-env"
	writeRemoteFile(t, client, wrapper, "#!/bin/sh\nexport BUILD_ENV_SECRET=env-secret-token\nexec "+dockerPath+" \"$@\"\n")
	remoteExec(t, client, "chmod +x "+wrapper)

	removeImage := func(name string) {
		t.Helper()
		remoteExec(t, client, "docker image rm -f "+name+" || true")
	}

	t.Run("build args and two-run idempotency", func(t *testing.T) {
		name := "dibra-image-build-args:latest"
		removeImage(name)
		defer removeImage(name)
		first := runImageBuild(t, client, "args-1", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: ArgsDockerfile
      args:
        BASE: `+baseImage+`
        TEST1: val1
        TEST2: val2
        TEST3: "True"
      pull: false
`)
		assertBuildChanged(t, first, true)
		if got := imageWorkingDir(t, first.result); got != "/opt/app" {
			t.Fatalf("WORKDIR = %q", got)
		}
		assertRawImageInspection(t, first.result["image"], dockerInspectImage(t, client, name))
		assertBuildCommand(t, first.result, "--build-arg", "--file")
		second := runImageBuild(t, client, "args-2", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: ArgsDockerfile
      args:
        BASE: `+baseImage+`
        TEST1: val1
        TEST2: val2
        TEST3: "True"
      pull: false
`)
		assertBuildChanged(t, second, false)
	})

	t.Run("custom dockerfile inspect and stderr", func(t *testing.T) {
		name := "dibra-image-build-dockerfile:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "dockerfile", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: CustomDockerfile
      pull: false
`)
		assertBuildChanged(t, result, true)
		if got := imageWorkingDir(t, result.result); got != "/newdata" {
			t.Fatalf("WORKDIR = %q", got)
		}
		stderr, _ := result.result["stderr"].(string)
		if !strings.Contains(stderr, "FROM") || !strings.Contains(stderr, "alpine:latest") {
			t.Fatalf("stderr missing FROM alpine line: %q", stderr)
		}
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
	})

	t.Run("docs example namespaced name and dockerfile", func(t *testing.T) {
		name := "localhost/dibra/python:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "docs-dockerfile", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: CustomDockerfile
`)
		assertBuildChanged(t, result, true)
		if got := imageWorkingDir(t, result.result); got != "/newdata" {
			t.Fatalf("WORKDIR = %q", got)
		}
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		assertBuildCommand(t, result.result, "--file", "--tag")
	})

	t.Run("platform linux two-run idempotency", func(t *testing.T) {
		name := "dibra-image-build-platform:latest"
		removeImage(name)
		defer removeImage(name)
		args := `
      name: ` + name + `
      path: ` + context + `/files
      platform: linux
      pull: false
`
		assertBuildChanged(t, runImageBuild(t, client, "platform-1", args), true)
		assertBuildChanged(t, runImageBuild(t, client, "platform-2", args), false)

		listName := "dibra-image-build-platform-list:latest"
		removeImage(listName)
		defer removeImage(listName)
		listResult := runImageBuild(t, client, "platform-list", `
      name: `+listName+`
      path: `+context+`/files
      platform:
        - linux
      pull: false
`)
		assertBuildChanged(t, listResult, true)
		assertBuildCommand(t, listResult.result, "--platform")
	})

	t.Run("docs example multi-platform list and tag", func(t *testing.T) {
		name := "dibra-image-build-multi:1.5.2"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "docs-multi-platform", `
      name: dibra-image-build-multi
      tag: "1.5.2"
      path: `+context+`/files
      platform:
        - linux/amd64
        - linux/arm64/v8
      pull: false
`)
		skipUnsupportedPlatforms(t, result)
		assertBuildChanged(t, result, true)
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		assertBuildCommand(t, result.result, "--platform", "linux/amd64", "linux/arm64/v8", "--tag")
		if inspect := remoteExec(t, client, "docker image inspect dibra-image-build-multi:latest 2>&1 || true"); !strings.Contains(strings.ToLower(inspect), "no such") {
			t.Fatalf("separate tag should not also create :latest: %s", inspect)
		}
	})

	t.Run("multi-stage target", func(t *testing.T) {
		name := "dibra-image-build-target:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "target", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: StagedDockerfile
      target: first
      pull: false
`)
		assertBuildChanged(t, result, true)
		if got := imageWorkingDir(t, result.result); got != "/first" {
			t.Fatalf("WORKDIR = %q, want /first", got)
		}
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
	})

	t.Run("relative dockerfile path", func(t *testing.T) {
		name := "dibra-image-build-nested:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "nested-dockerfile", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: nested/Dockerfile
      pull: false
`)
		assertBuildChanged(t, result, true)
		if got := imageWorkingDir(t, result.result); got != "/nested" {
			t.Fatalf("WORKDIR = %q", got)
		}
		assertBuildCommand(t, result.result, "--file")
	})

	t.Run("separate tag and embedded name precedence", func(t *testing.T) {
		tagged := "dibra-image-build-tag:v1"
		removeImage(tagged)
		defer removeImage(tagged)
		taggedResult := runImageBuild(t, client, "separate-tag", `
      name: dibra-image-build-tag
      tag: v1
      path: `+context+`/files
      pull: false
`)
		assertBuildChanged(t, taggedResult, true)
		assertRawImageInspection(t, taggedResult.result["image"], dockerInspectImage(t, client, tagged))
		if inspect := remoteExec(t, client, "docker image inspect dibra-image-build-tag:latest 2>&1 || true"); !strings.Contains(strings.ToLower(inspect), "no such") {
			t.Fatalf("tag option should not also create :latest: %s", inspect)
		}

		embedded := "dibra-image-build-embed:v2"
		removeImage(embedded)
		defer removeImage(embedded)
		embeddedResult := runImageBuild(t, client, "embedded-tag", `
      name: `+embedded+`
      tag: ignored
      path: `+context+`/files
      pull: false
`)
		assertBuildChanged(t, embeddedResult, true)
		assertRawImageInspection(t, embeddedResult.result["image"], dockerInspectImage(t, client, embedded))
		command := fmt.Sprint(embeddedResult.result["command"])
		if !strings.Contains(command, embedded) || strings.Contains(command, "ignored") {
			t.Fatalf("embedded tag should win: %s", command)
		}
	})

	t.Run("etc_hosts extra host", func(t *testing.T) {
		name := "dibra-image-build-hosts:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "hosts", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: HostsDockerfile
      pull: false
      etc_hosts:
        some-custom-host: "127.0.0.1"
`)
		assertBuildChanged(t, result, true)
	})

	t.Run("etc_hosts host-gateway", func(t *testing.T) {
		name := "dibra-image-build-host-gateway:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "hosts-gateway", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: HostsDockerfile
      pull: false
      etc_hosts:
        some-custom-host: host-gateway
`)
		assertBuildChanged(t, result, true)
		assertBuildCommand(t, result.result, "--add-host")
	})

	t.Run("shm_size", func(t *testing.T) {
		name := "dibra-image-build-shm:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "shm", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: CustomDockerfile
      pull: false
      shm_size: 128MB
`)
		assertBuildChanged(t, result, true)
	})

	t.Run("labels including spaced key", func(t *testing.T) {
		name := "dibra-image-build-labels:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "labels", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: CustomDockerfile
      pull: false
      labels:
        FOO: BAR
        "this is a label": "this is the label's value"
`)
		assertBuildChanged(t, result, true)
		labels := imageLabels(t, result.result)
		if labels["FOO"] != "BAR" || labels["this is a label"] != "this is the label's value" {
			t.Fatalf("labels = %#v", labels)
		}
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
	})

	t.Run("value secret appears base64-encoded in stderr", func(t *testing.T) {
		name := "dibra-image-build-secret:latest"
		removeImage(name)
		defer removeImage(name)
		secret := fmt.Sprintf("this is my secret %x", os.Getpid())
		result := runImageBuild(t, client, "secret-value", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: SecretsDockerfile
      pull: false
      nocache: true
      secrets:
        - id: my-awesome-secret
          type: value
          value: `+fmt.Sprintf("%q", secret)+`
`)
		assertBuildChanged(t, result, true)
		encoded := base64.StdEncoding.EncodeToString([]byte(secret))
		stderr, _ := result.result["stderr"].(string)
		if !strings.Contains(stderr, encoded) {
			t.Fatalf("stderr missing secret encoding %q: %q", encoded, stderr)
		}
		command, _ := result.result["command"].([]any)
		joined := fmt.Sprint(command)
		if strings.Contains(joined, secret) {
			t.Fatalf("secret leaked in command: %s", joined)
		}
	})

	t.Run("env secret via docker_cli wrapper", func(t *testing.T) {
		name := "dibra-image-build-env-secret:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "secret-env", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: EnvSecretDockerfile
      docker_cli: `+wrapper+`
      pull: false
      nocache: true
      secrets:
        - id: env-token
          type: env
          env: BUILD_ENV_SECRET
`)
		assertBuildChanged(t, result, true)
		stderr, _ := result.result["stderr"].(string)
		if !strings.Contains(stderr, "env-secret-token") {
			t.Fatalf("stderr missing env secret: %q", stderr)
		}
		assertBuildCommand(t, result.result, "--secret")
	})

	t.Run("file secret is consumed during build", func(t *testing.T) {
		name := "dibra-image-build-file-secret:latest"
		removeImage(name)
		defer removeImage(name)
		secretPath := context + "/files/token.txt"
		writeRemoteFile(t, client, secretPath, "file-secret-token")
		result := runImageBuild(t, client, "secret-file", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: FileSecretDockerfile
      pull: false
      nocache: true
      secrets:
        - id: file-token
          type: file
          src: `+secretPath+`
`)
		assertBuildChanged(t, result, true)
		stderr, _ := result.result["stderr"].(string)
		if !strings.Contains(stderr, "file-secret-token") {
			t.Fatalf("stderr missing file secret: %q", stderr)
		}
	})

	t.Run("tar output keeps named image and writes archive", func(t *testing.T) {
		name := "dibra-image-build-tar:latest"
		archive := context + "/container.tar"
		removeImage(name)
		remoteExec(t, client, "rm -f "+archive)
		defer removeImage(name)
		defer remoteExec(t, client, "rm -f "+archive)
		result := runImageBuild(t, client, "outputs-tar", `
      name: `+name+`
      path: `+context+`/files
      dockerfile: Dockerfile
      pull: false
      outputs:
        - type: tar
          dest: `+archive+`
`)
		if result.failed {
			if stderr, _ := result.result["stderr"].(string); strings.Contains(result.output, "multiple outputs currently unsupported") ||
				strings.Contains(stderr, "multiple outputs currently unsupported") {
				t.Skip("BuildKit daemon rejected multiple outputs")
			}
			t.Fatalf("tar output build failed: %s", result.output)
		}
		assertBuildChanged(t, result, true)
		if image, _ := result.result["image"].(map[string]any); len(image) == 0 {
			t.Fatalf("expected named image inspection, got %#v", result.result["image"])
		}
		if !remoteFileExists(t, client, archive) {
			t.Fatal("tar archive was not written")
		}
		assertBuildCommand(t, result.result, "--output")
		command := fmt.Sprint(result.result["command"])
		if strings.Contains(command, "--tag") {
			t.Fatalf("tar outputs must not pass --tag: %s", command)
		}
		remove := playbookHeader + `
  - name: Remove the named image after tar export
    docker_image_remove:
      name: ` + name + `
`
		if output := runPlaybook(t, remove); strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected named image to exist after tar output: %s", output)
		}
	})

	t.Run("docker output without dest", func(t *testing.T) {
		name := "dibra-image-build-docker-out:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "outputs-docker", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: docker
`)
		skipUnsupportedMultipleOutputs(t, result)
		assertBuildChanged(t, result, true)
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		command := fmt.Sprint(result.result["command"])
		if !strings.Contains(command, "type=docker") || strings.Contains(command, "type=docker,") {
			t.Fatalf("docker output must be type=docker without a trailing comma: %s", command)
		}
		if strings.Contains(command, "--tag") {
			t.Fatalf("outputs must not pass --tag: %s", command)
		}
	})

	t.Run("docker output dest writes archive", func(t *testing.T) {
		name := "dibra-image-build-docker-dest:latest"
		archive := context + "/image.docker.tar"
		removeImage(name)
		remoteExec(t, client, "rm -f "+archive)
		defer removeImage(name)
		defer remoteExec(t, client, "rm -f "+archive)
		result := runImageBuild(t, client, "outputs-docker-dest", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: docker
          dest: `+archive+`
`)
		skipUnsupportedMultipleOutputs(t, result)
		assertBuildChanged(t, result, true)
		if !remoteFileExists(t, client, archive) {
			t.Fatal("docker dest archive was not written")
		}
		command := fmt.Sprint(result.result["command"])
		if !strings.Contains(command, "type=docker,dest=") || strings.Contains(command, "--tag") {
			t.Fatalf("docker dest output missing or used --tag: %s", command)
		}
	})

	t.Run("image output without name uses name and tag", func(t *testing.T) {
		name := "dibra-image-build-image-out:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "outputs-image-default", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: image
`)
		assertBuildChanged(t, result, true)
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		command := fmt.Sprint(result.result["command"])
		if !strings.Contains(command, "type=image,name="+name) || strings.Contains(command, "--tag") {
			t.Fatalf("default image output should name the image and omit --tag: %s", command)
		}
	})

	t.Run("empty outputs list uses tag", func(t *testing.T) {
		name := "dibra-image-build-empty-outputs:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "outputs-empty", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      outputs: []
`)
		assertBuildChanged(t, result, true)
		assertBuildCommand(t, result.result, "--tag")
		if strings.Contains(fmt.Sprint(result.result["command"]), "--output") {
			t.Fatalf("empty outputs should not pass --output: %s", result.result["command"])
		}
	})

	t.Run("image output name list extra tag", func(t *testing.T) {
		name := "dibra-image-build-names:latest"
		extra := "dibra-image-build-names:extra"
		removeImage(name)
		removeImage(extra)
		defer removeImage(name)
		defer removeImage(extra)
		result := runImageBuild(t, client, "outputs-names", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: image
          name:
            - `+name+`
            - `+extra+`
`)
		assertBuildChanged(t, result, true)
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		if dockerInspectImage(t, client, extra)["Id"] != dockerInspectImage(t, client, name)["Id"] {
			t.Fatalf("extra output name did not point at the built image")
		}
		command := fmt.Sprint(result.result["command"])
		if !strings.Contains(command, extra) || strings.Contains(command, "--tag") {
			t.Fatalf("name list should keep extra tag and omit --tag: %s", command)
		}
	})

	t.Run("local and oci outputs keep named image", func(t *testing.T) {
		localName := "dibra-image-build-local:latest"
		localDir := context + "/local-out"
		removeImage(localName)
		remoteExec(t, client, "rm -rf "+localDir)
		defer removeImage(localName)
		defer remoteExec(t, client, "rm -rf "+localDir)
		localResult := runImageBuild(t, client, "outputs-local", `
      name: `+localName+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: local
          dest: `+localDir+`
`)
		skipUnsupportedMultipleOutputs(t, localResult)
		assertBuildChanged(t, localResult, true)
		assertRawImageInspection(t, localResult.result["image"], dockerInspectImage(t, client, localName))
		if !remoteFileExists(t, client, localDir) {
			t.Fatal("local output directory was not written")
		}
		if strings.TrimSpace(remoteExec(t, client, "ls -A "+localDir+" | wc -l")) == "0" {
			t.Fatal("local output directory is empty")
		}

		ociName := "dibra-image-build-oci:latest"
		ociPath := context + "/image.oci"
		removeImage(ociName)
		remoteExec(t, client, "rm -f "+ociPath)
		defer removeImage(ociName)
		defer remoteExec(t, client, "rm -f "+ociPath)
		ociResult := runImageBuild(t, client, "outputs-oci", `
      name: `+ociName+`
      path: `+context+`/files
      pull: false
      outputs:
        - type: oci
          dest: `+ociPath+`
`)
		skipUnsupportedMultipleOutputs(t, ociResult)
		assertBuildChanged(t, ociResult, true)
		if !remoteFileExists(t, client, ociPath) {
			t.Fatal("oci archive was not written")
		}
	})

	t.Run("cache_from and host network", func(t *testing.T) {
		name := "dibra-image-build-cache:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "cache-network", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      cache_from:
        - `+baseImage+`
      network: host
`)
		assertBuildChanged(t, result, true)
		command := fmt.Sprint(result.result["command"])
		if !strings.Contains(command, "--cache-from") || !strings.Contains(command, "--network") {
			t.Fatalf("command missing cache/network flags: %s", command)
		}
	})

	t.Run("docs connection options and pull", func(t *testing.T) {
		name := "dibra-image-build-connection:latest"
		removeImage(name)
		defer removeImage(name)
		result := runImageBuild(t, client, "docs-connection", `
      name: `+name+`
      path: `+context+`/files
      docker_host: unix:///var/run/docker.sock
      api_version: auto
      timeout: 120
      pull: true
`)
		assertBuildChanged(t, result, true)
		assertRawImageInspection(t, result.result["image"], dockerInspectImage(t, client, name))
		assertBuildCommand(t, result.result, "--pull")

		conflict := playbookHeader + `
  - name: docker_host and cli_context conflict
    docker_image_build:
      name: dibra-image-build-conflict
      path: ` + context + `/files
      docker_host: unix:///var/run/docker.sock
      cli_context: default
`
		if output := runPlaybook(t, conflict); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "mutually exclusive") {
			t.Fatalf("docker_host/cli_context: %s", output)
		}
	})

	t.Run("check mode predicts rebuild without mutating", func(t *testing.T) {
		name := "dibra-image-build-check:latest"
		removeImage(name)
		defer removeImage(name)
		create := runImageBuild(t, client, "check-create", `
      name: `+name+`
      path: `+context+`/files
      pull: false
`)
		assertBuildChanged(t, create, true)
		before := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+name))
		check := runImageBuildWithArgs(t, client, "check-rebuild", `
      name: `+name+`
      path: `+context+`/files
      pull: false
      rebuild: always
      nocache: true
`, "--check")
		assertBuildChanged(t, check, true)
		after := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+name))
		if before != after {
			t.Fatalf("check mode rebuilt image: before=%s after=%s", before, after)
		}
		if command, _ := check.result["command"].([]any); len(command) != 0 {
			t.Fatalf("check mode should not return a build command: %#v", command)
		}

		missing := "dibra-image-build-check-missing:latest"
		removeImage(missing)
		defer removeImage(missing)
		predicted := runImageBuildWithArgs(t, client, "check-missing", `
      name: `+missing+`
      path: `+context+`/files
      pull: false
`, "--check")
		assertBuildChanged(t, predicted, true)
		if inspect := remoteExec(t, client, "docker image inspect "+missing+" 2>&1 || true"); !strings.Contains(strings.ToLower(inspect), "no such") {
			t.Fatalf("check mode created a missing image: %s", inspect)
		}

		unchanged := runImageBuildWithArgs(t, client, "check-existing", `
      name: `+name+`
      path: `+context+`/files
      pull: false
`, "--check")
		assertBuildChanged(t, unchanged, false)
	})

	t.Run("rebuild always changes on a second real run", func(t *testing.T) {
		name := "dibra-image-build-rebuild:latest"
		removeImage(name)
		defer removeImage(name)
		args := `
      name: ` + name + `
      path: ` + context + `/files
      pull: false
      rebuild: always
      nocache: true
`
		assertBuildChanged(t, runImageBuild(t, client, "rebuild-1", args), true)
		assertBuildChanged(t, runImageBuild(t, client, "rebuild-2", args), true)
	})

	t.Run("validation failures", func(t *testing.T) {
		missingDir := playbookHeader + `
  - name: Missing context
    docker_image_build:
      name: dibra-image-build-missing
      path: /tmp/dibra-image-build-does-not-exist
`
		if output := runPlaybook(t, missingDir); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "is not an existing directory") {
			t.Fatalf("missing directory: %s", output)
		}

		digest := playbookHeader + `
  - name: Digest name
    docker_image_build:
      name: alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      path: ` + context + `/files
`
		if output := runPlaybook(t, digest); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "Image name must not be a digest") {
			t.Fatalf("digest name: %s", output)
		}

		missingFile := playbookHeader + `
  - name: Missing dockerfile
    docker_image_build:
      name: dibra-image-build-missing-file
      path: ` + context + `/files
      dockerfile: DoesNotExist
`
		if output := runPlaybook(t, missingFile); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "is not an existing file") {
			t.Fatalf("missing dockerfile: %s", output)
		}

		imageID := playbookHeader + `
  - name: Image ID
    docker_image_build:
      name: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      path: ` + context + `/files
`
		if output := runPlaybook(t, imageID); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "Image name must not be a digest") {
			t.Fatalf("image id: %s", output)
		}

		rebuild := playbookHeader + `
  - name: Invalid rebuild
    docker_image_build:
      name: dibra-image-build-rebuild-invalid
      path: ` + context + `/files
      rebuild: sometimes
`
		if output := runPlaybook(t, rebuild); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "rebuild must be one of never or always") {
			t.Fatalf("invalid rebuild: %s", output)
		}

		secret := playbookHeader + `
  - name: File secret without src
    docker_image_build:
      name: dibra-image-build-secret-invalid
      path: ` + context + `/files
      secrets:
        - id: token
          type: file
`
		if output := runPlaybook(t, secret); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "src is required") {
			t.Fatalf("secret src: %s", output)
		}

		outputs := playbookHeader + `
  - name: Tar output without dest
    docker_image_build:
      name: dibra-image-build-output-invalid
      path: ` + context + `/files
      outputs:
        - type: tar
`
		if output := runPlaybook(t, outputs); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "dest is required") {
			t.Fatalf("output dest: %s", output)
		}

		failedBuild := playbookHeader + `
  - name: Dockerfile RUN failure
    docker_image_build:
      name: dibra-image-build-fail
      path: ` + context + `/files
      dockerfile: FailDockerfile
      pull: false
      nocache: true
`
		if output := runPlaybook(t, failedBuild); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "Building dibra-image-build-fail:latest failed") {
			t.Fatalf("failed build: %s", output)
		}
	})
}

type imageBuildRun struct {
	output string
	result map[string]any
	failed bool
}

func runImageBuild(t *testing.T, client *ssh.Client, suffix, arguments string) imageBuildRun {
	t.Helper()
	return runImageBuildWithArgs(t, client, suffix, arguments)
}

func runImageBuildWithArgs(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) imageBuildRun {
	t.Helper()
	remotePath := "/tmp/dibra-image-build-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "build_result")
	playbook := playbookHeader + `
  - name: Build image
    community.docker.docker_image_build:
` + arguments + `
    register: build_result

  - name: Persist build result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	failed := strings.Contains(output, "FAILED")
	if failed {
		return imageBuildRun{output: output, result: map[string]any{}, failed: true}
	}
	return imageBuildRun{output: output, result: readRemoteJSONMap(t, client, remotePath)}
}

func assertBuildChanged(t *testing.T, run imageBuildRun, want bool) {
	t.Helper()
	if run.failed {
		t.Fatalf("build failed: %s", run.output)
	}
	if run.result["changed"] != want {
		t.Fatalf("changed = %#v, want %v\noutput: %s\nresult: %#v", run.result["changed"], want, run.output, run.result)
	}
}

func assertBuildCommand(t *testing.T, result map[string]any, flags ...string) {
	t.Helper()
	command := fmt.Sprint(result["command"])
	if !strings.Contains(command, "buildx") {
		t.Fatalf("command missing buildx: %s", command)
	}
	for _, flag := range flags {
		if !strings.Contains(command, flag) {
			t.Fatalf("command missing %s: %s", flag, command)
		}
	}
}

func skipUnsupportedMultipleOutputs(t *testing.T, run imageBuildRun) {
	t.Helper()
	if !run.failed {
		return
	}
	if strings.Contains(run.output, "multiple outputs currently unsupported") {
		t.Skip("BuildKit daemon rejected multiple outputs")
	}
}

func skipUnsupportedPlatforms(t *testing.T, run imageBuildRun) {
	t.Helper()
	if !run.failed {
		return
	}
	blob := strings.ToLower(run.output)
	for _, marker := range []string{
		"multiple platforms",
		"multi-platform",
		"no match for platform",
		"unknown architecture",
		"exec format error",
		"binfmt",
		"does not support",
	} {
		if strings.Contains(blob, marker) {
			t.Skip("build driver rejected the documented multi-platform example")
		}
	}
}

func imageWorkingDir(t *testing.T, result map[string]any) string {
	t.Helper()
	image, _ := result["image"].(map[string]any)
	config, _ := image["Config"].(map[string]any)
	dir, _ := config["WorkingDir"].(string)
	return dir
}

func imageLabels(t *testing.T, result map[string]any) map[string]string {
	t.Helper()
	image, _ := result["image"].(map[string]any)
	config, _ := image["Config"].(map[string]any)
	raw, _ := config["Labels"].(map[string]any)
	labels := map[string]string{}
	for key, value := range raw {
		labels[key] = fmt.Sprint(value)
	}
	return labels
}

func writeRemoteFile(t *testing.T, client *ssh.Client, path, contents string) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(contents))
	command := fmt.Sprintf("mkdir -p %s && echo %s | base64 -d > %s", filepath.Dir(path), encoded, path)
	if _, stderr, err := client.Run(command); err != nil {
		t.Fatalf("write %s: %v: %s", path, err, stderr)
	}
}
