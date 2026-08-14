//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerImageInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine  = "alpine:latest"
		busybox = "busybox:latest"
	)
	remoteExec(t, client, "docker pull "+alpine)
	remoteExec(t, client, "docker pull "+busybox)
	remoteExec(t, client, "docker image rm -f definitely-missing-image-info:latest || true")
	remoteExec(t, client, "rm -f /tmp/dibra-image-info-*.json /tmp/.dibra-agent")

	templatePath := writeResultTemplate(t, "image_info_result")
	runInfo := func(name, arguments string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-image-info-" + name + ".json"
		playbook := playbookHeader + `
  - name: Inspect images
    community.docker.docker_image_info:
` + arguments + `
    register: image_info_result

  - name: Persist image info result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s image info playbook failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("missing image is omitted without pulling", func(t *testing.T) {
		result := runInfo("missing", "      name: definitely-missing-image-info\n")
		if result["changed"] != false || len(resultList(t, result, "images")) != 0 {
			t.Fatalf("result = %#v", result)
		}
		if output := remoteExec(t, client, "docker image inspect definitely-missing-image-info:latest >/dev/null 2>&1; echo $?"); strings.TrimSpace(output) == "0" {
			t.Fatal("info module pulled the missing image")
		}
	})

	t.Run("single image defaults latest and returns raw inspection", func(t *testing.T) {
		result := runInfo("single", `      name: alpine
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
`)
		images := resultList(t, result, "images")
		if result["changed"] != false || len(images) != 1 {
			t.Fatalf("result = %#v", result)
		}
		assertRawImageInspection(t, images[0], dockerInspectImage(t, client, alpine))
	})

	t.Run("multiple images preserve order and skip missing entries", func(t *testing.T) {
		result := runInfo("multiple", `      name:
        - definitely-missing-image-info
        - busybox:latest
        - alpine
`)
		images := resultList(t, result, "images")
		if len(images) != 2 {
			t.Fatalf("images = %#v", images)
		}
		assertRawImageInspection(t, images[0], dockerInspectImage(t, client, busybox))
		assertRawImageInspection(t, images[1], dockerInspectImage(t, client, alpine))
	})

	t.Run("full and short IDs are accepted and duplicates preserved", func(t *testing.T) {
		fullID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+alpine))
		shortID := strings.TrimPrefix(fullID, "sha256:")
		shortID = shortID[:12]
		result := runInfo("ids", `      name:
        - `+fullID+`
        - `+shortID+`
        - `+alpine+`
`)
		images := resultList(t, result, "images")
		if len(images) != 3 {
			t.Fatalf("images = %#v", images)
		}
		expected := dockerInspectImage(t, client, alpine)
		for _, image := range images {
			assertRawImageInspection(t, image, expected)
		}
	})

	t.Run("omitting name fully inspects every listed image", func(t *testing.T) {
		result := runInfo("all", "      docker_host: unix:///var/run/docker.sock\n")
		images := resultList(t, result, "images")
		actualByID := make(map[string]map[string]any, len(images))
		for _, value := range images {
			if value == nil {
				continue
			}
			image, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("images entry = %T, want object or null", value)
			}
			id, _ := image["Id"].(string)
			if id == "" {
				t.Fatalf("listed image missing Id: %#v", image)
			}
			actualByID[id] = image
		}
		expectedIDs := engineListedImageIDs(t, client)
		if len(actualByID) != len(expectedIDs) {
			t.Fatalf("module returned %d images, Engine ImageList returned %d\nmodule: %v\nengine: %v", len(actualByID), len(expectedIDs), sortedStrings(mapKeys(actualByID)), expectedIDs)
		}
		for _, id := range expectedIDs {
			actual, found := actualByID[id]
			if !found {
				t.Fatalf("module result is missing image %s", id)
			}
			assertRawImageInspection(t, actual, dockerInspectImage(t, client, id))
		}
		for _, reference := range []string{alpine, busybox} {
			inspected := dockerInspectImage(t, client, reference)
			id, _ := inspected["Id"].(string)
			actual, found := actualByID[id]
			if !found {
				t.Fatalf("omitted-name list is missing fixture %s (%s)", reference, id)
			}
			assertRawImageInspection(t, actual, inspected)
		}
	})

	t.Run("check and diff modes stay read only", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Inspect images in check and diff mode
    docker_image_info:
      name:
        - alpine
        - busybox
`
		for iteration := 0; iteration < 2; iteration++ {
			output := runPlaybookWithArgs(t, playbook, "--check", "--diff")
			if strings.Contains(output, "FAILED") || strings.Contains(output, "SKIPPED") ||
				strings.Contains(output, "CHANGED") || !strings.Contains(output, "OK") {
				t.Fatalf("read-only run %d failed: %s", iteration+1, output)
			}
		}
	})
}

