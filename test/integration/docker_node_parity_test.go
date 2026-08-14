//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPlaybook_DockerNodeParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-node-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "node_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-node-" + name + ".json"
		playbook := playbookHeader + `
  - name: Manage node
    community.docker.docker_node:
` + arguments + `
    register: node_result
` + taskOptions + `

  - name: Persist node result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
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
	cycle := func(t *testing.T, prefix, arguments string) [4]map[string]any {
		t.Helper()
		return [4]map[string]any{
			success(t, prefix+"-check", arguments, "    check_mode: true\n"),
			success(t, prefix+"-real", arguments, ""),
			success(t, prefix+"-idem", arguments, ""),
			success(t, prefix+"-idem-check", arguments, "    check_mode: true\n"),
		}
	}
	assertChanged := func(t *testing.T, results [4]map[string]any, firstChanged bool) {
		t.Helper()
		want := []bool{firstChanged, firstChanged, false, false}
		for i, result := range results {
			if result["changed"] != want[i] {
				t.Fatalf("run %d changed=%v want %v result=%#v", i+1, result["changed"], want[i], result)
			}
		}
	}

	t.Run("not a swarm manager", func(t *testing.T) {
		infoTemplate := writeResultTemplate(t, "node_info_result")
		playbook := playbookHeader + `
  - name: Inspect nodes off swarm
    community.docker.docker_node_info:
    register: node_info_result

  - name: Persist
    template:
      src: ` + infoTemplate + `
      dest: /tmp/dibra-node-off-swarm.json
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error running docker swarm module: must run on swarm manager node") {
			t.Fatalf("off swarm = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	nodeID := strings.TrimSpace(mustRemote(t, client, "docker node ls --format '{{.ID}}' | head -n1"))
	hostname := strings.TrimSpace(mustRemote(t, client, "docker node ls --format '{{.Hostname}}' | head -n1"))
	if nodeID == "" || hostname == "" {
		t.Fatalf("missing node identity id=%q hostname=%q", nodeID, hostname)
	}
	byID := "      hostname: " + nodeID + "\n"

	t.Run("role manager unchanged", func(t *testing.T) {
		results := cycle(t, "role-manager", byID+"      role: manager\n")
		assertChanged(t, results, false)
		for _, result := range results {
			if nestedMap(result, "node", "Spec")["Role"] != "manager" {
				t.Fatalf("role = %#v", result["node"])
			}
		}
	})

	t.Run("last manager cannot become worker", func(t *testing.T) {
		check := success(t, "role-worker-check", byID+"      role: worker\n", "    check_mode: true\n")
		if check["changed"] != true {
			t.Fatalf("check = %#v", check)
		}
		_, output := run(t, "role-worker-real", byID+"      role: worker\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "attempting to demote the last manager of the swarm") {
			t.Fatalf("demote = %s", output)
		}
	})

	t.Run("availability pause drain active", func(t *testing.T) {
		for _, availability := range []string{"pause", "drain", "active"} {
			results := cycle(t, "avail-"+availability, byID+"      availability: "+availability+"\n")
			assertChanged(t, results, true)
			if nestedMap(results[1], "node", "Spec")["Availability"] != availability {
				t.Fatalf("%s = %#v", availability, results[1]["node"])
			}
		}
	})

	t.Run("merge add update and remove labels", func(t *testing.T) {
		single := cycle(t, "label-single", byID+"      labels:\n        label1: value1\n")
		assertChanged(t, single, true)
		if got := nestedMap(single[1], "node", "Spec", "Labels"); len(got) != 1 || got["label1"] != "value1" {
			t.Fatalf("single = %#v", got)
		}

		multiple := cycle(t, "label-multi", byID+`      labels:
        label2: value2
        label3: value3
        label4: value4
        label5: value5
        label6: value6
`)
		assertChanged(t, multiple, true)
		labels := nestedMap(multiple[1], "node", "Spec", "Labels")
		if len(labels) != 6 || labels["label1"] != "value1" || labels["label6"] != "value6" {
			t.Fatalf("multiple = %#v", labels)
		}

		updated := cycle(t, "label-update", byID+"      labels:\n        label1: value1111\n")
		assertChanged(t, updated, true)
		labels = nestedMap(updated[1], "node", "Spec", "Labels")
		if len(labels) != 6 || labels["label1"] != "value1111" || labels["label5"] != "value5" {
			t.Fatalf("update = %#v", labels)
		}

		updatedMulti := cycle(t, "label-update-multi", byID+`      labels:
        label2: value2222
        label3: value3333
`)
		assertChanged(t, updatedMulti, true)
		labels = nestedMap(updatedMulti[1], "node", "Spec", "Labels")
		if labels["label1"] != "value1111" || labels["label3"] != "value3333" || labels["label5"] != "value5" {
			t.Fatalf("update multi = %#v", labels)
		}

		removed := cycle(t, "label-remove", byID+"      labels_to_remove:\n        - label1\n")
		assertChanged(t, removed, true)
		labels = nestedMap(removed[1], "node", "Spec", "Labels")
		if _, found := labels["label1"]; found || len(labels) != 5 || labels["label5"] != "value5" {
			t.Fatalf("remove = %#v", labels)
		}

		missing := cycle(t, "label-missing", byID+"      labels_to_remove:\n        - labelnotexist\n")
		assertChanged(t, missing, false)
		if len(nestedMap(missing[1], "node", "Spec", "Labels")) != 5 {
			t.Fatalf("missing remove = %#v", missing[1])
		}

		removedTwo := cycle(t, "label-remove-two", byID+"      labels_to_remove:\n        - label2\n        - label3\n")
		assertChanged(t, removedTwo, true)
		labels = nestedMap(removedTwo[1], "node", "Spec", "Labels")
		if _, found := labels["label2"]; found || len(labels) != 3 {
			t.Fatalf("remove two = %#v", labels)
		}

		removedMix := cycle(t, "label-remove-mix", byID+"      labels_to_remove:\n        - label4\n        - labelisnotthere\n")
		assertChanged(t, removedMix, true)
		labels = nestedMap(removedMix[1], "node", "Spec", "Labels")
		if _, found := labels["label4"]; found || len(labels) != 2 {
			t.Fatalf("remove mix = %#v", labels)
		}

		addDel := cycle(t, "label-add-del", byID+`      labels:
        label7: value7
        label8: value8
      labels_to_remove:
        - label5
`)
		assertChanged(t, addDel, true)
		labels = nestedMap(addDel[1], "node", "Spec", "Labels")
		if _, found := labels["label5"]; found || len(labels) != 3 || labels["label8"] != "value8" {
			t.Fatalf("add/del = %#v", labels)
		}

		overlap := cycle(t, "label-overlap", byID+`      labels:
        label22: value22
        label6: value6666
      labels_to_remove:
        - label6
        - label7
`)
		assertChanged(t, overlap, true)
		labels = nestedMap(overlap[1], "node", "Spec", "Labels")
		if _, found := labels["label7"]; found || len(labels) != 3 || labels["label6"] != "value6666" || labels["label22"] != "value22" {
			t.Fatalf("overlap = %#v", labels)
		}

		replaced := cycle(t, "label-replace", byID+`      labels:
        label11: value11
        label12: value12
      labels_state: replace
`)
		assertChanged(t, replaced, true)
		labels = nestedMap(replaced[1], "node", "Spec", "Labels")
		if _, found := labels["label6"]; found || len(labels) != 2 || labels["label12"] != "value12" {
			t.Fatalf("replace = %#v", labels)
		}

		cleared := cycle(t, "label-clear", byID+"      labels_state: replace\n")
		assertChanged(t, cleared, true)
		if len(nestedMap(cleared[1], "node", "Spec", "Labels")) != 0 {
			t.Fatalf("clear = %#v", nestedMap(cleared[1], "node", "Spec", "Labels"))
		}
	})

	t.Run("hostname self inspect and docs extras", func(t *testing.T) {
		byHost := "      hostname: " + hostname + "\n"
		labeled := success(t, "docs-merge", byHost+"      labels:\n        key: value\n", "")
		if labeled["changed"] != true || nestedMap(labeled, "node", "Spec", "Labels")["key"] != "value" {
			t.Fatalf("docs merge = %#v", labeled)
		}
		inspected := strings.TrimSpace(mustRemote(t, client, "docker node inspect "+nodeID+" --format '{{json .Spec.Labels}}'"))
		var engineLabels map[string]any
		if err := json.Unmarshal([]byte(inspected), &engineLabels); err != nil {
			t.Fatalf("decode inspect labels: %v (%s)", err, inspected)
		}
		if engineLabels["key"] != "value" {
			t.Fatalf("engine labels = %#v", engineLabels)
		}

		selfDrain := success(t, "self-drain", "      self: true\n      availability: drain\n", "")
		if selfDrain["changed"] != true || nestedMap(selfDrain, "node", "Spec")["Availability"] != "drain" {
			t.Fatalf("self drain = %#v", selfDrain)
		}
		success(t, "self-active", "      self: true\n      availability: active\n", "")

		replaced := success(t, "docs-replace", byHost+`      labels:
        env: test
      labels_state: replace
`, "")
		if replaced["changed"] != true || nestedMap(replaced, "node", "Spec", "Labels")["env"] != "test" {
			t.Fatalf("docs replace = %#v", replaced)
		}

		removed := success(t, "docs-remove-keys", byHost+"      labels_to_remove:\n        - env\n", "")
		if removed["changed"] != true {
			t.Fatalf("docs remove = %#v", removed)
		}

		_, output := run(t, "invalid-availability", byID+"      availability: offline\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "availability must be") {
			t.Fatalf("invalid availability = %s", output)
		}

		integer := success(t, "integer-label", byHost+"      labels:\n        count: 1\n", "")
		if integer["changed"] != true || fmt.Sprint(nestedMap(integer, "node", "Spec", "Labels")["count"]) != "1" {
			t.Fatalf("integer label = %#v", integer)
		}

		success(t, "docs-clear", byHost+"      labels_state: replace\n", "")
	})
}

