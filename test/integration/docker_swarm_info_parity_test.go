//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerSwarmInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker service rm dibra-swarm-info-web dibra-swarm-info-agent >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-swarm-info-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker service rm dibra-swarm-info-web dibra-swarm-info-agent >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "swarm_info_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-swarm-info-" + name + ".json"
		playbook := playbookHeader + `
  - name: Inspect swarm
    community.docker.docker_swarm_info:
` + arguments + `
    register: swarm_info_result
` + taskOptions + `

  - name: Persist swarm info result
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

	t.Run("not a swarm manager", func(t *testing.T) {
		_, output := run(t, "not-manager", "      nodes: false\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error running docker swarm module: must run on swarm manager node") {
			t.Fatalf("not manager = %s", output)
		}
	})

	t.Run("facts nodes filters unlock key", func(t *testing.T) {
		mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")

		basic := success(t, "basic", "      nodes: false\n", "")
		assertSwarmInfoFlags(t, basic, true)
		facts := nestedMap(basic, "swarm_facts")
		tokens := nestedMap(facts, "JoinTokens")
		if facts["ID"] == nil || tokens["Manager"] == nil || tokens["Worker"] == nil {
			t.Fatalf("swarm_facts = %#v", facts)
		}
		if _, found := basic["swarm_unlock_key"]; found {
			t.Fatalf("unlock key present: %#v", basic)
		}
		if _, found := basic["nodes"]; found {
			t.Fatalf("unrequested nodes present: %#v", basic)
		}
		clusterID := strings.Trim(mustRemote(t, client, "docker info --format '{{.Swarm.Cluster.ID}}'"), " \n\"")
		if facts["ID"] != clusterID {
			t.Fatalf("swarm_facts.ID = %#v want %q", facts["ID"], clusterID)
		}

		check := success(t, "check", "      nodes: false\n", "    check_mode: true\n")
		assertSwarmInfoFlags(t, check, true)

		nodes := success(t, "nodes", "      nodes: true\n", "")
		assertSwarmInfoFlags(t, nodes, true)
		nodeList, _ := nodes["nodes"].([]any)
		if len(nodeList) != 1 {
			t.Fatalf("nodes = %#v", nodes["nodes"])
		}
		node, _ := nodeList[0].(map[string]any)
		for _, key := range []string{"ID", "Hostname", "Status", "Availability", "ManagerStatus", "EngineVersion"} {
			if _, found := node[key]; !found {
				t.Fatalf("missing node key %s in %#v", key, node)
			}
		}
		if _, found := node["CreatedAt"]; found {
			t.Fatalf("non-verbose node leaked CreatedAt: %#v", node)
		}
		if node["ManagerStatus"] != "Leader" {
			t.Fatalf("manager status = %#v", node["ManagerStatus"])
		}
		hostname, _ := node["Hostname"].(string)

		verbose := success(t, "nodes-verbose", "      nodes: true\n      verbose_output: true\n", "")
		verboseNode, _ := verbose["nodes"].([]any)[0].(map[string]any)
		if verboseNode["CreatedAt"] == nil || nestedMap(verboseNode, "Description")["Hostname"] == nil {
			t.Fatalf("verbose node = %#v", verboseNode)
		}

		filtered := success(t, "nodes-filter", "      nodes: true\n      nodes_filters:\n        name: "+hostname+"\n", "")
		if got := filtered["nodes"].([]any); len(got) != 1 {
			t.Fatalf("filtered nodes = %#v", filtered["nodes"])
		}
		roleFiltered := success(t, "nodes-role", "      nodes: true\n      nodes_filters:\n        role: manager\n", "")
		if got := roleFiltered["nodes"].([]any); len(got) != 1 {
			t.Fatalf("role-filtered nodes = %#v", roleFiltered["nodes"])
		}
		missing := success(t, "nodes-missing", "      nodes: true\n      nodes_filters:\n        name: node-missing-xyz\n", "")
		if got := missing["nodes"].([]any); len(got) != 0 {
			t.Fatalf("missing nodes = %#v", missing["nodes"])
		}

		unlocked := success(t, "unlock-absent", "      unlock_key: true\n", "")
		if _, found := unlocked["swarm_unlock_key"]; !found || unlocked["swarm_unlock_key"] != nil {
			t.Fatalf("unlocked key = %#v", unlocked)
		}

		mustRemote(t, client, "docker swarm update --autolock=true")
		locked := success(t, "unlock-present", "      unlock_key: true\n", "")
		key, _ := locked["swarm_unlock_key"].(string)
		if !strings.HasPrefix(key, "SWMKEY-") {
			t.Fatalf("locked key = %#v", locked["swarm_unlock_key"])
		}
		cliKey := strings.TrimSpace(mustRemote(t, client, "docker swarm unlock-key -q"))
		if key != cliKey {
			t.Fatalf("unlock key %q != docker swarm unlock-key %q", key, cliKey)
		}
		mustRemote(t, client, "docker swarm update --autolock=false")
	})

	t.Run("services tasks and extra filters", func(t *testing.T) {
		if !swarmActive(t, client) {
			mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
		}
		mustRemote(t, client, "docker pull alpine:latest")
		empty := success(t, "services-empty", "      services: true\n      tasks: true\n", "")
		if got := empty["services"].([]any); len(got) != 0 {
			t.Fatalf("expected no services: %#v", empty["services"])
		}

		mustRemote(t, client, "docker service create --name dibra-swarm-info-web --replicas 1 --label env=test -p 18080:80 alpine:latest sleep infinity")
		mustRemote(t, client, "docker service create --name dibra-swarm-info-agent --mode global --label env=test alpine:latest sleep infinity")
		waitForServiceTasks(t, client, "dibra-swarm-info-web")
		waitForServiceTasks(t, client, "dibra-swarm-info-agent")

		services := success(t, "services", "      services: true\n", "")
		serviceList, _ := services["services"].([]any)
		if len(serviceList) != 2 {
			t.Fatalf("services = %#v", services["services"])
		}
		byName := map[string]map[string]any{}
		for _, item := range serviceList {
			record, _ := item.(map[string]any)
			name, _ := record["Name"].(string)
			byName[name] = record
			for _, key := range []string{"ID", "Name", "Mode", "Replicas", "Image", "Ports"} {
				if _, found := record[key]; !found {
					t.Fatalf("missing service key %s in %#v", key, record)
				}
			}
		}
		web := byName["dibra-swarm-info-web"]
		if web["Mode"] != "Replicated" || intValue(web["Replicas"]) != 1 {
			t.Fatalf("web = %#v", web)
		}
		ports, _ := web["Ports"].([]any)
		if len(ports) == 0 {
			t.Fatalf("expected published ports: %#v", web)
		}
		agent := byName["dibra-swarm-info-agent"]
		if agent["Mode"] != "Global" || intValue(agent["Replicas"]) != 2 {
			t.Fatalf("global replicas should equal listed service count: %#v", agent)
		}

		named := success(t, "services-name", "      services: true\n      services_filters:\n        name: dibra-swarm-info-web\n", "")
		if got := named["services"].([]any); len(got) != 1 {
			t.Fatalf("named services = %#v", named["services"])
		}
		modeFiltered := success(t, "services-mode", "      services: true\n      services_filters:\n        mode: global\n", "")
		if got := modeFiltered["services"].([]any); len(got) != 1 {
			t.Fatalf("mode-filtered services = %#v", modeFiltered["services"])
		}
		labeled := success(t, "services-label", "      services: true\n      services_filters:\n        label: env=test\n", "")
		if got := labeled["services"].([]any); len(got) != 2 {
			t.Fatalf("label-filtered services = %#v", labeled["services"])
		}
		missingSvc := success(t, "services-missing", "      services: true\n      services_filters:\n        name: missing-service-xyz\n", "")
		if got := missingSvc["services"].([]any); len(got) != 0 {
			t.Fatalf("missing services = %#v", missingSvc["services"])
		}

		verboseSvc := success(t, "services-verbose", "      services: true\n      verbose_output: true\n", "")
		first, _ := verboseSvc["services"].([]any)[0].(map[string]any)
		if nestedMap(first, "Spec")["Name"] == nil {
			t.Fatalf("verbose service = %#v", first)
		}

		tasks := success(t, "tasks", "      tasks: true\n", "")
		taskList, _ := tasks["tasks"].([]any)
		if len(taskList) == 0 {
			t.Fatalf("expected tasks: %#v", tasks)
		}
		task, _ := taskList[0].(map[string]any)
		for _, key := range []string{"ID", "ContainerID", "Image", "Node", "DesiredState", "CurrentState", "Error"} {
			if _, found := task[key]; !found {
				t.Fatalf("missing task key %s in %#v", key, task)
			}
		}
		if _, found := task["Slot"]; found {
			t.Fatalf("non-verbose task leaked Slot: %#v", task)
		}
		nodeName := strings.TrimSpace(mustRemote(t, client, "docker node ls --format '{{.Hostname}}' | head -n1"))
		if task["Node"] != nodeName {
			t.Fatalf("task node %v != %q", task["Node"], nodeName)
		}

		serviceTasks := success(t, "tasks-service", "      tasks: true\n      tasks_filters:\n        service: dibra-swarm-info-web\n", "")
		if got := serviceTasks["tasks"].([]any); len(got) == 0 {
			t.Fatalf("service tasks = %#v", serviceTasks["tasks"])
		}
		running := success(t, "tasks-running", "      tasks: true\n      tasks_filters:\n        desired-state: running\n", "")
		if got := running["tasks"].([]any); len(got) == 0 {
			t.Fatalf("running tasks = %#v", running["tasks"])
		}
		nodeTasks := success(t, "tasks-node", "      tasks: true\n      tasks_filters:\n        node: "+nodeName+"\n", "")
		if got := nodeTasks["tasks"].([]any); len(got) == 0 {
			t.Fatalf("node tasks = %#v", nodeTasks["tasks"])
		}
		missingTasks := success(t, "tasks-missing", "      tasks: true\n      tasks_filters:\n        name: missing-task-xyz\n", "")
		if got := missingTasks["tasks"].([]any); len(got) != 0 {
			t.Fatalf("missing tasks = %#v", missingTasks["tasks"])
		}
		_, missingServiceFilter := run(t, "tasks-missing-service", "      tasks: true\n      tasks_filters:\n        service: missing-service-xyz\n", "")
		if !strings.Contains(missingServiceFilter, "FAILED") || !strings.Contains(missingServiceFilter, "service missing-service-xyz not found") {
			t.Fatalf("missing service task filter = %s", missingServiceFilter)
		}

		verboseTasks := success(t, "tasks-verbose", "      tasks: true\n      verbose_output: true\n", "")
		verboseTask, _ := verboseTasks["tasks"].([]any)[0].(map[string]any)
		if nestedMap(verboseTask, "Status")["State"] == nil {
			t.Fatalf("verbose task = %#v", verboseTask)
		}

		alias := success(t, "verbose-alias", "      nodes: true\n      verbose: true\n", "")
		aliasNode, _ := alias["nodes"].([]any)[0].(map[string]any)
		if aliasNode["CreatedAt"] == nil {
			t.Fatalf("verbose alias node = %#v", aliasNode)
		}
	})
}

func assertSwarmInfoFlags(t *testing.T, result map[string]any, manager bool) {
	t.Helper()
	if result["changed"] != false || result["can_talk_to_docker"] != true || result["docker_swarm_active"] != true || result["docker_swarm_manager"] != manager {
		t.Fatalf("flags = %#v", result)
	}
}

func waitForServiceTasks(t *testing.T, client *ssh.Client, name string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		state := strings.ToLower(mustRemote(t, client, "docker service ps "+name+" --format '{{.CurrentState}}' | head -n1"))
		if strings.Contains(state, "running") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("service %s did not reach running: %s", name, mustRemote(t, client, "docker service ps "+name))
}
