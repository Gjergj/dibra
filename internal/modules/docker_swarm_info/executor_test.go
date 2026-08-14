package docker_swarm_info

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type fakeSwarmInfoClient struct {
	client.APIClient
	info        system.Info
	infoErr     error
	swarm       swarm.Swarm
	inspectErr  error
	nodes       []swarm.Node
	services    []swarm.Service
	tasks       []swarm.Task
	unlockKey   string
	unlockErr   error
	nodeFilters client.Filters
	svcFilters  client.Filters
	taskFilters client.Filters
}

func (fake *fakeSwarmInfoClient) Close() error { return nil }

func (fake *fakeSwarmInfoClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: fake.info}, fake.infoErr
}

func (fake *fakeSwarmInfoClient) SwarmInspect(context.Context, client.SwarmInspectOptions) (client.SwarmInspectResult, error) {
	if fake.inspectErr != nil {
		return client.SwarmInspectResult{}, fake.inspectErr
	}
	return client.SwarmInspectResult{Swarm: fake.swarm}, nil
}

func (fake *fakeSwarmInfoClient) NodeList(_ context.Context, options client.NodeListOptions) (client.NodeListResult, error) {
	fake.nodeFilters = options.Filters
	return client.NodeListResult{Items: fake.nodes}, nil
}

func (fake *fakeSwarmInfoClient) ServiceList(_ context.Context, options client.ServiceListOptions) (client.ServiceListResult, error) {
	fake.svcFilters = options.Filters
	return client.ServiceListResult{Items: fake.services}, nil
}

func (fake *fakeSwarmInfoClient) TaskList(_ context.Context, options client.TaskListOptions) (client.TaskListResult, error) {
	fake.taskFilters = options.Filters
	return client.TaskListResult{Items: fake.tasks}, nil
}

func (fake *fakeSwarmInfoClient) SwarmGetUnlockKey(context.Context) (client.SwarmGetUnlockKeyResult, error) {
	return client.SwarmGetUnlockKeyResult{Key: fake.unlockKey}, fake.unlockErr
}

func swarmInfoDependencies(fake *fakeSwarmInfoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func managerInfo() system.Info {
	return system.Info{Swarm: swarm.Info{
		NodeID:           "node-1",
		LocalNodeState:   swarm.LocalNodeStateActive,
		ControlAvailable: true,
	}}
}

func sampleSwarm() swarm.Swarm {
	return swarm.Swarm{
		ClusterInfo: swarm.ClusterInfo{
			ID: "swarm-id",
			Meta: swarm.Meta{
				Version: swarm.Version{Index: 11},
			},
			Spec: swarm.Spec{Annotations: swarm.Annotations{Name: "default"}},
		},
		JoinTokens: swarm.JoinTokens{Worker: "SWMTKN-worker", Manager: "SWMTKN-manager"},
	}
}

func TestSwarmInfoConnectionFailure(t *testing.T) {
	response := ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return nil, errors.New("dial unix:///bad.sock")
		},
	})
	if !response.Failed || response.CanTalkToDocker || response.DockerSwarmActive || response.DockerSwarmManager {
		t.Fatalf("response = %#v", response)
	}
}

func TestSwarmInfoFailsWhenNotManager(t *testing.T) {
	fake := &fakeSwarmInfoClient{
		info:       system.Info{Swarm: swarm.Info{LocalNodeState: swarm.LocalNodeStateInactive}},
		inspectErr: errors.New("This node is not a swarm manager"),
	}
	response := ExecuteWithDependencies(Request{}, swarmInfoDependencies(fake))
	if !response.Failed || response.Msg != notSwarmManagerMsg {
		t.Fatalf("response = %#v", response)
	}
	if !response.CanTalkToDocker || response.DockerSwarmActive || response.DockerSwarmManager {
		t.Fatalf("flags = %#v", response)
	}
	if response.SwarmFacts != nil || response.SwarmUnlockKey != nil {
		t.Fatalf("unexpected facts = %#v", response)
	}
}

func TestSwarmInfoActiveWorkerIsNotManager(t *testing.T) {
	fake := &fakeSwarmInfoClient{
		info: system.Info{Swarm: swarm.Info{
			NodeID:         "node-2",
			LocalNodeState: swarm.LocalNodeStateActive,
		}},
		inspectErr: errors.New("This node is not a swarm manager"),
	}
	response := ExecuteWithDependencies(Request{UnlockKey: true}, swarmInfoDependencies(fake))
	if !response.Failed || !response.CanTalkToDocker || !response.DockerSwarmActive || response.DockerSwarmManager {
		t.Fatalf("response = %#v", response)
	}
	if _, found := jsonMap(t, response)["swarm_unlock_key"]; found {
		t.Fatalf("unlock key present on failure: %#v", response)
	}
}

