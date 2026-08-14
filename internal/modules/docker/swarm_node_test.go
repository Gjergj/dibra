package docker

import (
	"encoding/json"
	"testing"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func TestApplySwarmLeaderAddressWorkaroundUsesIPv4ManagerAddr(t *testing.T) {
	node := map[string]any{
		"Status":        map[string]any{"Addr": "10.0.0.5"},
		"ManagerStatus": map[string]any{"Leader": true, "Addr": "192.168.1.10:2377"},
	}
	ApplySwarmLeaderAddressWorkaround(node)
	status := node["Status"].(map[string]any)
	if status["Addr"] != "192.168.1.10" {
		t.Fatalf("Status.Addr = %#v", status["Addr"])
	}
}

func TestApplySwarmLeaderAddressWorkaroundKeepsStatusAddrForIPv6(t *testing.T) {
	node := map[string]any{
		"Status":        map[string]any{"Addr": "2001:db8::1"},
		"ManagerStatus": map[string]any{"Leader": true, "Addr": "[2001:db8::1]:2377"},
	}
	ApplySwarmLeaderAddressWorkaround(node)
	status := node["Status"].(map[string]any)
	if status["Addr"] != "2001:db8::1" {
		t.Fatalf("Status.Addr = %#v", status["Addr"])
	}
}

func TestNodeInspectionFallsBackToTypedNode(t *testing.T) {
	inspection, err := NodeInspection(client.NodeInspectResult{
		Node: swarm.Node{ID: "node-1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection["ID"] != "node-1" {
		t.Fatalf("inspection = %#v", inspection)
	}
}

func TestNodeInspectionPrefersRawJSON(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"ID": "raw-id", "Spec": map[string]any{"Role": "manager"}})
	inspection, err := NodeInspection(client.NodeInspectResult{
		Node: swarm.Node{ID: "typed-id"},
		Raw:  raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection["ID"] != "raw-id" {
		t.Fatalf("inspection = %#v", inspection)
	}
}
