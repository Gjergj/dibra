//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerSwarmServiceInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-dssi-%d", time.Now().UnixNano()%100000000)
	serviceName := prefix + "-1"
	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-dssi-*.json /tmp/.dibra-agent")
	defer func() {
		mustRemote(t, client, "docker service rm "+serviceName+" "+prefix+"-global >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()

	templatePath := writeResultTemplate(t, "service_info")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-dssi-" + name + ".json"
		playbook := playbookHeader + `
  - name: Inspect swarm service
    community.docker.docker_swarm_service_info:
` + arguments + `
    register: service_info
` + taskOptions + `

  - name: Persist service info result
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
		if result["failed"] == true {
			t.Fatalf("%s failed field: %#v", name, result)
		}
		return result
	}

	t.Run("not a swarm manager", func(t *testing.T) {
		_, output := run(t, "not-manager", "      name: "+serviceName+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error running docker swarm module: must run on swarm manager node") {
			t.Fatalf("not manager = %s", output)
		}
	})

	t.Run("missing required name", func(t *testing.T) {
		_, output := run(t, "missing-name", "", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("missing name = %s", output)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		_, output := run(t, "empty-name", "      name: \"\"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("empty name = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")

	createPlaybook := playbookHeader + `
  - name: Create service for info inspection
    community.docker.docker_swarm_service:
      name: ` + serviceName + `
      image: alpine:latest
      command: ["sleep", "infinity"]
      labels:
        dibra: service-info
        env: test
`
	if output := runPlaybook(t, createPlaybook); strings.Contains(output, "FAILED") {
		t.Fatalf("create service failed: %s", output)
	}

	t.Run("existing service by name", func(t *testing.T) {
		result := success(t, "by-name", "      name: "+serviceName+"\n", "")
		if result["exists"] != true {
			t.Fatalf("exists = %#v", result["exists"])
		}
		service := nestedMap(result, "service")
		id, _ := service["ID"].(string)
		if id == "" {
			t.Fatalf("service.ID = %#v", service["ID"])
		}
		spec := nestedMap(service, "Spec")
		if spec["Name"] != serviceName {
			t.Fatalf("Spec.Name = %#v", spec["Name"])
		}
		labels := nestedMap(service, "Spec", "Labels")
		if labels["dibra"] != "service-info" || labels["env"] != "test" {
			t.Fatalf("Spec.Labels = %#v", labels)
		}
		if _, ok := service["Version"]; !ok {
			t.Fatalf("missing Version in %#v", service)
		}
		if _, ok := result["tasks"]; ok {
			t.Fatalf("unexpected tasks field: %#v", result["tasks"])
		}
		if _, ok := result["service_id"]; ok {
			t.Fatalf("unexpected service_id field: %#v", result["service_id"])
		}
	})

	engineInspect := func(t *testing.T, ref string) map[string]any {
		t.Helper()
		raw := strings.TrimSpace(mustRemote(t, client, "docker service inspect "+ref+" --format '{{json .}}'"))
		var engine map[string]any
		if err := json.Unmarshal([]byte(raw), &engine); err != nil {
			t.Fatalf("decode docker service inspect: %v (%s)", err, raw)
		}
		return engine
	}

	serviceID := ""
	t.Run("matches docker service inspect", func(t *testing.T) {
		result := success(t, "inspect-compare", "      name: "+serviceName+"\n", "")
		moduleService := nestedMap(result, "service")
		engine := engineInspect(t, serviceName)
		serviceID, _ = engine["ID"].(string)
		if serviceID == "" {
			t.Fatalf("engine ID missing: %#v", engine)
		}
		if moduleService["ID"] != engine["ID"] {
			t.Fatalf("ID module=%v engine=%v", moduleService["ID"], engine["ID"])
		}
		moduleSpec := nestedMap(moduleService, "Spec")
		engineSpec := nestedMap(engine, "Spec")
		if moduleSpec["Name"] != engineSpec["Name"] {
			t.Fatalf("Spec.Name module=%v engine=%v", moduleSpec["Name"], engineSpec["Name"])
		}
		if !reflect.DeepEqual(nestedMap(moduleService, "Spec", "Labels"), nestedMap(engine, "Spec", "Labels")) {
			t.Fatalf("labels module=%#v engine=%#v", nestedMap(moduleService, "Spec", "Labels"), nestedMap(engine, "Spec", "Labels"))
		}
		if !reflect.DeepEqual(moduleService["ID"], engine["ID"]) || !reflect.DeepEqual(nestedMap(moduleService, "Version"), nestedMap(engine, "Version")) {
			t.Fatalf("version/id mismatch module=%#v engine=%#v", moduleService["Version"], engine["Version"])
		}
	})

	t.Run("existing service by id and short id", func(t *testing.T) {
		if serviceID == "" {
			serviceID, _ = engineInspect(t, serviceName)["ID"].(string)
		}
		if serviceID == "" {
			t.Fatal("missing service ID")
		}
		byID := success(t, "by-id", "      name: "+serviceID+"\n", "")
		if nestedMap(byID, "service")["ID"] != serviceID {
			t.Fatalf("by id = %#v", byID["service"])
		}
		if len(serviceID) < 12 {
			t.Fatalf("service ID too short: %q", serviceID)
		}
		shortID := serviceID[:12]
		byShort := success(t, "by-short-id", "      name: "+shortID+"\n", "")
		if nestedMap(byShort, "service")["ID"] != serviceID {
			t.Fatalf("by short id = %#v", byShort["service"])
		}
	})

	t.Run("missing service", func(t *testing.T) {
		result := success(t, "missing", "      name: random-service-xyz-"+prefix+"\n", "")
		if result["exists"] != false {
			t.Fatalf("exists = %#v", result["exists"])
		}
		if result["service"] != nil {
			t.Fatalf("service = %#v, want null", result["service"])
		}
	})

	t.Run("check mode and idempotency", func(t *testing.T) {
		check := success(t, "check", "      name: "+serviceName+"\n", "    check_mode: true\n")
		if check["exists"] != true {
			t.Fatalf("check = %#v", check)
		}
		if nestedMap(check, "service", "Spec")["Name"] != serviceName {
			t.Fatalf("check Spec.Name = %#v", nestedMap(check, "service", "Spec")["Name"])
		}
		first := success(t, "idem-1", "      name: "+serviceName+"\n", "")
		second := success(t, "idem-2", "      name: "+serviceName+"\n", "")
		if first["exists"] != true || second["exists"] != true {
			t.Fatalf("idempotency exists first=%v second=%v", first["exists"], second["exists"])
		}
		if nestedMap(first, "service")["ID"] != nestedMap(second, "service")["ID"] {
			t.Fatalf("service ID changed between runs: %v vs %v", nestedMap(first, "service")["ID"], nestedMap(second, "service")["ID"])
		}
	})

	t.Run("connection aliases", func(t *testing.T) {
		result := success(t, "connection", "      name: "+serviceName+"\n      docker_url: unix:///var/run/docker.sock\n      docker_api_version: auto\n", "")
		if result["exists"] != true || nestedMap(result, "service", "Spec")["Name"] != serviceName {
			t.Fatalf("connection = %#v", result)
		}
	})

	t.Run("short module name", func(t *testing.T) {
		remotePath := "/tmp/dibra-dssi-short-name.json"
		playbook := playbookHeader + `
  - name: Inspect with short name
    docker_swarm_service_info:
      name: ` + serviceName + `
    register: service_info

  - name: Persist short-name result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("short name failed: %s", output)
		}
		result := readRemoteJSONMap(t, client, remotePath)
		if result["exists"] != true || nestedMap(result, "service", "Spec")["Name"] != serviceName {
			t.Fatalf("short name = %#v", result)
		}
	})

	t.Run("global service inspect", func(t *testing.T) {
		globalName := prefix + "-global"
		create := playbookHeader + `
  - name: Create global service
    community.docker.docker_swarm_service:
      name: ` + globalName + `
      image: alpine:latest
      command: ["sleep", "infinity"]
      mode: global
`
		if output := runPlaybook(t, create); strings.Contains(output, "FAILED") {
			t.Fatalf("create global failed: %s", output)
		}
		result := success(t, "global", "      name: "+globalName+"\n", "")
		if result["exists"] != true {
			t.Fatalf("global exists = %#v", result)
		}
		mode := nestedMap(result, "service", "Spec", "Mode")
		if _, ok := mode["Global"]; !ok {
			t.Fatalf("Spec.Mode = %#v, want Global", mode)
		}
	})
}