func TestSwarmInfoReturnsRawSwarmFacts(t *testing.T) {
	fake := &fakeSwarmInfoClient{info: managerInfo(), swarm: sampleSwarm()}
	response := ExecuteWithDependencies(Request{}, swarmInfoDependencies(fake))
	if response.Failed || response.Changed || !response.CanTalkToDocker || !response.DockerSwarmActive || !response.DockerSwarmManager {
		t.Fatalf("response = %#v", response)
	}
	if response.SwarmFacts["ID"] != "swarm-id" {
		t.Fatalf("swarm_facts = %#v", response.SwarmFacts)
	}
	tokens := nestedMap(response.SwarmFacts, "JoinTokens")
	if tokens["Worker"] != "SWMTKN-worker" || tokens["Manager"] != "SWMTKN-manager" {
		t.Fatalf("join tokens = %#v", tokens)
	}
	if response.Nodes != nil || response.Services != nil || response.Tasks != nil {
		t.Fatalf("unrequested lists present: %#v", response)
	}
	if _, found := jsonMap(t, response)["swarm_unlock_key"]; found {
		t.Fatalf("unlock key present: %#v", response)
	}
}

func TestSwarmInfoProjectsNodesAndVerboseOutput(t *testing.T) {
	leader := true
	fake := &fakeSwarmInfoClient{
		info:  managerInfo(),
		swarm: sampleSwarm(),
		nodes: []swarm.Node{{
			ID: "node-1",
			Meta: swarm.Meta{
				CreatedAt: time.Unix(1, 0).UTC(),
				Version:   swarm.Version{Index: 3},
			},
			Description: swarm.NodeDescription{
				Hostname: "manager-1",
				Engine:   swarm.EngineDescription{EngineVersion: "29.7.2"},
			},
			Spec:          swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
			Status:        swarm.NodeStatus{State: swarm.NodeStateReady},
			ManagerStatus: &swarm.ManagerStatus{Leader: leader, Reachability: swarm.ReachabilityReachable},
		}},
	}
	response := ExecuteWithDependencies(Request{Nodes: true, NodesFilters: docker.FilterMap{"name": {"manager-1"}}}, swarmInfoDependencies(fake))
	if response.Failed || response.Nodes == nil || len(*response.Nodes) != 1 {
		t.Fatalf("response = %#v", response)
	}
	node := (*response.Nodes)[0]
	if node["ID"] != "node-1" || node["Hostname"] != "manager-1" || node["ManagerStatus"] != "Leader" || node["EngineVersion"] != "29.7.2" {
		t.Fatalf("node = %#v", node)
	}
	if _, found := node["CreatedAt"]; found {
		t.Fatalf("non-verbose node leaked CreatedAt: %#v", node)
	}
	if !fake.nodeFilters["name"]["manager-1"] {
		t.Fatalf("node filters = %#v", fake.nodeFilters)
	}

	verbose := ExecuteWithDependencies(Request{Nodes: true, VerboseOutput: true}, swarmInfoDependencies(fake))
	if verbose.Nodes == nil || (*verbose.Nodes)[0]["CreatedAt"] == nil {
		t.Fatalf("verbose nodes = %#v", verbose.Nodes)
	}
}

func TestSwarmInfoEmptyFilteredListsArePresent(t *testing.T) {
	fake := &fakeSwarmInfoClient{info: managerInfo(), swarm: sampleSwarm()}
	response := ExecuteWithDependencies(Request{
		Nodes:           true,
		Services:        true,
		Tasks:           true,
		NodesFilters:    docker.FilterMap{"name": {"missing"}},
		ServicesFilters: docker.FilterMap{"name": {"missing"}},
		TasksFilters:    docker.FilterMap{"service": {"missing"}},
	}, swarmInfoDependencies(fake))
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if response.Nodes == nil || !reflect.DeepEqual(*response.Nodes, []map[string]any{}) {
		t.Fatalf("nodes = %#v", response.Nodes)
	}
	if response.Services == nil || !reflect.DeepEqual(*response.Services, []map[string]any{}) {
		t.Fatalf("services = %#v", response.Services)
	}
	if response.Tasks == nil || !reflect.DeepEqual(*response.Tasks, []map[string]any{}) {
		t.Fatalf("tasks = %#v", response.Tasks)
	}
}

