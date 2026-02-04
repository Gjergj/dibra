package docker_container

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	state := req.State
	if state == "" {
		state = "started"
	}

	existing, err := cli.ContainerInspect(ctx, req.Name)
	exists := err == nil
	if err != nil && !docker.IsNotFoundError(err) {
		return Response{Failed: true, Msg: docker.WrapError("inspect container", req.Name, err).Error()}
	}

	switch state {
	case "absent":
		if !exists {
			return Response{Changed: false, Msg: "container already absent"}
		}
		return removeContainer(ctx, cli, req.Name, req.ForceKill, req.KeepVolumes)

	case "stopped":
		if !exists {
			return Response{Changed: false, Msg: "container not found"}
		}
		if !existing.State.Running {
			return Response{Changed: false, Msg: "container already stopped", Container: convertContainer(existing)}
		}
		return stopContainer(ctx, cli, req.Name, req.ForceKill)

	case "started", "present":
		return handlePresentOrStarted(ctx, cli, req, existing, exists, state)

	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
	}
}

func handlePresentOrStarted(ctx context.Context, cli *client.Client, req Request, existing types.ContainerJSON, exists bool, state string) Response {
	diffBuilder := docker.NewDiffBuilder()
	var actions []string

	pullPolicy := req.Pull
	if pullPolicy == "" {
		pullPolicy = PullMissing
	}

	if req.Image != "" {
		registryAuth := docker.EncodeRegistryAuth(req.RegistryUsername, req.RegistryPassword)
		pulled, pullErr := handleImagePull(ctx, cli, req.Image, pullPolicy, exists, registryAuth)
		if pullErr != nil {
			return Response{Failed: true, Msg: pullErr.Error()}
		}
		if pulled {
			actions = append(actions, "pulled")
		}
	}

	if !exists {
		resp := createAndStart(ctx, cli, req, state)
		if !resp.Failed {
			resp.Actions = append([]string{"created"}, actions...)
			if state == "started" {
				resp.Actions = append(resp.Actions, "started")
			}
		}
		return resp
	}

	recreatePolicy := req.Recreate
	if recreatePolicy == "" {
		recreatePolicy = RecreateAuto
	}

	if recreatePolicy == RecreateAlways {
		return recreateContainer(ctx, cli, req, state, actions)
	}

	needsRecreate, needsUpdate := compareContainer(ctx, cli, req, existing, diffBuilder)

	if recreatePolicy == RecreateNever {
		needsRecreate = false
	}

	if needsRecreate {
		resp := recreateContainer(ctx, cli, req, state, actions)
		resp.Diff = diffBuilder.DiffMap()
		return resp
	}

	if needsUpdate {
		updateResp := updateContainer(ctx, cli, existing.ID, req)
		if updateResp.Failed {
			return updateResp
		}
		actions = append(actions, "updated")
	}

	resp := reconcileNetworks(ctx, cli, req, existing, diffBuilder)
	if resp.Failed {
		return resp
	}
	if resp.Changed {
		actions = append(actions, "network_updated")
	}

	if state == "started" && !existing.State.Running {
		startResp := startContainer(ctx, cli, req.Name)
		if startResp.Failed {
			return startResp
		}
		actions = append(actions, "started")
		startResp.Actions = actions
		startResp.Diff = diffBuilder.DiffMap()
		return startResp
	}

	inspect, _ := cli.ContainerInspect(ctx, existing.ID)
	return Response{
		Changed:   len(actions) > 0 || diffBuilder.HasDiffs(),
		Container: convertContainer(inspect),
		Actions:   actions,
		Diff:      diffBuilder.DiffMap(),
	}
}

func handleImagePull(ctx context.Context, cli *client.Client, image string, pullPolicy PullPolicy, containerExists bool, registryAuth string) (bool, error) {
	switch pullPolicy {
	case PullNever:
		return false, nil
	case PullAlways:
		return pullImage(ctx, cli, image, registryAuth)
	case PullMissing:
		fallthrough
	default:
		if !containerExists {
			_, _, err := cli.ImageInspectWithRaw(ctx, image)
			if docker.IsNotFoundError(err) {
				return pullImage(ctx, cli, image, registryAuth)
			}
		}
		return false, nil
	}
}