func TestPlaybook_DockerImageLoadParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine  = "alpine:latest"
		busybox = "busybox:latest"
		base    = "/tmp/dibra-image-load"
	)
	remoteExec(t, client, "rm -rf "+base+" && mkdir -p "+base)
	remoteExec(t, client, "docker pull "+alpine)
	remoteExec(t, client, "docker pull "+busybox)
	alpineID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+alpine))
	busyboxID := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Id}}' "+busybox))
	defer remoteExec(t, client, "rm -rf "+base)

	archives := map[string]string{
		"names":     base + "/names.tar",
		"ids":       base + "/ids.tar",
		"mixed":     base + "/mixed.tar",
		"duplicate": base + "/duplicate.tar",
		"single-id": base + "/single-id.tar",
	}
	remoteExec(t, client, "docker image save -o "+archives["names"]+" "+alpine+" "+busybox)
	remoteExec(t, client, "docker image save -o "+archives["ids"]+" "+alpineID+" "+busyboxID)
	remoteExec(t, client, "docker image save -o "+archives["mixed"]+" "+alpine+" "+busyboxID)
	remoteExec(t, client, "docker image save -o "+archives["duplicate"]+" "+alpineID+" "+alpine)
	remoteExec(t, client, "docker image save -o "+archives["single-id"]+" "+alpineID)

	templatePath := writeResultTemplate(t, "image_load_result")
	removeImages := func() {
		t.Helper()
		remoteExec(t, client, "docker image rm -f "+alpine+" "+busybox+" "+alpineID+" "+busyboxID+" >/dev/null 2>&1 || true")
	}
	load := func(name, path string) map[string]any {
		t.Helper()
		remotePath := base + "/result-" + name + ".json"
		playbook := playbookHeader + `
  - name: Load image archive
    community.docker.docker_image_load:
      path: ` + path + `
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
    register: image_load_result

  - name: Persist image load result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s load failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("check mode is unsupported and cannot load", func(t *testing.T) {
		removeImages()
		playbook := playbookHeader + `
  - name: Loading is skipped in check mode
    docker_image_load:
      path: ` + archives["names"] + `
`
		output := runPlaybookWithArgs(t, playbook, "--check")
		if strings.Contains(output, "FAILED") || !strings.Contains(output, "SKIPPED") {
			t.Fatalf("check-mode behavior = %s", output)
		}
		if imageExists(t, client, alpine) || imageExists(t, client, busybox) {
			t.Fatal("check mode loaded images")
		}
	})

	t.Run("all images by ID and repeated load always change", func(t *testing.T) {
		removeImages()
		first := load("ids-first", archives["ids"])
		if first["changed"] != true {
			t.Fatalf("first load result = %#v", first)
		}
		assertLoadedInspections(t, client, first)
		second := load("ids-second", archives["ids"])
		expected := sortedStrings([]string{alpineID, busyboxID})
		for index, result := range []map[string]any{first, second} {
			if result["changed"] != true || !reflect.DeepEqual(sortedResultStrings(t, result, "image_names"), expected) {
				t.Fatalf("load %d result = %#v", index+1, result)
			}
		}
		assertLoadedInspections(t, client, second)
	})

	t.Run("mixed names and IDs match upstream Engine 29 alternatives", func(t *testing.T) {
		removeImages()
		result := load("mixed", archives["mixed"])
		names := sortedResultStrings(t, result, "image_names")
		allowed := [][]string{
			sortedStrings([]string{alpine, alpineID, busyboxID}),
			sortedStrings([]string{alpine, busyboxID}),
			{alpine},
		}
		if result["changed"] != true || !containsStringList(allowed, names) {
			t.Fatalf("mixed result = %#v", result)
		}
		if count := len(resultList(t, result, "images")); count < 1 || count > 3 {
			t.Fatalf("mixed images = %#v", result["images"])
		}
		assertLoadedInspections(t, client, result)
	})

	t.Run("same image twice preserves upstream one-or-two result", func(t *testing.T) {
		removeImages()
		result := load("duplicate", archives["duplicate"])
		names := sortedResultStrings(t, result, "image_names")
		if !containsStringList([][]string{{alpine}, sortedStrings([]string{alpine, alpineID})}, names) {
			t.Fatalf("duplicate result = %#v", result)
		}
		images := resultList(t, result, "images")
		if len(images) < 1 || len(images) > 2 {
			t.Fatalf("duplicate images = %#v", images)
		}
		for _, value := range images {
			if value.(map[string]any)["Id"] != alpineID {
				t.Fatalf("unexpected duplicate inspection: %#v", value)
			}
		}
	})

	t.Run("single ID remains untagged", func(t *testing.T) {
		removeImages()
		result := load("single-id", archives["single-id"])
		if !reflect.DeepEqual(resultStrings(t, result, "image_names"), []string{alpineID}) ||
			len(resultList(t, result, "images")) != 1 ||
			resultList(t, result, "images")[0].(map[string]any)["Id"] != alpineID {
			t.Fatalf("single-ID result = %#v", result)
		}
		if imageExists(t, client, alpine) {
			t.Fatalf("%s unexpectedly exists after ID-only load", alpine)
		}
	})

	t.Run("all images by name return names and complete inspections", func(t *testing.T) {
		removeImages()
		result := load("names", archives["names"])
		if !reflect.DeepEqual(sortedResultStrings(t, result, "image_names"), []string{alpine, busybox}) {
			t.Fatalf("names result = %#v", result)
		}
		assertLoadedInspections(t, client, result)
	})

	t.Run("missing and corrupt archives fail", func(t *testing.T) {
		missing := playbookHeader + `
  - name: Load missing archive
    docker_image_load:
      path: ` + base + `/missing.tar
`
		if output := runPlaybook(t, missing); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error opening archive") {
			t.Fatalf("missing archive output = %s", output)
		}

		remoteExec(t, client, "printf 'not a docker archive' > "+base+"/corrupt.tar")
		corrupt := playbookHeader + `
  - name: Load corrupt archive
    docker_image_load:
      path: ` + base + `/corrupt.tar
`
		if output := runPlaybook(t, corrupt); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error loading archive") {
			t.Fatalf("corrupt archive output = %s", output)
		}
	})
}

func writeResultTemplate(t *testing.T, variable string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), variable+".j2")
	if err := os.WriteFile(path, []byte(`{{ `+variable+` | to_json }}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readRemoteJSONMap(t *testing.T, client *ssh.Client, path string) map[string]any {
	t.Helper()
	raw, stderr, err := client.Run("cat " + path)
	if err != nil {
		t.Fatalf("read %s: %v: %s", path, err, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, raw)
	}
	return result
}

func dockerInspectImage(t *testing.T, client *ssh.Client, reference string) map[string]any {
	t.Helper()
	raw, stderr, err := client.Run("docker image inspect " + reference)
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", reference, err, stderr)
	}
	var images []map[string]any
	if err := json.Unmarshal([]byte(raw), &images); err != nil || len(images) != 1 {
		t.Fatalf("decode docker inspect %s: %v\n%s", reference, err, raw)
	}
	return images[0]
}

