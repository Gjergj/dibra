package docker_swarm

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type fakeClock struct{ sleeps int }

func (clock *fakeClock) Now() time.Time { return time.Unix(0, 0) }
func (clock *fakeClock) Sleep(time.Duration) {
	clock.sleeps++
}

type fakeSwarmClient struct {
	client.APIClient
	active       bool
	manager      bool
	swarm        swarm.Swarm
	nodes        map[string]swarm.Node
	unlockKey    string
	inits        []client.SwarmInitOptions
	updates      []client.SwarmUpdateOptions
	joins        []client.SwarmJoinOptions
	leaves       []client.SwarmLeaveOptions
	removedNodes []string
}

func (fake *fakeSwarmClient) Close() error { return nil }

func (fake *fakeSwarmClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	info := swarm.Info{LocalNodeState: swarm.LocalNodeStateInactive}
	if fake.active {
		info.LocalNodeState = swarm.LocalNodeStateActive
		info.NodeID = "node-1"
		info.ControlAvailable = fake.manager
	}
	return client.SystemInfoResult{Info: system.Info{Swarm: info}}, nil
}

func (fake *fakeSwarmClient) SwarmInspect(context.Context, client.SwarmInspectOptions) (client.SwarmInspectResult, error) {
	if !fake.active || !fake.manager {
		return client.SwarmInspectResult{}, errors.New("This node is not a swarm manager")
	}
	return client.SwarmInspectResult{Swarm: fake.swarm}, nil
}

func (fake *fakeSwarmClient) SwarmInit(_ context.Context, options client.SwarmInitOptions) (client.SwarmInitResult, error) {
	fake.inits = append(fake.inits, options)
	fake.active = true
	fake.manager = true
	fake.swarm = defaultSwarm()
	applyInit(&fake.swarm, options)
	if options.AutoLockManagers || options.Spec.EncryptionConfig.AutoLockManagers {
		fake.unlockKey = "SWMKEY-1-test"
	}
	return client.SwarmInitResult{NodeID: "node-1"}, nil
}

func (fake *fakeSwarmClient) SwarmUpdate(_ context.Context, options client.SwarmUpdateOptions) (client.SwarmUpdateResult, error) {
	fake.updates = append(fake.updates, options)
	fake.swarm.Spec = options.Spec
	fake.swarm.Version.Index++
	if options.RotateWorkerToken {
		fake.swarm.JoinTokens.Worker = "SWMTKN-worker-rotated"
	}
	if options.RotateManagerToken {
		fake.swarm.JoinTokens.Manager = "SWMTKN-manager-rotated"
	}
	if options.Spec.EncryptionConfig.AutoLockManagers {
		fake.unlockKey = "SWMKEY-1-test"
	} else {
		fake.unlockKey = ""
	}
	return client.SwarmUpdateResult{}, nil
}

func (fake *fakeSwarmClient) SwarmJoin(_ context.Context, options client.SwarmJoinOptions) (client.SwarmJoinResult, error) {
	fake.joins = append(fake.joins, options)
	fake.active = true
	fake.manager = false
	return client.SwarmJoinResult{}, nil
}

func (fake *fakeSwarmClient) SwarmLeave(_ context.Context, options client.SwarmLeaveOptions) (client.SwarmLeaveResult, error) {
	fake.leaves = append(fake.leaves, options)
	fake.active = false
	fake.manager = false
	fake.unlockKey = ""
	return client.SwarmLeaveResult{}, nil
}

func (fake *fakeSwarmClient) SwarmGetUnlockKey(context.Context) (client.SwarmGetUnlockKeyResult, error) {
	return client.SwarmGetUnlockKeyResult{Key: fake.unlockKey}, nil
}

func (fake *fakeSwarmClient) NodeInspect(_ context.Context, nodeID string, _ client.NodeInspectOptions) (client.NodeInspectResult, error) {
	node, found := fake.nodes[nodeID]
	if !found {
		return client.NodeInspectResult{}, errors.New("node not found")
	}
	return client.NodeInspectResult{Node: node}, nil
}

