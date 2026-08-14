package docker_swarm

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const (
	defaultListenAddr     = "0.0.0.0:2377"
	defaultAddrPoolCIDR   = "10.0.0.0/8"
	defaultSubnetSize     = 24
	nodeDownRetryCount    = 5
	nodeDownRetryInterval = 5 * time.Second
)

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	stateName, err := normalizeState(req.State)
	if err != nil {
		return failedResponse(err.Error())
	}
	req.State = stateName
	if req.ListenAddr == "" {
		req.ListenAddr = defaultListenAddr
	}

	if message := missingRequired(req); message != "" {
		return failedResponse(message)
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	manager := &swarmManager{
		req:       req,
		checkMode: state.CheckMode,
		diffMode:  state.DiffMode,
		cli:       apiClient,
		ctx:       ctx,
		clock:     dependencies.Clock,
		before:    map[string]any{},
		after:     map[string]any{},
		forceNew:  req.Force || req.ForceNewCluster,
	}
	return manager.run()
}

type swarmManager struct {
	req       Request
	checkMode bool
	diffMode  bool
	cli       client.APIClient
	ctx       context.Context
	clock     docker.Clock
	before    map[string]any
	after     map[string]any
	actions   []string
	changed   bool
	created   bool
	facts     map[string]any
	forceNew  bool
}

func (manager *swarmManager) run() Response {
	info, err := manager.info()
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to get docker info: %v", err))
	}

	var runErr error
	switch manager.req.State {
	case "present":
		runErr = manager.present(info)
	case "join":
		runErr = manager.join(info)
	case "absent":
		runErr = manager.leave(info)
	case "remove":
		runErr = manager.remove(info)
	}
	if runErr != nil {
		return failedResponse(runErr.Error())
	}

	response := Response{
		Changed:    manager.changed,
		Actions:    manager.actions,
		SwarmFacts: manager.facts,
	}
	if manager.diffMode {
		response.Diff = &Diff{Before: manager.before, After: manager.after}
	}
	return response
}

func (manager *swarmManager) present(info swarm.Info) error {
	isManager, err := manager.isManager()
	if err != nil {
		return err
	}
	if isManager && !manager.forceNew {
		return manager.update()
	}

	options, err := manager.initOptions()
	if err != nil {
		return err
	}
	manager.created = true
	manager.changed = true
	manager.addDiff("state", "absent", "present")
	if manager.checkMode {
		manager.actions = []string{"New Swarm cluster created: "}
		manager.facts = createFacts(nil, nil)
		return nil
	}
	if _, err := manager.cli.SwarmInit(manager.ctx, options); err != nil {
		return fmt.Errorf("Can not create a new Swarm Cluster: %v", err)
	}
	if err := manager.refreshFacts(); err != nil {
		return err
	}
	if !manager.isManagerNow() {
		return fmt.Errorf("Swarm not created or other error!")
	}
	id, _ := manager.facts["ID"].(string)
	manager.actions = []string{fmt.Sprintf("New Swarm cluster created: %s", id)}
	manager.facts = createFacts(manager.facts, manager.facts["UnlockKey"])
	return nil
}

func (manager *swarmManager) update() error {
	current, err := manager.inspect()
	if err != nil {
		return fmt.Errorf("Can not update a Swarm Cluster: %v", err)
	}
	if err := manager.loadFacts(current, false); err != nil {
		return err
	}
	differences := compareSpec(manager.req, current)
	if len(differences) == 0 {
		manager.actions = []string{"No modification"}
		return nil
	}
	for field, values := range differences {
		manager.addDiff(field, values[0], values[1])
	}
	manager.changed = true
	if !manager.checkMode {
		spec := overlaySpec(current.Spec, manager.req)
		if _, err := manager.cli.SwarmUpdate(manager.ctx, client.SwarmUpdateOptions{
			Version:            current.Version,
			Spec:               spec,
			RotateWorkerToken:  manager.req.RotateWorkerToken,
			RotateManagerToken: manager.req.RotateManagerToken,
		}); err != nil {
			return fmt.Errorf("Can not update a Swarm Cluster: %v", err)
		}
	}
	if err := manager.refreshFacts(); err != nil && !manager.checkMode {
		return err
	}
	manager.actions = []string{"Swarm cluster updated"}
	return nil
}

