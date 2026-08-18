package docker_container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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
	req = normalizeDefaults(req)
	if err := validateRequest(req); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create client", "", err).Error()}
	}
	defer cli.Close()
	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	inspectResult, inspectErr := cli.ContainerInspect(ctx, req.Name, client.ContainerInspectOptions{})
	exists := inspectErr == nil
	if inspectErr != nil && !docker.IsNotFoundError(inspectErr) {
		return Response{Failed: true, Msg: docker.WrapError("inspect container", req.Name, inspectErr).Error()}
	}
	diff := docker.NewDiffBuilder()
	actions := make([]map[string]any, 0)
	warnings := make([]string, 0)
	finish := func(response Response) Response {
		if len(warnings) > 0 {
			response.Warnings = append(append([]string{}, warnings...), response.Warnings...)
		}
		if state.DiffMode {
			response.Diff = diff.DiffMap()
		}
		if state.CheckMode || boolValue(req.Debug) {
			response.Actions = actions
		}
		return response
	}

	if req.State == "absent" {
		if !exists {
			return finish(Response{Msg: "container already absent"})
		}
		if inspectResult.Container.State != nil && inspectResult.Container.State.Running {
			diff.Add("running", false, true)
			action := map[string]any{"stopped": inspectResult.Container.ID, "timeout": req.StopTimeout}
			if req.ForceKill {
				action = map[string]any{"killed": inspectResult.Container.ID, "signal": effectiveKillSignal(req)}
			}
			actions = append(actions, action)
		}
		diff.Add("exists", false, true)
		actions = append(actions, removalAction(inspectResult.Container.ID, req))
		if state.CheckMode {
			return finish(Response{Changed: true})
		}
		response := removeContainer(ctx, cli, req, inspectResult.Container)
		return finish(response)
	}

	req = resolveContainerNamespaceModes(ctx, cli, req)
	desiredConfig, desiredHost, err := buildContainerConfig(req, dependencies.FileSystem)
	if err != nil {
		return finish(Response{Failed: true, Msg: err.Error()})
	}
	if req.Platform != "" {
		req.resolvedPlatform, err = resolvePlatform(ctx, cli, req.Platform)
		if err != nil {
			return finish(Response{Failed: true, Msg: err.Error()})
		}
	}

	imageChanged, imageAction, response := ensureImage(ctx, cli, req, exists, state.CheckMode, dependencies)
	if response.Failed {
		return finish(response)
	}
	if imageAction != nil {
		actions = append(actions, imageAction)
	}
	if err := applyDefaultHostIP(ctx, cli, req, desiredHost); err != nil {
		return finish(Response{Failed: true, Msg: err.Error()})
	}
	var desiredImage image.InspectResponse
	if req.Image != "" {
		var inspected client.ImageInspectResult
		inspected, err = cli.ImageInspect(ctx, req.Image)
		desiredImage = inspected.InspectResponse
		if err != nil && !(state.CheckMode && docker.IsNotFoundError(err)) {
			return finish(Response{Failed: true, Msg: docker.WrapError("inspect image", req.Image, err).Error()})
		}
	}
	if exists && inspectResult.Container.State != nil && inspectResult.Container.State.Status == "removing" {
		if !state.CheckMode {
			if waitErr := waitForRemoval(ctx, cli, inspectResult.Container.ID, req.RemovalWaitTimeout, dependencies.Clock); waitErr != nil {
				return finish(Response{Failed: true, Msg: waitErr.Error()})
			}
		}
		exists = false
	}

	if !exists {
		if req.Image == "" {
			return finish(Response{Failed: true, Msg: "cannot create container when image is not specified"})
		}
		diff.Add("exists", true, false)
		actions = append(actions, map[string]any{"created": "Created container"})
		if req.State == "started" || req.State == "healthy" {
			diff.Add("running", true, false)
		}
		if state.CheckMode {
			return finish(Response{Changed: true})
		}
		created := createContainer(ctx, cli, req, desiredConfig, desiredHost)
		if created.Failed {
			return finish(created)
		}
		warnings = append(warnings, created.Warnings...)
		createdID := created.Container["Id"].(string)
		if req.State == "started" || req.State == "healthy" {
			actions = append(actions, map[string]any{"started": createdID})
		}
		return finish(reconcileLifecycle(ctx, cli, req, createdID, true, dependencies.Clock))
	}

	if req.Image == "" {
		desiredConfig.Image = inspectResult.Container.Image
	}
	comparisonImage := desiredImage
	if (req.Image == "" || req.ImageComparison == "current-image") && inspectResult.Container.Image != "" {
		if currentImage, imageErr := inspectImage(ctx, cli, inspectResult.Container.Image); imageErr == nil {
			comparisonImage = currentImage
		} else if req.ImageComparison == "current-image" {
			comparisonImage = image.InspectResponse{}
		}
	}
	expectedConfig, expectedErr := expectedConfigWithImageDefaults(req, desiredConfig, comparisonImage)
	if expectedErr != nil {
		return finish(Response{Failed: true, Msg: expectedErr.Error()})
	}
	comparison := compareContainer(req, expectedConfig, desiredHost, inspectResult.Container, diff)
	if req.Platform != "" && comparisonMode(req, "platform") != "ignore" {
		currentImage, imageErr := inspectImage(ctx, cli, inspectResult.Container.Image)
		if imageErr != nil {
			return finish(Response{Failed: true, Msg: imageErr.Error()})
		}
		actualPlatform := imagePlatform(currentImage)
		desiredPlatform := req.resolvedPlatform
		canonicalDesired := desiredPlatform.OS + "/" + desiredPlatform.Architecture
		if desiredPlatform.Variant != "" {
			canonicalDesired += "/" + desiredPlatform.Variant
		}
		if actualPlatform != canonicalDesired {
			diff.Add("platform", canonicalDesired, actualPlatform)
			comparison.recreate = true
		}
	}
	if req.Image != "" && comparisonMode(req, "image") != "ignore" {
		if desiredImage.ID != "" && desiredImage.ID != inspectResult.Container.Image {
			diff.Add("image", desiredImage.ID, inspectResult.Container.Image)
			comparison.recreate = true
		}
		if req.ImageNameMismatch == "recreate" && inspectResult.Container.Config != nil && inspectResult.Container.Config.Image != req.Image {
			diff.Add("image_name", req.Image, inspectResult.Container.Config.Image)
			comparison.recreate = true
		}
	}
	if req.Recreate == RecreateAlways {
		comparison.recreate = true
		diff.Add("recreate", true, false)
	}
	if req.Recreate == RecreateNever {
		comparison.recreate = false
	}

	if comparison.recreate {
		if inspectResult.Container.State != nil && inspectResult.Container.State.Running {
			if req.ForceKill {
				actions = append(actions, map[string]any{"killed": inspectResult.Container.ID, "signal": effectiveKillSignal(req)})
			} else {
				actions = append(actions, map[string]any{"stopped": inspectResult.Container.ID, "timeout": req.StopTimeout})
			}
		}
		actions = append(actions, removalAction(inspectResult.Container.ID, req), map[string]any{"created": "Created container"})
		if state.CheckMode {
			return finish(Response{Changed: true, Container: convertContainer(inspectResult.Container)})
		}
		removeResponse := removeContainer(ctx, cli, req, inspectResult.Container)
		if removeResponse.Failed {
			return finish(removeResponse)
		}
		if waitErr := waitForRemoval(ctx, cli, inspectResult.Container.ID, req.RemovalWaitTimeout, dependencies.Clock); waitErr != nil {
			return finish(Response{Failed: true, Msg: waitErr.Error()})
		}
		created := createContainer(ctx, cli, req, desiredConfig, desiredHost)
		if created.Failed {
			return finish(created)
		}
		warnings = append(warnings, created.Warnings...)
		createdID := created.Container["Id"].(string)
		if req.State == "started" || req.State == "healthy" {
			actions = append(actions, map[string]any{"started": createdID})
		}
		return finish(reconcileLifecycle(ctx, cli, req, createdID, true, dependencies.Clock))
	}

	changed := imageChanged
	if comparison.update {
		actions = append(actions, map[string]any{"updated": inspectResult.Container.ID})
		changed = true
		if !state.CheckMode {
			updateResponse := updateContainer(ctx, cli, inspectResult.Container.ID, req, desiredHost)
			if updateResponse.Failed {
				return finish(updateResponse)
			}
			warnings = append(warnings, updateResponse.Warnings...)
		}
	}
	connect, disconnect, networkErr := networkDifferences(req, inspectResult.Container, diff)
	if networkErr != nil {
		return finish(Response{Failed: true, Msg: networkErr.Error()})
	}
	if len(connect)+len(disconnect) > 0 {
		changed = true
		for _, name := range disconnect {
			actions = append(actions, map[string]any{"removed_from_network": name})
		}
		for _, desired := range connect {
			actions = append(actions, map[string]any{"added_to_network": desired.Name, "network_parameters": desired})
		}
		if !state.CheckMode {
			if networkResponse := applyNetworkChanges(ctx, cli, inspectResult.Container.ID, connect, disconnect); networkResponse.Failed {
				return finish(networkResponse)
			}
		}
	}

	lifecycleChange := needsLifecycleChange(req, inspectResult.Container)
	if lifecycleChange != "" {
		changed = true
		actions = append(actions, lifecycleAction(lifecycleChange, inspectResult.Container.ID, req))
		diff.Add(lifecycleDiffField(lifecycleChange), lifecycleDiffDesired(lifecycleChange), lifecycleDiffCurrent(lifecycleChange))
	}
	if state.CheckMode {
		return finish(Response{Changed: changed, Container: convertContainer(inspectResult.Container)})
	}
	lifecycle := reconcileLifecycle(ctx, cli, req, inspectResult.Container.ID, false, dependencies.Clock)
	if lifecycle.Failed {
		return finish(lifecycle)
	}
	lifecycle.Changed = lifecycle.Changed || changed
	return finish(lifecycle)
}

