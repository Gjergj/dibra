package docker_network

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var (
	cidrIPv4 = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[1-2][0-9]|3[0-2])$`)
	cidrIPv6 = regexp.MustCompile(`^[0-9a-fA-F:]+/([0-9]|[1-9][0-9]|1[0-2][0-9])$`)
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
	req, err := normalizeRequest(req)
	if err != nil {
		return failedResponse(err.Error())
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	raw, inspect, exists, err := inspectNetwork(ctx, cli, req.Name)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}

	manager := &networkManager{
		cli:         cli,
		ctx:         ctx,
		req:         req,
		state:       state,
		clock:       dependencies.Clock,
		existing:    inspect,
		existingRaw: raw,
		exists:      exists,
		tracker:     &diffTracker{},
		debug:       req.Debug != nil && *req.Debug,
	}
	if len(req.Connected) == 0 && exists {
		manager.req.Connected = containerNamesInNetwork(inspect)
	}

	if manager.req.State == "absent" {
		return manager.absent()
	}
	return manager.present()
}

func normalizeRequest(req Request) (Request, error) {
	if req.Name == "" {
		return req, fmt.Errorf("name is required")
	}
	switch req.State {
	case "", "present":
		req.State = "present"
	case "absent":
	default:
		return req, fmt.Errorf("state must be present or absent")
	}
	if req.Driver == "" && (req.providedArguments == nil || !req.argumentProvided("driver")) {
		req.Driver = "bridge"
	}
	if req.ConfigOnly != nil && *req.ConfigOnly {
		req.Driver = "null"
	}
	if req.DriverOptions == nil && req.Options != nil {
		req.DriverOptions = req.Options
	}
	if len(req.DriverOptions) > 0 {
		stringified, err := docker.StringifyAPIMap(req.DriverOptions)
		if err != nil {
			return req, err
		}
		req.DriverOptions = map[string]any{}
		for key, value := range stringified {
			req.DriverOptions[key] = value
		}
	}
	if req.IPAMDriverOptions != nil {
		stringified, err := docker.StringifyAPIMap(req.IPAMDriverOptions)
		if err != nil {
			return req, err
		}
		req.IPAMDriverOptions = map[string]any{}
		for key, value := range stringified {
			req.IPAMDriverOptions[key] = value
		}
	}
	if len(req.IPAMConfig) > 0 {
		for _, config := range req.IPAMConfig {
			if config.Subnet == "" {
				continue
			}
			if err := validateCIDR(config.Subnet); err != nil {
				return req, err
			}
		}
	}
	return req, nil
}

func validateCIDR(cidr string) error {
	if cidrIPv4.MatchString(cidr) || cidrIPv6.MatchString(cidr) {
		return nil
	}
	return fmt.Errorf("%q is not a valid CIDR", cidr)
}

type networkManager struct {
	cli         client.APIClient
	ctx         context.Context
	req         Request
	state       execution.State
	clock       docker.Clock
	existing    network.Inspect
	existingRaw map[string]any
	exists      bool
	changed     bool
	actions     []string
	tracker     *diffTracker
	debug       bool
}

func (manager *networkManager) present() Response {
	different := false
	if manager.exists {
		different = manager.hasDifferentConfig()
	}
	manager.tracker.add("exists", true, manager.exists)
	if manager.req.Force || different {
		if resp := manager.removeNetwork(); resp.Failed {
			return resp
		}
		manager.exists = false
		manager.existing = network.Inspect{}
		manager.existingRaw = nil
	}
	if resp := manager.createNetwork(); resp.Failed {
		return resp
	}
	if resp := manager.connectContainers(); resp.Failed {
		return resp
	}
	if !manager.req.Appends {
		if resp := manager.disconnectMissing(); resp.Failed {
			return resp
		}
	}
	return manager.finish(manager.inspectResult())
}

func (manager *networkManager) absent() Response {
	manager.tracker.add("exists", false, manager.exists)
	if resp := manager.removeNetwork(); resp.Failed {
		return resp
	}
	response := manager.finish(Response{Changed: manager.changed, Network: nil})
	if !manager.exists && !manager.changed {
		response.Network = nil
	}
	return response
}

func (manager *networkManager) createNetwork() Response {
	if manager.exists {
		return Response{}
	}
	opts, err := createOptions(manager.req)
	if err != nil {
		return failedResponse(err.Error())
	}
	if !manager.state.CheckMode {
		created, err := manager.cli.NetworkCreate(manager.ctx, manager.req.Name, opts)
		if err != nil {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
		raw, inspect, found, err := inspectNetwork(manager.ctx, manager.cli, created.ID)
		if err != nil {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
		if !found {
			raw, inspect, found, err = inspectNetwork(manager.ctx, manager.cli, manager.req.Name)
			if err != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
			}
		}
		if found {
			manager.existing = inspect
			manager.existingRaw = raw
			manager.exists = true
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Created network %s with driver %s", manager.req.Name, manager.req.Driver))
	manager.changed = true
	return Response{}
}

func (manager *networkManager) removeNetwork() Response {
	if !manager.exists {
		return Response{}
	}
	if resp := manager.disconnectAllContainers(); resp.Failed {
		return resp
	}
	if !manager.state.CheckMode {
		if _, err := manager.cli.NetworkRemove(manager.ctx, manager.req.Name, client.NetworkRemoveOptions{}); err != nil && !docker.IsNotFoundError(err) {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
		if manager.existing.Scope == "swarm" {
			for {
				_, _, found, err := inspectNetwork(manager.ctx, manager.cli, manager.req.Name)
				if err != nil {
					return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
				}
				if !found {
					break
				}
				manager.clock.Sleep(100 * time.Millisecond)
			}
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Removed network %s", manager.req.Name))
	manager.changed = true
	return Response{}
}

func (manager *networkManager) connectContainers() Response {
	for _, name := range manager.req.Connected {
		if manager.isContainerConnected(name) {
			continue
		}
		exists, err := manager.containerExists(name)
		if err != nil {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
		if !exists {
			continue
		}
		if !manager.state.CheckMode {
			if _, err := manager.cli.NetworkConnect(manager.ctx, manager.req.Name, client.NetworkConnectOptions{Container: name}); err != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
			}
			if raw, inspect, found, inspectErr := inspectNetwork(manager.ctx, manager.cli, manager.req.Name); inspectErr != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", inspectErr))
			} else if found {
				manager.existing = inspect
				manager.existingRaw = raw
				manager.exists = true
			}
		}
		manager.actions = append(manager.actions, fmt.Sprintf("Connected container %s", name))
		manager.changed = true
		manager.tracker.add("connected."+name, true, false)
	}
	return Response{}
}

func (manager *networkManager) disconnectMissing() Response {
	if !manager.exists {
		return Response{}
	}
	wanted := make(map[string]bool, len(manager.req.Connected))
	for _, name := range manager.req.Connected {
		wanted[name] = true
	}
	for id, endpoint := range manager.existing.Containers {
		name := endpoint.Name
		if name == "" {
			name = id
		}
		if wanted[name] || wanted[id] {
			continue
		}
		if resp := manager.disconnectContainer(name); resp.Failed {
			return resp
		}
	}
	return Response{}
}

func (manager *networkManager) disconnectAllContainers() Response {
	if manager.state.CheckMode {
		for id, endpoint := range manager.existing.Containers {
			name := endpoint.Name
			if name == "" {
				name = id
			}
			manager.actions = append(manager.actions, fmt.Sprintf("Disconnected container %s", name))
			manager.changed = true
			manager.tracker.add("connected."+name, false, true)
		}
		return Response{}
	}
	raw, inspect, found, err := inspectNetwork(manager.ctx, manager.cli, manager.req.Name)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if !found {
		return Response{}
	}
	manager.existing = inspect
	manager.existingRaw = raw
	for id, endpoint := range inspect.Containers {
		name := endpoint.Name
		if name == "" {
			name = id
		}
		if resp := manager.disconnectContainer(name); resp.Failed {
			return resp
		}
	}
	return Response{}
}

func (manager *networkManager) disconnectContainer(name string) Response {
	if !manager.state.CheckMode {
		if _, err := manager.cli.NetworkDisconnect(manager.ctx, manager.req.Name, client.NetworkDisconnectOptions{Container: name, Force: true}); err != nil && !docker.IsNotFoundError(err) {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Disconnected container %s", name))
	manager.changed = true
	manager.tracker.add("connected."+name, false, true)
	return Response{}
}

func (manager *networkManager) isContainerConnected(name string) bool {
	if !manager.exists {
		return false
	}
	for id, endpoint := range manager.existing.Containers {
		if endpoint.Name == name || id == name {
			return true
		}
	}
	return false
}

func (manager *networkManager) containerExists(name string) (bool, error) {
	_, err := manager.cli.ContainerInspect(manager.ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (manager *networkManager) inspectResult() Response {
	if manager.state.CheckMode {
		return Response{Changed: manager.changed, Network: manager.existingRaw}
	}
	raw, _, found, err := inspectNetwork(manager.ctx, manager.cli, manager.req.Name)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if !found {
		return Response{Changed: manager.changed, Network: nil}
	}
	return Response{Changed: manager.changed, Network: raw}
}

func (manager *networkManager) finish(response Response) Response {
	if response.Failed {
		return response
	}
	response.Changed = manager.changed
	if manager.state.CheckMode || manager.debug {
		response.Actions = manager.actions
	}
	if manager.state.DiffMode {
		response.Diff = manager.tracker.diff()
	}
	return response
}

func (manager *networkManager) hasDifferentConfig() bool {
	net := manager.existing
	different := false
	if manager.req.ConfigOnly != nil && *manager.req.ConfigOnly != net.ConfigOnly {
		manager.tracker.add("config_only", *manager.req.ConfigOnly, net.ConfigOnly)
		different = true
	}
	if manager.req.ConfigFrom != "" && manager.req.ConfigFrom != net.ConfigFrom.Network {
		manager.tracker.add("config_from", manager.req.ConfigFrom, net.ConfigFrom.Network)
		different = true
	}
	if manager.req.Driver != "" && manager.req.Driver != net.Driver {
		manager.tracker.add("driver", manager.req.Driver, net.Driver)
		different = true
	}
	if len(manager.req.DriverOptions) > 0 {
		if !driverOptionsMatch(stringMap(manager.req.DriverOptions), net.Options) {
			if len(net.Options) == 0 {
				manager.tracker.add("driver_options", manager.req.DriverOptions, net.Options)
			} else {
				for key, value := range stringMap(manager.req.DriverOptions) {
					if net.Options[key] != value {
						manager.tracker.add("driver_options."+key, value, net.Options[key])
					}
				}
			}
			different = true
		}
	}
	if manager.req.IPAMDriver != "" && manager.req.IPAMDriver != net.IPAM.Driver {
		manager.tracker.add("ipam_driver", manager.req.IPAMDriver, net.IPAM.Driver)
		different = true
	}
	if manager.req.IPAMDriverOptions != nil {
		existingOptions := net.IPAM.Options
		if existingOptions == nil {
			existingOptions = map[string]string{}
		}
		requested := stringMap(manager.req.IPAMDriverOptions)
		if !reflect.DeepEqual(existingOptions, requested) {
			manager.tracker.add("ipam_driver_options", requested, existingOptions)
			different = true
		}
	}
	if len(manager.req.IPAMConfig) > 0 {
		if compareIPAMConfig(manager.req.IPAMConfig, net.IPAM, manager.tracker) {
			different = true
		}
	}
	if manager.req.EnableIPv4 != nil && *manager.req.EnableIPv4 != net.EnableIPv4 {
		manager.tracker.add("enable_ipv4", *manager.req.EnableIPv4, net.EnableIPv4)
		different = true
	}
	if manager.req.EnableIPv6 != nil && *manager.req.EnableIPv6 != net.EnableIPv6 {
		manager.tracker.add("enable_ipv6", *manager.req.EnableIPv6, net.EnableIPv6)
		different = true
	}
	if manager.req.Internal != nil && *manager.req.Internal != net.Internal {
		manager.tracker.add("internal", *manager.req.Internal, net.Internal)
		different = true
	}
	if manager.req.Scope != "" && manager.req.Scope != net.Scope {
		manager.tracker.add("scope", manager.req.Scope, net.Scope)
		different = true
	}
	if manager.req.Attachable != nil && *manager.req.Attachable != net.Attachable {
		manager.tracker.add("attachable", *manager.req.Attachable, net.Attachable)
		different = true
	}
	if manager.req.Ingress != nil && *manager.req.Ingress != net.Ingress {
		manager.tracker.add("ingress", *manager.req.Ingress, net.Ingress)
		different = true
	}
	if len(manager.req.Labels) > 0 {
		if !labelsMatch(manager.req.Labels, net.Labels) {
			if len(net.Labels) == 0 {
				manager.tracker.add("labels", manager.req.Labels, net.Labels)
			} else {
				for key, value := range manager.req.Labels {
					if net.Labels[key] != value {
						manager.tracker.add("labels."+key, value, net.Labels[key])
					}
				}
			}
			different = true
		}
	}
	return different
}

func createOptions(req Request) (client.NetworkCreateOptions, error) {
	opts := client.NetworkCreateOptions{
		Driver:  req.Driver,
		Options: stringMap(req.DriverOptions),
		Labels:  req.Labels,
	}
	if req.ConfigOnly != nil {
		opts.ConfigOnly = *req.ConfigOnly
	}
	if req.ConfigFrom != "" {
		opts.ConfigFrom = req.ConfigFrom
	}
	opts.EnableIPv4 = req.EnableIPv4
	opts.EnableIPv6 = req.EnableIPv6
	if req.Internal != nil && *req.Internal {
		opts.Internal = true
	}
	if req.Scope != "" {
		opts.Scope = req.Scope
	}
	if req.Attachable != nil {
		opts.Attachable = *req.Attachable
	}
	if req.Ingress != nil {
		opts.Ingress = *req.Ingress
	}

	ipamPools := make([]network.IPAMConfig, 0, len(req.IPAMConfig))
	for _, cfg := range req.IPAMConfig {
		subnet, err := parsePrefix(cfg.Subnet, "subnet")
		if err != nil {
			return client.NetworkCreateOptions{}, err
		}
		gateway, err := parseAddress(cfg.Gateway, "gateway")
		if err != nil {
			return client.NetworkCreateOptions{}, err
		}
		ipRange, err := parsePrefix(cfg.IPRange, "iprange")
		if err != nil {
			return client.NetworkCreateOptions{}, err
		}
		auxAddresses := make(map[string]netip.Addr, len(cfg.AuxAddresses))
		for name, value := range cfg.AuxAddresses {
			address, err := parseAddress(value, fmt.Sprintf("aux_addresses.%s", name))
			if err != nil {
				return client.NetworkCreateOptions{}, err
			}
			auxAddresses[name] = address
		}
		ipamPools = append(ipamPools, network.IPAMConfig{
			Subnet:     subnet,
			Gateway:    gateway,
			IPRange:    ipRange,
			AuxAddress: auxAddresses,
		})
	}
	if req.IPAMDriver != "" || req.IPAMDriverOptions != nil || len(ipamPools) > 0 {
		opts.IPAM = &network.IPAM{
			Driver:  req.IPAMDriver,
			Config:  ipamPools,
			Options: stringMap(req.IPAMDriverOptions),
		}
	}
	return opts, nil
}

func compareIPAMConfig(requested []IPAMConfig, existing network.IPAM, tracker *diffTracker) bool {
	if len(existing.Config) == 0 {
		tracker.add("ipam_config", requested, existing.Config)
		return true
	}
	existingPools := make([]normalizedIPAM, 0, len(existing.Config))
	for _, cfg := range existing.Config {
		existingPools = append(existingPools, normalizeExistingIPAM(cfg))
	}
	different := false
	for idx, cfg := range requested {
		requestedPool := normalizeRequestedIPAM(cfg)
		matched := normalizedIPAM{}
		found := false
		for _, existingPool := range existingPools {
			if ipamSubset(requestedPool, existingPool) {
				matched = existingPool
				found = true
				break
			}
		}
		_ = found
		if cfg.Subnet != "" && requestedPool.Subnet != matched.Subnet {
			tracker.add(fmt.Sprintf("ipam_config[%d].subnet", idx), cfg.Subnet, matched.Subnet)
			different = true
		}
		if cfg.Gateway != "" && requestedPool.Gateway != matched.Gateway {
			tracker.add(fmt.Sprintf("ipam_config[%d].gateway", idx), cfg.Gateway, matched.Gateway)
			different = true
		}
		if cfg.IPRange != "" && requestedPool.IPRange != matched.IPRange {
			tracker.add(fmt.Sprintf("ipam_config[%d].iprange", idx), cfg.IPRange, matched.IPRange)
			different = true
		}
		if cfg.AuxAddresses != nil && !reflect.DeepEqual(requestedPool.AuxAddresses, matched.AuxAddresses) {
			tracker.add(fmt.Sprintf("ipam_config[%d].aux_addresses", idx), cfg.AuxAddresses, matched.AuxAddresses)
			different = true
		}
	}
	return different
}

type normalizedIPAM struct {
	Subnet       string
	IPRange      string
	Gateway      string
	AuxAddresses map[string]string
}

func normalizeRequestedIPAM(cfg IPAMConfig) normalizedIPAM {
	aux := map[string]string{}
	for key, value := range cfg.AuxAddresses {
		aux[key] = docker.NormalizeIPAddress(value)
	}
	if cfg.AuxAddresses == nil {
		aux = nil
	}
	return normalizedIPAM{
		Subnet:       docker.NormalizeIPNetwork(cfg.Subnet),
		IPRange:      docker.NormalizeIPNetwork(cfg.IPRange),
		Gateway:      docker.NormalizeIPAddress(cfg.Gateway),
		AuxAddresses: aux,
	}
}

func normalizeExistingIPAM(cfg network.IPAMConfig) normalizedIPAM {
	aux := make(map[string]string, len(cfg.AuxAddress))
	for key, value := range cfg.AuxAddress {
		aux[key] = docker.NormalizeIPAddress(addrString(value))
	}
	return normalizedIPAM{
		Subnet:       docker.NormalizeIPNetwork(prefixString(cfg.Subnet)),
		IPRange:      docker.NormalizeIPNetwork(prefixString(cfg.IPRange)),
		Gateway:      docker.NormalizeIPAddress(addrString(cfg.Gateway)),
		AuxAddresses: aux,
	}
}

func ipamSubset(requested, existing normalizedIPAM) bool {
	if requested.Subnet != "" && requested.Subnet != existing.Subnet {
		return false
	}
	if requested.IPRange != "" && requested.IPRange != existing.IPRange {
		return false
	}
	if requested.Gateway != "" && requested.Gateway != existing.Gateway {
		return false
	}
	if requested.AuxAddresses != nil && !reflect.DeepEqual(requested.AuxAddresses, existing.AuxAddresses) {
		return false
	}
	return true
}

func driverOptionsMatch(requested, existing map[string]string) bool {
	if len(requested) == 0 {
		return true
	}
	if len(existing) == 0 {
		return false
	}
	for key, value := range requested {
		if existing[key] != value {
			return false
		}
	}
	return true
}

func labelsMatch(requested, existing map[string]string) bool {
	if len(requested) == 0 {
		return true
	}
	if len(existing) == 0 {
		return false
	}
	for key, value := range requested {
		if existing[key] != value {
			return false
		}
	}
	return true
}

func stringMap(values map[string]any) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		if text, ok := value.(string); ok {
			result[key] = text
			continue
		}
		result[key] = fmt.Sprint(value)
	}
	return result
}

func containerNamesInNetwork(inspect network.Inspect) ContainerNames {
	if len(inspect.Containers) == 0 {
		return nil
	}
	names := make(ContainerNames, 0, len(inspect.Containers))
	for _, endpoint := range inspect.Containers {
		if endpoint.Name != "" {
			names = append(names, endpoint.Name)
		}
	}
	return names
}

func inspectNetwork(ctx context.Context, cli client.APIClient, name string) (map[string]any, network.Inspect, bool, error) {
	result, err := cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{Verbose: true})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return nil, network.Inspect{}, false, nil
		}
		return nil, network.Inspect{}, false, err
	}
	raw, err := inspectionFromResult(result)
	if err != nil {
		return nil, network.Inspect{}, false, err
	}
	return raw, result.Network, true, nil
}

func inspectionFromResult(result client.NetworkInspectResult) (map[string]any, error) {
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Network)
}

func parsePrefix(value, field string) (netip.Prefix, error) {
	if value == "" {
		return netip.Prefix{}, nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return prefix, nil
}

func parseAddress(value, field string) (netip.Addr, error) {
	if value == "" {
		return netip.Addr{}, nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return address, nil
}

func prefixString(prefix netip.Prefix) string {
	if !prefix.IsValid() {
		return ""
	}
	return prefix.String()
}

func addrString(address netip.Addr) string {
	if !address.IsValid() {
		return ""
	}
	return address.String()
}

type diffTracker struct {
	before map[string]any
	after  map[string]any
}

func (tracker *diffTracker) add(name string, parameter, active any) {
	if tracker.before == nil {
		tracker.before = map[string]any{}
		tracker.after = map[string]any{}
	}
	tracker.before[name] = active
	tracker.after[name] = parameter
}

func (tracker *diffTracker) diff() *Diff {
	if tracker.before == nil {
		return &Diff{Before: map[string]any{}, After: map[string]any{}}
	}
	return &Diff{Before: tracker.before, After: tracker.after}
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
