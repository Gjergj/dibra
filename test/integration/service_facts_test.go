//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type ServiceFactsResponse struct {
	Changed  bool                   `json:"changed"`
	Failed   bool                   `json:"failed"`
	Msg      string                 `json:"msg,omitempty"`
	Services map[string]ServiceInfo `json:"services"`
}

type ServiceInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Status string `json:"status,omitempty"`
	Source string `json:"source"`
}

func getServiceFacts(t *testing.T, client interface{ Run(string) (string, string, error) }) ServiceFactsResponse {
	t.Helper()
	stdout, stderr, err := client.Run(`echo '{"module":"service_facts","args":{}}' | /tmp/.goansible-agent`)
	if err != nil {
		t.Fatalf("Agent execution failed: %v, stderr: %s", err, stderr)
	}

	var resp ServiceFactsResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func TestPlaybook_ServiceFactsGather(t *testing.T) {
	playbook := playbookHeader + `
  - name: Gather service facts
    service_facts:
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes (service_facts should never report changed)")
	}
}

func TestPlaybook_ServiceFactsContainsSSH(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	found := false
	for name := range resp.Services {
		if strings.Contains(name, "ssh") {
			found = true
			svc := resp.Services[name]
			if svc.Source != "systemd" {
				t.Errorf("SSH service source should be systemd, got: %s", svc.Source)
			}
			if svc.State != "running" {
				t.Errorf("SSH service should be running (we're connected via SSH), got: %s", svc.State)
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find ssh service in results")
	}
}

func TestPlaybook_ServiceFactsIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp1 := getServiceFacts(t, client)
	resp2 := getServiceFacts(t, client)

	if resp1.Changed || resp2.Changed {
		t.Error("service_facts should never report changed=true")
	}

	if len(resp1.Services) != len(resp2.Services) {
		t.Errorf("Service count changed between runs: %d vs %d", len(resp1.Services), len(resp2.Services))
	}
}

func TestPlaybook_ServiceFactsSystemdSource(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	result := remoteExec(t, client, "test -e /run/systemd/system && echo yes || echo no")
	if result != "yes" {
		t.Skip("Skipping: systemd not available")
	}

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	foundSystemd := false
	for _, svc := range resp.Services {
		if svc.Source == "systemd" {
			foundSystemd = true
			break
		}
	}
	if !foundSystemd {
		t.Error("Expected at least one systemd service on a systemd system")
	}
}

func TestPlaybook_ServiceFactsDetectsRunningService(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl start cron 2>/dev/null || true")

	actualStatus := remoteExec(t, client, "systemctl is-active cron || echo inactive")
	if actualStatus != "active" {
		t.Skip("cron service not available or won't start")
	}

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	cronSvc, found := resp.Services["cron.service"]
	if !found {
		t.Fatal("cron.service not found in service facts")
	}

	if cronSvc.State != "running" {
		t.Errorf("Expected cron state=running, got: %s", cronSvc.State)
	}
	if cronSvc.Source != "systemd" {
		t.Errorf("Expected cron source=systemd, got: %s", cronSvc.Source)
	}
}

func TestPlaybook_ServiceFactsDetectsStoppedService(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl stop cron 2>/dev/null || true")
	defer client.Run("systemctl start cron 2>/dev/null || true")

	actualStatus := remoteExec(t, client, "systemctl is-active cron || echo inactive")
	if actualStatus == "active" {
		t.Skip("cron service did not stop")
	}

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	cronSvc, found := resp.Services["cron.service"]
	if !found {
		t.Fatal("cron.service not found in service facts")
	}

	if cronSvc.State != "stopped" {
		t.Errorf("Expected cron state=stopped, got: %s", cronSvc.State)
	}
}

func TestPlaybook_ServiceFactsDetectsEnabledStatus(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl enable cron 2>/dev/null || true")

	actualStatus := remoteExec(t, client, "systemctl is-enabled cron || echo unknown")
	if actualStatus != "enabled" {
		t.Skipf("cron service not enabled, got: %s", actualStatus)
	}

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	cronSvc, found := resp.Services["cron.service"]
	if !found {
		t.Fatal("cron.service not found in service facts")
	}

	if cronSvc.Status != "enabled" {
		t.Errorf("Expected cron status=enabled, got: %s", cronSvc.Status)
	}
}

func TestPlaybook_ServiceFactsDetectsDisabledStatus(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("systemctl disable cron 2>/dev/null || true")
	defer client.Run("systemctl enable cron 2>/dev/null || true")

	actualStatus := remoteExec(t, client, "systemctl is-enabled cron 2>/dev/null || true")
	if actualStatus != "disabled" {
		t.Skipf("cron service not disabled, got: %s", actualStatus)
	}

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	cronSvc, found := resp.Services["cron.service"]
	if !found {
		t.Fatal("cron.service not found in service facts")
	}

	if cronSvc.Status != "disabled" {
		t.Errorf("Expected cron status=disabled, got: %s", cronSvc.Status)
	}
}

func TestPlaybook_ServiceFactsServiceNameMatchesKey(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	for mapKey, svc := range resp.Services {
		if mapKey != svc.Name {
			t.Errorf("Map key %q doesn't match service name %q", mapKey, svc.Name)
		}
	}
}

func TestPlaybook_ServiceFactsValidStates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	validStates := map[string]bool{
		"running": true,
		"stopped": true,
		"failed":  true,
		"unknown": true,
	}

	for name, svc := range resp.Services {
		if !validStates[svc.State] {
			t.Errorf("Service %s has invalid state: %s", name, svc.State)
		}
	}
}

func TestPlaybook_ServiceFactsValidSources(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	validSources := map[string]bool{
		"systemd": true,
		"sysv":    true,
	}

	for name, svc := range resp.Services {
		if !validSources[svc.Source] {
			t.Errorf("Service %s has invalid source: %s", name, svc.Source)
		}
	}
}

func TestPlaybook_ServiceFactsValidStatuses(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	validStatuses := map[string]bool{
		"enabled":         true,
		"disabled":        true,
		"static":          true,
		"indirect":        true,
		"masked":          true,
		"alias":           true,
		"linked":          true,
		"generated":       true,
		"enabled-runtime": true,
		"linked-runtime":  true,
		"not-found":       true,
		"unknown":         true,
		"active":          true,
		"inactive":        true,
		"failed":          true,
		"":                true,
	}

	for name, svc := range resp.Services {
		if svc.Source == "systemd" && !validStatuses[svc.Status] {
			t.Errorf("Systemd service %s has invalid status: %s", name, svc.Status)
		}
	}
}

func TestPlaybook_ServiceFactsDiscoversManyServices(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resp := getServiceFacts(t, client)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	if len(resp.Services) < 5 {
		t.Errorf("Expected at least 5 services, got: %d", len(resp.Services))
	}
}

func TestPlaybook_ServiceFactsNeverReportsChanged(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	for i := 0; i < 3; i++ {
		resp := getServiceFacts(t, client)
		if resp.Changed {
			t.Errorf("Run %d: service_facts reported changed=true", i+1)
		}
	}
}
