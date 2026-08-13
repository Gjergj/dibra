//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlaybook_DockerImageLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ctx := "busybox"
	tag := "latest"
	imageName := ctx + ":" + tag

	// Ensure clean state
	remoteExec(t, client, "docker rmi "+imageName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent") // Force update agent

	// 1. Pull Image
	t.Log("Step 1: Pull Image")
	playbookPull := playbookHeader + `
  - name: Pull busybox
    docker_image:
      name: ` + ctx + `
      tag: ` + tag + `
      source: pull
      state: present
`
	output1 := runPlaybook(t, playbookPull)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Pull failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first pull")
	}

	// Verify
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if !strings.Contains(images, imageName) {
		t.Errorf("Image %s not found in docker images. Got: %s", imageName, images)
	}

	// 2. Idempotency (Pull again)
	t.Log("Step 2: Pull Idempotency")
	output2 := runPlaybook(t, playbookPull)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second pull")
	}

	// 3. Remove Image
	t.Log("Step 3: Remove Image")
	playbookRemove := playbookHeader + `
  - name: Remove busybox
    docker_image:
      name: ` + ctx + `
      tag: ` + tag + `
      state: absent
`
	output3 := runPlaybook(t, playbookRemove)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Remove failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify removal
	imagesAfter := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if strings.Contains(imagesAfter, imageName) {
		t.Errorf("Image %s still exists after removal", imageName)
	}

	// 4. Remove Idempotency
	t.Log("Step 4: Remove Idempotency")
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "CHANGED") {
		t.Error("Expected no changes on second remove")
	}
}

// TestPlaybook_DockerImageParity ports the pinned upstream build, archive,
// load, local, check-mode, return-contract, and idempotency paths to Engine 29.
func TestPlaybook_DockerImageParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	buildName := "dibra-docker-image-parity:latest"
	loadName := "dibra-docker-image-loaded:latest"
	buildDir := "/tmp/dibra-docker-image-context"
	archivePath := "/tmp/dibra-docker-image.tar"
	loadPath := "/tmp/dibra-docker-image-load.tar"
	registryName := "dibra-docker-image-registry"
	pushName := "127.0.0.1:5000/dibra/parity:latest"
	remoteExec(t, client, "docker rm -f "+registryName+" >/dev/null 2>&1 || true")
	remoteExec(t, client, "docker rmi -f "+buildName+" "+loadName+" "+pushName+" >/dev/null 2>&1 || true")
	remoteExec(t, client, "rm -rf "+buildDir+" "+archivePath+" "+loadPath)
	remoteExec(t, client, "mkdir -p "+buildDir)
	remoteExec(t, client, fmt.Sprintf("printf 'FROM scratch\\nLABEL parity=verified\\n' > %s/Dockerfile", buildDir))
	defer remoteExec(t, client, "docker rm -f "+registryName+" >/dev/null 2>&1 || true")
	defer remoteExec(t, client, "docker rmi -f "+buildName+" "+loadName+" "+pushName+" >/dev/null 2>&1 || true")
	defer remoteExec(t, client, "rm -rf "+buildDir+" "+archivePath+" "+loadPath)

	buildPlaybook := playbookHeader + `
  - name: Build through the legacy docker_image API contract
    community.docker.docker_image:
      name: dibra-docker-image-parity
      source: build
      build:
        path: ` + buildDir + `
        dockerfile: Dockerfile
        pull: false
        rm: true
        nocache: false
        platform: linux
        labels:
          parity: verified
      state: present
`
	firstBuild := runPlaybook(t, buildPlaybook)
	if strings.Contains(firstBuild, "FAILED") || !strings.Contains(firstBuild, "CHANGED") {
		t.Fatalf("docker_image build failed: %s", firstBuild)
	}
	secondBuild := runPlaybook(t, buildPlaybook)
	if strings.Contains(secondBuild, "FAILED") || strings.Contains(secondBuild, "CHANGED") {
		t.Fatalf("docker_image build was not idempotent: %s", secondBuild)
	}
	if inspect := remoteExec(t, client, "docker inspect --format '{{index .Config.Labels \"parity\"}}' "+buildName); !strings.Contains(inspect, "verified") {
		t.Fatalf("built image is missing its label: %s", inspect)
	}

	remoteExec(t, client, "docker run -d --name "+registryName+" -p 5000:5000 registry:2")
	pushPlaybook := playbookHeader + `
  - name: Tag and push through the docker_image contract
    docker_image:
      name: ` + buildName + `
      source: local
      repository: ` + pushName + `
      push: true
      state: present
`
	firstPush := runPlaybook(t, pushPlaybook)
	if strings.Contains(firstPush, "FAILED") {
		t.Fatalf("docker_image push failed: %s", firstPush)
	}
	secondPush := runPlaybook(t, pushPlaybook)
	if strings.Contains(secondPush, "FAILED") || strings.Contains(secondPush, "CHANGED") {
		t.Fatalf("docker_image push was not idempotent: %s", secondPush)
	}
	remoteExec(t, client, "docker rmi "+pushName)
	remoteExec(t, client, "docker pull "+pushName)

	checkRemove := playbookHeader + `
  - name: Predict image removal
    docker_image:
      name: ` + buildName + `
      state: absent
      force_absent: true
`
	checkOutput := runPlaybookWithArgs(t, checkRemove, "--check")
	if strings.Contains(checkOutput, "FAILED") || strings.Contains(checkOutput, "SKIPPED") || !strings.Contains(checkOutput, "CHANGED") {
		t.Fatalf("docker_image check mode did not predict removal: %s", checkOutput)
	}
	if inspect := remoteExec(t, client, "docker inspect "+buildName+" >/dev/null && echo present"); !strings.Contains(inspect, "present") {
		t.Fatal("docker_image check mode removed the image")
	}

	archivePlaybook := playbookHeader + `
  - name: Archive an existing image
    docker_image:
      name: ` + buildName + `
      source: local
      archive_path: ` + archivePath + `
      state: present
`
	archiveOutput := runPlaybook(t, archivePlaybook)
	if strings.Contains(archiveOutput, "FAILED") || !strings.Contains(archiveOutput, "CHANGED") {
		t.Fatalf("docker_image archive failed: %s", archiveOutput)
	}
	if exists := remoteExec(t, client, "test -s "+archivePath+" && echo present"); !strings.Contains(exists, "present") {
		t.Fatal("docker_image did not write its archive")
	}

	remoteExec(t, client, "docker tag "+buildName+" "+loadName)
	remoteExec(t, client, "docker save -o "+loadPath+" "+loadName)
	remoteExec(t, client, "docker rmi "+loadName)
	loadPlaybook := playbookHeader + `
  - name: Load the requested image from an archive
    docker_image:
      name: ` + loadName + `
      source: load
      load_path: ` + loadPath + `
      state: present
`
	firstLoad := runPlaybook(t, loadPlaybook)
	if strings.Contains(firstLoad, "FAILED") || !strings.Contains(firstLoad, "CHANGED") {
		t.Fatalf("docker_image load failed: %s", firstLoad)
	}
	secondLoad := runPlaybook(t, loadPlaybook)
	if strings.Contains(secondLoad, "FAILED") || strings.Contains(secondLoad, "CHANGED") {
		t.Fatalf("docker_image load was not idempotent: %s", secondLoad)
	}
}
