//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerImageExportParity independently ports the pinned
// community.docker docker_image_export integration targets
// (tasks/tests/basic.yml and tasks/tests/platform.yml), the 5.2.2 module
// documentation examples, and documented force/check/error contracts that
// those targets do not run.
func TestPlaybook_DockerImageExportParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine  = "alpine:latest"
		busybox = "busybox:latest"
		root    = "/tmp/dibra-image-export-parity"
	)
	remoteExec(t, client, "docker pull "+alpine)
	remoteExec(t, client, "docker pull "+busybox)
	remoteExec(t, client, "rm -rf "+root+" && mkdir -p "+root)
	remoteExec(t, client, "rm -f /tmp/.dibra-agent /tmp/dibra-image-export-*.json")
	defer remoteExec(t, client, "rm -rf "+root)

	alpineID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+alpine))
	busyboxID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+busybox))
	if alpineID == "" || busyboxID == "" || alpineID == busyboxID {
		t.Fatalf("expected two distinct local images, alpine=%q busybox=%q", alpineID, busyboxID)
	}
	architecture := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Architecture}}' "+alpine))

	t.Run("missing image fails", func(t *testing.T) {
		output := runImageExportOutput(t, `
      names: [definitely-missing-image-export:latest]
      path: `+root+`/missing.tar
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Image definitely-missing-image-export:latest not found") {
			t.Fatalf("missing image: %s", output)
		}
	})

	t.Run("empty names fail", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Export without names
    docker_image_export:
      path: `+root+`/empty.tar
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(strings.ToLower(output), "at least one") {
			t.Fatalf("empty names: %s", output)
		}
	})

	t.Run("basic multi-name and id combinations", func(t *testing.T) {
		tasks := []struct {
			file   string
			images []string
		}{
			{file: "archive-1.tar", images: []string{alpine, busybox}},
			{file: "archive-2.tar", images: []string{alpineID, busyboxID}},
			{file: "archive-3.tar", images: []string{alpine, busyboxID}},
			{file: "archive-4.tar", images: []string{alpineID, alpine}},
			{file: "archive-5.tar", images: []string{alpineID}},
		}
		for _, task := range tasks {
			namesYAML := ""
			for _, image := range task.images {
				namesYAML += "        - " + image + "\n"
			}
			result := runImageExport(t, client, strings.TrimSuffix(task.file, ".tar"), `
      names:
`+namesYAML+`      path: `+root+`/`+task.file+`
`)
			if result["changed"] != true {
				t.Fatalf("%s unchanged: %#v", task.file, result)
			}
			exported := resultList(t, result, "images")
			if len(exported) != len(task.images) {
				t.Fatalf("%s images len=%d want %d: %#v", task.file, len(exported), len(task.images), result["images"])
			}
			for index, requested := range task.images {
				want := dockerInspectImage(t, client, requested)
				assertRawImageInspection(t, exported[index], want)
			}

			manifestRaw := remoteExec(t, client, "tar -xOf "+root+"/"+task.file+" manifest.json")
			var manifest []map[string]any
			if err := json.Unmarshal([]byte(manifestRaw), &manifest); err != nil || len(manifest) == 0 {
				t.Fatalf("%s manifest: %v %s", task.file, err, manifestRaw)
			}
			// Engine 29's containerd store can emit manifest config IDs that
			// differ from inspect IDs, so upstream skips unique-ID equality
			// against manifest.json on Docker >= 29 (PR 1199).
		}
	})

	t.Run("docs examples name and names", func(t *testing.T) {
		single := runImageExport(t, client, "docs-name", `
      name: `+alpine+`
      path: `+root+`/centos-7.tar
`)
		if single["changed"] != true || len(resultList(t, single, "images")) != 1 {
			t.Fatalf("docs name = %#v", single)
		}
		assertRawImageInspection(t, resultList(t, single, "images")[0], dockerInspectImage(t, client, alpine))

		multiple := runImageExport(t, client, "docs-names", `
      names:
        - `+alpine+`
        - `+busybox+`
      path: `+root+`/various.tar
`)
		if multiple["changed"] != true || len(resultList(t, multiple, "images")) != 2 {
			t.Fatalf("docs names = %#v", multiple)
		}
	})

	t.Run("second export without force still succeeds", func(t *testing.T) {
		path := root + "/second.tar"
		first := runImageExport(t, client, "second-first", `
      name: `+alpine+`
      path: `+path+`
`)
		if first["changed"] != true {
			t.Fatalf("first export = %#v", first)
		}
		second := runImageExport(t, client, "second-again", `
      name: `+alpine+`
      path: `+path+`
`)
		if second["failed"] == true {
			t.Fatalf("second export failed: %#v", second)
		}
		if !remoteFileExists(t, client, path) {
			t.Fatal("second export removed the archive")
		}
	})

	t.Run("force overwrites an existing archive", func(t *testing.T) {
		path := root + "/force.tar"
		first := runImageExport(t, client, "force-first", `
      name: `+alpine+`
      path: `+path+`
`)
		if first["changed"] != true {
			t.Fatalf("first force export = %#v", first)
		}
		forced := runImageExport(t, client, "force-true", `
      name: `+alpine+`
      path: `+path+`
      force: true
`)
		if forced["changed"] != true || !strings.Contains(resultString(forced, "msg"), "force=true") {
			t.Fatalf("forced export = %#v", forced)
		}
		if !remoteFileExists(t, client, path) {
			t.Fatal("force export removed the archive")
		}
	})

	t.Run("platform check mode then real export", func(t *testing.T) {
		archive := root + "/platform-test.tar"
		remoteExec(t, client, "rm -f "+archive)
		check := runImageExportWithArgs(t, client, "platform-check", `
      name: `+alpine+`
      path: `+archive+`
      platform: linux/`+architecture+`
`, "--check")
		if check["changed"] != true {
			t.Fatalf("platform check = %#v", check)
		}
		if remoteFileExists(t, client, archive) {
			t.Fatal("platform check mode wrote the archive")
		}
		real := runImageExport(t, client, "platform-real", `
      name: `+alpine+`
      path: `+archive+`
      platform: linux/`+architecture+`
`)
		if real["changed"] != true {
			t.Fatalf("platform export = %#v", real)
		}
		assertRawImageInspection(t, resultList(t, real, "images")[0], dockerInspectImage(t, client, alpine))
		if !remoteFileExists(t, client, archive) {
			t.Fatal("platform export did not write the archive")
		}
	})

	t.Run("tag default and docker_host", func(t *testing.T) {
		result := runImageExport(t, client, "tag-host", `
      name: alpine
      tag: latest
      path: `+root+`/tag-host.tar
      docker_host: unix:///var/run/docker.sock
      api_version: auto
`)
		if result["changed"] != true {
			t.Fatalf("tag/host export = %#v", result)
		}
		assertRawImageInspection(t, resultList(t, result, "images")[0], dockerInspectImage(t, client, alpine))
	})

	t.Run("missing path and invalid tag fail", func(t *testing.T) {
		if output := runPlaybook(t, playbookHeader+`
  - name: Missing path
    docker_image_export:
      name: `+alpine+`
`); !strings.Contains(output, "FAILED") || !strings.Contains(strings.ToLower(output), "path") {
			t.Fatalf("missing path: %s", output)
		}
		if output := runPlaybook(t, playbookHeader+`
  - name: Invalid tag
    docker_image_export:
      name: alpine
      tag: "not a tag"
      path: `+root+`/bad-tag.tar
`); !strings.Contains(output, "FAILED") || !strings.Contains(output, "not a valid docker tag") {
			t.Fatalf("invalid tag: %s", output)
		}
		if output := runPlaybook(t, playbookHeader+`
  - name: Invalid platform
    docker_image_export:
      name: `+alpine+`
      path: `+root+`/bad-platform.tar
      platform: linux/
`); !strings.Contains(output, "FAILED") || !strings.Contains(strings.ToLower(output), "platform") {
			t.Fatalf("invalid platform: %s", output)
		}
	})

	t.Run("platform requires API 1.48", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Old API platform
    docker_image_export:
      name: `+alpine+`
      path: `+root+`/old-api.tar
      platform: linux/amd64
      api_version: "1.47"
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "1.48") {
			t.Fatalf("old API platform: %s", output)
		}
	})
}

func runImageExport(t *testing.T, client *ssh.Client, suffix, arguments string) map[string]any {
	t.Helper()
	run := runImageExportResult(t, client, suffix, arguments)
	if run.failed {
		t.Fatalf("export failed: %s", run.output)
	}
	return run.result
}

func runImageExportWithArgs(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) map[string]any {
	t.Helper()
	run := runImageExportResult(t, client, suffix, arguments, extra...)
	if run.failed {
		t.Fatalf("export failed: %s", run.output)
	}
	return run.result
}

func runImageExportOutput(t *testing.T, arguments string) string {
	t.Helper()
	return runImageExportResult(t, nil, "fail", arguments).output
}

type imageExportRun struct {
	output string
	result map[string]any
	failed bool
}

func runImageExportResult(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) imageExportRun {
	t.Helper()
	remotePath := "/tmp/dibra-image-export-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "export_result")
	playbook := playbookHeader + `
  - name: Export images
    community.docker.docker_image_export:
` + arguments + `
    register: export_result

  - name: Persist export result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	failed := strings.Contains(output, "FAILED")
	run := imageExportRun{output: output, failed: failed}
	if failed || client == nil {
		return run
	}
	run.result = readRemoteJSONMap(t, client, remotePath)
	return run
}
