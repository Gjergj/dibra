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
	remoteExec(t, client, fmt.Sprintf("printf 'FROM alpine:latest\\nARG RELEASE\\nRUN echo \"$RELEASE\" > /release.txt\\n' > %s/Dockerfile", buildDir))
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
      args:
        RELEASE: "2026.08"
      etc_hosts:
        host.test: host-gateway
      labels:
        parity: verified
      platform: linux
      shm_size: 128MB
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

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") || strings.Contains(output, "CHANGED") {
		t.Fatalf("Expected second build to be idempotent: %s", output)
	}

	beforeID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+imageName+":latest"))
	checkPlaybook := strings.Replace(playbook, "      shm_size: 128MB", "      shm_size: 128MB\n      rebuild: always\n      nocache: true", 1)
	output = runPlaybookWithArgs(t, checkPlaybook, "--check")
	if strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("Expected rebuild=always check mode to predict a build: %s", output)
	}
	afterID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+imageName+":latest"))
	if beforeID != afterID {
		t.Fatalf("check mode rebuilt image: before=%s after=%s", beforeID, afterID)
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
	architecture := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Architecture}}' "+sourceImage))
	remoteExec(t, client, "rm -f "+exportPath)
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")
	defer remoteExec(t, client, "rm -f "+exportPath)

	// 1. Predict export without writing
	playbook := playbookHeader + `
  - name: Export docker image
    docker_image_export:
      name: ` + sourceImage + `
      path: ` + exportPath + `
      platform: linux/` + architecture + `
`
	output := runPlaybookWithArgs(t, playbook, "--check")
	if strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("Check-mode export failed: %s", output)
	}
	if remoteFileExists(t, client, exportPath) {
		t.Fatal("Check mode wrote the image archive")
	}

	// 2. Export image
	t.Log("Step 2: Export image to archive")
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Export failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on image export")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Second export failed: %s", output)
	}
	// Engine 29's containerd image store can emit a manifest config ID that
	// differs from the inspect ID, so upstream does not guarantee this second
	// run is unchanged on the pinned baseline.

	// Verify file exists and is a tar (at least exists)
	if !remoteFileExists(t, client, exportPath) {
		t.Errorf("Exported file not found: %s", exportPath)
	}
	manifest := remoteExec(t, client, "tar -xOf "+exportPath+" manifest.json")
	if !strings.Contains(manifest, sourceImage) {
		t.Fatalf("Archive manifest does not contain %s: %s", sourceImage, manifest)
	}
}