func pullImage(ctx context.Context, cli *client.Client, image string, registryAuth string) (bool, error) {
	existingID := ""
	if inspect, _, err := cli.ImageInspectWithRaw(ctx, image); err == nil {
		existingID = inspect.ID
	}

	pullOpts := types.ImagePullOptions{}
	if registryAuth != "" {
		pullOpts.RegistryAuth = registryAuth
	}

	reader, err := cli.ImagePull(ctx, image, pullOpts)
	if err != nil {
		return false, docker.WrapError("pull image", image, err)
	}
	defer reader.Close()

	result := docker.ParsePullPushStream(reader)
	if result.Error != nil {
		return false, docker.WrapError("pull image", image, result.Error)
	}

	if existingID != "" {
		if inspect, _, err := cli.ImageInspectWithRaw(ctx, image); err == nil {
			if inspect.ID == existingID {
				return false, nil
			}
		}
	}

	return true, nil
}

func compareContainer(ctx context.Context, cli *client.Client, req Request, existing types.ContainerJSON, diff *docker.DiffBuilder) (needsRecreate, needsUpdate bool) {
	if req.Image != "" {
		imageID, err := resolveImageID(ctx, cli, req.Image)
		if err == nil && existing.Image != imageID {
			diff.Add("image", req.Image, existing.Config.Image)
			needsRecreate = true
		}
	}

	if req.Command != nil {
		desiredCmd := parseCommand(req.Command)
		if !docker.CompareStringSlicesOrdered(desiredCmd, existing.Config.Cmd) {
			diff.Add("command", desiredCmd, existing.Config.Cmd)
			needsRecreate = true
		}
	}

	if req.Entrypoint != nil {
		desiredEntry := parseCommand(req.Entrypoint)
		if !docker.CompareStringSlicesOrdered(desiredEntry, existing.Config.Entrypoint) {
			diff.Add("entrypoint", desiredEntry, existing.Config.Entrypoint)
			needsRecreate = true
		}
	}

	if len(req.Env) > 0 {
		desiredMap := req.Env
		currentMap := docker.EnvSliceToMap(existing.Config.Env)
		for k, v := range desiredMap {
			if cv, ok := currentMap[k]; !ok || cv != v {
				diff.Add("env."+k, v, cv)
				needsRecreate = true
			}
		}
	}

	if req.User != "" && diff.AddIfDifferentStr("user", req.User, existing.Config.User) {
		needsRecreate = true
	}
	if req.WorkingDir != "" && diff.AddIfDifferentStr("working_dir", req.WorkingDir, existing.Config.WorkingDir) {
		needsRecreate = true
	}
	if req.Hostname != "" && diff.AddIfDifferentStr("hostname", req.Hostname, existing.Config.Hostname) {
		needsRecreate = true
	}
	if req.Domainname != "" && diff.AddIfDifferentStr("domainname", req.Domainname, existing.Config.Domainname) {
		needsRecreate = true
	}

	if len(req.Labels) > 0 && !docker.CompareMaps(req.Labels, existing.Config.Labels) {
		diff.Add("labels", req.Labels, existing.Config.Labels)
		needsRecreate = true
	}

	if len(req.Ports) > 0 {
		desiredPorts, _ := buildPortBindings(req.Ports)
		if !docker.ComparePortBindings(desiredPorts, existing.HostConfig.PortBindings) {
			diff.Add("ports", req.Ports, existing.HostConfig.PortBindings)
			needsRecreate = true
		}
	}

	if len(req.Volumes) > 0 {
		desiredVolumes := docker.NormalizeMounts(req.Volumes)
		currentVolumes := docker.NormalizeMounts(existing.HostConfig.Binds)
		if !docker.CompareStringSlices(desiredVolumes, currentVolumes) {
			diff.Add("volumes", desiredVolumes, currentVolumes)
			needsRecreate = true
		}
	}

	if req.NetworkMode != "" {
		currentMode := normalizeNetworkMode(string(existing.HostConfig.NetworkMode))
		desiredMode := normalizeNetworkMode(req.NetworkMode)
		if desiredMode != currentMode {
			diff.Add("network_mode", req.NetworkMode, string(existing.HostConfig.NetworkMode))
			needsRecreate = true
		}
	}

	if diff.AddIfDifferentBool("privileged", req.Privileged, existing.HostConfig.Privileged) {
		needsRecreate = true
	}

	if !docker.CompareStringSlices(req.CapAdd, existing.HostConfig.CapAdd) {
		diff.Add("cap_add", req.CapAdd, existing.HostConfig.CapAdd)
		needsRecreate = true
	}
	if !docker.CompareStringSlices(req.CapDrop, existing.HostConfig.CapDrop) {
		diff.Add("cap_drop", req.CapDrop, existing.HostConfig.CapDrop)
		needsRecreate = true
	}

	if req.Init && (existing.HostConfig.Init == nil || !*existing.HostConfig.Init) {
		diff.Add("init", true, false)
		needsRecreate = true
	}

	needsUpdate = checkMutableFields(req, existing, diff)

	return needsRecreate, needsUpdate
}