func ensureImage(ctx context.Context, cli client.APIClient, req Request, containerExists, checkMode bool, dependencies docker.Dependencies) (bool, map[string]any, Response) {
	if req.Image == "" {
		return false, nil, Response{}
	}
	inspect, inspectErr := cli.ImageInspect(ctx, req.Image)
	present := inspectErr == nil
	if inspectErr != nil && !docker.IsNotFoundError(inspectErr) {
		return false, nil, Response{Failed: true, Msg: docker.WrapError("inspect image", req.Image, inspectErr).Error()}
	}
	if isImageID(req.Image) {
		if !present {
			return false, nil, Response{Failed: true, Msg: fmt.Sprintf("Cannot find image with ID %s", req.Image)}
		}
		return false, nil, Response{}
	}
	if req.Pull == PullNever {
		if !present {
			return false, nil, Response{Failed: true, Msg: fmt.Sprintf("Cannot find image with name %s, and pull=never", req.Image)}
		}
		return false, nil, Response{}
	}
	shouldPull := !present || req.Pull == PullAlways
	if !shouldPull {
		return false, nil, Response{}
	}
	if checkMode {
		changed := !present || req.PullCheckModeBehavior == "always"
		if !changed {
			return false, nil, Response{}
		}
		action := map[string]any{"pulled_image": req.Image}
		if !present {
			action["changed"] = true
		}
		return true, action, Response{}
	}
	var auth string
	var authErr error
	if req.RegistryUsername != "" || req.RegistryPassword != "" {
		auth, authErr = docker.EncodeRegistryAuthForImage(req.Image, req.RegistryUsername, req.RegistryPassword)
	} else {
		auth, authErr = docker.ResolveRegistryAuthForImageContext(ctx, req.Image, dependencies, false)
	}
	if authErr != nil {
		return false, nil, Response{Failed: true, Msg: docker.WrapError("resolve registry authentication", req.Image, authErr).Error()}
	}
	options := client.ImagePullOptions{RegistryAuth: auth}
	if req.resolvedPlatform != nil {
		options.Platforms = []ocispec.Platform{*req.resolvedPlatform}
	}
	reader, err := cli.ImagePull(ctx, req.Image, options)
	if err != nil {
		return false, nil, Response{Failed: true, Msg: docker.WrapError("pull image", req.Image, err).Error()}
	}
	defer reader.Close()
	stream := docker.ParsePullPushStream(reader)
	if stream.Error != nil {
		return false, nil, Response{Failed: true, Msg: docker.WrapError("pull image", req.Image, stream.Error).Error()}
	}
	if present {
		updated, err := cli.ImageInspect(ctx, req.Image)
		if err == nil && updated.ID == inspect.ID {
			return false, map[string]any{"pulled_image": req.Image, "changed": false}, Response{}
		}
	}
	return true, map[string]any{"pulled_image": req.Image, "changed": true}, Response{}
}

