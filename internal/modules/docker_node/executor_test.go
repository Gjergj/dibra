package docker_node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type nodeClient struct {
	client.APIClient
	manager   bool
	info      system.Info
	nodes     map[string]swarm.Node
	updates   []client.NodeUpdateOptions
	updateErr error
}

func (fake *nodeClient) Close() error { return nil }

func (fake *nodeClient) SwarmInspect(context.Context, client.SwarmInspectOptions) (client.SwarmInspectResult, error) {
	if !fake.manager {
		return client.SwarmInspectResult{}, errors.New("This node is not a swarm manager")
	}
	return client.SwarmInspectResult{Swarm: swarm.Swarm{}}, nil
}

func (fake *nodeClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: fake.info}, nil
}

func (fake *nodeClient) NodeInspect(_ context.Context, id string, _ client.NodeInspectOptions) (client.NodeInspectResult, error) {
	node, err := fake.lookup(id)
	if err != nil {
		return client.NodeInspectResult{}, err
	}
	raw, _ := json.Marshal(node)
	return client.NodeInspectResult{Node: node, Raw: raw}, nil
}

func (fake *nodeClient) NodeUpdate(_ context.Context, id string, options client.NodeUpdateOptions) (client.NodeUpdateResult, error) {
	fake.updates = append(fake.updates, options)
	if fake.updateErr != nil {
		return client.NodeUpdateResult{}, fake.updateErr
	}
	node, err := fake.lookup(id)
	if err != nil {
		return client.NodeUpdateResult{}, err
	}
	if options.Spec.Role == swarm.NodeRoleWorker && node.Spec.Role == swarm.NodeRoleManager && fake.managerCount() <= 1 {
		return client.NodeUpdateResult{}, errors.New("rpc error: code = FailedPrecondition desc = attempting to demote the last manager of the swarm")
	}
	node.Spec = options.Spec
	node.Version.Index++
	fake.nodes[node.ID] = node
	return client.NodeUpdateResult{}, nil
}

func (fake *nodeClient) lookup(id string) (swarm.Node, error) {
	if node, found := fake.nodes[id]; found {
		return node, nil
	}
	for _, node := range fake.nodes {
		if node.ID == id || node.Description.Hostname == id {
			return node, nil
		}
	}
	return swarm.Node{}, errdefs.ErrNotFound
}

func (fake *nodeClient) managerCount() int {
	count := 0
	for _, node := range fake.nodes {
		if node.Spec.Role == swarm.NodeRoleManager {
			count++
		}
	}
	return count
}

func nodeDependencies(fake *nodeClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func managerNode() swarm.Node {
	return swarm.Node{
		ID: "node-1",
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 3},
		},
		Description: swarm.NodeDescription{Hostname: "testhost"},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
			Annotations:  swarm.Annotations{Labels: map[string]string{}},
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady, Addr: "127.0.0.1"},
		ManagerStatus: &swarm.ManagerStatus{
			Leader:       true,
			Reachability: swarm.ReachabilityReachable,
			Addr:         "127.0.0.1:2377",
		},
	}
}

func managerClient(node swarm.Node) *nodeClient {
	if node.Spec.Labels == nil {
		node.Spec.Labels = map[string]string{}
	}
	return &nodeClient{
		manager: true,
		info: system.Info{Swarm: swarm.Info{
			NodeID:           node.ID,
			LocalNodeState:   swarm.LocalNodeStateActive,
			ControlAvailable: true,
		}},
		nodes: map[string]swarm.Node{node.ID: node},
	}
}

func specLabels(response Response) map[string]any {
	spec, _ := response.Node["Spec"].(map[string]any)
	labels, _ := spec["Labels"].(map[string]any)
	if labels == nil {
		return map[string]any{}
	}
	return labels
}