func (manager *swarmManager) join(info swarm.Info) error {
	if isSwarmNode(info) {
		manager.actions = []string{"This node is already part of a swarm."}
		return nil
	}
	manager.changed = true
	manager.addDiff("joined", false, true)
	if manager.checkMode {
		manager.actions = []string{"New node is added to swarm cluster"}
		return nil
	}
	if _, err := manager.cli.SwarmJoin(manager.ctx, client.SwarmJoinOptions{
		ListenAddr:    manager.req.ListenAddr,
		AdvertiseAddr: manager.req.AdvertiseAddr,
		DataPathAddr:  manager.req.DataPathAddr,
		RemoteAddrs:   manager.req.RemoteAddrs,
		JoinToken:     manager.req.JoinToken,
	}); err != nil {
		return fmt.Errorf("Can not join the Swarm Cluster: %v", err)
	}
	manager.actions = []string{"New node is added to swarm cluster"}
	return nil
}

func (manager *swarmManager) leave(info swarm.Info) error {
	if !isSwarmNode(info) {
		manager.actions = []string{"This node is not part of a swarm."}
		return nil
	}
	manager.changed = true
	manager.addDiff("joined", "present", "absent")
	if manager.checkMode {
		manager.actions = []string{"Node has left the swarm cluster"}
		return nil
	}
	if _, err := manager.cli.SwarmLeave(manager.ctx, client.SwarmLeaveOptions{Force: manager.req.Force || manager.forceNew}); err != nil {
		return fmt.Errorf("This node can not leave the Swarm Cluster: %v", err)
	}
	manager.actions = []string{"Node has left the swarm cluster"}
	return nil
}

func (manager *swarmManager) remove(info swarm.Info) error {
	if !info.ControlAvailable {
		return fmt.Errorf("This node is not a manager.")
	}
	down, err := manager.nodeIsDown(manager.req.NodeID)
	if err != nil {
		return nil
	}
	if !down {
		return fmt.Errorf("Can not remove the node. The status node is ready and not down.")
	}
	manager.changed = true
	manager.addDiff("joined", true, false)
	if manager.checkMode {
		manager.actions = []string{"Node is removed from swarm cluster."}
		return nil
	}
	if _, err := manager.cli.NodeRemove(manager.ctx, manager.req.NodeID, client.NodeRemoveOptions{Force: manager.req.Force}); err != nil {
		return fmt.Errorf("Can not remove the node from the Swarm Cluster: %v", err)
	}
	manager.actions = []string{"Node is removed from swarm cluster."}
	return nil
}

func (manager *swarmManager) nodeIsDown(nodeID string) (bool, error) {
	for attempt := 0; attempt < nodeDownRetryCount; attempt++ {
		if attempt > 0 {
			manager.clock.Sleep(nodeDownRetryInterval)
		}
		result, err := manager.cli.NodeInspect(manager.ctx, nodeID, client.NodeInspectOptions{})
		if err != nil {
			return false, err
		}
		if result.Node.Status.State == swarm.NodeStateDown {
			return true, nil
		}
	}
	return false, nil
}

func (manager *swarmManager) info() (swarm.Info, error) {
	result, err := manager.cli.Info(manager.ctx, client.InfoOptions{})
	if err != nil {
		return swarm.Info{}, err
	}
	return result.Info.Swarm, nil
}