func inspectImage(ctx context.Context, cli client.APIClient, reference string) (image.InspectResponse, error) {
	result, err := cli.ImageInspect(ctx, reference)
	if err != nil {
		return image.InspectResponse{}, docker.WrapError("inspect image", reference, err)
	}
	return result.InspectResponse, nil
}

func expectedConfigWithImageDefaults(req Request, desired *container.Config, imageInfo image.InspectResponse) (*container.Config, error) {
	result := *desired
	if imageInfo.Config == nil {
		return &result, nil
	}
	if req.Env != nil || req.EnvFile != "" {
		environment := docker.EnvSliceToMap(imageInfo.Config.Env)
		for key, value := range docker.EnvSliceToMap(desired.Env) {
			environment[key] = value
		}
		result.Env = environmentSlice(environment)
	}
	if req.Labels != nil {
		if req.ImageLabelMismatch == "fail" && comparisonMode(req, "labels") == "strict" {
			missing := make([]string, 0)
			for label := range imageInfo.Config.Labels {
				if _, found := req.Labels[label]; !found {
					missing = append(missing, label)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				quoted := make([]string, len(missing))
				for index, label := range missing {
					quoted[index] = fmt.Sprintf("%q", label)
				}
				return nil, fmt.Errorf("some labels should be removed but are present in the base image; set image_label_mismatch to ignore to retain them. Labels: %s", strings.Join(quoted, ", "))
			}
		}
		if req.ImageLabelMismatch == "ignore" {
			result.Labels = make(map[string]string, len(imageInfo.Config.Labels)+len(req.Labels))
			for key, value := range imageInfo.Config.Labels {
				result.Labels[key] = value
			}
			for key, value := range req.Labels {
				result.Labels[key] = value
			}
		}
	}
	if req.ExposedPorts != nil || req.PublishedPorts != nil {
		ports := make(network.PortSet, len(imageInfo.Config.ExposedPorts)+len(result.ExposedPorts))
		for value := range imageInfo.Config.ExposedPorts {
			if port, err := network.ParsePort(value); err == nil {
				ports[port] = struct{}{}
			}
		}
		for value := range result.ExposedPorts {
			ports[value] = struct{}{}
		}
		result.ExposedPorts = ports
	}
	if req.Volumes != nil {
		volumes := make(map[string]struct{}, len(imageInfo.Config.Volumes)+len(result.Volumes))
		for value := range imageInfo.Config.Volumes {
			volumes[value] = struct{}{}
		}
		for value := range result.Volumes {
			volumes[value] = struct{}{}
		}
		result.Volumes = volumes
	}
	if req.Command != nil && len(result.Cmd) == 0 {
		result.Cmd = append([]string(nil), imageInfo.Config.Cmd...)
	}
	return &result, nil
}

func environmentSlice(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func imagePlatform(value image.InspectResponse) string {
	if value.Os == "" || value.Architecture == "" {
		return value.Os
	}
	operatingSystem, _ := normalizePlatformOS(value.Os)
	architecture, variant := normalizePlatformArch(value.Architecture, value.Variant)
	result := operatingSystem + "/" + architecture
	if variant != "" {
		result += "/" + variant
	}
	return result
}

func resolveContainerNamespaceModes(ctx context.Context, cli client.APIClient, req Request) Request {
	resolve := func(value string) string {
		name, found := strings.CutPrefix(value, "container:")
		if !found || name == "" {
			return value
		}
		inspected, err := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
		if err != nil || inspected.Container.ID == "" {
			return value
		}
		return "container:" + inspected.Container.ID
	}
	req.NetworkMode = resolve(req.NetworkMode)
	req.IPCMode = resolve(req.IPCMode)
	req.PIDMode = resolve(req.PIDMode)
	return req
}

func applyDefaultHostIP(ctx context.Context, cli client.APIClient, req Request, host *container.HostConfig) error {
	if len(req.PublishedPorts) == 0 {
		return nil
	}
	defaultIP := "0.0.0.0"
	if req.DefaultHostIP != nil {
		defaultIP = strings.Trim(*req.DefaultHostIP, "[]")
	} else {
		for _, desired := range req.Networks {
			inspected, err := cli.NetworkInspect(ctx, desired.Name, client.NetworkInspectOptions{})
			if err != nil {
				return docker.WrapError("inspect network for default_host_ip", desired.Name, err)
			}
			if inspected.Network.Driver == "bridge" {
				if value := inspected.Network.Options["com.docker.network.bridge.host_binding_ipv4"]; value != "" {
					defaultIP = value
					break
				}
			}
		}
	}
	if defaultIP == "" {
		return nil
	}
	address, err := netip.ParseAddr(defaultIP)
	if err != nil {
		return fmt.Errorf("invalid default_host_ip %q: %w", defaultIP, err)
	}
	for port, bindings := range host.PortBindings {
		for index := range bindings {
			if !bindings[index].HostIP.IsValid() {
				bindings[index].HostIP = address
			}
		}
		host.PortBindings[port] = bindings
	}
	return nil
}

func waitForRemoval(ctx context.Context, cli client.APIClient, id string, timeout *float64, clock docker.Clock) error {
	started := clock.Now()
	for {
		_, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if docker.IsNotFoundError(err) {
			return nil
		}
		if err != nil {
			return docker.WrapError("wait for container removal", id, err)
		}
		if timeout != nil && clock.Now().Sub(started) >= time.Duration(*timeout*float64(time.Second)) {
			return fmt.Errorf("timeout of %g seconds exceeded while waiting for container %q to be removed", *timeout, id)
		}
		clock.Sleep(100 * time.Millisecond)
	}
}

func createContainer(ctx context.Context, cli client.APIClient, req Request, config *container.Config, hostConfig *container.HostConfig) Response {
	options := client.ContainerCreateOptions{Config: config, HostConfig: hostConfig, Name: req.Name}
	if req.resolvedPlatform != nil {
		options.Platform = req.resolvedPlatform
	}
	if boolValue(req.NetworksCLICompatible) && len(req.Networks) > 0 {
		options.NetworkingConfig = &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		for _, desired := range req.Networks {
			settings, err := endpointSettings(desired)
			if err != nil {
				return Response{Failed: true, Msg: err.Error()}
			}
			options.NetworkingConfig.EndpointsConfig[desired.Name] = settings
		}
	}
	if req.MacAddress != "" {
		if options.NetworkingConfig == nil {
			options.NetworkingConfig = &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		}
		primary := primaryNetworkName(req)
		if primary == "host" || primary == "none" || strings.HasPrefix(primary, "container:") {
			return Response{Failed: true, Msg: fmt.Sprintf("mac_address cannot be used with network_mode %q", req.NetworkMode)}
		}
		settings := options.NetworkingConfig.EndpointsConfig[primary]
		if settings == nil {
			settings = &network.EndpointSettings{}
			options.NetworkingConfig.EndpointsConfig[primary] = settings
		}
		address, parseErr := net.ParseMAC(strings.ReplaceAll(req.MacAddress, "-", ":"))
		if parseErr != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid mac_address %q: %v", req.MacAddress, parseErr)}
		}
		settings.MacAddress = network.HardwareAddr(address)
	}
	created, err := cli.ContainerCreate(ctx, options)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create container", req.Name, err).Error()}
	}
	if req.Networks != nil {
		inspected, inspectErr := cli.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
			return Response{Failed: true, Msg: docker.WrapError("inspect created container networks", created.ID, inspectErr).Error()}
		}
		connect, disconnect, differenceErr := networkDifferencesForCreate(req, inspected.Container, docker.NewDiffBuilder())
		if differenceErr != nil {
			_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
			return Response{Failed: true, Msg: differenceErr.Error()}
		}
		if response := applyNetworkChanges(ctx, cli, created.ID, connect, disconnect); response.Failed {
			_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
			return response
		}
	}
	return Response{
		Changed:   true,
		Msg:       "container created",
		Container: map[string]interface{}{"Id": created.ID},
		Warnings:  dockerWarnings(created.Warnings),
	}
}