func assertRawImageInspection(t *testing.T, actual any, expected map[string]any) {
	t.Helper()
	actualMap, ok := actual.(map[string]any)
	if !ok || !reflect.DeepEqual(actualMap, expected) {
		t.Fatalf("module inspection does not match docker inspect\ndiff: %s\nmodule keys: %v\ndocker keys: %v", inspectionDiff(actualMap, expected), sortedMapKeys(actualMap), sortedMapKeys(expected))
	}
}

func inspectionDiff(actual, expected map[string]any) string {
	if actual == nil {
		return "module result is not an object"
	}
	var parts []string
	for _, key := range sortedMapKeys(expected) {
		if !reflect.DeepEqual(actual[key], expected[key]) {
			parts = append(parts, fmt.Sprintf("%s: module=%T docker=%T", key, actual[key], expected[key]))
		}
	}
	for _, key := range sortedMapKeys(actual) {
		if _, found := expected[key]; !found {
			parts = append(parts, fmt.Sprintf("%s: extra in module", key))
		}
	}
	if len(parts) > 12 {
		parts = append(parts[:12], "...")
	}
	return strings.Join(parts, "; ")
}

func sortedMapKeys(values map[string]any) []string {
	if values == nil {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resultList(t *testing.T, result map[string]any, key string) []any {
	t.Helper()
	values, ok := result[key].([]any)
	if !ok {
		t.Fatalf("%s = %T, want list in %#v", key, result[key], result)
	}
	return values
}

func resultStrings(t *testing.T, result map[string]any, key string) []string {
	t.Helper()
	values := resultList(t, result, key)
	stringsResult := make([]string, len(values))
	for index, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s[%d] = %T, want string", key, index, value)
		}
		stringsResult[index] = text
	}
	return stringsResult
}