func checkMutableFields(req Request, existing types.ContainerJSON, diff *docker.DiffBuilder) bool {
	needsUpdate := false

	if req.RestartPolicy != "" {
		desiredPolicy, desiredMax := parseRestartPolicy(req.RestartPolicy)
		currentPolicy := existing.HostConfig.RestartPolicy.Name
		currentMax := existing.HostConfig.RestartPolicy.MaximumRetryCount
		if desiredPolicy != currentPolicy || desiredMax != currentMax {
			diff.Add("restart_policy", req.RestartPolicy, fmt.Sprintf("%s:%d", currentPolicy, currentMax))
			needsUpdate = true
		}
	}

	if req.Memory != "" {
		desiredMem, _ := units.RAMInBytes(req.Memory)
		if desiredMem != existing.HostConfig.Memory {
			diff.Add("memory", req.Memory, existing.HostConfig.Memory)
			needsUpdate = true
		}
	}

	if req.CPUs > 0 {
		desiredNano := int64(req.CPUs * 1e9)
		if desiredNano != existing.HostConfig.NanoCPUs {
			diff.Add("cpus", req.CPUs, float64(existing.HostConfig.NanoCPUs)/1e9)
			needsUpdate = true
		}
	}

	if req.PidsLimit > 0 {
		currentPids := int64(0)
		if existing.HostConfig.PidsLimit != nil {
			currentPids = *existing.HostConfig.PidsLimit
		}
		if req.PidsLimit != currentPids {
			diff.Add("pids_limit", req.PidsLimit, currentPids)
			needsUpdate = true
		}
	}

	return needsUpdate
}

func updateContainer(ctx context.Context, cli *client.Client, containerID string, req Request) Response {
	updateConfig := container.UpdateConfig{
		Resources: container.Resources{},
	}

	if req.RestartPolicy != "" {
		name, maxRetry := parseRestartPolicy(req.RestartPolicy)
		updateConfig.RestartPolicy = container.RestartPolicy{Name: name, MaximumRetryCount: maxRetry}
	}

	if req.Memory != "" {
		mem, err := units.RAMInBytes(req.Memory)
		if err == nil {
			updateConfig.Resources.Memory = mem
		}
	}

	if req.MemorySwap != "" {
		if req.MemorySwap == "-1" {
			updateConfig.Resources.MemorySwap = -1
		} else {
			swap, err := units.RAMInBytes(req.MemorySwap)
			if err == nil {
				updateConfig.Resources.MemorySwap = swap
			}
		}
	}

	if req.CPUs > 0 {
		updateConfig.Resources.NanoCPUs = int64(req.CPUs * 1e9)
	}

	if req.PidsLimit > 0 {
		updateConfig.Resources.PidsLimit = &req.PidsLimit
	}

	_, err := cli.ContainerUpdate(ctx, containerID, updateConfig)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("update container", containerID, err).Error()}
	}

	return Response{Changed: true, Msg: "container updated"}
}