func updateContainer(ctx context.Context, cli client.APIClient, id string, req Request, desired *container.HostConfig) Response {
	options := client.ContainerUpdateOptions{Resources: &container.Resources{}}
	resources := options.Resources
	if req.BlkioWeight != nil {
		resources.BlkioWeight = desired.BlkioWeight
	}
	if req.CPUPeriod != nil {
		resources.CPUPeriod = desired.CPUPeriod
	}
	if req.CPUQuota != nil {
		resources.CPUQuota = desired.CPUQuota
	}
	if req.CPUShares != nil {
		resources.CPUShares = desired.CPUShares
	}
	if req.argumentProvided("cpuset_cpus", req.CPUSetCPUs != "") {
		resources.CpusetCpus = desired.CpusetCpus
	}
	if req.argumentProvided("cpuset_mems", req.CPUSetMems != "") {
		resources.CpusetMems = desired.CpusetMems
	}
	if req.Memory != nil {
		resources.Memory = desired.Memory
	}
	if req.MemoryReservation != nil {
		resources.MemoryReservation = desired.MemoryReservation
	}
	if req.MemorySwap != nil {
		resources.MemorySwap = desired.MemorySwap
	}
	if req.RestartPolicy != "" {
		options.RestartPolicy = &desired.RestartPolicy
	}
	updated, err := cli.ContainerUpdate(ctx, id, options)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("update container", id, err).Error()}
	}
	return Response{Changed: true, Warnings: dockerWarnings(updated.Warnings)}
}