func (manager *swarmManager) isManager() (bool, error) {
	_, err := manager.inspect()
	if err != nil {
		if isSwarmInspectUnavailable(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (manager *swarmManager) isManagerNow() bool {
	_, err := manager.inspect()
	return err == nil
}

func (manager *swarmManager) inspect() (swarm.Swarm, error) {
	result, err := manager.cli.SwarmInspect(manager.ctx, client.SwarmInspectOptions{})
	if err != nil {
		return swarm.Swarm{}, err
	}
	return result.Swarm, nil
}

func (manager *swarmManager) refreshFacts() error {
	current, err := manager.inspect()
	if err != nil {
		return fmt.Errorf("failed to inspect swarm: %v", err)
	}
	return manager.loadFacts(current, manager.shouldReturnUnlockKey())
}

func (manager *swarmManager) loadFacts(current swarm.Swarm, includeUnlockKey bool) error {
	facts, err := docker.InspectionMap(current)
	if err != nil {
		return err
	}
	if includeUnlockKey {
		if key, err := manager.unlockKey(); err == nil {
			facts["UnlockKey"] = key
		} else {
			facts["UnlockKey"] = nil
		}
	} else {
		facts["UnlockKey"] = nil
	}
	manager.facts = facts
	return nil
}

func (manager *swarmManager) unlockKey() (string, error) {
	result, err := manager.cli.SwarmGetUnlockKey(manager.ctx)
	if err != nil {
		return "", err
	}
	return result.Key, nil
}

func (manager *swarmManager) shouldReturnUnlockKey() bool {
	return boolValue(manager.req.AutolockManagers) && (manager.created || manager.hasAutolockChange())
}

func (manager *swarmManager) hasAutolockChange() bool {
	_, found := manager.before["autolock_managers"]
	return found
}

func (manager *swarmManager) initOptions() (client.SwarmInitOptions, error) {
	options := client.SwarmInitOptions{
		ListenAddr:       manager.req.ListenAddr,
		AdvertiseAddr:    manager.req.AdvertiseAddr,
		DataPathAddr:     manager.req.DataPathAddr,
		ForceNewCluster:  manager.forceNew,
		Spec:             initSpec(manager.req),
		AutoLockManagers: boolValue(manager.req.AutolockManagers),
	}
	if manager.req.DataPathPort != nil {
		if *manager.req.DataPathPort < 0 {
			return options, fmt.Errorf("data_path_port must be a non-negative port number")
		}
		options.DataPathPort = uint32(*manager.req.DataPathPort)
	}
	pools, subnet, err := addressPoolOptions(manager.req)
	if err != nil {
		return options, err
	}
	options.DefaultAddrPool = pools
	options.SubnetSize = subnet
	return options, nil
}

func (manager *swarmManager) addDiff(field string, active, parameter any) {
	manager.before[field] = active
	manager.after[field] = parameter
}

func normalizeState(state string) (string, error) {
	if state == "" {
		return "present", nil
	}
	switch state {
	case "present", "join", "absent", "remove":
		return state, nil
	default:
		return "", fmt.Errorf("state must be present, join, absent, or remove, got %q", state)
	}
}

func missingRequired(req Request) string {
	var missing []string
	switch req.State {
	case "join":
		if len(req.RemoteAddrs) == 0 {
			missing = append(missing, "remote_addrs")
		}
		if req.JoinToken == "" {
			missing = append(missing, "join_token")
		}
	case "remove":
		if req.NodeID == "" {
			missing = append(missing, "node_id")
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("state is %s but all of the following are missing: %s", req.State, strings.Join(missing, ", "))
}

func isSwarmNode(info swarm.Info) bool {
	if info.NodeID != "" {
		return true
	}
	switch info.LocalNodeState {
	case swarm.LocalNodeStateActive, swarm.LocalNodeStatePending, swarm.LocalNodeStateLocked:
		return true
	default:
		return false
	}
}

func isSwarmInspectUnavailable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not a swarm manager") ||
		strings.Contains(text, "this node is not a swarm manager") ||
		strings.Contains(text, "503") ||
		strings.Contains(text, "406")
}

func createFacts(inspect map[string]any, unlockKey any) map[string]any {
	facts := map[string]any{
		"JoinTokens": nil,
		"UnlockKey":  unlockKey,
	}
	if inspect != nil {
		if tokens, ok := inspect["JoinTokens"]; ok {
			facts["JoinTokens"] = tokens
		}
	}
	return facts
}

func addressPoolOptions(req Request) ([]netip.Prefix, uint32, error) {
	pools := req.DefaultAddrPool
	var subnet uint32
	if req.SubnetSize != nil {
		if *req.SubnetSize <= 0 || *req.SubnetSize > 32 {
			return nil, 0, fmt.Errorf("subnet_size must be between 1 and 32")
		}
		subnet = uint32(*req.SubnetSize)
		if len(pools) == 0 {
			pools = []string{defaultAddrPoolCIDR}
		}
	} else if len(pools) > 0 {
		subnet = defaultSubnetSize
	}
	if len(pools) == 0 {
		return nil, 0, nil
	}
	prefixes := make([]netip.Prefix, 0, len(pools))
	for _, pool := range pools {
		prefix, err := netip.ParsePrefix(pool)
		if err != nil {
			return nil, 0, fmt.Errorf("%q is not a valid CIDR", pool)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, subnet, nil
}

func initSpec(req Request) swarm.Spec {
	spec := swarm.Spec{}
	applySpec(&spec, req, false)
	return spec
}

func overlaySpec(current swarm.Spec, req Request) swarm.Spec {
	spec := current
	if current.Labels != nil {
		spec.Labels = docker.NormalizeLabels(current.Labels)
	}
	applySpec(&spec, req, true)
	return spec
}

func applySpec(spec *swarm.Spec, req Request, overlay bool) {
	if req.Name != "" {
		spec.Name = req.Name
	} else if !overlay && spec.Name == "" {
		spec.Name = "default"
	}
	if req.Labels != nil {
		spec.Labels = docker.NormalizeLabels(req.Labels)
		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}
	}
	if req.TaskHistoryRetentionLimit != nil {
		limit := int64(*req.TaskHistoryRetentionLimit)
		spec.Orchestration.TaskHistoryRetentionLimit = &limit
	}
	if req.SnapshotInterval != nil {
		spec.Raft.SnapshotInterval = uint64(*req.SnapshotInterval)
	}
	if req.KeepOldSnapshots != nil {
		keep := uint64(*req.KeepOldSnapshots)
		spec.Raft.KeepOldSnapshots = &keep
	}
	if req.LogEntriesForSlowFollowers != nil {
		spec.Raft.LogEntriesForSlowFollowers = uint64(*req.LogEntriesForSlowFollowers)
	}
	if req.HeartbeatTick != nil {
		spec.Raft.HeartbeatTick = *req.HeartbeatTick
	}
	if req.ElectionTick != nil {
		spec.Raft.ElectionTick = *req.ElectionTick
	}
	if req.DispatcherHeartbeatPeriod != nil {
		spec.Dispatcher.HeartbeatPeriod = time.Duration(*req.DispatcherHeartbeatPeriod)
	}
	if req.NodeCertExpiry != nil {
		spec.CAConfig.NodeCertExpiry = time.Duration(*req.NodeCertExpiry)
	}
	if req.SigningCACert != "" {
		spec.CAConfig.SigningCACert = req.SigningCACert
	}
	if req.SigningCAKey != "" {
		spec.CAConfig.SigningCAKey = req.SigningCAKey
	}
	if req.CAForceRotate != nil {
		spec.CAConfig.ForceRotate = uint64(*req.CAForceRotate)
	}
	if req.AutolockManagers != nil {
		spec.EncryptionConfig.AutoLockManagers = *req.AutolockManagers
	}
}

func compareSpec(req Request, current swarm.Swarm) map[string][2]any {
	differences := map[string][2]any{}
	spec := current.Spec
	if req.Name != "" && req.Name != spec.Name {
		differences["name"] = [2]any{spec.Name, req.Name}
	}
	if req.Labels != nil && !labelsEqual(req.Labels, spec.Labels) {
		differences["labels"] = [2]any{spec.Labels, req.Labels}
	}
	if changed, active := int64Changed(req.SnapshotInterval, int64(spec.Raft.SnapshotInterval)); changed {
		differences["snapshot_interval"] = [2]any{active, *req.SnapshotInterval}
	}
	keep := uint64(0)
	if spec.Raft.KeepOldSnapshots != nil {
		keep = *spec.Raft.KeepOldSnapshots
	}
	if changed, active := int64Changed(req.KeepOldSnapshots, int64(keep)); changed {
		differences["keep_old_snapshots"] = [2]any{active, *req.KeepOldSnapshots}
	}
	if changed, active := int64Changed(req.LogEntriesForSlowFollowers, int64(spec.Raft.LogEntriesForSlowFollowers)); changed {
		differences["log_entries_for_slow_followers"] = [2]any{active, *req.LogEntriesForSlowFollowers}
	}
	if changed, active := intChanged(req.HeartbeatTick, spec.Raft.HeartbeatTick); changed {
		differences["heartbeat_tick"] = [2]any{active, *req.HeartbeatTick}
	}
	if changed, active := intChanged(req.ElectionTick, spec.Raft.ElectionTick); changed {
		differences["election_tick"] = [2]any{active, *req.ElectionTick}
	}
	if changed, active := int64Changed(req.DispatcherHeartbeatPeriod, spec.Dispatcher.HeartbeatPeriod.Nanoseconds()); changed {
		differences["dispatcher_heartbeat_period"] = [2]any{active, *req.DispatcherHeartbeatPeriod}
	}
	var history int64
	if spec.Orchestration.TaskHistoryRetentionLimit != nil {
		history = *spec.Orchestration.TaskHistoryRetentionLimit
	}
	if changed, active := int64Changed(req.TaskHistoryRetentionLimit, history); changed {
		differences["task_history_retention_limit"] = [2]any{active, *req.TaskHistoryRetentionLimit}
	}
	if changed, active := int64Changed(req.NodeCertExpiry, spec.CAConfig.NodeCertExpiry.Nanoseconds()); changed {
		differences["node_cert_expiry"] = [2]any{active, *req.NodeCertExpiry}
	}
	if changed, active := int64Changed(req.CAForceRotate, int64(spec.CAConfig.ForceRotate)); changed {
		differences["ca_force_rotate"] = [2]any{active, *req.CAForceRotate}
	}
	if req.SigningCACert != "" {
		differences["signing_ca_cert"] = [2]any{nil, req.SigningCACert}
	}
	if req.SigningCAKey != "" {
		differences["signing_ca_key"] = [2]any{nil, req.SigningCAKey}
	}
	if req.AutolockManagers != nil && *req.AutolockManagers != spec.EncryptionConfig.AutoLockManagers {
		differences["autolock_managers"] = [2]any{spec.EncryptionConfig.AutoLockManagers, *req.AutolockManagers}
	}
	if req.RotateWorkerToken {
		differences["rotate_worker_token"] = [2]any{false, true}
	}
	if req.RotateManagerToken {
		differences["rotate_manager_token"] = [2]any{false, true}
	}
	return differences
}

func labelsEqual(requested, active map[string]string) bool {
	if len(requested) == 0 && len(active) == 0 {
		return true
	}
	return reflect.DeepEqual(requested, active)
}

func intChanged(requested *int, active int) (bool, int) {
	if requested == nil {
		return false, active
	}
	return *requested != active, active
}

func int64Changed(requested *int, active int64) (bool, int64) {
	if requested == nil {
		return false, active
	}
	return int64(*requested) != active, active
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