func TestNodeFailsWhenNotSwarmManager(t *testing.T) {
	fake := &nodeClient{manager: false, nodes: map[string]swarm.Node{}}
	response := ExecuteWithDependencies(Request{Hostname: "testhost"}, nodeDependencies(fake))
	if !response.Failed || response.Msg != docker.NotSwarmManagerMsg {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeRequiresHostnameUnlessSelf(t *testing.T) {
	response := ExecuteWithDependencies(Request{}, nodeDependencies(managerClient(managerNode())))
	if !response.Failed || !strings.Contains(response.Msg, "hostname is required") {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeRejectsInvalidChoices(t *testing.T) {
	for _, req := range []Request{
		{Hostname: "testhost", Availability: "offline"},
		{Hostname: "testhost", Role: "leader"},
		{Hostname: "testhost", LabelsState: "append"},
	} {
		response := ExecuteWithDependencies(req, nodeDependencies(managerClient(managerNode())))
		if !response.Failed {
			t.Fatalf("expected validation failure for %#v: %#v", req, response)
		}
	}
}

func TestNodeMissingHostnameFails(t *testing.T) {
	response := ExecuteWithDependencies(Request{Hostname: "missing"}, nodeDependencies(managerClient(managerNode())))
	if !response.Failed || !strings.Contains(response.Msg, "Error while reading from Swarm manager:") {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeManagerRoleIsUnchanged(t *testing.T) {
	fake := managerClient(managerNode())
	response := ExecuteWithDependencies(Request{Hostname: "node-1", Role: "manager"}, nodeDependencies(fake))
	if response.Failed || response.Changed || len(fake.updates) != 0 {
		t.Fatalf("response = %#v updates=%d", response, len(fake.updates))
	}
	if spec, _ := response.Node["Spec"].(map[string]any); spec["Role"] != "manager" {
		t.Fatalf("node = %#v", response.Node)
	}
}

func TestNodeLastManagerDemoteFailsOnRealRunAndCheckPredictsChange(t *testing.T) {
	fake := managerClient(managerNode())
	check := ExecuteWithDependenciesAndState(Request{Hostname: "testhost", Role: "worker"}, nodeDependencies(fake), execution.State{CheckMode: true})
	if check.Failed || !check.Changed || len(fake.updates) != 0 {
		t.Fatalf("check = %#v updates=%d", check, len(fake.updates))
	}

	real := ExecuteWithDependencies(Request{Hostname: "testhost", Role: "worker"}, nodeDependencies(fake))
	if !real.Failed || !strings.Contains(real.Msg, "attempting to demote the last manager of the swarm") {
		t.Fatalf("real = %#v", real)
	}
}

func TestNodeAvailabilityCheckModeDoesNotUpdate(t *testing.T) {
	fake := managerClient(managerNode())
	response := ExecuteWithDependenciesAndState(Request{Hostname: "node-1", Availability: "pause"}, nodeDependencies(fake), execution.State{CheckMode: true})
	if response.Failed || !response.Changed || len(fake.updates) != 0 {
		t.Fatalf("response = %#v updates=%d", response, len(fake.updates))
	}
}

func TestNodeAvailabilityUpdatesAndIsIdempotent(t *testing.T) {
	fake := managerClient(managerNode())
	changed := ExecuteWithDependencies(Request{Hostname: "node-1", Availability: "drain"}, nodeDependencies(fake))
	if changed.Failed || !changed.Changed || len(fake.updates) != 1 {
		t.Fatalf("changed = %#v updates=%d", changed, len(fake.updates))
	}
	if spec, _ := changed.Node["Spec"].(map[string]any); spec["Availability"] != "drain" {
		t.Fatalf("node = %#v", changed.Node)
	}

	again := ExecuteWithDependencies(Request{Hostname: "node-1", Availability: "drain"}, nodeDependencies(fake))
	if again.Failed || again.Changed || len(fake.updates) != 1 {
		t.Fatalf("idempotent = %#v updates=%d", again, len(fake.updates))
	}
}

func TestNodeSelfSelectsLocalNode(t *testing.T) {
	fake := managerClient(managerNode())
	response := ExecuteWithDependencies(Request{Self: true, Availability: "pause"}, nodeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
}

func TestNodeMergeLabelsKeepsUnspecifiedKeys(t *testing.T) {
	node := managerNode()
	node.Spec.Labels = map[string]string{"label1": "value1"}
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{
		Hostname: "node-1",
		Labels:   LabelMap{"label2": "value2"},
	}, nodeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	labels := specLabels(response)
	if labels["label1"] != "value1" || labels["label2"] != "value2" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestNodeLabelsToRemoveIgnoresMissingAndKeepsOverlap(t *testing.T) {
	node := managerNode()
	node.Spec.Labels = map[string]string{"keep": "yes", "drop": "gone", "overlap": "old"}
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{
		Hostname:       "node-1",
		Labels:         LabelMap{"overlap": "new"},
		LabelsToRemove: []string{"drop", "missing", "overlap"},
	}, nodeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	labels := specLabels(response)
	if _, found := labels["drop"]; found {
		t.Fatalf("drop still present: %#v", labels)
	}
	if labels["overlap"] != "new" || labels["keep"] != "yes" {
		t.Fatalf("labels = %#v", labels)
	}

	again := ExecuteWithDependencies(Request{
		Hostname:       "node-1",
		LabelsToRemove: []string{"missing"},
	}, nodeDependencies(fake))
	if again.Failed || again.Changed {
		t.Fatalf("missing remove = %#v", again)
	}
}

func TestNodeReplaceWithOmittedLabelsClearsAll(t *testing.T) {
	node := managerNode()
	node.Spec.Labels = map[string]string{"label1": "value1"}
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{Hostname: "node-1", LabelsState: "replace"}, nodeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(specLabels(response)) != 0 {
		t.Fatalf("labels = %#v", specLabels(response))
	}

	again := ExecuteWithDependencies(Request{Hostname: "node-1", LabelsState: "replace"}, nodeDependencies(fake))
	if again.Failed || again.Changed {
		t.Fatalf("idempotent replace = %#v", again)
	}
}

func TestNodeReplaceWithEmptyLabelsClearsAll(t *testing.T) {
	node := managerNode()
	node.Spec.Labels = map[string]string{"label1": "value1"}
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{
		Hostname:    "node-1",
		Labels:      LabelMap{},
		LabelsState: "replace",
	}, nodeDependencies(fake))
	if response.Failed || !response.Changed || len(specLabels(response)) != 0 {
		t.Fatalf("response = %#v labels=%#v", response, specLabels(response))
	}
}

func TestNodeReplaceIgnoresLabelsToRemove(t *testing.T) {
	node := managerNode()
	node.Spec.Labels = map[string]string{"old": "1"}
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{
		Hostname:       "node-1",
		Labels:         LabelMap{"new": "2"},
		LabelsState:    "replace",
		LabelsToRemove: []string{"new"},
	}, nodeDependencies(fake))
	if response.Failed || specLabels(response)["new"] != "2" {
		t.Fatalf("response = %#v labels=%#v", response, specLabels(response))
	}
}

func TestLabelMapSanitizesIntegersAndRejectsBools(t *testing.T) {
	var labels LabelMap
	if err := json.Unmarshal([]byte(`{"count":1,"name":"web"}`), &labels); err != nil {
		t.Fatal(err)
	}
	if labels["count"] != "1" || labels["name"] != "web" {
		t.Fatalf("labels = %#v", labels)
	}
	if err := json.Unmarshal([]byte(`{"ok":true}`), &labels); err == nil {
		t.Fatal("expected bool label to fail")
	}
	if err := json.Unmarshal([]byte(`{"ratio":1.5}`), &labels); err == nil {
		t.Fatal("expected float label to fail")
	}
}

func TestNodeDownLocalNodeFails(t *testing.T) {
	node := managerNode()
	node.Status.State = swarm.NodeStateDown
	fake := managerClient(node)
	response := ExecuteWithDependencies(Request{Hostname: "node-1", Availability: "drain"}, nodeDependencies(fake))
	if !response.Failed || response.Msg != "Can not update the node. The node is down." {
		t.Fatalf("response = %#v", response)
	}
}