func dockerWarnings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = "Docker warning: " + value
	}
	return result
}

func applyNetworkChanges(ctx context.Context, cli client.APIClient, id string, connect []Network, disconnect []string) Response {
	for _, name := range disconnect {
		if _, err := cli.NetworkDisconnect(ctx, name, client.NetworkDisconnectOptions{Container: id}); err != nil && !docker.IsNotFoundError(err) {
			return Response{Failed: true, Msg: docker.WrapError("disconnect network", name, err).Error()}
		}
	}
	for _, desired := range connect {
		settings, err := endpointSettings(desired)
		if err != nil {
			return Response{Failed: true, Msg: err.Error()}
		}
		// Reconnecting is required when endpoint settings differ.
		_, _ = cli.NetworkDisconnect(ctx, desired.Name, client.NetworkDisconnectOptions{Container: id, Force: true})
		if _, err := cli.NetworkConnect(ctx, desired.Name, client.NetworkConnectOptions{Container: id, EndpointConfig: settings}); err != nil {
			return Response{Failed: true, Msg: docker.WrapError("connect network", desired.Name, err).Error()}
		}
	}
	return Response{Changed: len(connect)+len(disconnect) > 0}
}

func needsLifecycleChange(req Request, existing container.InspectResponse) string {
	if existing.State == nil {
		return ""
	}
	switch {
	case (req.State == "started" || req.State == "healthy") && !existing.State.Running:
		return "started"
	case (req.State == "started" || req.State == "healthy") && req.Restart:
		return "restarted"
	case req.State == "stopped" && existing.State.Running:
		return "stopped"
	case req.Paused != nil && existing.State.Paused != *req.Paused:
		if *req.Paused {
			return "paused"
		}
		return "unpaused"
	default:
		return ""
	}
}

