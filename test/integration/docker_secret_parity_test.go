//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerSecretParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-ds-%d", time.Now().UnixNano()%100000000)
	passwordName := prefix + "-db"
	rollingName := prefix + "-roll"
	keepName := prefix + "-keep"
	inuseName := prefix + "-inuse"
	serviceName := prefix + "-svc"
	emptyName := prefix + "-empty"
	keylessName := prefix + "-keyless"
	fileName := prefix + "-file"
	dataFile := "/tmp/" + prefix + "-data"
	missingFile := "/tmp/" + prefix + "-missing"

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-ds-*.json /tmp/.dibra-agent "+dataFile)
	defer func() {
		mustRemote(t, client, "docker service rm "+serviceName+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker secret ls -q | xargs -r docker secret rm >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()

	templatePath := writeResultTemplate(t, "secret_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-ds-" + name + ".json"
		playbook := playbookHeader + `
  - name: Manage docker secret
    community.docker.docker_secret:
` + arguments + `
    register: secret_result
` + taskOptions + `

  - name: Persist secret result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") || strings.Contains(output, "Failed to load config") {
			return nil, output
		}
		return readRemoteJSONMap(t, client, remotePath), output
	}
	success := func(t *testing.T, name, arguments, taskOptions string) map[string]any {
		t.Helper()
		result, output := run(t, name, arguments, taskOptions)
		if result == nil {
			t.Fatalf("%s failed: %s", name, output)
		}
		return result
	}
	inspect := func(t *testing.T, ref string) map[string]any {
		t.Helper()
		raw := strings.TrimSpace(mustRemote(t, client, "docker secret inspect "+ref+" --format '{{json .}}'"))
		var engine map[string]any
		if err := json.Unmarshal([]byte(raw), &engine); err != nil {
			t.Fatalf("inspect %s: %v (%s)", ref, err, raw)
		}
		return engine
	}
	exists := func(ref string) bool {
		t.Helper()
		output := remoteExec(t, client, "docker secret inspect "+ref+" >/dev/null 2>&1; echo $?")
		return strings.TrimSpace(output) == "0"
	}

	t.Run("missing required name", func(t *testing.T) {
		_, output := run(t, "missing-name", "      state: present\n      data: x\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("missing name = %s", output)
		}
	})

	t.Run("present without data", func(t *testing.T) {
		_, output := run(t, "missing-data", "      name: foo\n      state: present\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "state is present but any of the following are missing: data, data_src") {
			t.Fatalf("missing data = %s", output)
		}
	})

	t.Run("data and data_src exclusive", func(t *testing.T) {
		_, output := run(t, "exclusive", "      name: foo\n      data: x\n      data_src: /tmp/x\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "parameters are mutually e") || !strings.Contains(output, "clusive: data, data_src") {
			t.Fatalf("exclusive = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")

	t.Run("check mode does not create", func(t *testing.T) {
		predicted := success(t, "check-create", "      name: "+passwordName+"\n      data: opensesame!\n", "    check_mode: true\n")
		if predicted["changed"] != true || predicted["secret_name"] != passwordName {
			t.Fatalf("check create = %#v", predicted)
		}
		if predicted["secret_id"] != nil && predicted["secret_id"] != "" {
			t.Fatalf("check create must omit secret_id: %#v", predicted["secret_id"])
		}
		if exists(passwordName) {
			t.Fatal("check mode created a secret")
		}
	})

	var secretID string
	t.Run("create inspect idempotent data_src and base64", func(t *testing.T) {
		created := success(t, "create", "      name: "+passwordName+"\n      data: opensesame!\n", "")
		if created["changed"] != true || created["secret_name"] != passwordName {
			t.Fatalf("create = %#v", created)
		}
		secretID, _ = created["secret_id"].(string)
		if secretID == "" {
			t.Fatalf("missing secret_id: %#v", created)
		}
		engine := inspect(t, secretID)
		spec := nestedMap(engine, "Spec")
		if spec["Name"] != passwordName {
			t.Fatalf("inspect name = %#v", spec["Name"])
		}
		labels := nestedMap(engine, "Spec", "Labels")
		if labels["ansible_key"] == nil || labels["ansible_key"] == "" {
			t.Fatalf("missing ansible_key: %#v", labels)
		}

		idempotent := success(t, "idempotent", "      name: "+passwordName+"\n      data: opensesame!\n", "")
		if idempotent["changed"] != false || idempotent["secret_id"] != secretID {
			t.Fatalf("idempotent = %#v", idempotent)
		}

		mustRemote(t, client, "printf '%s' 'opensesame!' > "+dataFile)
		fromFile := success(t, "data-src", "      name: "+passwordName+"\n      data_src: "+dataFile+"\n", "")
		if fromFile["changed"] != false {
			t.Fatalf("data_src = %#v", fromFile)
		}

		fromB64 := success(t, "b64", "      name: "+passwordName+"\n      data: b3BlbnNlc2FtZSE=\n      data_is_b64: true\n", "")
		if fromB64["changed"] != false {
			t.Fatalf("data_is_b64 = %#v", fromB64)
		}
	})

	t.Run("data change recreates", func(t *testing.T) {
		updated := success(t, "update", "      name: "+passwordName+"\n      data: newpassword!\n", "")
		if updated["changed"] != true || updated["secret_id"] == secretID {
			t.Fatalf("update = %#v previous=%s", updated, secretID)
		}
		if exists(secretID) {
			t.Fatal("old secret id should be gone after non-rolling update")
		}
		secretID, _ = updated["secret_id"].(string)
	})

	t.Run("labels allow more present", func(t *testing.T) {
		added := success(t, "labels-add", `      name: `+passwordName+`
      data: newpassword!
      labels:
        bar: baz
        one: "1"
        two: "2"
`, "")
		if added["changed"] != true {
			t.Fatalf("add labels = %#v", added)
		}
		same := success(t, "labels-same", `      name: `+passwordName+`
      data: newpassword!
      labels:
        bar: baz
        one: "1"
        two: "2"
`, "")
		if same["changed"] != false {
			t.Fatalf("same labels = %#v", same)
		}
		less := success(t, "labels-less", `      name: `+passwordName+`
      data: newpassword!
      labels:
        bar: baz
        one: "1"
`, "")
		if less["changed"] != false {
			t.Fatalf("allow_more_present dropped two = %#v", less)
		}
		changed := success(t, "labels-change", `      name: `+passwordName+`
      data: newpassword!
      labels:
        bar: monkey
        one: "1"
`, "")
		if changed["changed"] != true {
			t.Fatalf("label value change = %#v", changed)
		}
	})

	t.Run("force recreates", func(t *testing.T) {
		before := success(t, "force-before", "      name: "+passwordName+"\n      data: newpassword!\n      labels:\n        bar: monkey\n        one: \"1\"\n", "")
		forced := success(t, "force", "      name: "+passwordName+"\n      data: newpassword!\n      force: true\n      labels:\n        bar: monkey\n        one: \"1\"\n", "")
		if forced["changed"] != true || forced["secret_id"] == before["secret_id"] {
			t.Fatalf("force = %#v before=%#v", forced, before)
		}
	})

	t.Run("empty data is rejected by Engine 29", func(t *testing.T) {
		_, output := run(t, "empty", "      name: "+emptyName+"\n      data: \"\"\n", "")
		if !strings.Contains(output, "FAILED") || (!strings.Contains(output, "must be larger than 0") && !strings.Contains(output, "secret data")) {
			t.Fatalf("empty data = %s", output)
		}
	})

	t.Run("bool labels fail sanitize", func(t *testing.T) {
		_, output := run(t, "bool-label", `      name: `+passwordName+`
      data: newpassword!
      labels:
        bad: true
`, "")
		if !strings.Contains(output, `The value true for "bad" of labels`) {
			t.Fatalf("bool labels = %s", output)
		}
	})

	t.Run("connection alias docker_url", func(t *testing.T) {
		result := success(t, "alias", "      name: "+passwordName+"\n      data: newpassword!\n      labels:\n        bar: monkey\n        one: \"1\"\n      docker_url: unix:///var/run/docker.sock\n", "")
		if result["changed"] != false {
			t.Fatalf("docker_url alias = %#v", result)
		}
	})

	t.Run("absent is idempotent", func(t *testing.T) {
		removed := success(t, "absent", "      name: "+passwordName+"\n      state: absent\n", "")
		if removed["changed"] != true || exists(passwordName) {
			t.Fatalf("absent = %#v exists=%t", removed, exists(passwordName))
		}
		again := success(t, "absent-again", "      name: "+passwordName+"\n      state: absent\n", "")
		if again["changed"] != false {
			t.Fatalf("absent idempotent = %#v", again)
		}
	})

	t.Run("missing ansible_key without force is unchanged", func(t *testing.T) {
		mustRemote(t, client, "printf '%s' 'plain' | docker secret create "+keylessName+" -")
		result := success(t, "keyless", "      name: "+keylessName+"\n      data: other\n", "")
		if result["changed"] != false {
			t.Fatalf("missing ansible_key = %#v", result)
		}
		success(t, "keyless-absent", "      name: "+keylessName+"\n      state: absent\n", "")
	})

	t.Run("data_src missing file", func(t *testing.T) {
		_, output := run(t, "missing-src", "      name: "+passwordName+"\n      data_src: "+missingFile+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error while reading "+missingFile) {
			t.Fatalf("missing data_src = %s", output)
		}
	})

	t.Run("data_src creates from target file", func(t *testing.T) {
		mustRemote(t, client, "printf '%s' 'from-target' > "+dataFile)
		created := success(t, "src-create", "      name: "+fileName+"\n      data_src: "+dataFile+"\n", "")
		if created["changed"] != true || created["secret_name"] != fileName {
			t.Fatalf("data_src create = %#v", created)
		}
		labels := nestedMap(inspect(t, created["secret_id"].(string)), "Spec", "Labels")
		if labels["ansible_key"] == nil || labels["ansible_key"] == "" {
			t.Fatalf("data_src ansible_key = %#v", labels)
		}
		again := success(t, "src-idempotent", "      name: "+fileName+"\n      data: from-target\n", "")
		if again["changed"] != false {
			t.Fatalf("data then matching data_src contents = %#v", again)
		}
		success(t, "src-absent", "      name: "+fileName+"\n      state: absent\n", "")
	})

	t.Run("check mode does not remove", func(t *testing.T) {
		created := success(t, "check-absent-setup", "      name: "+passwordName+"\n      data: stay\n", "")
		predicted := success(t, "check-absent", "      name: "+passwordName+"\n      state: absent\n", "    check_mode: true\n")
		if predicted["changed"] != true {
			t.Fatalf("check absent = %#v", predicted)
		}
		if !exists(created["secret_id"].(string)) {
			t.Fatal("check mode removed a secret")
		}
		success(t, "check-absent-cleanup", "      name: "+passwordName+"\n      state: absent\n", "")
	})

	t.Run("rolling versions", func(t *testing.T) {
		first := success(t, "roll-v1", "      name: "+rollingName+"\n      data: opensesame!\n      rolling_versions: true\n", "")
		if first["changed"] != true || first["secret_name"] != rollingName+"_v1" {
			t.Fatalf("rolling v1 = %#v", first)
		}
		engine := inspect(t, first["secret_id"].(string))
		labels := nestedMap(engine, "Spec", "Labels")
		if labels["ansible_key"] == nil || labels["ansible_version"] != "1" {
			t.Fatalf("rolling labels = %#v", labels)
		}

		second := success(t, "roll-v2", "      name: "+rollingName+"\n      data: newpassword!\n      rolling_versions: true\n", "")
		if second["changed"] != true || second["secret_name"] != rollingName+"_v2" || second["secret_id"] == first["secret_id"] {
			t.Fatalf("rolling v2 = %#v", second)
		}
		if !exists(first["secret_id"].(string)) || !exists(second["secret_id"].(string)) {
			t.Fatal("rolling update should keep the previous version by default")
		}

		removed := success(t, "roll-absent", "      name: "+rollingName+"\n      rolling_versions: true\n      state: absent\n", "")
		if removed["changed"] != true || exists(first["secret_id"].(string)) || exists(second["secret_id"].(string)) {
			t.Fatalf("rolling absent = %#v", removed)
		}
	})

	t.Run("versions_to_keep", func(t *testing.T) {
		success(t, "keep-v1", "      name: "+keepName+"\n      data: a\n      rolling_versions: true\n      versions_to_keep: 1\n", "")
		second := success(t, "keep-v2", "      name: "+keepName+"\n      data: b\n      rolling_versions: true\n      versions_to_keep: 1\n", "")
		if second["secret_name"] != keepName+"_v2" {
			t.Fatalf("keep v2 = %#v", second)
		}
		listed := strings.TrimSpace(mustRemote(t, client, "docker secret ls --format '{{.Name}}' --filter name="+keepName))
		if listed != keepName+"_v2" {
			t.Fatalf("versions_to_keep=1 listed %q", listed)
		}

		keepAll := success(t, "keep-all", "      name: "+keepName+"\n      data: c\n      rolling_versions: true\n      versions_to_keep: -1\n", "")
		if keepAll["secret_name"] != keepName+"_v3" {
			t.Fatalf("keep -1 = %#v", keepAll)
		}
		listed = strings.TrimSpace(mustRemote(t, client, "docker secret ls --format '{{.Name}}' --filter name="+keepName+" | sort"))
		if !strings.Contains(listed, keepName+"_v2") || !strings.Contains(listed, keepName+"_v3") {
			t.Fatalf("versions_to_keep=-1 listed %q", listed)
		}
		success(t, "keep-absent", "      name: "+keepName+"\n      rolling_versions: true\n      state: absent\n", "")
	})

	t.Run("rolling update while attached to a service", func(t *testing.T) {
		mustRemote(t, client, "docker pull alpine:latest >/dev/null")
		first := success(t, "inuse-v1", "      name: "+inuseName+"\n      data: first\n      rolling_versions: true\n", "")
		createService := playbookHeader + `
  - name: Attach secret to a service
    community.docker.docker_swarm_service:
      name: ` + serviceName + `
      image: alpine:latest
      command: ["sleep", "infinity"]
      secrets:
        - secret_name: ` + inuseName + `_v1
          filename: /run/secrets/inuse
`
		if output := runPlaybook(t, createService); strings.Contains(output, "FAILED") {
			t.Fatalf("create service failed: %s", output)
		}
		updated := success(t, "inuse-v2", "      name: "+inuseName+"\n      data: second\n      rolling_versions: true\n", "")
		if updated["changed"] != true || updated["secret_name"] != inuseName+"_v2" {
			t.Fatalf("rolling while in use = %#v", updated)
		}
		if !exists(first["secret_id"].(string)) {
			t.Fatal("attached v1 must remain until the service releases it")
		}
		mustRemote(t, client, "docker service rm "+serviceName+" >/dev/null 2>&1 || true")
		success(t, "inuse-absent", "      name: "+inuseName+"\n      rolling_versions: true\n      state: absent\n", "")
	})
}