func TestPlaybook_DockerNodeInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-node-info-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "node_info_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-node-info-" + name + ".json"
		playbook := playbookHeader + `
  - name: Inspect nodes
    community.docker.docker_node_info:
` + arguments + `
    register: node_info_result
` + taskOptions + `

  - name: Persist node info result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
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
		if result["changed"] != false {
			t.Fatalf("%s changed: %#v", name, result)
		}
		return result
	}

	t.Run("not a swarm manager", func(t *testing.T) {
		_, output := run(t, "not-manager", "", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error running docker swarm module: must run on swarm manager node") {
			t.Fatalf("not manager = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")

	t.Run("list self name missing and extras", func(t *testing.T) {
		all := success(t, "all", "", "")
		nodes, _ := all["nodes"].([]any)
		if len(nodes) == 0 {
			t.Fatalf("all = %#v", all)
		}
		first, _ := nodes[0].(map[string]any)
		id, _ := first["ID"].(string)
		if id == "" {
			t.Fatalf("nodes[0] = %#v", first)
		}

		self := success(t, "self", "      self: true\n", "")
		selfNodes, _ := self["nodes"].([]any)
		if len(selfNodes) != 1 {
			t.Fatalf("self = %#v", self)
		}
		selfNode, _ := selfNodes[0].(map[string]any)
		hostname, _ := nestedMap(selfNode, "Description")["Hostname"].(string)
		if hostname == "" {
			t.Fatalf("self node = %#v", selfNode)
		}

		byName := success(t, "by-name", "      name: "+hostname+"\n", "")
		if got := byName["nodes"].([]any); len(got) != 1 {
			t.Fatalf("by name = %#v", byName)
		}

		byID := success(t, "by-id", "      name: "+id+"\n", "")
		if got := byID["nodes"].([]any); len(got) != 1 {
			t.Fatalf("by id = %#v", byID)
		}

		missing := success(t, "missing", "      name: node-missing-xyz\n", "")
		if got := missing["nodes"].([]any); len(got) != 0 {
			t.Fatalf("missing = %#v", missing)
		}

		mixed := success(t, "mixed-list", "      name:\n        - "+hostname+"\n        - node-missing-xyz\n", "")
		if got := mixed["nodes"].([]any); len(got) != 1 {
			t.Fatalf("mixed = %#v", mixed)
		}

		selfIgnoresName := success(t, "self-ignores-name", "      self: true\n      name: node-missing-xyz\n", "")
		if got := selfIgnoresName["nodes"].([]any); len(got) != 1 {
			t.Fatalf("self ignores name = %#v", selfIgnoresName)
		}

		check := success(t, "check", "      self: true\n", "    check_mode: true\n")
		if got := check["nodes"].([]any); len(got) != 1 {
			t.Fatalf("check = %#v", check)
		}

		raw := strings.TrimSpace(mustRemote(t, client, "docker node inspect "+id+" --format '{{json .}}'"))
		var engine map[string]any
		if err := json.Unmarshal([]byte(raw), &engine); err != nil {
			t.Fatalf("decode docker node inspect: %v (%s)", err, raw)
		}
		moduleNode, _ := byID["nodes"].([]any)[0].(map[string]any)
		if moduleNode["ID"] != engine["ID"] {
			t.Fatalf("ID %v != %v", moduleNode["ID"], engine["ID"])
		}
		moduleSpec := nestedMap(moduleNode, "Spec")
		engineSpec := nestedMap(engine, "Spec")
		if moduleSpec["Role"] != engineSpec["Role"] || moduleSpec["Availability"] != engineSpec["Availability"] {
			t.Fatalf("spec module=%#v engine=%#v", moduleSpec, engineSpec)
		}
	})
}