func reconcileLifecycle(ctx context.Context, cli client.APIClient, req Request, id string, created bool, clock docker.Clock) Response {
	inspect, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("inspect container", id, err).Error()}
	}
	changed := created
	if req.State == "started" || req.State == "healthy" {
		if inspect.Container.State == nil || !inspect.Container.State.Running {
			if _, err := cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
				return Response{Failed: true, Msg: docker.WrapError("start container", id, err).Error()}
			}
			changed = true
		} else if req.Restart && !created {
			if _, err := cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{Timeout: req.StopTimeout}); err != nil {
				return Response{Failed: true, Msg: docker.WrapError("restart container", id, err).Error()}
			}
			changed = true
		}
	} else if req.State == "stopped" && inspect.Container.State != nil && inspect.Container.State.Running {
		if response := stopContainer(ctx, cli, req, id); response.Failed {
			return response
		}
		changed = true
	}

	latest, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("inspect container", id, err).Error()}
	}
	if (req.State == "started" || req.State == "healthy") && req.Paused != nil && latest.Container.State != nil && latest.Container.State.Paused != *req.Paused {
		if *req.Paused {
			_, err = cli.ContainerPause(ctx, id, client.ContainerPauseOptions{})
		} else {
			_, err = cli.ContainerUnpause(ctx, id, client.ContainerUnpauseOptions{})
		}
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("set paused state", id, err).Error()}
		}
		changed = true
	}
	if req.State == "healthy" {
		healthy, waitErr := waitForHealthy(ctx, cli, id, req.HealthyWaitTimeout, clock)
		if waitErr != nil {
			return Response{Failed: true, Msg: waitErr.Error(), Container: healthy}
		}
	}
	if boolValue(req.Detach) || (req.Detach == nil && req.ContainerDefaultBehavior != "compatibility") || req.State == "present" || req.State == "stopped" {
		latest, _ = cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		return Response{Changed: changed, Container: convertContainer(latest.Container)}
	}
	return waitForExit(ctx, cli, req, id, changed)
}

func stopContainer(ctx context.Context, cli client.APIClient, req Request, id string) Response {
	if inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err == nil && inspected.Container.State != nil && inspected.Container.State.Paused {
		if _, err := cli.ContainerUnpause(ctx, id, client.ContainerUnpauseOptions{}); err != nil {
			return Response{Failed: true, Msg: docker.WrapError("unpause container before stopping", id, err).Error()}
		}
	}
	if req.ForceKill {
		signal := req.KillSignal
		if signal == "" {
			signal = "SIGKILL"
		}
		if _, err := cli.ContainerKill(ctx, id, client.ContainerKillOptions{Signal: signal}); err != nil {
			return Response{Failed: true, Msg: docker.WrapError("kill container", id, err).Error()}
		}
	} else if _, err := cli.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: req.StopTimeout}); err != nil {
		return Response{Failed: true, Msg: docker.WrapError("stop container", id, err).Error()}
	}
	return Response{Changed: true}
}