func reconcileNetworks(ctx context.Context, cli *client.Client, req Request, existing types.ContainerJSON, diff *docker.DiffBuilder) Response {
	if len(req.Networks) == 0 {
		return Response{Changed: false}
	}

	currentNetworks := make(map[string]bool)
	primaryNetwork := ""
	if existing.NetworkSettings != nil {
		for name := range existing.NetworkSettings.Networks {
			currentNetworks[name] = true
		}
		// The primary network is the one set via NetworkMode
		primaryNetwork = normalizeNetworkMode(string(existing.HostConfig.NetworkMode))
	}

	desiredNetworks := make(map[string]Network)
	for _, n := range req.Networks {
		desiredNetworks[n.Name] = n
	}

	changed := false

	// Connect to new networks
	for name, netConfig := range desiredNetworks {
		if !currentNetworks[name] {
			endpointConfig := &network.EndpointSettings{
				Aliases: netConfig.Aliases,
				Links:   netConfig.Links,
			}
			if netConfig.IPv4Address != "" {
				endpointConfig.IPAMConfig = &network.EndpointIPAMConfig{
					IPv4Address: netConfig.IPv4Address,
				}
			}
			if netConfig.IPv6Address != "" {
				if endpointConfig.IPAMConfig == nil {
					endpointConfig.IPAMConfig = &network.EndpointIPAMConfig{}
				}
				endpointConfig.IPAMConfig.IPv6Address = netConfig.IPv6Address
			}

			if err := cli.NetworkConnect(ctx, name, existing.ID, endpointConfig); err != nil {
				return Response{Failed: true, Msg: docker.WrapError("connect network", name, err).Error()}
			}
			diff.Add("network."+name, "connected", "disconnected")
			changed = true
		}
	}

	// Disconnect from networks not in desired list (except primary network)
	if !req.NetworksAppend {
		for name := range currentNetworks {
			// Skip primary network - cannot disconnect from it
			if name == primaryNetwork {
				continue
			}
			// Skip if in desired list
			if _, desired := desiredNetworks[name]; desired {
				continue
			}
			if err := cli.NetworkDisconnect(ctx, name, existing.ID, false); err != nil {
				// Don't fail on disconnect errors - network might already be gone
				if !docker.IsNotFoundError(err) {
					return Response{Failed: true, Msg: docker.WrapError("disconnect network", name, err).Error()}
				}
			}
			diff.Add("network."+name, "disconnected", "connected")
			changed = true
		}
	}

	return Response{Changed: changed}
}

func resolveImageID(ctx context.Context, cli *client.Client, image string) (string, error) {
	inspect, _, err := cli.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return "", err
	}
	return inspect.ID, nil
}

func recreateContainer(ctx context.Context, cli *client.Client, req Request, state string, prevActions []string) Response {
	removeResp := removeContainer(ctx, cli, req.Name, req.ForceKill, req.KeepVolumes)
	if removeResp.Failed {
		return removeResp
	}

	createResp := createAndStart(ctx, cli, req, state)
	if !createResp.Failed {
		createResp.Actions = append(prevActions, "recreated")
		if state == "started" {
			createResp.Actions = append(createResp.Actions, "started")
		}
	}
	return createResp
}

func createAndStart(ctx context.Context, cli *client.Client, req Request, state string) Response {
	config, hostConfig, err := buildContainerConfig(req)
	if err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	created, err := cli.ContainerCreate(ctx, config, hostConfig, nil, nil, req.Name)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create container", req.Name, err).Error()}
	}

	for _, n := range req.Networks {
		endpointConfig := &network.EndpointSettings{
			Aliases: n.Aliases,
			Links:   n.Links,
		}
		if n.IPv4Address != "" {
			endpointConfig.IPAMConfig = &network.EndpointIPAMConfig{IPv4Address: n.IPv4Address}
		}
		if n.IPv6Address != "" {
			if endpointConfig.IPAMConfig == nil {
				endpointConfig.IPAMConfig = &network.EndpointIPAMConfig{}
			}
			endpointConfig.IPAMConfig.IPv6Address = n.IPv6Address
		}
		if err := cli.NetworkConnect(ctx, n.Name, created.ID, endpointConfig); err != nil {
			cli.ContainerRemove(ctx, created.ID, types.ContainerRemoveOptions{Force: true})
			return Response{Failed: true, Msg: docker.WrapError("connect network", n.Name, err).Error()}
		}
	}

	if state == "present" {
		return Response{Changed: true, Msg: "container created", Container: map[string]interface{}{"Id": created.ID}}
	}

	if err := cli.ContainerStart(ctx, created.ID, types.ContainerStartOptions{}); err != nil {
		cli.ContainerRemove(ctx, created.ID, types.ContainerRemoveOptions{Force: true})
		return Response{Failed: true, Msg: docker.WrapError("start container", req.Name, err).Error()}
	}

	inspect, _ := cli.ContainerInspect(ctx, created.ID)
	return Response{Changed: true, Msg: "container started", Container: convertContainer(inspect)}
}