func (fake *fakeSwarmClient) NodeRemove(_ context.Context, nodeID string, _ client.NodeRemoveOptions) (client.NodeRemoveResult, error) {
	fake.removedNodes = append(fake.removedNodes, nodeID)
	delete(fake.nodes, nodeID)
	return client.NodeRemoveResult{}, nil
}

func swarmDependencies(fake *fakeSwarmClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		Clock:       &fakeClock{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func defaultSwarm() swarm.Swarm {
	history := int64(5)
	keep := uint64(0)
	prefix := netip.MustParsePrefix("10.0.0.0/8")
	return swarm.Swarm{
		ClusterInfo: swarm.ClusterInfo{
			ID: "swarm-id",
			Meta: swarm.Meta{
				Version: swarm.Version{Index: 1},
			},
			Spec: swarm.Spec{
				Annotations: swarm.Annotations{Name: "default", Labels: map[string]string{}},
				Orchestration: swarm.OrchestrationConfig{
					TaskHistoryRetentionLimit: &history,
				},
				Raft: swarm.RaftConfig{
					SnapshotInterval:           10000,
					KeepOldSnapshots:           &keep,
					LogEntriesForSlowFollowers: 500,
					ElectionTick:               10,
					HeartbeatTick:              1,
				},
				Dispatcher: swarm.DispatcherConfig{
					HeartbeatPeriod: 5 * time.Second,
				},
				CAConfig: swarm.CAConfig{
					NodeCertExpiry: 90 * 24 * time.Hour,
				},
			},
			DefaultAddrPool: []netip.Prefix{prefix},
			SubnetSize:      24,
		},
		JoinTokens: swarm.JoinTokens{
			Worker:  "SWMTKN-worker",
			Manager: "SWMTKN-manager",
		},
	}
}

func applyInit(current *swarm.Swarm, options client.SwarmInitOptions) {
	applySpec(&current.Spec, Request{}, true)
	if options.Spec.Name != "" {
		current.Spec.Name = options.Spec.Name
	}
	if options.Spec.Labels != nil {
		current.Spec.Labels = options.Spec.Labels
	}
	current.Spec.Raft = mergeRaft(current.Spec.Raft, options.Spec.Raft)
	if options.Spec.Dispatcher.HeartbeatPeriod != 0 {
		current.Spec.Dispatcher.HeartbeatPeriod = options.Spec.Dispatcher.HeartbeatPeriod
	}
	if options.Spec.Orchestration.TaskHistoryRetentionLimit != nil {
		current.Spec.Orchestration.TaskHistoryRetentionLimit = options.Spec.Orchestration.TaskHistoryRetentionLimit
	}
	if options.Spec.CAConfig.NodeCertExpiry != 0 {
		current.Spec.CAConfig.NodeCertExpiry = options.Spec.CAConfig.NodeCertExpiry
	}
	current.Spec.CAConfig.ForceRotate = options.Spec.CAConfig.ForceRotate
	current.Spec.CAConfig.SigningCACert = options.Spec.CAConfig.SigningCACert
	current.Spec.CAConfig.SigningCAKey = options.Spec.CAConfig.SigningCAKey
	current.Spec.EncryptionConfig.AutoLockManagers = options.AutoLockManagers || options.Spec.EncryptionConfig.AutoLockManagers
	if len(options.DefaultAddrPool) > 0 {
		current.DefaultAddrPool = options.DefaultAddrPool
	}
	if options.SubnetSize > 0 {
		current.SubnetSize = options.SubnetSize
	}
}

func mergeRaft(current, requested swarm.RaftConfig) swarm.RaftConfig {
	if requested.SnapshotInterval != 0 {
		current.SnapshotInterval = requested.SnapshotInterval
	}
	if requested.KeepOldSnapshots != nil {
		current.KeepOldSnapshots = requested.KeepOldSnapshots
	}
	if requested.LogEntriesForSlowFollowers != 0 {
		current.LogEntriesForSlowFollowers = requested.LogEntriesForSlowFollowers
	}
	if requested.ElectionTick != 0 {
		current.ElectionTick = requested.ElectionTick
	}
	if requested.HeartbeatTick != 0 {
		current.HeartbeatTick = requested.HeartbeatTick
	}
	return current
}

func intPtr(value int) *int    { return &value }
func boolPtr(value bool) *bool { return &value }

func TestJoinAndRemoveRequireFields(t *testing.T) {
	join := Execute(Request{State: "join"})
	if !join.Failed || join.Msg != "state is join but all of the following are missing: remote_addrs, join_token" {
		t.Fatalf("join = %#v", join)
	}
	remove := Execute(Request{State: "remove"})
	if !remove.Failed || remove.Msg != "state is remove but all of the following are missing: node_id" {
		t.Fatalf("remove = %#v", remove)
	}
}

func TestPresentCheckModeDoesNotInit(t *testing.T) {
	fake := &fakeSwarmClient{}
	response := ExecuteWithDependenciesAndState(Request{AdvertiseAddr: "127.0.0.1"}, swarmDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || len(fake.inits) != 0 {
		t.Fatalf("response = %#v inits=%#v", response, fake.inits)
	}
	if len(response.Actions) != 1 || response.Actions[0] != "New Swarm cluster created: " {
		t.Fatalf("actions = %#v", response.Actions)
	}
	if response.Diff == nil || response.Diff.After["state"] != "present" {
		t.Fatalf("diff = %#v", response.Diff)
	}
}

func TestPresentInitThenIdempotent(t *testing.T) {
	fake := &fakeSwarmClient{}
	created := ExecuteWithDependencies(Request{AdvertiseAddr: "127.0.0.1", Name: "default"}, swarmDependencies(fake))
	if created.Failed || !created.Changed || len(fake.inits) != 1 {
		t.Fatalf("created = %#v inits=%#v", created, fake.inits)
	}
	if created.Actions[0] != "New Swarm cluster created: swarm-id" {
		t.Fatalf("actions = %#v", created.Actions)
	}
	tokens, _ := created.SwarmFacts["JoinTokens"].(map[string]any)
	if tokens["Worker"] != "SWMTKN-worker" || tokens["Manager"] != "SWMTKN-manager" {
		t.Fatalf("facts = %#v", created.SwarmFacts)
	}

	again := ExecuteWithDependenciesAndState(Request{AdvertiseAddr: "127.0.0.1", Name: "default"}, swarmDependencies(fake), execution.State{DiffMode: true})
	if again.Failed || again.Changed || again.Actions[0] != "No modification" {
		t.Fatalf("idempotent = %#v", again)
	}
	if again.Diff == nil || again.Diff.Before == nil || again.Diff.After == nil {
		t.Fatalf("diff = %#v", again.Diff)
	}
}

func TestLeaveCheckAndIdempotent(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	check := ExecuteWithDependenciesAndState(Request{State: "absent", Force: true}, swarmDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if check.Failed || !check.Changed || len(fake.leaves) != 0 {
		t.Fatalf("check = %#v", check)
	}
	left := ExecuteWithDependencies(Request{State: "absent", Force: true}, swarmDependencies(fake))
	if left.Failed || !left.Changed || len(fake.leaves) != 1 || left.Actions[0] != "Node has left the swarm cluster" {
		t.Fatalf("leave = %#v leaves=%#v", left, fake.leaves)
	}
	again := ExecuteWithDependencies(Request{State: "absent", Force: true}, swarmDependencies(fake))
	if again.Failed || again.Changed || again.Actions[0] != "This node is not part of a swarm." {
		t.Fatalf("absent again = %#v", again)
	}
}

func TestAutolockManagersCheckDiffAndUnlockKey(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	check := ExecuteWithDependenciesAndState(Request{AutolockManagers: boolPtr(true)}, swarmDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if check.Failed || !check.Changed || len(fake.updates) != 0 || check.Actions[0] != "Swarm cluster updated" {
		t.Fatalf("check = %#v", check)
	}
	enabled := ExecuteWithDependenciesAndState(Request{AutolockManagers: boolPtr(true)}, swarmDependencies(fake), execution.State{DiffMode: true})
	if enabled.Failed || !enabled.Changed || enabled.SwarmFacts["UnlockKey"] != "SWMKEY-1-test" {
		t.Fatalf("enable = %#v", enabled)
	}
	same := ExecuteWithDependencies(Request{AutolockManagers: boolPtr(true)}, swarmDependencies(fake))
	if same.Failed || same.Changed || same.SwarmFacts["UnlockKey"] != nil {
		t.Fatalf("idempotent = %#v", same)
	}
	disabled := ExecuteWithDependencies(Request{AutolockManagers: boolPtr(false)}, swarmDependencies(fake))
	if disabled.Failed || !disabled.Changed || disabled.SwarmFacts["UnlockKey"] != nil {
		t.Fatalf("disable = %#v", disabled)
	}
}

func TestSpecOptionsUpdateAndIdempotent(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	cases := []struct {
		name    string
		request Request
	}{
		{"election_tick", Request{ElectionTick: intPtr(20)}},
		{"heartbeat_tick", Request{HeartbeatTick: intPtr(2)}},
		{"snapshot_interval", Request{SnapshotInterval: intPtr(12345)}},
		{"keep_old_snapshots", Request{KeepOldSnapshots: intPtr(1)}},
		{"log_entries_for_slow_followers", Request{LogEntriesForSlowFollowers: intPtr(42)}},
		{"dispatcher_heartbeat_period", Request{DispatcherHeartbeatPeriod: intPtr(10)}},
		{"task_history_retention_limit", Request{TaskHistoryRetentionLimit: intPtr(23)}},
		{"node_cert_expiry", Request{NodeCertExpiry: intPtr(7896000000000000)}},
		{"ca_force_rotate", Request{CAForceRotate: intPtr(1)}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			check := ExecuteWithDependenciesAndState(test.request, swarmDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
			if check.Failed || !check.Changed || check.Actions[0] != "Swarm cluster updated" {
				t.Fatalf("check = %#v", check)
			}
			updated := ExecuteWithDependenciesAndState(test.request, swarmDependencies(fake), execution.State{DiffMode: true})
			if updated.Failed || !updated.Changed {
				t.Fatalf("update = %#v", updated)
			}
			same := ExecuteWithDependencies(test.request, swarmDependencies(fake))
			if same.Failed || same.Changed || same.Actions[0] != "No modification" {
				t.Fatalf("idempotent = %#v", same)
			}
		})
	}
}

func TestLabelsOmitVersusEmpty(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	set := ExecuteWithDependencies(Request{Labels: map[string]string{"a": "v1", "b": "v2"}}, swarmDependencies(fake))
	if set.Failed || !set.Changed {
		t.Fatalf("set = %#v", set)
	}
	omitted := ExecuteWithDependencies(Request{}, swarmDependencies(fake))
	if omitted.Failed || omitted.Changed {
		t.Fatalf("omit = %#v", omitted)
	}
	if fake.swarm.Spec.Labels["a"] != "v1" || fake.swarm.Spec.Labels["b"] != "v2" {
		t.Fatalf("labels = %#v", fake.swarm.Spec.Labels)
	}
	cleared := ExecuteWithDependencies(Request{Labels: map[string]string{}}, swarmDependencies(fake))
	if cleared.Failed || !cleared.Changed || len(fake.swarm.Spec.Labels) != 0 {
		t.Fatalf("clear = %#v labels=%#v", cleared, fake.swarm.Spec.Labels)
	}
	emptyAgain := ExecuteWithDependencies(Request{Labels: map[string]string{}}, swarmDependencies(fake))
	if emptyAgain.Failed || emptyAgain.Changed {
		t.Fatalf("empty idempotent = %#v", emptyAgain)
	}
}

func TestRotateTokensAlwaysChangeWhenTrue(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	rotated := ExecuteWithDependencies(Request{RotateManagerToken: true, RotateWorkerToken: true}, swarmDependencies(fake))
	if rotated.Failed || !rotated.Changed || len(fake.updates) != 1 {
		t.Fatalf("rotate = %#v", rotated)
	}
	if !fake.updates[0].RotateManagerToken || !fake.updates[0].RotateWorkerToken {
		t.Fatalf("update = %#v", fake.updates[0])
	}
	unchanged := ExecuteWithDependencies(Request{RotateManagerToken: false}, swarmDependencies(fake))
	if unchanged.Failed || unchanged.Changed {
		t.Fatalf("false rotate = %#v", unchanged)
	}
}

func TestDataPathOptionsOnInit(t *testing.T) {
	fake := &fakeSwarmClient{}
	port := 9789
	response := ExecuteWithDependencies(Request{
		AdvertiseAddr: "127.0.0.1",
		DataPathAddr:  "127.0.0.1",
		DataPathPort:  &port,
	}, swarmDependencies(fake))
	if response.Failed || !response.Changed || len(fake.inits) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if fake.inits[0].DataPathAddr != "127.0.0.1" || fake.inits[0].DataPathPort != 9789 {
		t.Fatalf("init = %#v", fake.inits[0])
	}
}

func TestDefaultAddrPoolAndSubnetSizeInit(t *testing.T) {
	fake := &fakeSwarmClient{}
	response := ExecuteWithDependencies(Request{
		DefaultAddrPool: []string{"2.0.0.0/16"},
		SubnetSize:      intPtr(26),
	}, swarmDependencies(fake))
	if response.Failed || !response.Changed || len(fake.inits) != 1 {
		t.Fatalf("response = %#v inits=%#v", response, fake.inits)
	}
	if len(fake.inits[0].DefaultAddrPool) != 1 || fake.inits[0].DefaultAddrPool[0].String() != "2.0.0.0/16" || fake.inits[0].SubnetSize != 26 {
		t.Fatalf("init = %#v", fake.inits[0])
	}
	again := ExecuteWithDependencies(Request{DefaultAddrPool: []string{"2.0.0.0/16"}}, swarmDependencies(fake))
	if again.Failed || again.Changed {
		t.Fatalf("addr pool is init-only: %#v", again)
	}
	facts, _ := again.SwarmFacts["DefaultAddrPool"].([]any)
	if len(facts) != 1 || facts[0] != "2.0.0.0/16" {
		t.Fatalf("facts = %#v", again.SwarmFacts)
	}
}

func TestSigningCAAlwaysUpdatesWhenSpecified(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	first := ExecuteWithDependencies(Request{SigningCACert: "CERT", SigningCAKey: "KEY"}, swarmDependencies(fake))
	if first.Failed || !first.Changed {
		t.Fatalf("first = %#v", first)
	}
	second := ExecuteWithDependencies(Request{SigningCACert: "CERT", SigningCAKey: "KEY"}, swarmDependencies(fake))
	if second.Failed || !second.Changed {
		t.Fatalf("signing CA is never compared to active values: %#v", second)
	}
}

func TestForcePresentReinitializes(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	response := ExecuteWithDependencies(Request{Force: true, AdvertiseAddr: "127.0.0.1"}, swarmDependencies(fake))
	if response.Failed || !response.Changed || len(fake.inits) != 1 || !fake.inits[0].ForceNewCluster {
		t.Fatalf("force = %#v inits=%#v", response, fake.inits)
	}
}

func TestRemoveDownNode(t *testing.T) {
	fake := &fakeSwarmClient{
		active:  true,
		manager: true,
		swarm:   defaultSwarm(),
		nodes: map[string]swarm.Node{
			"dead-node": {ID: "dead-node", Status: swarm.NodeStatus{State: swarm.NodeStateDown}},
			"live-node": {ID: "live-node", Status: swarm.NodeStatus{State: swarm.NodeStateReady}},
		},
	}
	ready := ExecuteWithDependencies(Request{State: "remove", NodeID: "live-node"}, swarmDependencies(fake))
	if !ready.Failed || ready.Msg != "Can not remove the node. The status node is ready and not down." {
		t.Fatalf("ready = %#v", ready)
	}
	removed := ExecuteWithDependencies(Request{State: "remove", NodeID: "dead-node"}, swarmDependencies(fake))
	if removed.Failed || !removed.Changed || len(fake.removedNodes) != 1 {
		t.Fatalf("remove = %#v nodes=%#v", removed, fake.removedNodes)
	}
}

func TestJoinExistingNodeIsUnchanged(t *testing.T) {
	fake := &fakeSwarmClient{active: true, manager: true, swarm: defaultSwarm()}
	response := ExecuteWithDependencies(Request{
		State:       "join",
		JoinToken:   "token",
		RemoteAddrs: []string{"127.0.0.1:2377"},
	}, swarmDependencies(fake))
	if response.Failed || response.Changed || response.Actions[0] != "This node is already part of a swarm." {
		t.Fatalf("join = %#v", response)
	}
}