func removeContainer(ctx context.Context, cli client.APIClient, req Request, existing container.InspectResponse) Response {
	if existing.State != nil && existing.State.Paused {
		_, _ = cli.ContainerUnpause(ctx, existing.ID, client.ContainerUnpauseOptions{})
	}
	if existing.State != nil && existing.State.Running {
		if response := stopContainer(ctx, cli, req, existing.ID); response.Failed {
			return response
		}
	}
	removeVolumes := !boolValue(req.KeepVolumes)
	if _, err := cli.ContainerRemove(ctx, existing.ID, client.ContainerRemoveOptions{Force: req.ForceKill, RemoveVolumes: removeVolumes}); err != nil {
		message := err.Error()
		alreadyRemoving := strings.Contains(message, "removal of container ") && strings.Contains(message, " is already in progress")
		if !docker.IsNotFoundError(err) && !alreadyRemoving {
			return Response{Failed: true, Msg: docker.WrapError("remove container", existing.ID, err).Error()}
		}
	}
	return Response{Changed: true, Msg: "container removed"}
}

func waitForHealthy(ctx context.Context, cli client.APIClient, id string, timeout *float64, clock docker.Clock) (map[string]interface{}, error) {
	started := clock.Now()
	for {
		inspect, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return nil, docker.WrapError("inspect container health", id, err)
		}
		if inspect.Container.State == nil || inspect.Container.State.Health == nil || inspect.Container.State.Health.Status == "healthy" {
			return convertContainer(inspect.Container), nil
		}
		if status := inspect.Container.State.Health.Status; status != "starting" && status != "unhealthy" {
			return convertContainer(inspect.Container), fmt.Errorf("encountered unexpected health state %q while waiting for container %q", status, id)
		}
		if timeout != nil && clock.Now().Sub(started) >= time.Duration(*timeout*float64(time.Second)) {
			timeoutText := strconv.FormatFloat(*timeout, 'f', -1, 64)
			if !strings.Contains(timeoutText, ".") {
				timeoutText += ".0"
			}
			return convertContainer(inspect.Container), fmt.Errorf("Timeout of %s seconds exceeded while waiting for container %q", timeoutText, id)
		}
		clock.Sleep(time.Second)
	}
}

func waitForExit(ctx context.Context, cli client.APIClient, req Request, id string, changed bool) Response {
	wait := cli.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var result container.WaitResponse
	select {
	case err := <-wait.Error:
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("wait for container", id, err).Error()}
		}
	case result = <-wait.Result:
	}
	status := result.StatusCode
	output := ""
	realOutput := false
	if !boolValue(req.AutoRemove) {
		loggingDriver := ""
		if inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err == nil && inspected.Container.HostConfig != nil {
			loggingDriver = inspected.Container.HostConfig.LogConfig.Type
		}
		if loggingDriver != "" && loggingDriver != "json-file" && loggingDriver != "journald" && loggingDriver != "local" {
			output = fmt.Sprintf("Result logged using `%s` driver", loggingDriver)
		} else {
			logs, err := cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
			if err == nil {
				output = readContainerLogs(logs, boolValue(req.TTY))
				realOutput = true
				_ = logs.Close()
			}
		}
	} else {
		output = "Cannot retrieve result as auto_remove is enabled"
	}
	if req.Cleanup {
		_, _ = cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: !boolValue(req.KeepVolumes)})
	}
	response := Response{Changed: changed, Status: &status}
	if req.OutputLogs && realOutput {
		response.Stdout = output
	}
	if inspect, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err == nil {
		response.Container = convertContainer(inspect.Container)
	} else {
		response.Container = map[string]interface{}{"Output": output}
	}
	response.Container["Output"] = output
	if status != 0 {
		response.Failed = true
		response.Msg = output
	}
	return response
}

func readContainerLogs(reader io.Reader, tty bool) string {
	if tty {
		data, _ := io.ReadAll(reader)
		return string(data)
	}
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		data, _ := io.ReadAll(reader)
		return string(data)
	}
	return stdout.String() + stderr.String()
}

func lifecycleDiffField(action string) string {
	if action == "paused" || action == "unpaused" {
		return "paused"
	}
	if action == "restarted" {
		return "restarted"
	}
	return "running"
}
func lifecycleDiffDesired(action string) bool {
	return action == "started" || action == "restarted" || action == "paused"
}
func lifecycleDiffCurrent(action string) bool { return !lifecycleDiffDesired(action) }

func effectiveKillSignal(req Request) string {
	if req.KillSignal != "" {
		return req.KillSignal
	}
	return "SIGKILL"
}

func removalAction(id string, req Request) map[string]any {
	return map[string]any{
		"removed":      id,
		"volume_state": !boolValue(req.KeepVolumes),
		"link":         false,
		"force":        req.ForceKill,
	}
}

