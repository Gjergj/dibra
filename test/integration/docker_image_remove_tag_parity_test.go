//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerImageTagParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const base = "/tmp/dibra-image-tag"
	prefix := "dibra-image-tag-parity"
	targets := []string{
		prefix + "-one:latest",
		prefix + "-one:foo",
		prefix + "-one:bar",
		prefix + "-two:baz",
	}
	mustRemote(t, client, "rm -rf "+base+" && mkdir -p "+base+" && rm -f /tmp/.dibra-agent")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null && docker pull busybox:latest >/dev/null")
	mustRemote(t, client, "docker image rm -f "+strings.Join(targets, " ")+" >/dev/null 2>&1 || true")
	defer func() {
		mustRemote(t, client, "docker image rm -f "+strings.Join(targets, " ")+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "rm -rf "+base)
	}()

	alpineID := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{.Id}}' alpine:latest"))
	busyboxID := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{.Id}}' busybox:latest"))
	alpineDigest := strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{index .RepoDigests 0}}' alpine:latest"))
	if alpineID == busyboxID || !strings.Contains(alpineDigest, "@sha256:") {
		t.Fatalf("unexpected source identities alpine=%s busybox=%s digest=%s", alpineID, busyboxID, alpineDigest)
	}

	templatePath := writeResultTemplate(t, "image_tag_result")
	runTag := func(name, arguments, taskOptions string) map[string]any {
		t.Helper()
		remotePath := base + "/" + name + ".json"
		playbook := playbookHeader + `
  - name: Tag image
    community.docker.docker_image_tag:
` + arguments + `
    register: image_tag_result
` + taskOptions + `

  - name: Persist tag result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s tag failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("check mode predicts multiple missing tags without mutation", func(t *testing.T) {
		result := runTag("missing-check", `      name: alpine:latest
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
      timeout: 60
      debug: true
`, "    check_mode: true\n    diff: true\n")
		if result["changed"] != true || imageExists(t, client, targets[0]) || imageExists(t, client, targets[1]) {
			t.Fatalf("result=%#v target states=%v/%v", result, imageExists(t, client, targets[0]), imageExists(t, client, targets[1]))
		}
		if got := strings.Join(resultStrings(t, result, "tagged_images"), ","); got != strings.Join(targets[:2], ",") {
			t.Fatalf("tagged_images = %q", got)
		}
		before, after := tagDiffImages(t, result)
		for index := range before {
			if before[index]["exists"] != false || after[index]["id"] != alpineID {
				t.Fatalf("diff[%d] = %#v -> %#v", index, before[index], after[index])
			}
		}
	})

	t.Run("real tag and repeated different input are idempotent", func(t *testing.T) {
		sourceBefore := dockerInspectImage(t, client, "alpine:latest")
		first := runTag("first", `      name: alpine:latest
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
`, "    diff: true\n")
		if first["changed"] != true {
			t.Fatalf("first = %#v", first)
		}
		assertRawImageInspection(t, first["image"], sourceBefore)
		for _, target := range targets[:2] {
			if id := imageIDOnRemote(t, client, target); id != alpineID {
				t.Fatalf("%s ID = %s, want %s", target, id, alpineID)
			}
		}

		repeated := runTag("repeated", `      name: alpine:latest
      tag: foo
      repository:
        - `+targets[0]+`
        - `+strings.TrimSuffix(targets[1], ":foo")+`
`, "    check_mode: true\n    diff: true\n")
		if repeated["changed"] != false || len(resultStrings(t, repeated, "tagged_images")) != 0 {
			t.Fatalf("repeated = %#v", repeated)
		}
		before, after := tagDiffImages(t, repeated)
		if fmt.Sprint(before) != fmt.Sprint(after) {
			t.Fatalf("idempotent diff = %#v", repeated["diff"])
		}
	})

	t.Run("only a missing target changes", func(t *testing.T) {
		result := runTag("one-more", `      name: alpine
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
        - `+targets[3]+`
`, "    diff: true\n")
		if result["changed"] != true || strings.Join(resultStrings(t, result, "tagged_images"), ",") != targets[3] {
			t.Fatalf("result = %#v", result)
		}
		before, after := tagDiffImages(t, result)
		if fmt.Sprint(before[0]) != fmt.Sprint(after[0]) || fmt.Sprint(before[1]) != fmt.Sprint(after[1]) ||
			before[2]["exists"] != false || after[2]["id"] != alpineID {
			t.Fatalf("diff = %#v", result["diff"])
		}
	})

	t.Run("keep preserves wrong targets and creates missing target", func(t *testing.T) {
		result := runTag("keep", `      name: busybox
      existing_images: keep
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
        - `+targets[2]+`
`, "    check_mode: true\n    diff: true\n")
		if result["changed"] != true || strings.Join(resultStrings(t, result, "tagged_images"), ",") != targets[2] {
			t.Fatalf("check result = %#v", result)
		}
		before, after := tagDiffImages(t, result)
		if before[0]["id"] != alpineID || after[0]["id"] != alpineID ||
			before[1]["id"] != alpineID || after[1]["id"] != alpineID ||
			before[2]["exists"] != false || after[2]["id"] != busyboxID {
			t.Fatalf("check diff = %#v", result["diff"])
		}

		actual := runTag("keep-actual", `      name: busybox
      existing_images: keep
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
        - `+targets[2]+`
`, "    diff: true\n")
		if actual["changed"] != true || imageIDOnRemote(t, client, targets[2]) != busyboxID ||
			imageIDOnRemote(t, client, targets[0]) != alpineID {
			t.Fatalf("actual = %#v", actual)
		}
	})

	t.Run("overwrite changes only wrong targets", func(t *testing.T) {
		check := runTag("overwrite-check", `      name: busybox
      existing_images: overwrite
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
        - `+targets[2]+`
`, "    check_mode: true\n    diff: true\n")
		if check["changed"] != true || strings.Join(resultStrings(t, check, "tagged_images"), ",") !=
			strings.Join([]string{targets[0], targets[1]}, ",") {
			t.Fatalf("check = %#v", check)
		}
		before, after := tagDiffImages(t, check)
		if before[0]["id"] != alpineID || after[0]["id"] != busyboxID ||
			before[1]["id"] != alpineID || after[1]["id"] != busyboxID ||
			before[2]["id"] != busyboxID || after[2]["id"] != busyboxID {
			t.Fatalf("check diff = %#v", check["diff"])
		}

		actual := runTag("overwrite-actual", `      name: busybox
      existing_images: overwrite
      repository:
        - `+targets[0]+`
        - `+targets[1]+`
        - `+targets[2]+`
`, "    diff: true\n")
		if actual["changed"] != true {
			t.Fatalf("actual = %#v", actual)
		}
		for _, target := range targets[:3] {
			if imageIDOnRemote(t, client, target) != busyboxID {
				t.Fatalf("%s was not overwritten", target)
			}
		}
	})

	t.Run("digest and image ID sources are accepted", func(t *testing.T) {
		mustRemote(t, client, "docker tag busybox:latest "+targets[3])
		digestResult := runTag("digest-source", `      name: `+alpineDigest+`
      repository:
        - `+targets[3]+`
`, "    diff: true\n")
		if digestResult["changed"] != true || imageIDOnRemote(t, client, targets[3]) != alpineID {
			t.Fatalf("digest result = %#v", digestResult)
		}

		idResult := runTag("id-source", `      name: `+alpineID+`
      repository:
        - `+targets[1]+`
`, "    diff: true\n")
		if idResult["changed"] != true || imageIDOnRemote(t, client, targets[1]) != alpineID {
			t.Fatalf("ID result = %#v", idResult)
		}
	})

	t.Run("validation failures match upstream", func(t *testing.T) {
		digest := strings.SplitN(alpineDigest, "@", 2)[1]
		cases := []struct {
			name      string
			arguments string
			want      string
		}{
			{"digest target", "      name: alpine\n      repository:\n        - target@" + digest + "\n", "repository[1] must not have a digest"},
			{"ID target", "      name: alpine\n      repository:\n        - " + alpineID + "\n", "repository[1] must not be an image ID"},
			{"missing source", "      name: dibra-missing-source\n      repository:\n        - target:latest\n", "Cannot find image dibra-missing-source:latest"},
			{"invalid mode", "      name: alpine\n      existing_images: invalid\n      repository:\n        - target:latest\n", "existing_images must be one of keep or overwrite"},
		}
		for _, test := range cases {
			output := runPlaybook(t, playbookHeader+`
  - name: Invalid tag
    docker_image_tag:
`+test.arguments)
			if !strings.Contains(output, "FAILED") || !strings.Contains(output, test.want) {
				t.Fatalf("%s output = %s", test.name, output)
			}
		}
	})
}

func TestPlaybook_DockerImageRemoveParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const base = "/tmp/dibra-image-remove"
	prefix := "dibra-image-remove-parity"
	tags := []string{prefix + ":latest", prefix + ":foo", prefix + ":bar"}
	mustRemote(t, client, "rm -rf "+base+" && mkdir -p "+base+" && rm -f /tmp/.dibra-agent")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	mustRemote(t, client, "docker image rm -f "+strings.Join(tags, " ")+" >/dev/null 2>&1 || true")
	defer func() {
		mustRemote(t, client, "docker image rm -f "+strings.Join(tags, " ")+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "rm -rf "+base)
	}()

	templatePath := writeResultTemplate(t, "image_remove_result")
	runRemove := func(name, arguments, taskOptions string) map[string]any {
		t.Helper()
		remotePath := base + "/" + name + ".json"
		playbook := playbookHeader + `
  - name: Remove image
    community.docker.docker_image_remove:
` + arguments + `
    register: image_remove_result
` + taskOptions + `

  - name: Persist remove result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s removal failed: %s", name, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("missing image is unchanged in check and real modes", func(t *testing.T) {
		for _, test := range []struct {
			name, options string
		}{{"check", "    check_mode: true\n    diff: true\n"}, {"real", "    diff: true\n"}} {
			result := runRemove("missing-"+test.name, "      name: "+tags[0]+"\n", test.options)
			if result["changed"] != false || len(resultStrings(t, result, "deleted")) != 0 ||
				len(resultStrings(t, result, "untagged")) != 0 {
				t.Fatalf("%s = %#v", test.name, result)
			}
			before, after := resultDiff(t, result)
			if before["exists"] != false || after["exists"] != false {
				t.Fatalf("%s diff = %#v", test.name, result["diff"])
			}
		}
	})

	mustRemote(t, client, "docker tag alpine:latest "+strings.Join(tags, " && docker tag alpine:latest "))
	sourceID := imageIDOnRemote(t, client, "alpine:latest")

	t.Run("tag removal check mode predicts the real diff and returns raw inspection", func(t *testing.T) {
		beforeImage := dockerInspectImage(t, client, tags[0])
		check := runRemove("tag-check", `      name: `+prefix+`
      tag: latest
      force: false
      prune: false
      docker_url: unix:///var/run/docker.sock
`, "    check_mode: true\n    diff: true\n")
		if check["changed"] != true || !imageExists(t, client, tags[0]) {
			t.Fatalf("check = %#v", check)
		}
		assertRawImageInspection(t, check["image"], beforeImage)
		if strings.Join(resultStrings(t, check, "untagged"), ",") != tags[0] ||
			len(resultStrings(t, check, "deleted")) != 0 {
			t.Fatalf("check returns = %#v", check)
		}

		actual := runRemove("tag-actual", `      name: `+tags[0]+`
      prune: false
`, "    diff: true\n")
		if actual["changed"] != true || imageExists(t, client, tags[0]) {
			t.Fatalf("actual = %#v", actual)
		}
		if fmt.Sprint(check["diff"]) != fmt.Sprint(actual["diff"]) ||
			fmt.Sprint(check["untagged"]) != fmt.Sprint(actual["untagged"]) {
			t.Fatalf("check/actual mismatch check=%#v actual=%#v", check, actual)
		}
	})

	t.Run("removed tag is idempotent", func(t *testing.T) {
		for _, options := range []string{"    check_mode: true\n    diff: true\n", "    diff: true\n"} {
			result := runRemove("tag-idempotent-"+fmt.Sprint(len(options)), "      name: "+tags[0]+"\n", options)
			if result["changed"] != false {
				t.Fatalf("result = %#v", result)
			}
		}
	})

	t.Run("force on a tag removes only that tag", func(t *testing.T) {
		check := runRemove("force-tag-check", "      name: "+tags[1]+"\n      force: true\n",
			"    check_mode: true\n    diff: true\n")
		actual := runRemove("force-tag-actual", "      name: "+tags[1]+"\n      force: true\n", "    diff: true\n")
		if check["changed"] != true || actual["changed"] != true || imageExists(t, client, tags[1]) ||
			!imageExists(t, client, tags[2]) || len(resultStrings(t, actual, "deleted")) != 0 {
			t.Fatalf("check=%#v actual=%#v", check, actual)
		}
		if fmt.Sprint(check["diff"]) != fmt.Sprint(actual["diff"]) {
			t.Fatalf("force-tag diffs differ: %#v / %#v", check["diff"], actual["diff"])
		}
	})

	t.Run("image ID requires force in check mode", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Remove ID without force
    docker_image_remove:
      name: `+sourceID+`
    check_mode: true
`)
		if !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "Cannot delete image by ID that is still in use - use force=true") {
			t.Fatalf("output = %s", output)
		}
		if !imageExists(t, client, tags[2]) {
			t.Fatal("failed check mode mutated the image")
		}
	})

	t.Run("force removal by ID predicts deletion and removes every alias", func(t *testing.T) {
		check := runRemove("id-force-check", "      name: "+sourceID+"\n      force: true\n",
			"    check_mode: true\n    diff: true\n")
		if check["changed"] != true || len(resultStrings(t, check, "deleted")) != 1 ||
			resultStrings(t, check, "deleted")[0] != sourceID || !imageExists(t, client, tags[2]) {
			t.Fatalf("check = %#v", check)
		}
		actual := runRemove("id-force-actual", "      name: "+sourceID+"\n      force: true\n", "    diff: true\n")
		if actual["changed"] != true || imageExists(t, client, tags[2]) {
			t.Fatalf("actual = %#v", actual)
		}
		_, checkAfter := resultDiff(t, check)
		_, actualAfter := resultDiff(t, actual)
		if checkAfter["exists"] != false || actualAfter["exists"] != false ||
			!containsString(resultStrings(t, actual, "deleted"), sourceID) {
			t.Fatalf("check=%#v actual=%#v", check, actual)
		}
		// Docker 29 can report a different set of untagged digest aliases than
		// upstream can predict in check mode. The pinned upstream test permits it.
		if !containsString(resultStrings(t, actual, "untagged"), tags[2]) {
			t.Fatalf("actual untagged = %#v", actual["untagged"])
		}
	})

	t.Run("removed ID is idempotent", func(t *testing.T) {
		for _, options := range []string{"    check_mode: true\n    diff: true\n", "    diff: true\n"} {
			result := runRemove("id-idempotent-"+fmt.Sprint(len(options)), "      name: "+sourceID+"\n      force: true\n", options)
			if result["changed"] != false {
				t.Fatalf("result = %#v", result)
			}
		}
	})

	t.Run("invalid tag fails before Engine mutation", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Invalid removal
    docker_image_remove:
      name: alpine
      tag: foo/bar
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, `"foo/bar" is not a valid docker tag`) {
			t.Fatalf("output = %s", output)
		}
	})
}

func tagDiffImages(t *testing.T, result map[string]any) ([]map[string]any, []map[string]any) {
	t.Helper()
	before, after := resultDiff(t, result)
	return resultMapSlice(t, before, "images"), resultMapSlice(t, after, "images")
}

func resultMapSlice(t *testing.T, result map[string]any, key string) []map[string]any {
	t.Helper()
	values, ok := result[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, result[key])
	}
	maps := make([]map[string]any, len(values))
	for index, value := range values {
		maps[index], ok = value.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] = %#v, want object", key, index, value)
		}
	}
	return maps
}

func imageIDOnRemote(t *testing.T, client *ssh.Client, reference string) string {
	t.Helper()
	return strings.TrimSpace(mustRemote(t, client, "docker image inspect --format '{{.Id}}' "+reference))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