func TestSwarmInfoProjectsServicesAndGlobalReplicaCount(t *testing.T) {
	replicas := uint64(3)
	fake := &fakeSwarmInfoClient{
		info:  managerInfo(),
		swarm: sampleSwarm(),
		services: []swarm.Service{
			{
				ID: "svc-replicated",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "web"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"},
					},
					Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
					EndpointSpec: &swarm.EndpointSpec{Ports: []swarm.PortConfig{{
						TargetPort:    80,
						PublishedPort: 8080,
					}}},
				},
			},
			{
				ID: "svc-global",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "agent"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{Image: "alpine:latest"},
					},
					Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}},
				},
			},
		},
	}
	response := ExecuteWithDependencies(Request{Services: true, ServicesFilters: docker.FilterMap{"label": {"env=test"}}}, swarmInfoDependencies(fake))
	if response.Failed || response.Services == nil || len(*response.Services) != 2 {
		t.Fatalf("response = %#v", response)
	}
	web := (*response.Services)[0]
	if web["Name"] != "web" || web["Mode"] != "Replicated" || intValue(web["Replicas"]) != 3 {
		t.Fatalf("replicated = %#v", web)
	}
	ports, _ := web["Ports"].([]any)
	if len(ports) != 1 {
		t.Fatalf("ports = %#v", web["Ports"])
	}
	agent := (*response.Services)[1]
	if agent["Mode"] != "Global" || intValue(agent["Replicas"]) != 2 {
		t.Fatalf("global replicas should equal listed service count: %#v", agent)
	}
	if !fake.svcFilters["label"]["env=test"] {
		t.Fatalf("service filters = %#v", fake.svcFilters)
	}

	verbose := ExecuteWithDependencies(Request{Services: true, VerboseOutput: true}, swarmInfoDependencies(fake))
	if verbose.Services == nil || nestedMap((*verbose.Services)[0], "Spec")["Name"] != "web" {
		t.Fatalf("verbose services = %#v", verbose.Services)
	}
}

func TestSwarmInfoProjectsTasksWithNodeHostname(t *testing.T) {
	fake := &fakeSwarmInfoClient{
		info:  managerInfo(),
		swarm: sampleSwarm(),
		nodes: []swarm.Node{{
			ID:          "node-1",
			Description: swarm.NodeDescription{Hostname: "manager-1"},
		}},
		tasks: []swarm.Task{{
			ID:     "task-1",
			NodeID: "node-1",
			Spec: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "alpine:latest"},
			},
			DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateRunning,
				Err:   "boom",
				ContainerStatus: &swarm.ContainerStatus{
					ContainerID: "abc123",
				},
			},
		}},
	}
	response := ExecuteWithDependencies(Request{Tasks: true, TasksFilters: docker.FilterMap{"desired-state": {"running"}}}, swarmInfoDependencies(fake))
	if response.Failed || response.Tasks == nil || len(*response.Tasks) != 1 {
		t.Fatalf("response = %#v", response)
	}
	task := (*response.Tasks)[0]
	if task["ID"] != "task-1" || task["ContainerID"] != "abc123" || task["Node"] != "manager-1" || task["Error"] != "boom" {
		t.Fatalf("task = %#v", task)
	}
	if task["DesiredState"] != "running" || task["CurrentState"] != "running" || task["Image"] != "alpine:latest" {
		t.Fatalf("task states = %#v", task)
	}
	if !fake.taskFilters["desired-state"]["running"] {
		t.Fatalf("task filters = %#v", fake.taskFilters)
	}
}

func TestSwarmInfoUnlockKeyNullWhenUnlocked(t *testing.T) {
	fake := &fakeSwarmInfoClient{info: managerInfo(), swarm: sampleSwarm()}
	response := ExecuteWithDependencies(Request{UnlockKey: true}, swarmInfoDependencies(fake))
	encoded := jsonMap(t, response)
	if encoded["swarm_unlock_key"] != nil {
		t.Fatalf("expected null unlock key: %#v", encoded)
	}
	if _, found := encoded["swarm_unlock_key"]; !found {
		t.Fatal("swarm_unlock_key omitted")
	}
}

func TestSwarmInfoUnlockKeyReturnedWhenLocked(t *testing.T) {
	fake := &fakeSwarmInfoClient{info: managerInfo(), swarm: sampleSwarm(), unlockKey: "SWMKEY-1-secret"}
	response := ExecuteWithDependencies(Request{UnlockKey: true}, swarmInfoDependencies(fake))
	if response.Failed || response.SwarmUnlockKey != "SWMKEY-1-secret" {
		t.Fatalf("response = %#v", response)
	}
}

func jsonMap(t *testing.T, response Response) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case uint64:
		return int(typed)
	default:
		return 0
	}
}
