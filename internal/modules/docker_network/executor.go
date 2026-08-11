package docker_network

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	// Find network by name
	existing, exists, err := findNetwork(cli, ctx, req.Name)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("inspect network", req.Name, err).Error()}
	}

	if state == "absent" {
		return handleAbsent(cli, ctx, req.Name, existing, exists)
	}

	if state == "present" {
		return handlePresent(cli, ctx, req, existing, exists)
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}

// findNetwork looks up a network by name
func findNetwork(cli *client.Client, ctx context.Context, name string) (network.Inspect, bool, error) {
	result, err := cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{Verbose: true})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return network.Inspect{}, false, nil
		}
		return network.Inspect{}, false, err
	}
	return result.Network, true, nil
}

// handleAbsent removes a network if it exists
func handleAbsent(cli *client.Client, ctx context.Context, name string, existing network.Inspect, exists bool) Response {
	if !exists {
		return Response{Changed: false, Msg: "network already absent"}
	}

	// Disconnect any connected containers first
	for containerID := range existing.Containers {
		if _, err := cli.NetworkDisconnect(ctx, existing.ID, client.NetworkDisconnectOptions{Container: containerID, Force: true}); err != nil {
			// Log but continue - container might have been removed
			if !docker.IsNotFoundError(err) {
				return Response{Failed: true, Msg: docker.WrapError("disconnect container", containerID, err).Error()}
			}
		}
	}

	if _, err := cli.NetworkRemove(ctx, name, client.NetworkRemoveOptions{}); err != nil {
		return Response{Failed: true, Msg: docker.WrapError("remove network", name, err).Error()}
	}
	return Response{Changed: true, Msg: "network removed", NetworkID: existing.ID}
}

// handlePresent ensures a network exists with the desired configuration
func handlePresent(cli *client.Client, ctx context.Context, req Request, existing network.Inspect, exists bool) Response {
	diffBuilder := docker.NewDiffBuilder()

	if exists {
		// Check if we need to recreate (immutable fields changed)
		needsRecreate := checkNeedsRecreate(req, existing, diffBuilder)

		if needsRecreate && !req.Force {
			return Response{
				Changed: false,
				Failed:  true,
				Msg:     fmt.Sprintf("network exists with different immutable settings (use force=true to recreate): %v", diffBuilder.DiffMap()),
				Diff:    diffBuilder.DiffMap(),
			}
		}

		if needsRecreate && req.Force {
			// Force recreate: remove and create again
			resp := handleAbsent(cli, ctx, req.Name, existing, true)
			if resp.Failed {
				return resp
			}
			exists = false
		}
	}

	if !exists {
		// Create network
		networkID, err := createNetwork(cli, ctx, req)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("create network", req.Name, err).Error()}
		}

		// Connect containers if specified
		if len(req.Connected) > 0 {
			if _, err := reconcileConnectedContainers(cli, ctx, networkID, req.Connected, nil, false); err != nil {
				return Response{Failed: true, Msg: err.Error(), NetworkID: networkID}
			}
		}

		return Response{Changed: true, Msg: "network created", NetworkID: networkID, Diff: diffBuilder.DiffMap()}
	}

	// Network exists, check for container connection changes
	changed := false
	if len(req.Connected) > 0 || (!req.Appends && len(existing.Containers) > 0) {
		currentContainers := make(map[string]network.EndpointResource)
		for id, endpoint := range existing.Containers {
			currentContainers[id] = endpoint
		}

		connectChanged, err := reconcileConnectedContainers(cli, ctx, existing.ID, req.Connected, currentContainers, req.Appends)
		if err != nil {
			return Response{Failed: true, Msg: err.Error(), NetworkID: existing.ID}
		}
		if connectChanged {
			changed = true
		}
	}

	if changed {
		return Response{Changed: true, Msg: "network connections updated", NetworkID: existing.ID}
	}

	return Response{Changed: false, Msg: "network already exists", NetworkID: existing.ID}
}

