//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerSwarmParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-swarm-*.json /tmp/.dibra-agent /tmp/dibra-swarm-ca-*")
	defer mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "swarm_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-swarm-" + name + ".json"
		playbook := playbookHeader + `
  - name: Manage swarm
    community.docker.docker_swarm:
` + arguments + `
    register: swarm_result
` + taskOptions + `

  - name: Persist swarm result
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

	t.Run("basic validation", func(t *testing.T) {
		_, joinOutput := run(t, "join-missing", "      state: join\n", "")
		if !strings.Contains(joinOutput, "FAILED") || !strings.Contains(joinOutput, "state is join but all of the following are missing: remote_addrs, join_token") {
			t.Fatalf("join missing = %s", joinOutput)
		}
		_, removeOutput := run(t, "remove-missing", "      state: remove\n", "")
		if !strings.Contains(removeOutput, "FAILED") || !strings.Contains(removeOutput, "state is remove but all of the following are missing: node_id") {
			t.Fatalf("remove missing = %s", removeOutput)
		}
	})

	t.Run("basic create and leave", func(t *testing.T) {
		presentArgs := "      state: present\n      advertise_addr: 127.0.0.1\n"
		check := success(t, "create-check", presentArgs, "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, check, true, "New Swarm cluster created: ")
		if swarmActive(t, client) {
			t.Fatal("check mode initialized swarm")
		}

		created := success(t, "create", presentArgs, "    diff: true\n")
		assertSwarmChanged(t, created, true, "New Swarm cluster created: ")
		tokens := nestedMap(created, "swarm_facts", "JoinTokens")
		if tokens["Worker"] == nil || tokens["Manager"] == nil {
			t.Fatalf("create facts = %#v", created["swarm_facts"])
		}
		if !swarmActive(t, client) {
			t.Fatal("swarm was not created")
		}

		idempotent := success(t, "create-idempotent", presentArgs, "    diff: true\n")
		assertSwarmChanged(t, idempotent, false, "No modification")
		checkIdempotent := success(t, "create-idempotent-check", presentArgs, "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, checkIdempotent, false, "No modification")

		forced := success(t, "create-force", presentArgs+"      force: true\n", "    diff: true\n")
		assertSwarmChanged(t, forced, true, "New Swarm cluster created: ")
		forceCheck := success(t, "create-force-check", presentArgs+"      force: true\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, forceCheck, true, "New Swarm cluster created: ")

		leaveCheck := success(t, "leave-check", "      state: absent\n      force: true\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, leaveCheck, true, "Node has left the swarm cluster")
		if !swarmActive(t, client) {
			t.Fatal("check mode left the swarm")
		}
		left := success(t, "leave", "      state: absent\n      force: true\n", "    diff: true\n")
		assertSwarmChanged(t, left, true, "Node has left the swarm cluster")
		if swarmActive(t, client) {
			t.Fatal("swarm still active")
		}
		leaveAgain := success(t, "leave-again", "      state: absent\n      force: true\n", "    diff: true\n")
		assertSwarmChanged(t, leaveAgain, false, "This node is not part of a swarm.")
		leaveAgainCheck := success(t, "leave-again-check", "      state: absent\n      force: true\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, leaveAgainCheck, false, "This node is not part of a swarm.")
	})

	t.Run("options", func(t *testing.T) {
		created := success(t, "options-init", "      state: present\n      advertise_addr: 127.0.0.1\n      name: default\n", "    diff: true\n")
		assertSwarmChanged(t, created, true, "New Swarm cluster created: ")

		assertOptionCycle(t, success, "autolock",
			"      state: present\n      autolock_managers: true\n",
			"      state: present\n      autolock_managers: false\n",
		)
		enabled := success(t, "autolock-on", "      state: present\n      autolock_managers: true\n", "    diff: true\n")
		if key, _ := nestedMap(enabled, "swarm_facts")["UnlockKey"].(string); !strings.HasPrefix(key, "SWMKEY-") {
			t.Fatalf("unlock key = %#v", enabled["swarm_facts"])
		}
		sameAutolock := success(t, "autolock-on-again", "      state: present\n      autolock_managers: true\n", "    diff: true\n")
		if sameAutolock["changed"] != false || nestedMap(sameAutolock, "swarm_facts")["UnlockKey"] != nil {
			t.Fatalf("autolock idempotent = %#v", sameAutolock)
		}
		success(t, "autolock-off", "      state: present\n      autolock_managers: false\n", "    diff: true\n")

		assertOptionCycle(t, success, "ca-force-rotate",
			"      state: present\n      ca_force_rotate: 1\n",
			"      state: present\n      ca_force_rotate: 0\n",
		)
		assertOptionCycle(t, success, "dispatcher",
			"      state: present\n      dispatcher_heartbeat_period: 10\n",
			"      state: present\n      dispatcher_heartbeat_period: 23\n",
		)
		assertOptionCycle(t, success, "election",
			"      state: present\n      election_tick: 20\n",
			"      state: present\n      election_tick: 5\n",
		)
		assertOptionCycle(t, success, "heartbeat",
			"      state: present\n      heartbeat_tick: 2\n",
			"      state: present\n      heartbeat_tick: 3\n",
		)
		assertOptionCycle(t, success, "keep-snapshots",
			"      state: present\n      keep_old_snapshots: 1\n",
			"      state: present\n      keep_old_snapshots: 2\n",
		)
		assertOptionCycle(t, success, "slow-followers",
			"      state: present\n      log_entries_for_slow_followers: 42\n",
			"      state: present\n      log_entries_for_slow_followers: 23\n",
		)
		assertOptionCycle(t, success, "snapshot",
			"      state: present\n      snapshot_interval: 12345\n",
			"      state: present\n      snapshot_interval: 54321\n",
		)
		assertOptionCycle(t, success, "history",
			"      state: present\n      task_history_retention_limit: 23\n",
			"      state: present\n      task_history_retention_limit: 7\n",
		)
		assertOptionCycle(t, success, "cert-expiry",
			"      state: present\n      node_cert_expiry: 7896000000000000\n",
			"      state: present\n      node_cert_expiry: 8766000000000000\n",
		)

		labelCheck := success(t, "labels-check", "      state: present\n      labels:\n        a: v1\n        b: v2\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, labelCheck, true, "Swarm cluster updated")
		labeled := success(t, "labels", "      state: present\n      labels:\n        a: v1\n        b: v2\n", "    diff: true\n")
		assertSwarmChanged(t, labeled, true, "Swarm cluster updated")
		labelSame := success(t, "labels-same", "      state: present\n      labels:\n        a: v1\n        b: v2\n", "    diff: true\n")
		assertSwarmChanged(t, labelSame, false, "No modification")
		labelSameCheck := success(t, "labels-same-check", "      state: present\n      labels:\n        a: v1\n        b: v2\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, labelSameCheck, false, "No modification")
		labelChangeCheck := success(t, "labels-change-check", "      state: present\n      labels:\n        a: v1\n        c: v3\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, labelChangeCheck, true, "Swarm cluster updated")
		labelChanged := success(t, "labels-change", "      state: present\n      labels:\n        a: v1\n        c: v3\n", "    diff: true\n")
		assertSwarmChanged(t, labelChanged, true, "Swarm cluster updated")
		omitted := success(t, "labels-omit", "      state: present\n", "    diff: true\n")
		assertSwarmChanged(t, omitted, false, "No modification")
		omittedCheck := success(t, "labels-omit-check", "      state: present\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, omittedCheck, false, "No modification")
		stillThere := success(t, "labels-still-there", "      state: present\n      labels:\n        a: v1\n        c: v3\n", "    diff: true\n")
		assertSwarmChanged(t, stillThere, false, "No modification")
		if nodeHasLabel(t, client, "a") {
			t.Fatal("cluster labels leaked onto the local node")
		}
		emptyCheck := success(t, "labels-empty-check", "      state: present\n      labels: {}\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, emptyCheck, true, "Swarm cluster updated")
		emptied := success(t, "labels-empty", "      state: present\n      labels: {}\n", "    diff: true\n")
		assertSwarmChanged(t, emptied, true, "Swarm cluster updated")
		emptyAgain := success(t, "labels-empty-again", "      state: present\n      labels: {}\n", "    diff: true\n")
		assertSwarmChanged(t, emptyAgain, false, "No modification")

		nameSame := success(t, "name-default", "      state: present\n      name: default\n", "    diff: true\n")
		assertSwarmChanged(t, nameSame, false, "No modification")
		nameCheck := success(t, "name-default-check", "      state: present\n      name: default\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, nameCheck, false, "No modification")
		_, nameChange := run(t, "name-foobar", "      state: present\n      name: foobar\n", "    diff: true\n")
		if !strings.Contains(nameChange, "FAILED") && !strings.Contains(nameChange, "CHANGED") {
			t.Fatalf("changing the hardcoded swarm name should fail or change: %s", nameChange)
		}

		rotateManagerCheck := success(t, "rotate-manager-check", "      state: present\n      rotate_manager_token: true\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, rotateManagerCheck, true, "Swarm cluster updated")
		rotateManager := success(t, "rotate-manager", "      state: present\n      rotate_manager_token: true\n", "    diff: true\n")
		assertSwarmChanged(t, rotateManager, true, "Swarm cluster updated")
		rotateManagerFalse := success(t, "rotate-manager-false", "      state: present\n      rotate_manager_token: false\n", "    diff: true\n")
		assertSwarmChanged(t, rotateManagerFalse, false, "No modification")
		rotateWorkerCheck := success(t, "rotate-worker-check", "      state: present\n      rotate_worker_token: true\n", "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, rotateWorkerCheck, true, "Swarm cluster updated")
		rotateWorker := success(t, "rotate-worker", "      state: present\n      rotate_worker_token: true\n", "    diff: true\n")
		assertSwarmChanged(t, rotateWorker, true, "Swarm cluster updated")
		rotateWorkerFalse := success(t, "rotate-worker-false", "      state: present\n      rotate_worker_token: false\n", "    diff: true\n")
		assertSwarmChanged(t, rotateWorkerFalse, false, "No modification")

		combined := success(t, "combined-spec", "      state: present\n      election_tick: 12\n      snapshot_interval: 11111\n      labels:\n        env: test\n", "    diff: true\n")
		assertSwarmChanged(t, combined, true, "Swarm cluster updated")
		spec := swarmSpec(t, client)
		if raft, _ := spec["Raft"].(map[string]any); intValue(raft["ElectionTick"]) != 12 || intValue(raft["SnapshotInterval"]) != 11111 {
			t.Fatalf("combined spec = %#v", spec)
		}
		if labels, _ := spec["Labels"].(map[string]any); labels["env"] != "test" {
			t.Fatalf("combined labels = %#v", spec["Labels"])
		}
	})

	t.Run("address pools", func(t *testing.T) {
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
		created := success(t, "addr-pool", "      state: present\n      advertise_addr: 127.0.0.1\n      default_addr_pool:\n        - 2.0.0.0/16\n", "    diff: true\n")
		assertSwarmChanged(t, created, true, "New Swarm cluster created: ")
		again := success(t, "addr-pool-again", "      state: present\n      default_addr_pool:\n        - 2.0.0.0/16\n", "    diff: true\n")
		assertSwarmChanged(t, again, false, "No modification")
		if pools := stringList(nestedMap(again, "swarm_facts")["DefaultAddrPool"]); len(pools) != 1 || pools[0] != "2.0.0.0/16" {
			t.Fatalf("addr pool facts = %#v", again["swarm_facts"])
		}

		success(t, "leave-for-subnet", "      state: absent\n      force: true\n", "    diff: true\n")
		subnet := success(t, "subnet", "      state: present\n      force: true\n      advertise_addr: 127.0.0.1\n      subnet_size: 26\n", "    diff: true\n")
		assertSwarmChanged(t, subnet, true, "New Swarm cluster created: ")
		subnetAgain := success(t, "subnet-again", "      state: present\n      subnet_size: 26\n", "    diff: true\n")
		assertSwarmChanged(t, subnetAgain, false, "No modification")
		if intValue(nestedMap(subnetAgain, "swarm_facts")["SubnetSize"]) != 26 {
			t.Fatalf("subnet facts = %#v", subnetAgain["swarm_facts"])
		}

		success(t, "leave-for-both", "      state: absent\n      force: true\n", "    diff: true\n")
		both := success(t, "pool-and-subnet", "      state: present\n      advertise_addr: 127.0.0.1\n      default_addr_pool:\n        - 172.31.0.0/16\n      subnet_size: 28\n", "    diff: true\n")
		assertSwarmChanged(t, both, true, "New Swarm cluster created: ")
		bothAgain := success(t, "pool-and-subnet-again", "      state: present\n", "    diff: true\n")
		if intValue(nestedMap(bothAgain, "swarm_facts")["SubnetSize"]) != 28 {
			t.Fatalf("combined pool facts = %#v", bothAgain["swarm_facts"])
		}
	})

	t.Run("signing ca", func(t *testing.T) {
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
		if strings.TrimSpace(remoteExec(t, client, "command -v openssl >/dev/null && echo yes || echo no")) != "yes" {
			t.Skip("openssl is required to generate swarm signing CAs")
		}
		for _, key := range []string{"key1", "key2"} {
			base := "/tmp/dibra-swarm-ca-" + key
			mustRemote(t, client, "openssl req -x509 -newkey rsa:2048 -nodes -keyout "+base+".key -out "+base+".pem -days 3650 -subj '/CN=dibra-swarm-"+key+"' -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign'")
		}
		cert1 := jsonString(mustRemote(t, client, "cat /tmp/dibra-swarm-ca-key1.pem"))
		key1 := jsonString(mustRemote(t, client, "cat /tmp/dibra-swarm-ca-key1.key"))
		cert2 := jsonString(mustRemote(t, client, "cat /tmp/dibra-swarm-ca-key2.pem"))
		key2 := jsonString(mustRemote(t, client, "cat /tmp/dibra-swarm-ca-key2.key"))

		caArgs := func(cert, key string) string {
			return "      state: present\n      advertise_addr: 127.0.0.1\n      signing_ca_cert: " + cert + "\n      signing_ca_key: " + key + "\n      timeout: 120\n"
		}
		check := success(t, "ca-check", caArgs(cert1, key1), "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, check, true, "New Swarm cluster created: ")
		created := success(t, "ca-create", caArgs(cert1, key1), "    diff: true\n")
		assertSwarmChanged(t, created, true, "New Swarm cluster created: ")
		changeCheck := success(t, "ca-change-check", caArgs(cert2, key2), "    check_mode: true\n    diff: true\n")
		assertSwarmChanged(t, changeCheck, true, "Swarm cluster updated")
		changed := success(t, "ca-change", caArgs(cert2, key2), "    diff: true\n")
		assertSwarmChanged(t, changed, true, "Swarm cluster updated")
	})
}

func assertOptionCycle(t *testing.T, success func(*testing.T, string, string, string) map[string]any, name, first, second string) {
	t.Helper()
	assertSwarmChanged(t, success(t, name+"-check", first, "    check_mode: true\n    diff: true\n"), true, "Swarm cluster updated")
	assertSwarmChanged(t, success(t, name, first, "    diff: true\n"), true, "Swarm cluster updated")
	assertSwarmChanged(t, success(t, name+"-same", first, "    diff: true\n"), false, "No modification")
	assertSwarmChanged(t, success(t, name+"-same-check", first, "    check_mode: true\n    diff: true\n"), false, "No modification")
	assertSwarmChanged(t, success(t, name+"-change-check", second, "    check_mode: true\n    diff: true\n"), true, "Swarm cluster updated")
	assertSwarmChanged(t, success(t, name+"-change", second, "    diff: true\n"), true, "Swarm cluster updated")
}

func assertSwarmChanged(t *testing.T, result map[string]any, changed bool, actionPrefix string) {
	t.Helper()
	if result["changed"] != changed {
		t.Fatalf("changed=%v result=%#v", changed, result)
	}
	actions := stringList(result["actions"])
	if len(actions) == 0 || !strings.HasPrefix(actions[0], actionPrefix) {
		t.Fatalf("actions=%#v want prefix %q", result["actions"], actionPrefix)
	}
	diff, _ := result["diff"].(map[string]any)
	if diff == nil {
		t.Fatalf("diff missing: %#v", result)
	}
	if _, ok := diff["before"]; !ok {
		t.Fatalf("diff.before missing: %#v", diff)
	}
	if _, ok := diff["after"]; !ok {
		t.Fatalf("diff.after missing: %#v", diff)
	}
}

func swarmActive(t *testing.T, client *ssh.Client) bool {
	t.Helper()
	return strings.TrimSpace(remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")) == "active"
}

func swarmSpec(t *testing.T, client *ssh.Client) map[string]any {
	t.Helper()
	raw := mustRemote(t, client, "docker info --format '{{json .Swarm.Cluster.Spec}}'")
	var spec map[string]any
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("decode swarm spec: %v (%s)", err, raw)
	}
	return spec
}

func nodeHasLabel(t *testing.T, client *ssh.Client, key string) bool {
	t.Helper()
	raw := strings.TrimSpace(remoteExec(t, client, "docker info --format '{{index .Swarm.NodeID}}'"))
	if raw == "" {
		return false
	}
	labels := strings.TrimSpace(remoteExec(t, client, "docker node inspect "+raw+" --format '{{json .Spec.Labels}}'"))
	return strings.Contains(labels, `"`+key+`"`)
}

func nestedMap(values map[string]any, keys ...string) map[string]any {
	current := values
	for _, key := range keys {
		next, _ := current[key].(map[string]any)
		current = next
	}
	if current == nil {
		return map[string]any{}
	}
	return current
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, _ := item.(string)
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
