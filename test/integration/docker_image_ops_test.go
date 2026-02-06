//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlaybook_DockerImageBuild(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	imageName := "build-test-image"
	buildDir := "/tmp/docker-build-test"

	// Clean up and prepare build environment on remote host
	remoteExec(t, client, "docker rmi "+imageName+":latest || true")
	remoteExec(t, client, "rm -rf "+buildDir+" && mkdir -p "+buildDir)
	remoteExec(t, client, fmt.Sprintf("echo 'FROM alpine:latest' > %s/Dockerfile", buildDir))
	remoteExec(t, client, fmt.Sprintf("echo 'RUN echo \"built at $(date)\" > /built.txt' >> %s/Dockerfile", buildDir))
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	defer remoteExec(t, client, "docker rmi "+imageName+":latest || true")
	defer remoteExec(t, client, "rm -rf "+buildDir)

	// 1. Build image
	t.Log("Step 1: Build image from Dockerfile")
	playbook := playbookHeader + `
  - name: Build docker image
    docker_image_build:
      name: ` + imageName + `
      tag: latest
      path: ` + buildDir + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Build failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on image build")
	}

	// Verify image exists
	inspect := remoteExec(t, client, "docker image inspect "+imageName+":latest")
	if !strings.Contains(inspect, imageName) {
		t.Errorf("Image not found after build: %s", inspect)
	}
}

func TestPlaybook_DockerImageLoad(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	sourceImage := "alpine:latest"
	loadTag := "load-test-image:latest"
	archivePath := "/tmp/load-test.tar"

	// Clean up and prepare archive
	remoteExec(t, client, "docker pull "+sourceImage)
	remoteExec(t, client, "docker tag "+sourceImage+" "+loadTag)
	remoteExec(t, client, "docker save -o "+archivePath+" "+loadTag)
	remoteExec(t, client, "docker rmi "+loadTag)
	defer remoteExec(t, client, "rm -f "+archivePath)
	defer remoteExec(t, client, "docker rmi "+loadTag+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Load image
	t.Log("Step 1: Load image from archive")
	playbook := playbookHeader + `
  - name: Load docker image
    docker_image_load:
      path: ` + archivePath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Load failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on image load")
	}

	// Verify image exists
	inspect := remoteExec(t, client, "docker image inspect "+loadTag)
	if !strings.Contains(inspect, "load-test-image") {
		t.Errorf("Image not found after load: %s", inspect)
	}
}

func TestPlaybook_DockerImageExport(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	sourceImage := "alpine:latest"
	exportPath := "/tmp/export-test.tar"

	// Prepare: ensure image exists and clean up old export
	remoteExec(t, client, "docker pull "+sourceImage)
	remoteExec(t, client, "rm -f "+exportPath)
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")
	defer remoteExec(t, client, "rm -f "+exportPath)

	// 1. Export image
	t.Log("Step 1: Export image to archive")
	playbook := playbookHeader + `
  - name: Export docker image
    docker_image_export:
      name: ` + sourceImage + `
      path: ` + exportPath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Export failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on image export")
	}

	// Verify file exists and is a tar (at least exists)
	if !remoteFileExists(t, client, exportPath) {
		t.Errorf("Exported file not found: %s", exportPath)
	}
}