// checkNeedsRecreate compares immutable network settings
// Only checks fields that were explicitly set in the request (non-zero values)
// This allows updating just container connections without re-specifying all options
func checkNeedsRecreate(req Request, existing network.Inspect, diff *docker.DiffBuilder) bool {
	needsRecreate := false

	// Driver is immutable - only check if explicitly specified
	if req.Driver != "" && req.Driver != existing.Driver {
		diff.Add("driver", req.Driver, existing.Driver)
		needsRecreate = true
	}

	// Internal is immutable - but only matters if it's set to true and differs
	// Default is false, so we only trigger recreate if user sets it true and network is not internal
	if req.Internal && req.Internal != existing.Internal {
		diff.Add("internal", req.Internal, existing.Internal)
		needsRecreate = true
	}

	// EnableIPv6 is immutable - only check if set to true
	if req.EnableIPv6 && req.EnableIPv6 != existing.EnableIPv6 {
		diff.Add("enable_ipv6", req.EnableIPv6, existing.EnableIPv6)
		needsRecreate = true
	}

	// Ingress is immutable - only check if set to true
	if req.Ingress && req.Ingress != existing.Ingress {
		diff.Add("ingress", req.Ingress, existing.Ingress)
		needsRecreate = true
	}

	// ConfigOnly is immutable - only check if set to true
	if req.ConfigOnly && req.ConfigOnly != existing.ConfigOnly {
		diff.Add("config_only", req.ConfigOnly, existing.ConfigOnly)
		needsRecreate = true
	}

	// Driver options are immutable - check if requested options are present
	// Note: Docker may add additional default options, so we only check if our requested options match
	// Only check if user specified any options
	if len(req.Options) > 0 && !mapsContain(existing.Options, req.Options) {
		diff.Add("options", req.Options, existing.Options)
		needsRecreate = true
	}

	// IPAM config is immutable - only check if explicitly specified
	if len(req.IPAMConfig) > 0 && !compareIPAMConfig(req.IPAMConfig, existing.IPAM) {
		diff.Add("ipam_config", req.IPAMConfig, formatExistingIPAM(existing.IPAM))
		needsRecreate = true
	}

	// Labels are mutable in theory but Docker doesn't support updating them
	// So we treat them as immutable for now - check if requested labels are present
	// Only check if user specified any labels
	if len(req.Labels) > 0 && !mapsContain(existing.Labels, req.Labels) {
		diff.Add("labels", req.Labels, existing.Labels)
		needsRecreate = true
	}

	// Attachable is immutable - only check if set to true
	if req.Attachable && req.Attachable != existing.Attachable {
		diff.Add("attachable", req.Attachable, existing.Attachable)
		needsRecreate = true
	}

	return needsRecreate
}

// compareIPAMConfig compares requested IPAM config with existing
func compareIPAMConfig(requested []IPAMConfig, existing network.IPAM) bool {
	if len(requested) == 0 && len(existing.Config) == 0 {
		return true
	}

	if len(requested) != len(existing.Config) {
		return false
	}

	// Sort for comparison
	reqSorted := make([]string, len(requested))
	for i, cfg := range requested {
		reqSorted[i] = fmt.Sprintf("%s|%s|%s", cfg.Subnet, cfg.Gateway, cfg.IPRange)
	}
	sort.Strings(reqSorted)

	existSorted := make([]string, len(existing.Config))
	for i, cfg := range existing.Config {
		existSorted[i] = fmt.Sprintf("%s|%s|%s", cfg.Subnet, cfg.Gateway, cfg.IPRange)
	}
	sort.Strings(existSorted)

	for i := range reqSorted {
		if reqSorted[i] != existSorted[i] {
			return false
		}
	}

	return true
}

// formatExistingIPAM formats existing IPAM config for diff output
func formatExistingIPAM(ipam network.IPAM) []map[string]string {
	result := make([]map[string]string, len(ipam.Config))
	for i, cfg := range ipam.Config {
		result[i] = map[string]string{
			"subnet":   prefixString(cfg.Subnet),
			"gateway":  addrString(cfg.Gateway),
			"ip_range": prefixString(cfg.IPRange),
		}
	}
	return result
}

