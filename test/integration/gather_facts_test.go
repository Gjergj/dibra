//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type GatherFactsResponse struct {
	Changed      bool                   `json:"changed"`
	Failed       bool                   `json:"failed"`
	Msg          string                 `json:"msg,omitempty"`
	AnsibleFacts map[string]interface{} `json:"ansible_facts"`
}

func getGatherFactsResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args map[string]interface{}) GatherFactsResponse {
	t.Helper()
	ensureGatherFactsAgent(t)

	argsJSON := "{}"
	if args != nil {
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("Failed to marshal args: %v", err)
		}
		argsJSON = string(data)
	}

	cmd := fmt.Sprintf(`echo '{"module":"gather_facts","args":%s}' | /tmp/.dibra-agent`, argsJSON)
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Agent execution failed: %v, stderr: %s", err, stderr)
	}

	var resp GatherFactsResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

var gatherFactsAgentOnce sync.Once

func ensureGatherFactsAgent(t *testing.T) {
	t.Helper()
	gatherFactsAgentOnce.Do(func() {
		output := runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected agent upload to succeed, got: %s", output)
		}
	})
}

func getFactMap(t *testing.T, facts map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	raw, ok := facts[key]
	if !ok {
		t.Fatalf("Expected fact %q", key)
	}
	nested, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected fact %q to be a map", key)
	}
	return nested
}

func TestGatherFactsBasic(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getGatherFactsResult(t, client, nil)
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("gather_facts should never report changed=true")
	}

	facts := resp.AnsibleFacts
	if facts["user_id"] == "" {
		t.Error("Expected user_id fact")
	}
	if facts["hostname"] == "" {
		t.Error("Expected hostname fact")
	}
	if facts["system"] == "" {
		t.Error("Expected system fact")
	}
	if facts["distribution"] == "" {
		t.Error("Expected distribution fact")
	}

	env := getFactMap(t, facts, "env")
	if env["PATH"] == "" {
		t.Error("Expected PATH in env facts")
	}

	dateTime := getFactMap(t, facts, "date_time")
	if dateTime["year"] == nil {
		t.Error("Expected date_time.year")
	}
}

func TestGatherFactsSubsetNetworkOnly(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getGatherFactsResult(t, client, map[string]interface{}{
		"gather_subset": []string{"!all", "network"},
	})
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	facts := resp.AnsibleFacts
	if facts["interfaces"] == nil {
		t.Fatal("Expected interfaces fact for network subset")
	}
	if facts["default_ipv4"] == nil {
		t.Fatal("Expected default_ipv4 fact for network subset")
	}
	if _, ok := facts["user_id"]; ok {
		t.Error("Did not expect user_id in network-only subset")
	}
	if _, ok := facts["env"]; ok {
		t.Error("Did not expect env in network-only subset")
	}
}

func TestGatherFactsSubsetNegation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getGatherFactsResult(t, client, map[string]interface{}{
		"gather_subset": []string{"!hardware"},
	})
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	facts := resp.AnsibleFacts
	if _, ok := facts["user_id"]; !ok {
		t.Error("Expected user_id in !hardware subset")
	}
	if _, ok := facts["env"]; !ok {
		t.Error("Expected env in !hardware subset")
	}
	if _, ok := facts["mounts"]; ok {
		t.Error("Did not expect mounts in !hardware subset")
	}
	if _, ok := facts["memory_mb"]; ok {
		t.Error("Did not expect memory_mb in !hardware subset")
	}
}

func TestGatherFactsFilterDateTime(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getGatherFactsResult(t, client, map[string]interface{}{
		"filter": "ansible_date_time",
	})
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	facts := resp.AnsibleFacts
	if _, ok := facts["date_time"]; !ok {
		t.Fatal("Expected date_time fact with filter")
	}
	if _, ok := facts["env"]; ok {
		t.Error("Did not expect env with date_time filter")
	}
}

func TestGatherFactsBadSubset(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getGatherFactsResult(t, client, map[string]interface{}{
		"gather_subset": "nonsense",
	})
	if !resp.Failed {
		t.Fatal("Expected gather_facts to fail on bad subset")
	}
	if !strings.Contains(resp.Msg, "bad subset") {
		t.Fatalf("Expected bad subset error, got: %s", resp.Msg)
	}
}

func TestGatherFactsLocalFacts(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "mkdir -p /etc/ansible/facts.d")
	remoteExec(t, client, "echo '{ \"fact_dir\": \"default\" }' > /etc/ansible/facts.d/testfact.fact")
	t.Cleanup(func() {
		_, _, _ = client.Run("rm -f /etc/ansible/facts.d/testfact.fact")
	})

	resp := getGatherFactsResult(t, client, map[string]interface{}{
		"gather_subset": []string{"!all", "local"},
	})
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	facts := resp.AnsibleFacts
	locals := getFactMap(t, facts, "local")
	testfact, ok := locals["testfact"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected local.testfact map")
	}
	if testfact["fact_dir"] != "default" {
		t.Errorf("Expected local fact_dir to be default, got %v", testfact["fact_dir"])
	}
}

func TestPlaybookGatherFactsRuntime(t *testing.T) {
	playbook := playbookHeader + `
  - name: Gather facts
    gather_facts:

  - name: Write facts file
    copy:
      content: "user={{ ansible_user_id }} host={{ ansible_hostname }}"
      dest: /tmp/dibra-gather-facts.txt
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	client := getClient(t)
	defer client.Close()
	t.Cleanup(func() {
		remoteExec(t, client, "rm -f /tmp/dibra-gather-facts.txt")
	})

	content := remoteFileContent(t, client, "/tmp/dibra-gather-facts.txt")
	if !strings.Contains(content, "user=") || !strings.Contains(content, "host=") {
		t.Fatalf("Unexpected facts content: %s", content)
	}
}

func TestPlaybookGatherFactsHostvars(t *testing.T) {
	playbook := playbookHeader + `
  - name: Gather facts
    gather_facts:

  - name: Write hostvars file
    copy:
      content: "user={{ hostvars[inventory_hostname].ansible_user_id }}"
      dest: /tmp/dibra-gather-hostvars.txt
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	client := getClient(t)
	defer client.Close()
	t.Cleanup(func() {
		remoteExec(t, client, "rm -f /tmp/dibra-gather-hostvars.txt")
	})

	content := remoteFileContent(t, client, "/tmp/dibra-gather-hostvars.txt")
	if !strings.Contains(content, "user=") {
		t.Fatalf("Unexpected hostvars content: %s", content)
	}
}