func sortedResultStrings(t *testing.T, result map[string]any, key string) []string {
	t.Helper()
	return sortedStrings(resultStrings(t, result, key))
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func mapKeys(values map[string]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func containsStringList(options [][]string, value []string) bool {
	for _, option := range options {
		if reflect.DeepEqual(option, value) {
			return true
		}
	}
	return false
}

func assertLoadedInspections(t *testing.T, client *ssh.Client, result map[string]any) {
	t.Helper()
	names := resultStrings(t, result, "image_names")
	images := resultList(t, result, "images")
	if len(names) != len(images) {
		t.Fatalf("image_names/images length mismatch: %#v", result)
	}
	for index, name := range names {
		assertRawImageInspection(t, images[index], dockerInspectImage(t, client, name))
	}
}

func uniqueNonemptyLines(output string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
	}
	return result
}

func engineListedImageIDs(t *testing.T, client *ssh.Client) []string {
	t.Helper()
	raw := remoteExec(t, client, "curl -sS --unix-socket /var/run/docker.sock http://localhost/images/json")
	var summaries []struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal([]byte(raw), &summaries); err != nil {
		t.Fatalf("decode Engine image list: %v\n%s", err, raw)
	}
	ids := make([]string, 0, len(summaries))
	seen := map[string]bool{}
	for _, summary := range summaries {
		if summary.ID == "" || seen[summary.ID] {
			continue
		}
		seen[summary.ID] = true
		ids = append(ids, summary.ID)
	}
	return ids
}

func imageExists(t *testing.T, client *ssh.Client, reference string) bool {
	t.Helper()
	output, stderr, err := client.Run("docker image inspect " + reference + " >/dev/null 2>&1; echo $?")
	if err != nil {
		t.Fatalf("inspect image existence: %v: %s", err, stderr)
	}
	return strings.TrimSpace(output) == "0"
}