// createNetwork creates a new Docker network with the specified configuration
func createNetwork(cli *client.Client, ctx context.Context, req Request) (string, error) {
	opts := client.NetworkCreateOptions{
		Driver:     req.Driver,
		Options:    req.Options,
		Internal:   req.Internal,
		Attachable: req.Attachable,
		Labels:     req.Labels,
		ConfigOnly: req.ConfigOnly,
		Ingress:    req.Ingress,
	}
	if req.EnableIPv6 {
		opts.EnableIPv6 = &req.EnableIPv6
	}

	// Handle Scope if specified
	if req.Scope != "" {
		opts.Scope = req.Scope
	}

	// Handle ConfigFrom
	if req.ConfigFrom != "" {
		opts.ConfigFrom = req.ConfigFrom
	}

	// Build IPAM configuration
	if len(req.IPAMConfig) > 0 || req.IPAMDriver != "" {
		ipamConfigs := make([]network.IPAMConfig, 0, len(req.IPAMConfig))
		for _, cfg := range req.IPAMConfig {
			subnet, err := parsePrefix(cfg.Subnet, "subnet")
			if err != nil {
				return "", err
			}
			gateway, err := parseAddress(cfg.Gateway, "gateway")
			if err != nil {
				return "", err
			}
			ipRange, err := parsePrefix(cfg.IPRange, "ip_range")
			if err != nil {
				return "", err
			}
			auxAddresses := make(map[string]netip.Addr, len(cfg.AuxAddress))
			for name, value := range cfg.AuxAddress {
				address, err := parseAddress(value, fmt.Sprintf("aux_address.%s", name))
				if err != nil {
					return "", err
				}
				auxAddresses[name] = address
			}
			ipamCfg := network.IPAMConfig{
				Subnet:     subnet,
				Gateway:    gateway,
				IPRange:    ipRange,
				AuxAddress: auxAddresses,
			}
			ipamConfigs = append(ipamConfigs, ipamCfg)
		}

		driver := req.IPAMDriver
		if driver == "" {
			driver = "default"
		}

		opts.IPAM = &network.IPAM{
			Driver:  driver,
			Config:  ipamConfigs,
			Options: req.IPAMDriverOptions,
		}
	}

	resp, err := cli.NetworkCreate(ctx, req.Name, opts)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

// reconcileConnectedContainers ensures the desired containers are connected to the network
func reconcileConnectedContainers(cli *client.Client, ctx context.Context, networkID string, desired []ConnectedContainer, current map[string]network.EndpointResource, appends bool) (bool, error) {
	changed := false
	// Build map of desired container names/IDs
	desiredMap := make(map[string]ConnectedContainer)
	for _, c := range desired {
		desiredMap[c.Name] = c
	}

	// Disconnect containers not in desired list (unless appends is true)
	if !appends && current != nil {
		for containerID, endpoint := range current {
			containerName := endpoint.Name
			if containerName == "" {
				containerName = containerID
			}

			// Check if this container should remain connected
			_, wantedByID := desiredMap[containerID]
			_, wantedByName := desiredMap[containerName]

			if !wantedByID && !wantedByName {
				if _, err := cli.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{Container: containerID}); err != nil {
					if !docker.IsNotFoundError(err) {
						return false, docker.WrapError("disconnect container", containerName, err)
					}
				}
				changed = true
			}
		}
	}

	// Connect desired containers
	for _, c := range desired {
		// Check if already connected
		alreadyConnected := false
		var currentEndpoint network.EndpointResource
		for containerID, endpoint := range current {
			if containerID == c.Name || endpoint.Name == c.Name {
				alreadyConnected = true
				currentEndpoint = endpoint
				break
			}
		}

		// Build endpoint settings
		endpointSettings := &network.EndpointSettings{
			Aliases:    c.Aliases,
			Links:      c.Links,
			DriverOpts: c.DriverOpts,
		}

		if c.IPv4Address != "" || c.IPv6Address != "" {
			ipv4Address, err := parseAddress(c.IPv4Address, "ipv4_address")
			if err != nil {
				return false, docker.WrapError("connect container", c.Name, err)
			}
			ipv6Address, err := parseAddress(c.IPv6Address, "ipv6_address")
			if err != nil {
				return false, docker.WrapError("connect container", c.Name, err)
			}
			endpointSettings.IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: ipv4Address,
				IPv6Address: ipv6Address,
			}
		}

		if alreadyConnected {
			// Check if endpoint settings need updating
			if needsEndpointUpdate(c, currentEndpoint) {
				// Disconnect and reconnect with new settings
				if _, err := cli.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{Container: c.Name}); err != nil {
					if !docker.IsNotFoundError(err) {
						return false, docker.WrapError("disconnect container for update", c.Name, err)
					}
				}
				if _, err := cli.NetworkConnect(ctx, networkID, client.NetworkConnectOptions{Container: c.Name, EndpointConfig: endpointSettings}); err != nil {
					return false, docker.WrapError("reconnect container", c.Name, err)
				}
				changed = true
			}
		} else {
			// Connect new container
			if _, err := cli.NetworkConnect(ctx, networkID, client.NetworkConnectOptions{Container: c.Name, EndpointConfig: endpointSettings}); err != nil {
				// Check if it's because container doesn't exist
				if docker.IsNotFoundError(err) {
					return false, docker.WrapError("connect container (not found)", c.Name, err)
				}
				return false, docker.WrapError("connect container", c.Name, err)
			}
			changed = true
		}
	}

	return changed, nil
}

// needsEndpointUpdate checks if a container's endpoint settings need updating
// Note: EndpointResource has limited fields - only basic IP info is available
func needsEndpointUpdate(desired ConnectedContainer, current network.EndpointResource) bool {
	// Check IP addresses
	if desired.IPv4Address != "" {
		if !current.IPv4Address.IsValid() || current.IPv4Address.Addr().String() != desired.IPv4Address {
			return true
		}
	}
	if desired.IPv6Address != "" {
		if !current.IPv6Address.IsValid() || current.IPv6Address.Addr().String() != desired.IPv6Address {
			return true
		}
	}

	// Note: EndpointResource doesn't include aliases - would need container inspect
	// For now, if aliases are specified, we assume update is needed if not already connected
	// This is a limitation - aliases can only be checked via container inspect

	return false
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

// mapsContain checks if all keys in 'required' exist in 'actual' with the same values
// This allows 'actual' to have additional keys that aren't in 'required'
func mapsContain(actual, required map[string]string) bool {
	if len(required) == 0 {
		return true
	}
	if actual == nil {
		return false
	}
	for k, v := range required {
		if actualVal, ok := actual[k]; !ok || actualVal != v {
			return false
		}
	}
	return true
}