func lifecycleAction(action, id string, req Request) map[string]any {
	switch action {
	case "paused":
		return map[string]any{"set_paused": true}
	case "unpaused":
		return map[string]any{"set_paused": false}
	case "restarted":
		return map[string]any{"restarted": id, "timeout": req.StopTimeout}
	case "stopped":
		if req.ForceKill {
			return map[string]any{"killed": id, "signal": effectiveKillSignal(req)}
		}
		return map[string]any{"stopped": id, "timeout": req.StopTimeout}
	default:
		return map[string]any{"started": id}
	}
}

func convertContainer(value container.InspectResponse) map[string]interface{} {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{})
	if err := json.Unmarshal(encoded, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func resolvePlatform(ctx context.Context, cli client.APIClient, value string) (*ocispec.Platform, error) {
	parts := strings.Split(value, "/")
	daemonOS, daemonArch := "", ""
	if len(parts) == 1 {
		info, err := cli.Info(ctx, client.InfoOptions{})
		if err != nil {
			return nil, docker.WrapError("inspect Docker daemon platform", "", err)
		}
		daemonOS, daemonArch = info.Info.OSType, info.Info.Architecture
	}
	return parsePlatform(value, daemonOS, daemonArch)
}

func parsePlatform(value, daemonOS, daemonArch string) (*ocispec.Platform, error) {
	parts := strings.Split(value, "/")
	if len(parts) == 0 || len(parts) > 3 {
		return nil, fmt.Errorf("invalid platform %q; expected os[/architecture[/variant]]", value)
	}
	for _, part := range parts {
		if part == "" || !isPlatformPart(part) {
			return nil, fmt.Errorf("invalid platform %q", value)
		}
	}
	if len(parts) == 1 {
		part := strings.ToLower(parts[0])
		if normalizedOS, found := normalizePlatformOS(part); found {
			architecture, variant := normalizePlatformArch(daemonArch, "")
			return &ocispec.Platform{OS: normalizedOS, Architecture: architecture, Variant: variant}, nil
		}
		architecture, variant := normalizePlatformArch(part, "")
		if !knownPlatformArch(architecture) {
			return nil, fmt.Errorf("invalid platform %q: unknown OS or architecture", value)
		}
		operatingSystem, _ := normalizePlatformOS(daemonOS)
		return &ocispec.Platform{OS: operatingSystem, Architecture: architecture, Variant: variant}, nil
	}
	operatingSystem, _ := normalizePlatformOS(parts[0])
	if operatingSystem == "" {
		operatingSystem = strings.ToLower(parts[0])
	}
	variant := ""
	if len(parts) == 3 {
		variant = strings.ToLower(parts[2])
	}
	architecture, variant := normalizePlatformArch(parts[1], variant)
	if len(parts) == 2 && architecture == "arm" && variant == "v7" {
		variant = ""
	}
	return &ocispec.Platform{OS: operatingSystem, Architecture: architecture, Variant: variant}, nil
}

func isImageID(value string) bool {
	value = strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func primaryNetworkName(req Request) string {
	if len(req.Networks) > 0 && boolValue(req.NetworksCLICompatible) {
		return req.Networks[0].Name
	}
	mode := normalizeNetworkMode(req.NetworkMode)
	if mode == "host" || mode == "none" || strings.HasPrefix(mode, "container:") {
		return mode
	}
	return mode
}

func isPlatformPart(value string) bool {
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func normalizePlatformOS(value string) (string, bool) {
	value = strings.ToLower(value)
	if value == "macos" {
		value = "darwin"
	}
	known := map[string]bool{"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true, "illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true, "plan9": true, "solaris": true, "windows": true, "zos": true}
	return value, known[value]
}

func normalizePlatformArch(architecture, variant string) (string, string) {
	architecture = strings.ToLower(architecture)
	variant = strings.ToLower(variant)
	switch architecture {
	case "i386":
		architecture = "386"
	case "x86_64", "x86-64":
		architecture = "amd64"
	case "aarch64":
		architecture = "arm64"
	case "armhf":
		architecture, variant = "arm", "v7"
	case "armel":
		architecture, variant = "arm", "v6"
	}
	if architecture == "arm64" && (variant == "8" || variant == "v8") {
		variant = ""
	}
	if architecture == "arm" && variant != "" && !strings.HasPrefix(variant, "v") {
		variant = "v" + variant
	}
	return architecture, variant
}

func knownPlatformArch(value string) bool {
	known := map[string]bool{"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true, "arm64be": true, "ppc64": true, "ppc64le": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "mips64p32": true, "mips64p32le": true, "ppc": true, "riscv": true, "riscv64": true, "s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true}
	return known[value]
}