func buildContainerConfig(req Request) (*container.Config, *container.HostConfig, error) {
	config := &container.Config{
		Image:      req.Image,
		Env:        convertEnv(req.Env),
		Hostname:   req.Hostname,
		Domainname: req.Domainname,
		User:       req.User,
		WorkingDir: req.WorkingDir,
		Labels:     req.Labels,
	}

	if req.Command != nil {
		config.Cmd = parseCommand(req.Command)
	}
	if req.Entrypoint != nil {
		config.Entrypoint = parseCommand(req.Entrypoint)
	}

	portBindings, exposedPorts := buildPortBindings(req.Ports)
	for _, p := range req.ExposedPorts {
		port := nat.Port(p)
		exposedPorts[port] = struct{}{}
	}
	config.ExposedPorts = exposedPorts

	hostConfig := &container.HostConfig{
		AutoRemove:   req.AutoRemove,
		Privileged:   req.Privileged,
		NetworkMode:  container.NetworkMode(req.NetworkMode),
		Binds:        req.Volumes,
		PortBindings: portBindings,
		CapAdd:       req.CapAdd,
		CapDrop:      req.CapDrop,
		Links:        req.Links,
		Sysctls:      req.Sysctls,
		SecurityOpt:  req.SecurityOpt,
	}

	if req.RestartPolicy != "" {
		name, maxRetry := parseRestartPolicy(req.RestartPolicy)
		hostConfig.RestartPolicy = container.RestartPolicy{Name: name, MaximumRetryCount: maxRetry}
	}

	if req.LogDriver != "" {
		hostConfig.LogConfig = container.LogConfig{
			Type:   req.LogDriver,
			Config: req.LogOptions,
		}
	}

	if len(req.Devices) > 0 {
		for _, d := range req.Devices {
			hostConfig.Devices = append(hostConfig.Devices, parseDevice(d))
		}
	}

	if req.Healthcheck != nil {
		config.Healthcheck = buildHealthcheck(req.Healthcheck)
	}

	if req.Init {
		init := true
		hostConfig.Init = &init
	}

	if len(req.Tmpfs) > 0 {
		hostConfig.Tmpfs = make(map[string]string)
		for _, t := range req.Tmpfs {
			parts := strings.SplitN(t, ":", 2)
			if len(parts) == 2 {
				hostConfig.Tmpfs[parts[0]] = parts[1]
			} else {
				hostConfig.Tmpfs[parts[0]] = ""
			}
		}
	}

	if req.ShmSize != "" {
		size, err := units.RAMInBytes(req.ShmSize)
		if err == nil {
			hostConfig.ShmSize = size
		}
	}

	if req.Memory != "" {
		mem, err := units.RAMInBytes(req.Memory)
		if err == nil {
			hostConfig.Memory = mem
		}
	}

	if req.MemorySwap != "" {
		if req.MemorySwap == "-1" {
			hostConfig.MemorySwap = -1
		} else {
			swap, err := units.RAMInBytes(req.MemorySwap)
			if err == nil {
				hostConfig.MemorySwap = swap
			}
		}
	}

	if req.CPUs > 0 {
		hostConfig.NanoCPUs = int64(req.CPUs * 1e9)
	}

	if req.PidsLimit > 0 {
		hostConfig.PidsLimit = &req.PidsLimit
	}

	for _, u := range req.Ulimits {
		hostConfig.Ulimits = append(hostConfig.Ulimits, &units.Ulimit{
			Name: u.Name,
			Soft: u.Soft,
			Hard: u.Hard,
		})
	}

	return config, hostConfig, nil
}

