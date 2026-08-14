package docker_node_info

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type infoClient struct {
	client.APIClient
	manager bool
	info    system.Info
	nodes   map[string]swarm.Node
}

func (fake *infoClient) Close() error { return nil }

func (fake *infoClient) SwarmInspect(context.Context, client.SwarmInspectOptions) (client.SwarmInspectResult, error) {
	if !fake.manager {
		return client.SwarmInspectResult{}, errors.New("This node is not a swarm manager")
	}
	return client.SwarmInspectResult{Swarm: swarm.Swarm{}}, nil
}

func (fake *infoClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: fake.info}, nil
}

func (fake *infoClient) NodeList(context.Context, client.NodeListOptions) (client.NodeListResult, error) {
	items := make([]swarm.Node, 0, len(fake.nodes))
	for _, node := range fake.nodes {
		items = append(items, node)
	}
	return client.NodeListResult{Items: items}, nil
}

func (fake *infoClient) NodeInspect(_ context.Context, id string, _ client.NodeInspectOptions) (client.NodeInspectResult, error) {
	if node, found := fake.nodes[id]; found {
		raw, _ := json.Marshal(node)
		return client.NodeInspectResult{Node: node, Raw: raw}, nil
	}
	for _, node := range fake.nodes {
		if node.ID == id || node.Description.Hostname == id {
			raw, _ := json.Marshal(node)
			return client.NodeInspectResult{Node: node, Raw: raw}, nil
		}
	}
	return client.NodeInspectResult{}, errdefs.ErrNotFound
}

func infoDependencies(fake *infoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func sampleNode(id, hostname string) swarm.Node {
	return swarm.Node{
		ID:          id,
		Description: swarm.NodeDescription{Hostname: hostname},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady, Addr: "127.0.0.1"},
		ManagerStatus: &swarm.ManagerStatus{
			Leader: true,
			Addr:   "127.0.0.1:2377",
		},
	}
}

func managerInfoClient() *infoClient {
	node := sampleNode("node-1", "testhost")
	return &infoClient{
		manager: true,
		info: system.Info{Swarm: swarm.Info{
			NodeID:           node.ID,
			LocalNodeState:   swarm.LocalNodeStateActive,
			ControlAvailable: true,
		}},
		nodes: map[string]swarm.Node{node.ID: node},
	}
}

func TestNodeInfoFailsWhenNotSwarmManager(t *testing.T) {
	fake := &infoClient{manager: false, nodes: map[string]swarm.Node{}}
	response := ExecuteWithDependencies(Request{}, infoDependencies(fake))
	if !response.Failed || response.Msg != docker.NotSwarmManagerMsg {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeInfoListsAllWhenNameOmitted(t *testing.T) {
	fake := managerInfoClient()
	fake.nodes["node-2"] = sampleNode("node-2", "worker")
	response := ExecuteWithDependencies(Request{}, infoDependencies(fake))
	if response.Failed || len(response.Nodes) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Nodes[0]["ID"] == nil {
		t.Fatalf("nodes = %#v", response.Nodes)
	}
}

func TestNodeInfoSelfReturnsLocalNode(t *testing.T) {
	response := ExecuteWithDependencies(Request{Self: true, Name: StringList{"ignored"}}, infoDependencies(managerInfoClient()))
	if response.Failed || len(response.Nodes) != 1 || response.Nodes[0]["ID"] != "node-1" {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeInfoNameScalarAndMissing(t *testing.T) {
	fake := managerInfoClient()
	found := ExecuteWithDependencies(Request{Name: StringList{"testhost"}}, infoDependencies(fake))
	if found.Failed || len(found.Nodes) != 1 || found.Nodes[0]["ID"] != "node-1" {
		t.Fatalf("found = %#v", found)
	}

	missing := ExecuteWithDependencies(Request{Name: StringList{"node-missing"}}, infoDependencies(fake))
	if missing.Failed || missing.Nodes == nil || len(missing.Nodes) != 0 {
		t.Fatalf("missing = %#v", missing)
	}

	mixed := ExecuteWithDependencies(Request{Name: StringList{"testhost", "node-missing"}}, infoDependencies(fake))
	if mixed.Failed || len(mixed.Nodes) != 1 {
		t.Fatalf("mixed = %#v", mixed)
	}
}

func TestNodeInfoEmptyNameListReturnsEmpty(t *testing.T) {
	response := ExecuteWithDependencies(Request{Name: StringList{}}, infoDependencies(managerInfoClient()))
	if response.Failed || response.Nodes == nil || len(response.Nodes) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeInfoNameListJSONAcceptsScalar(t *testing.T) {
	var names StringList
	if err := json.Unmarshal([]byte(`"testhost"`), &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "testhost" {
		t.Fatalf("names = %#v", names)
	}
}