func buildPortBindings(ports []string) (nat.PortMap, nat.PortSet) {
	portMap := make(nat.PortMap)
	exposedPorts := make(nat.PortSet)

	for _, p := range ports {
		binding, err := docker.ParsePortBinding(p)
		if err != nil {
			continue
		}
		key := binding.ContainerPortKey()
		exposedPorts[key] = struct{}{}
		portMap[key] = append(portMap[key], binding.NatPortBinding())
	}

	return portMap, exposedPorts
}

func buildHealthcheck(hc *Healthcheck) *container.HealthConfig {
	config := &container.HealthConfig{
		Test:    hc.Test,
		Retries: hc.Retries,
	}

	if hc.Interval != "" {
		if d, err := time.ParseDuration(hc.Interval); err == nil {
			config.Interval = d
		}
	}
	if hc.Timeout != "" {
		if d, err := time.ParseDuration(hc.Timeout); err == nil {
			config.Timeout = d
		}
	}
	if hc.StartPeriod != "" {
		if d, err := time.ParseDuration(hc.StartPeriod); err == nil {
			config.StartPeriod = d
		}
	}

	return config
}

func parseDevice(spec string) container.DeviceMapping {
	parts := strings.Split(spec, ":")
	device := container.DeviceMapping{
		PathOnHost: parts[0],
	}
	if len(parts) > 1 {
		device.PathInContainer = parts[1]
	} else {
		device.PathInContainer = parts[0]
	}
	if len(parts) > 2 {
		device.CgroupPermissions = parts[2]
	} else {
		device.CgroupPermissions = "rwm"
	}
	return device
}

func parseRestartPolicy(policy string) (string, int) {
	parts := strings.Split(policy, ":")
	name := parts[0]
	maxRetry := 0
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			maxRetry = n
		}
	}
	return name, maxRetry
}

func removeContainer(ctx context.Context, cli *client.Client, name string, force bool, keepVolumes bool) Response {
	existing, err := cli.ContainerInspect(ctx, name)
	if err == nil && existing.State.Running {
		timeout := 10
		if force {
			cli.ContainerKill(ctx, name, "SIGKILL")
		} else {
			cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
		}
	}

	opts := types.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: !keepVolumes,
	}
	if err := cli.ContainerRemove(ctx, name, opts); err != nil {
		return Response{Failed: true, Msg: docker.WrapError("remove container", name, err).Error()}
	}
	return Response{Changed: true, Msg: "container removed", Actions: []string{"removed"}}
}

func stopContainer(ctx context.Context, cli *client.Client, name string, force bool) Response {
	if force {
		if err := cli.ContainerKill(ctx, name, "SIGKILL"); err != nil {
			return Response{Failed: true, Msg: docker.WrapError("kill container", name, err).Error()}
		}
	} else {
		timeout := 10
		if err := cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout}); err != nil {
			return Response{Failed: true, Msg: docker.WrapError("stop container", name, err).Error()}
		}
	}
	return Response{Changed: true, Msg: "container stopped", Actions: []string{"stopped"}}
}

func startContainer(ctx context.Context, cli *client.Client, name string) Response {
	if err := cli.ContainerStart(ctx, name, types.ContainerStartOptions{}); err != nil {
		return Response{Failed: true, Msg: docker.WrapError("start container", name, err).Error()}
	}
	inspect, _ := cli.ContainerInspect(ctx, name)
	return Response{Changed: true, Container: convertContainer(inspect), Actions: []string{"started"}}
}

func parseCommand(cmd interface{}) []string {
	if s, ok := cmd.(string); ok {
		return strings.Fields(s)
	}
	if list, ok := cmd.([]interface{}); ok {
		res := make([]string, len(list))
		for i, v := range list {
			res[i] = fmt.Sprint(v)
		}
		return res
	}
	if list, ok := cmd.([]string); ok {
		return list
	}
	return nil
}

func normalizeNetworkMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "default" || mode == "bridge" {
		return "bridge"
	}
	return mode
}

func convertEnv(env map[string]string) []string {
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res
}

func convertContainer(c types.ContainerJSON) map[string]interface{} {
	return map[string]interface{}{
		"Id":              c.ID,
		"Name":            c.Name,
		"State":           c.State,
		"NetworkSettings": c.NetworkSettings,
		"Config":          c.Config,
		"HostConfig":      c.HostConfig,
	}
}
