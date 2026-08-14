package docker_swarm_service

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
)

type serviceState struct {
	image                 string
	command               []string
	args                  []string
	endpointMode          *string
	dns                   []string
	dnsSearch             []string
	dnsOptions            []string
	healthcheck           map[string]any
	healthcheckDisabled   bool
	healthcheckSpecified  bool
	hostname              *string
	hosts                 map[string]string
	tty                   *bool
	env                   []string
	forceUpdate           uint64
	groups                []string
	logDriver             *string
	logDriverOptions      map[string]string
	labels                map[string]string
	containerLabels       map[string]string
	sysctls               map[string]string
	limitCPU              *float64
	limitMemory           *int64
	reserveCPU            *float64
	reserveMemory         *int64
	mode                  string
	user                  *string
	mounts                []map[string]any
	configs               []map[string]any
	secrets               []map[string]any
	constraints           []string
	replicasMaxPerNode    *uint64
	networks              []map[string]any
	stopGracePeriod       *int64
	stopSignal            *string
	publish               []map[string]any
	placementPreferences  []map[string]any
	replicas              *uint64
	readOnly              *bool
	restartPolicy         *string
	restartPolicyAttempts *uint64
	restartPolicyDelay    *int64
	restartPolicyWindow   *int64
	rollbackConfig        map[string]any
	updateDelay           *int64
	updateParallelism     *uint64
	updateFailureAction   *string
	updateMonitor         *int64
	updateMaxFailureRatio *float32
	updateOrder           *string
	workingDir            *string
	init                  *bool
	capAdd                []string
	capDrop               []string
	serviceID             string
	serviceVersion        uint64
}

type comparisonResult struct {
	changed      bool
	needsRebuild bool
	forceUpdate  bool
	changes      []string
	before       map[string]any
	after        map[string]any
}

func (state *serviceState) facts() map[string]any {
	return map[string]any{
		"image":                    state.image,
		"mounts":                   anyOrNil(state.mounts),
		"configs":                  anyOrNil(state.configs),
		"networks":                 anyOrNil(state.networks),
		"command":                  anyOrNil(state.command),
		"args":                     anyOrNil(state.args),
		"tty":                      state.tty,
		"dns":                      anyOrNil(state.dns),
		"dns_search":               anyOrNil(state.dnsSearch),
		"dns_options":              anyOrNil(state.dnsOptions),
		"healthcheck":              state.healthcheck,
		"healthcheck_disabled":     nilIfFalse(state.healthcheckDisabled),
		"hostname":                 state.hostname,
		"hosts":                    mapOrNil(state.hosts),
		"env":                      anyOrNil(state.env),
		"force_update":             nilIfZero(state.forceUpdate),
		"groups":                   anyOrNil(state.groups),
		"log_driver":               state.logDriver,
		"log_driver_options":       mapOrNil(state.logDriverOptions),
		"publish":                  anyOrNil(state.publish),
		"constraints":              anyOrNil(state.constraints),
		"replicas_max_per_node":    state.replicasMaxPerNode,
		"placement_preferences":    anyOrNil(state.placementPreferences),
		"labels":                   mapOrNil(state.labels),
		"container_labels":         mapOrNil(state.containerLabels),
		"sysctls":                  mapOrNil(state.sysctls),
		"mode":                     state.mode,
		"replicas":                 state.replicas,
		"endpoint_mode":            state.endpointMode,
		"restart_policy":           state.restartPolicy,
		"secrets":                  anyOrNil(state.secrets),
		"stop_grace_period":        state.stopGracePeriod,
		"stop_signal":              state.stopSignal,
		"limit_cpu":                state.limitCPU,
		"limit_memory":             state.limitMemory,
		"read_only":                state.readOnly,
		"reserve_cpu":              state.reserveCPU,
		"reserve_memory":           state.reserveMemory,
		"restart_policy_delay":     state.restartPolicyDelay,
		"restart_policy_attempts":  state.restartPolicyAttempts,
		"restart_policy_window":    state.restartPolicyWindow,
		"rollback_config":          state.rollbackConfig,
		"update_delay":             state.updateDelay,
		"update_parallelism":       state.updateParallelism,
		"update_failure_action":    state.updateFailureAction,
		"update_monitor":           state.updateMonitor,
		"update_max_failure_ratio": state.updateMaxFailureRatio,
		"update_order":             state.updateOrder,
		"user":                     state.user,
		"working_dir":              state.workingDir,
		"init":                     state.init,
		"cap_add":                  anyOrNil(state.capAdd),
		"cap_drop":                 anyOrNil(state.capDrop),
	}
}

func desiredService(req Request, image string, old *serviceState, secretIDs, configIDs, networkIDs map[string]string, forceUpdate uint64, readFile func(string) ([]byte, error)) (*serviceState, error) {
	state := &serviceState{image: image, mode: "replicated"}
	if req.Mode != "" {
		state.mode = req.Mode
	}
	command, err := parseCommand(req.Command)
	if err != nil {
		return nil, err
	}
	state.command = command
	state.args = req.Args
	if req.EndpointMode != "" {
		state.endpointMode = stringPtr(req.EndpointMode)
	}
	state.dns = req.DNS
	state.dnsSearch = req.DNSSearch
	state.dnsOptions = req.DNSOptions
	if req.Healthcheck != nil {
		state.healthcheckSpecified = true
		healthcheck, disabled, err := parseHealthcheck(req.Healthcheck)
		if err != nil {
			return nil, err
		}
		state.healthcheck = healthcheck
		state.healthcheckDisabled = disabled
	}
	state.hostname = req.Hostname
	if req.Hosts != nil {
		state.hosts = map[string]string(req.Hosts)
	}
	state.tty = req.TTY
	env, err := getDockerEnvironment(req.Env, req.EnvFiles, readFile)
	if err != nil {
		return nil, err
	}
	state.env = env
	if req.Labels != nil {
		state.labels = map[string]string(req.Labels)
	}
	if req.ContainerLabels != nil {
		state.containerLabels = map[string]string(req.ContainerLabels)
	}
	if req.Sysctls != nil {
		state.sysctls = map[string]string(req.Sysctls)
	}
	state.stopSignal = req.StopSignal
	state.user = req.User
	state.workingDir = req.WorkingDir
	state.readOnly = req.ReadOnly
	state.init = req.Init
	state.capAdd = normalizeCapabilityAdd(req.CapAdd)
	state.capDrop = normalizeCapabilityDrop(req.CapDrop)
	if req.ForceUpdate {
		state.forceUpdate = forceUpdate
	}
	if req.Groups != nil {
		groups := make([]string, 0, len(req.Groups))
		for _, group := range req.Groups {
			groups = append(groups, fmt.Sprint(group))
		}
		state.groups = groups
	}
	if req.Networks != nil {
		networks, err := getDockerNetworks(req.Networks, networkIDs)
		if err != nil {
			return nil, err
		}
		state.networks = networks
	}
	if err := applyNestedSpecs(state, req); err != nil {
		return nil, err
	}
	if req.StopGracePeriod != nil {
		state.stopGracePeriod = &req.StopGracePeriod.Nanoseconds
	}
	replicas, err := resolveReplicas(req.Replicas, old, state.mode)
	if err != nil {
		return nil, err
	}
	state.replicas = replicas
	if req.Publish != nil {
		publish := make([]map[string]any, 0, len(req.Publish))
		for _, port := range req.Publish {
			item := map[string]any{
				"protocol":       defaultString(port.Protocol, "tcp"),
				"mode":           pointerValue(port.Mode),
				"published_port": pointerValue(port.PublishedPort),
				"target_port":    port.TargetPort,
			}
			publish = append(publish, item)
		}
		state.publish = publish
	}
	if req.Mounts != nil {
		mounts, err := parseMounts(req.Mounts)
		if err != nil {
			return nil, err
		}
		state.mounts = mounts
	}
	if req.Configs != nil {
		configs, err := parseFileRefs(req.Configs, true, configIDs)
		if err != nil {
			return nil, err
		}
		state.configs = configs
	}
	if req.Secrets != nil {
		secrets, err := parseFileRefs(req.Secrets, false, secretIDs)
		if err != nil {
			return nil, err
		}
		state.secrets = secrets
	}
	return state, nil
}

func applyNestedSpecs(state *serviceState, req Request) error {
	limits := req.Limits
	if limits == nil && (req.LimitCPU != nil || req.LimitMemory != nil) {
		limits = &ResourceSpec{CPUs: req.LimitCPU}
		if req.LimitMemory != nil {
			limits.Memory = &SizeValue{Bytes: *req.LimitMemory}
		}
	}
	if limits != nil {
		state.limitCPU = limits.CPUs
		if limits.Memory != nil {
			bytes := limits.Memory.Bytes
			state.limitMemory = &bytes
		}
	}
	if req.Reservations != nil {
		state.reserveCPU = req.Reservations.CPUs
		if req.Reservations.Memory != nil {
			bytes := req.Reservations.Memory.Bytes
			state.reserveMemory = &bytes
		}
	}
	placement := req.Placement
	if placement == nil && req.Constraint != nil {
		placement = &PlacementSpec{Constraints: req.Constraint}
	}
	if placement != nil {
		state.constraints = placement.Constraints
		state.placementPreferences = placement.Preferences
		state.replicasMaxPerNode = placement.ReplicasMaxPerNode
	}
	restart := req.RestartConfig
	if restart == nil && req.RestartPolicy != "" {
		restart = &RestartSpec{Condition: stringPtr(req.RestartPolicy)}
	}
	if restart != nil {
		state.restartPolicy = restart.Condition
		if restart.Delay != nil {
			state.restartPolicyDelay = &restart.Delay.Nanoseconds
		}
		state.restartPolicyAttempts = restart.MaxAttempts
		if restart.Window != nil {
			state.restartPolicyWindow = &restart.Window.Nanoseconds
		}
	}
	update := req.UpdateConfig
	if update == nil && hasFlatUpdate(req) {
		update = flatUpdateSpec(req)
	}
	if update != nil {
		state.updateParallelism = update.Parallelism
		if update.Delay != nil {
			state.updateDelay = &update.Delay.Nanoseconds
		}
		state.updateFailureAction = update.FailureAction
		if update.Monitor != nil {
			state.updateMonitor = &update.Monitor.Nanoseconds
		}
		state.updateMaxFailureRatio = update.MaxFailureRatio
		state.updateOrder = update.Order
	}
	rollback := req.RollbackConfig
	if rollback == nil && hasFlatRollback(req) {
		rollback = flatRollbackSpec(req)
	}
	if rollback != nil {
		state.rollbackConfig = rollbackMap(rollback)
	}
	logging := req.Logging
	if logging != nil {
		state.logDriver = logging.Driver
		state.logDriverOptions = logging.Options
	}
	return nil
}

func hasFlatUpdate(req Request) bool {
	return req.UpdateDelay != "" || req.UpdateParallelism != nil || req.UpdateFailureAction != "" || req.UpdateOrder != "" || req.UpdateMonitor != "" || req.MaxFailureRatio != nil
}

func hasFlatRollback(req Request) bool {
	return req.RollbackDelay != "" || req.RollbackParallelism != nil || req.RollbackFailureAction != "" || req.RollbackOrder != "" || req.RollbackMonitor != "" || req.RollbackMaxFailureRatio != nil
}

func flatUpdateSpec(req Request) *UpdateSpec {
	spec := &UpdateSpec{
		Parallelism:     req.UpdateParallelism,
		FailureAction:   nilIfEmpty(req.UpdateFailureAction),
		Order:           nilIfEmpty(req.UpdateOrder),
		MaxFailureRatio: req.MaxFailureRatio,
	}
	if req.UpdateDelay != "" {
		spec.Delay = &DurationValue{}
		if parsed, err := nanosecondsFromRaw("update_delay", req.UpdateDelay); err == nil && parsed != nil {
			spec.Delay.Nanoseconds = *parsed
		}
	}
	if req.UpdateMonitor != "" {
		spec.Monitor = &DurationValue{}
		if parsed, err := nanosecondsFromRaw("update_monitor", req.UpdateMonitor); err == nil && parsed != nil {
			spec.Monitor.Nanoseconds = *parsed
		}
	}
	return spec
}

func flatRollbackSpec(req Request) *UpdateSpec {
	spec := &UpdateSpec{
		Parallelism:     req.RollbackParallelism,
		FailureAction:   nilIfEmpty(req.RollbackFailureAction),
		Order:           nilIfEmpty(req.RollbackOrder),
		MaxFailureRatio: req.RollbackMaxFailureRatio,
	}
	if req.RollbackDelay != "" {
		spec.Delay = &DurationValue{}
		if parsed, err := nanosecondsFromRaw("rollback_delay", req.RollbackDelay); err == nil && parsed != nil {
			spec.Delay.Nanoseconds = *parsed
		}
	}
	if req.RollbackMonitor != "" {
		spec.Monitor = &DurationValue{}
		if parsed, err := nanosecondsFromRaw("rollback_monitor", req.RollbackMonitor); err == nil && parsed != nil {
			spec.Monitor.Nanoseconds = *parsed
		}
	}
	return spec
}

func rollbackMap(spec *UpdateSpec) map[string]any {
	result := map[string]any{}
	if spec.Parallelism != nil {
		result["parallelism"] = *spec.Parallelism
	} else {
		result["parallelism"] = nil
	}
	if spec.Delay != nil {
		result["delay"] = spec.Delay.Nanoseconds
	} else {
		result["delay"] = nil
	}
	if spec.FailureAction != nil {
		result["failure_action"] = *spec.FailureAction
	} else {
		result["failure_action"] = nil
	}
	if spec.Monitor != nil {
		result["monitor"] = spec.Monitor.Nanoseconds
	} else {
		result["monitor"] = nil
	}
	if spec.MaxFailureRatio != nil {
		result["max_failure_ratio"] = *spec.MaxFailureRatio
	} else {
		result["max_failure_ratio"] = nil
	}
	if spec.Order != nil {
		result["order"] = *spec.Order
	} else {
		result["order"] = nil
	}
	return result
}

func resolveReplicas(value *int64, old *serviceState, mode string) (*uint64, error) {
	if mode == "global" {
		return nil, nil
	}
	if value == nil || *value == -1 {
		if old != nil {
			return old.replicas, nil
		}
		one := uint64(1)
		return &one, nil
	}
	if *value < 0 {
		return nil, fmt.Errorf("replicas must be >= 0")
	}
	parsed := uint64(*value)
	return &parsed, nil
}

func parseMounts(mounts []MountSpec) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(mounts))
	for _, item := range mounts {
		mountType := item.Type
		if mountType == "" {
			mountType = "bind"
		}
		source := ""
		if item.Source != nil {
			source = *item.Source
		}
		if item.Source == nil && mountType != "tmpfs" {
			return nil, fmt.Errorf("Source must be specified for mounts which are not of type tmpfs")
		}
		readonly := item.Readonly
		if readonly == nil {
			readonly = item.ReadOnly
		}
		noCopy := item.NoCopy
		if noCopy == nil {
			noCopy = item.VolumeNoCopy
		}
		propagation := item.Propagation
		if propagation == nil && item.BindPropagation != "" {
			propagation = stringPtr(item.BindPropagation)
		}
		labels := item.Labels
		if labels == nil {
			labels = item.VolumeLabels
		}
		driver := item.DriverConfig
		if driver == nil && (item.VolumeDriver != "" || len(item.VolumeOptions) > 0) {
			driver = &MountDriver{Name: item.VolumeDriver, Options: item.VolumeOptions}
		}
		var tmpfsSize any
		if item.TmpfsSize != nil {
			tmpfsSize = item.TmpfsSize.Bytes
		}
		result = append(result, map[string]any{
			"readonly":      readonly,
			"type":          mountType,
			"source":        source,
			"target":        item.Target,
			"labels":        labels,
			"no_copy":       noCopy,
			"propagation":   propagation,
			"driver_config": driverMap(driver),
			"tmpfs_mode":    parseMode(item.TmpfsMode),
			"tmpfs_size":    tmpfsSize,
		})
	}
	return result, nil
}

func driverMap(driver *MountDriver) any {
	if driver == nil {
		return nil
	}
	return map[string]any{"name": driver.Name, "options": driver.Options}
}

func parseFileRefs(refs []FileReference, configs bool, ids map[string]string) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		name := ref.ConfigName
		id := ref.ConfigID
		key := "config"
		if !configs {
			name = ref.SecretName
			id = ref.SecretID
			key = "secret"
		}
		if id == "" {
			resolved, found := ids[name]
			if !found {
				return nil, fmt.Errorf("Could not find a %s named %q", key, name)
			}
			id = resolved
		}
		filename := ref.Filename
		if filename == "" {
			filename = name
		}
		result = append(result, map[string]any{
			key + "_id":   id,
			key + "_name": name,
			"filename":    filename,
			"uid":         stringifyScalar(ref.UID),
			"gid":         stringifyScalar(ref.GID),
			"mode":        parseMode(ref.Mode),
		})
	}
	return result, nil
}

func stringifyScalar(value any) any {
	if value == nil {
		return nil
	}
	return fmt.Sprint(value)
}

func parseMode(value any) any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case float64:
		return uint32(typed)
	case int:
		return uint32(typed)
	case int64:
		return uint32(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return uint32(parsed)
	case string:
		parsed, err := strconv.ParseUint(typed, 8, 32)
		if err != nil {
			parsed, err = strconv.ParseUint(typed, 10, 32)
			if err != nil {
				return typed
			}
		}
		return uint32(parsed)
	default:
		return value
	}
}

func serviceFromInspect(service swarm.Service) (*serviceState, error) {
	state := &serviceState{
		serviceID:      service.ID,
		serviceVersion: service.Version.Index,
		mode:           "replicated",
	}
	spec := service.Spec
	containerSpec := spec.TaskTemplate.ContainerSpec
	if containerSpec == nil {
		containerSpec = &swarm.ContainerSpec{}
	}
	state.image = containerSpec.Image
	if containerSpec.User != "" {
		state.user = stringPtr(containerSpec.User)
	}
	state.env = containerSpec.Env
	state.command = containerSpec.Command
	state.args = containerSpec.Args
	state.groups = containerSpec.Groups
	if containerSpec.StopGracePeriod != nil {
		nanos := int64(*containerSpec.StopGracePeriod)
		state.stopGracePeriod = &nanos
	}
	if containerSpec.StopSignal != "" {
		state.stopSignal = stringPtr(containerSpec.StopSignal)
	}
	if containerSpec.Dir != "" {
		state.workingDir = stringPtr(containerSpec.Dir)
	}
	state.readOnly = boolPtr(containerSpec.ReadOnly)
	if !containerSpec.ReadOnly {
		state.readOnly = nil
	}
	state.capAdd = normalizeCapabilityAdd(containerSpec.CapabilityAdd)
	state.capDrop = normalizeCapabilityDrop(containerSpec.CapabilityDrop)
	state.sysctls = containerSpec.Sysctls
	if containerSpec.Healthcheck != nil {
		state.healthcheck = healthcheckFacts(containerSpec.Healthcheck)
	}
	if spec.UpdateConfig != nil {
		state.updateDelay = durationPtr(spec.UpdateConfig.Delay)
		state.updateParallelism = uint64Ptr(spec.UpdateConfig.Parallelism)
		if spec.UpdateConfig.FailureAction != "" {
			state.updateFailureAction = stringPtr(string(spec.UpdateConfig.FailureAction))
		}
		state.updateMonitor = durationPtr(spec.UpdateConfig.Monitor)
		state.updateMaxFailureRatio = float32Ptr(spec.UpdateConfig.MaxFailureRatio)
		if spec.UpdateConfig.Order != "" {
			state.updateOrder = stringPtr(string(spec.UpdateConfig.Order))
		}
	}
	if spec.RollbackConfig != nil {
		state.rollbackConfig = map[string]any{
			"parallelism":       spec.RollbackConfig.Parallelism,
			"delay":             spec.RollbackConfig.Delay.Nanoseconds(),
			"failure_action":    string(spec.RollbackConfig.FailureAction),
			"monitor":           spec.RollbackConfig.Monitor.Nanoseconds(),
			"max_failure_ratio": spec.RollbackConfig.MaxFailureRatio,
			"order":             string(spec.RollbackConfig.Order),
		}
	}
	if containerSpec.DNSConfig != nil {
		for _, addr := range containerSpec.DNSConfig.Nameservers {
			state.dns = append(state.dns, addr.String())
		}
		state.dnsSearch = containerSpec.DNSConfig.Search
		state.dnsOptions = containerSpec.DNSConfig.Options
	}
	if containerSpec.Hostname != "" {
		state.hostname = stringPtr(containerSpec.Hostname)
	}
	if len(containerSpec.Hosts) > 0 {
		state.hosts = parseInspectHosts(containerSpec.Hosts)
	}
	state.tty = boolPtr(containerSpec.TTY)
	if placement := spec.TaskTemplate.Placement; placement != nil {
		state.constraints = placement.Constraints
		if placement.MaxReplicas > 0 {
			state.replicasMaxPerNode = uint64Ptr(placement.MaxReplicas)
		}
		for _, preference := range placement.Preferences {
			if preference.Spread != nil {
				state.placementPreferences = append(state.placementPreferences, map[string]any{
					"spread": preference.Spread.SpreadDescriptor,
				})
			}
		}
	}
	if policy := spec.TaskTemplate.RestartPolicy; policy != nil {
		if policy.Condition != "" {
			state.restartPolicy = stringPtr(string(policy.Condition))
		}
		if policy.Delay != nil {
			state.restartPolicyDelay = durationPtr(*policy.Delay)
		}
		state.restartPolicyAttempts = policy.MaxAttempts
		if policy.Window != nil {
			state.restartPolicyWindow = durationPtr(*policy.Window)
		}
	}
	if spec.EndpointSpec != nil {
		if spec.EndpointSpec.Mode != "" {
			state.endpointMode = stringPtr(string(spec.EndpointSpec.Mode))
		}
		for _, port := range spec.EndpointSpec.Ports {
			var mode any
			if port.PublishMode != "" {
				mode = string(port.PublishMode)
			}
			state.publish = append(state.publish, map[string]any{
				"protocol":       string(port.Protocol),
				"mode":           mode,
				"published_port": port.PublishedPort,
				"target_port":    port.TargetPort,
			})
		}
	}
	if resources := spec.TaskTemplate.Resources; resources != nil {
		if resources.Limits != nil {
			if resources.Limits.NanoCPUs != 0 {
				cpu := float64(resources.Limits.NanoCPUs) / 1e9
				state.limitCPU = &cpu
			}
			if resources.Limits.MemoryBytes != 0 {
				state.limitMemory = &resources.Limits.MemoryBytes
			}
		}
		if resources.Reservations != nil {
			if resources.Reservations.NanoCPUs != 0 {
				cpu := float64(resources.Reservations.NanoCPUs) / 1e9
				state.reserveCPU = &cpu
			}
			if resources.Reservations.MemoryBytes != 0 {
				state.reserveMemory = &resources.Reservations.MemoryBytes
			}
		}
	}
	state.labels = spec.Labels
	if spec.TaskTemplate.LogDriver != nil {
		state.logDriver = stringPtr(spec.TaskTemplate.LogDriver.Name)
		state.logDriverOptions = spec.TaskTemplate.LogDriver.Options
	}
	state.containerLabels = containerSpec.Labels
	switch {
	case spec.Mode.Replicated != nil:
		state.mode = "replicated"
		state.replicas = spec.Mode.Replicated.Replicas
	case spec.Mode.Global != nil:
		state.mode = "global"
	case spec.Mode.ReplicatedJob != nil:
		state.mode = "replicated-job"
		state.replicas = spec.Mode.ReplicatedJob.MaxConcurrent
		if spec.Mode.ReplicatedJob.TotalCompletions != nil {
			state.replicas = spec.Mode.ReplicatedJob.TotalCompletions
		}
	default:
		return nil, fmt.Errorf("Unknown service mode: %#v", spec.Mode)
	}
	for _, item := range containerSpec.Mounts {
		state.mounts = append(state.mounts, inspectMount(item))
	}
	for _, item := range containerSpec.Configs {
		fileName, uid, gid, mode := "", "", "", os.FileMode(0)
		if item.File != nil {
			fileName, uid, gid, mode = item.File.Name, item.File.UID, item.File.GID, item.File.Mode
		}
		state.configs = append(state.configs, inspectFileRef(item.ConfigID, item.ConfigName, fileName, uid, gid, mode, true))
	}
	for _, item := range containerSpec.Secrets {
		fileName, uid, gid, mode := "", "", "", os.FileMode(0)
		if item.File != nil {
			fileName, uid, gid, mode = item.File.Name, item.File.UID, item.File.GID, item.File.Mode
		}
		state.secrets = append(state.secrets, inspectFileRef(item.SecretID, item.SecretName, fileName, uid, gid, mode, false))
	}
	for _, item := range spec.TaskTemplate.Networks {
		network := map[string]any{"id": item.Target}
		if item.Aliases != nil {
			network["aliases"] = item.Aliases
		}
		if item.DriverOpts != nil {
			network["options"] = item.DriverOpts
		}
		state.networks = append(state.networks, network)
	}
	if containerSpec.Init != nil {
		state.init = containerSpec.Init
	} else {
		state.init = boolPtr(false)
	}
	return state, nil
}

func healthcheckFacts(config *container.HealthConfig) map[string]any {
	result := map[string]any{}
	if len(config.Test) > 0 {
		result["test"] = config.Test
	}
	if config.Interval != 0 {
		result["interval"] = int64(config.Interval)
	}
	if config.Timeout != 0 {
		result["timeout"] = int64(config.Timeout)
	}
	if config.StartPeriod != 0 {
		result["start_period"] = int64(config.StartPeriod)
	}
	if config.Retries != 0 {
		result["retries"] = config.Retries
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseInspectHosts(hosts []string) map[string]string {
	result := map[string]string{}
	for _, host := range hosts {
		var ip, hostname string
		if strings.Contains(host, ":") {
			parts := strings.SplitN(host, ":", 2)
			hostname, ip = parts[0], parts[1]
		} else {
			parts := strings.SplitN(host, " ", 2)
			if len(parts) == 2 {
				ip, hostname = parts[0], parts[1]
			} else {
				continue
			}
		}
		result[hostname] = ip
	}
	return result
}

func inspectMount(item mount.Mount) map[string]any {
	var driver any
	if item.VolumeOptions != nil && item.VolumeOptions.DriverConfig != nil {
		driver = map[string]any{
			"name":    item.VolumeOptions.DriverConfig.Name,
			"options": item.VolumeOptions.DriverConfig.Options,
		}
	}
	var labels map[string]string
	var noCopy any
	if item.VolumeOptions != nil {
		labels = item.VolumeOptions.Labels
		noCopy = item.VolumeOptions.NoCopy
	}
	var propagation any
	if item.BindOptions != nil {
		propagation = string(item.BindOptions.Propagation)
	}
	var tmpfsMode any
	var tmpfsSize any
	if item.TmpfsOptions != nil {
		tmpfsSize = item.TmpfsOptions.SizeBytes
		tmpfsMode = uint32(item.TmpfsOptions.Mode)
	}
	return map[string]any{
		"source":        item.Source,
		"type":          string(item.Type),
		"target":        item.Target,
		"readonly":      item.ReadOnly,
		"propagation":   propagation,
		"no_copy":       noCopy,
		"labels":        labels,
		"driver_config": driver,
		"tmpfs_mode":    tmpfsMode,
		"tmpfs_size":    tmpfsSize,
	}
}

func inspectFileRef(id, name, filename, uid, gid string, mode os.FileMode, config bool) map[string]any {
	key := "config"
	if !config {
		key = "secret"
	}
	item := map[string]any{
		key + "_id":   id,
		key + "_name": name,
	}
	if filename != "" || uid != "" || gid != "" || mode != 0 {
		item["filename"] = filename
		item["uid"] = uid
		item["gid"] = gid
		item["mode"] = uint32(mode)
	}
	return item
}

func (state *serviceState) compare(old *serviceState) (comparisonResult, error) {
	result := comparisonResult{before: map[string]any{}, after: map[string]any{}}
	add := func(name string, desired, active any) {
		result.changes = append(result.changes, name)
		result.before[name] = active
		result.after[name] = desired
	}
	if state.endpointMode != nil && deref(state.endpointMode) != deref(old.endpointMode) {
		add("endpoint_mode", state.endpointMode, old.endpointMode)
	}
	changed, err := hasListChanged(toAnySlice(state.env), toAnySlice(old.env), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("env", state.env, old.env)
	}
	if state.logDriver != nil && deref(state.logDriver) != deref(old.logDriver) {
		add("log_driver", state.logDriver, old.logDriver)
	}
	if state.logDriverOptions != nil && !mapsEqual(state.logDriverOptions, old.logDriverOptions) {
		add("log_opt", state.logDriverOptions, old.logDriverOptions)
	}
	if state.mode != old.mode {
		result.needsRebuild = true
		add("mode", state.mode, old.mode)
	}
	changed, err = hasListChanged(toAnySlice(state.mounts), toAnySlice(old.mounts), true, "target")
	if err != nil {
		return result, err
	}
	if changed {
		add("mounts", state.mounts, old.mounts)
	}
	changed, err = hasListChanged(toAnySlice(state.configs), toAnySlice(old.configs), true, "config_name")
	if err != nil {
		return result, err
	}
	if changed {
		add("configs", state.configs, old.configs)
	}
	changed, err = hasListChanged(toAnySlice(state.secrets), toAnySlice(old.secrets), true, "secret_name")
	if err != nil {
		return result, err
	}
	if changed {
		add("secrets", state.secrets, old.secrets)
	}
	if haveNetworksChanged(state.networks, old.networks) {
		add("networks", state.networks, old.networks)
	}
	if !uintEqual(state.replicas, old.replicas) {
		add("replicas", state.replicas, old.replicas)
	}
	changed, err = hasListChanged(toAnySlice(state.command), toAnySlice(old.command), false, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("command", state.command, old.command)
	}
	changed, err = hasListChanged(toAnySlice(state.args), toAnySlice(old.args), false, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("args", state.args, old.args)
	}
	changed, err = hasListChanged(toAnySlice(state.constraints), toAnySlice(old.constraints), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("constraints", state.constraints, old.constraints)
	}
	if state.replicasMaxPerNode != nil && !uintEqual(state.replicasMaxPerNode, old.replicasMaxPerNode) {
		add("replicas_max_per_node", state.replicasMaxPerNode, old.replicasMaxPerNode)
	}
	changed, err = hasListChanged(toAnySlice(state.placementPreferences), toAnySlice(old.placementPreferences), false, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("placement_preferences", state.placementPreferences, old.placementPreferences)
	}
	changed, err = hasListChanged(toAnySlice(state.groups), toAnySlice(old.groups), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("groups", state.groups, old.groups)
	}
	if state.labels != nil && !mapsEqual(state.labels, old.labels) {
		add("labels", state.labels, old.labels)
	}
	if state.limitCPU != nil && !floatEqual(state.limitCPU, old.limitCPU) {
		add("limit_cpu", state.limitCPU, old.limitCPU)
	}
	if state.limitMemory != nil && !intEqual(state.limitMemory, old.limitMemory) {
		add("limit_memory", state.limitMemory, old.limitMemory)
	}
	if state.reserveCPU != nil && !floatEqual(state.reserveCPU, old.reserveCPU) {
		add("reserve_cpu", state.reserveCPU, old.reserveCPU)
	}
	if state.reserveMemory != nil && !intEqual(state.reserveMemory, old.reserveMemory) {
		add("reserve_memory", state.reserveMemory, old.reserveMemory)
	}
	if state.containerLabels != nil && !mapsEqual(state.containerLabels, old.containerLabels) {
		add("container_labels", state.containerLabels, old.containerLabels)
	}
	if state.sysctls != nil && !mapsEqual(state.sysctls, old.sysctls) {
		add("sysctls", state.sysctls, old.sysctls)
	}
	if state.stopSignal != nil && deref(state.stopSignal) != deref(old.stopSignal) {
		add("stop_signal", state.stopSignal, old.stopSignal)
	}
	if state.stopGracePeriod != nil && !intEqual(state.stopGracePeriod, old.stopGracePeriod) {
		add("stop_grace_period", state.stopGracePeriod, old.stopGracePeriod)
	}
	if hasPublishChanged(state.publish, old.publish) {
		add("publish", state.publish, old.publish)
	}
	if state.readOnly != nil && derefBool(state.readOnly) != derefBool(old.readOnly) {
		add("read_only", state.readOnly, old.readOnly)
	}
	if state.restartPolicy != nil && deref(state.restartPolicy) != deref(old.restartPolicy) {
		add("restart_policy", state.restartPolicy, old.restartPolicy)
	}
	if state.restartPolicyAttempts != nil && !uintEqual(state.restartPolicyAttempts, old.restartPolicyAttempts) {
		add("restart_policy_attempts", state.restartPolicyAttempts, old.restartPolicyAttempts)
	}
	if state.restartPolicyDelay != nil && !intEqual(state.restartPolicyDelay, old.restartPolicyDelay) {
		add("restart_policy_delay", state.restartPolicyDelay, old.restartPolicyDelay)
	}
	if state.restartPolicyWindow != nil && !intEqual(state.restartPolicyWindow, old.restartPolicyWindow) {
		add("restart_policy_window", state.restartPolicyWindow, old.restartPolicyWindow)
	}
	if hasDictChanged(state.rollbackConfig, old.rollbackConfig) {
		add("rollback_config", state.rollbackConfig, old.rollbackConfig)
	}
	if state.updateDelay != nil && !intEqual(state.updateDelay, old.updateDelay) {
		add("update_delay", state.updateDelay, old.updateDelay)
	}
	if state.updateParallelism != nil && !uintEqual(state.updateParallelism, old.updateParallelism) {
		add("update_parallelism", state.updateParallelism, old.updateParallelism)
	}
	if state.updateFailureAction != nil && deref(state.updateFailureAction) != deref(old.updateFailureAction) {
		add("update_failure_action", state.updateFailureAction, old.updateFailureAction)
	}
	if state.updateMonitor != nil && !intEqual(state.updateMonitor, old.updateMonitor) {
		add("update_monitor", state.updateMonitor, old.updateMonitor)
	}
	if state.updateMaxFailureRatio != nil && !float32Equal(state.updateMaxFailureRatio, old.updateMaxFailureRatio) {
		add("update_max_failure_ratio", state.updateMaxFailureRatio, old.updateMaxFailureRatio)
	}
	if state.updateOrder != nil && deref(state.updateOrder) != deref(old.updateOrder) {
		add("update_order", state.updateOrder, old.updateOrder)
	}
	if state.image != "" {
		if imageChanged, active := hasImageChanged(state.image, old.image); imageChanged {
			add("image", state.image, active)
		}
	}
	if state.user != nil && deref(state.user) != "" && deref(state.user) != deref(old.user) {
		add("user", state.user, old.user)
	}
	changed, err = hasListChanged(toAnySlice(state.dns), toAnySlice(old.dns), false, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("dns", state.dns, old.dns)
	}
	changed, err = hasListChanged(toAnySlice(state.dnsSearch), toAnySlice(old.dnsSearch), false, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("dns_search", state.dnsSearch, old.dnsSearch)
	}
	changed, err = hasListChanged(toAnySlice(state.dnsOptions), toAnySlice(old.dnsOptions), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("dns_options", state.dnsOptions, old.dnsOptions)
	}
	if hasHealthcheckChanged(state, old) {
		add("healthcheck", state.healthcheck, old.healthcheck)
	}
	if state.hostname != nil && deref(state.hostname) != deref(old.hostname) {
		add("hostname", state.hostname, old.hostname)
	}
	if state.hosts != nil && !mapsEqual(state.hosts, old.hosts) {
		add("hosts", state.hosts, old.hosts)
	}
	if state.tty != nil && derefBool(state.tty) != derefBool(old.tty) {
		add("tty", state.tty, old.tty)
	}
	if state.workingDir != nil && deref(state.workingDir) != deref(old.workingDir) {
		add("working_dir", state.workingDir, old.workingDir)
	}
	if state.forceUpdate != 0 {
		result.forceUpdate = true
	}
	if state.init != nil && derefBool(state.init) != derefBool(old.init) {
		add("init", state.init, old.init)
	}
	changed, err = hasListChanged(toAnySlice(state.capAdd), toAnySlice(old.capAdd), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("cap_add", state.capAdd, old.capAdd)
	}
	changed, err = hasListChanged(toAnySlice(state.capDrop), toAnySlice(old.capDrop), true, "")
	if err != nil {
		return result, err
	}
	if changed {
		add("cap_drop", state.capDrop, old.capDrop)
	}
	result.changed = len(result.changes) > 0 || result.forceUpdate
	return result, nil
}

func hasImageChanged(desired, active string) (bool, string) {
	if !strings.Contains(desired, "@") {
		active = strings.Split(active, "@")[0]
	}
	return desired != active, active
}

func hasHealthcheckChanged(desired, old *serviceState) bool {
	if !desired.healthcheckSpecified {
		return false
	}
	if !desired.healthcheckDisabled && desired.healthcheck == nil && old.healthcheck == nil {
		return false
	}
	if desired.healthcheckDisabled {
		if old.healthcheck == nil {
			return false
		}
		if test, ok := old.healthcheck["test"].([]string); ok && len(test) == 1 && test[0] == "NONE" {
			return false
		}
		if test, ok := old.healthcheck["test"].([]any); ok && len(test) == 1 && fmt.Sprint(test[0]) == "NONE" {
			return false
		}
	}
	return !jsonEqual(desired.healthcheck, old.healthcheck)
}

func hasPublishChanged(desired, old []map[string]any) bool {
	if desired == nil {
		return false
	}
	if old == nil {
		old = []map[string]any{}
	}
	if len(desired) != len(old) {
		return true
	}
	left := copyNetworkList(desired)
	right := copyNetworkList(old)
	sort.SliceStable(left, func(i, j int) bool { return publishLessThan(left[i], left[j]) })
	sort.SliceStable(right, func(i, j int) bool { return publishLessThan(right[i], right[j]) })
	for index := range left {
		ignored := map[string]bool{}
		if isFalsy(left[index]["mode"]) {
			ignored["mode"] = true
		}
		if isFalsy(left[index]["published_port"]) {
			ignored["published_port"] = true
		}
		filteredLeft := map[string]any{}
		filteredRight := map[string]any{}
		for key, value := range left[index] {
			if !ignored[key] {
				filteredLeft[key] = value
			}
		}
		for key, value := range right[index] {
			if !ignored[key] {
				filteredRight[key] = value
			}
		}
		if !jsonEqual(filteredLeft, filteredRight) {
			return true
		}
	}
	return false
}

func publishLessThan(left, right map[string]any) bool {
	lp, rp := publishSortKey(left), publishSortKey(right)
	if lp[0] != rp[0] {
		return lp[0] < rp[0]
	}
	if lp[1] != rp[1] {
		return lp[1] < rp[1]
	}
	return lp[2] < rp[2]
}

func publishSortKey(item map[string]any) [3]string {
	return [3]string{
		fmt.Sprintf("%010d", toUint64(item["published_port"])),
		fmt.Sprintf("%010d", toUint64(item["target_port"])),
		fmt.Sprint(item["protocol"]),
	}
}

func normalizeCapabilityAdd(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		name := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(value)), "CAP_")
		if name == "" {
			result[index] = value
			continue
		}
		result[index] = "CAP_" + name
	}
	return result
}

func normalizeCapabilityDrop(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(value)), "CAP_")
	}
	return result
}

func toUint64(value any) uint64 {
	switch typed := value.(type) {
	case uint32:
		return uint64(typed)
	case uint64:
		return typed
	case int:
		return uint64(typed)
	case int64:
		return uint64(typed)
	case float64:
		return uint64(typed)
	case *uint32:
		if typed == nil {
			return 0
		}
		return uint64(*typed)
	default:
		parsed, _ := strconv.ParseUint(fmt.Sprint(value), 10, 64)
		return parsed
	}
}

func (state *serviceState) applyToSpec(existing swarm.ServiceSpec, name string) swarm.ServiceSpec {
	spec := cloneSpec(existing)
	spec.Name = name
	if spec.TaskTemplate.ContainerSpec == nil {
		spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	state.writeSpecified(&spec)
	return spec
}

func (state *serviceState) buildCreateSpec(name string) swarm.ServiceSpec {
	spec := swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}}
	spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	state.writeSpecified(&spec)
	return spec
}

func (state *serviceState) writeSpecified(spec *swarm.ServiceSpec) {
	containerSpec := spec.TaskTemplate.ContainerSpec
	if state.image != "" {
		containerSpec.Image = state.image
	}
	if state.command != nil {
		containerSpec.Command = state.command
	}
	if state.args != nil {
		containerSpec.Args = state.args
	}
	if state.env != nil {
		containerSpec.Env = state.env
	}
	if state.user != nil {
		containerSpec.User = *state.user
	}
	if state.containerLabels != nil {
		containerSpec.Labels = state.containerLabels
	}
	if state.sysctls != nil {
		containerSpec.Sysctls = state.sysctls
	}
	if state.healthcheckSpecified {
		if state.healthcheck != nil {
			containerSpec.Healthcheck = healthcheckConfig(state.healthcheck)
		} else if state.healthcheckDisabled {
			containerSpec.Healthcheck = &container.HealthConfig{Test: []string{"NONE"}}
		} else {
			containerSpec.Healthcheck = nil
		}
	}
	if state.hostname != nil {
		containerSpec.Hostname = *state.hostname
	}
	if state.hosts != nil {
		containerSpec.Hosts = formatHosts(state.hosts)
	}
	if state.readOnly != nil {
		containerSpec.ReadOnly = *state.readOnly
	}
	if state.stopGracePeriod != nil {
		duration := time.Duration(*state.stopGracePeriod)
		containerSpec.StopGracePeriod = &duration
	}
	if state.stopSignal != nil {
		containerSpec.StopSignal = *state.stopSignal
	}
	if state.tty != nil {
		containerSpec.TTY = *state.tty
	}
	if state.groups != nil {
		containerSpec.Groups = state.groups
	}
	if state.workingDir != nil {
		containerSpec.Dir = *state.workingDir
	}
	if state.mounts != nil {
		containerSpec.Mounts = buildMounts(state.mounts)
	}
	if state.configs != nil {
		containerSpec.Configs = buildConfigs(state.configs)
	}
	if state.secrets != nil {
		containerSpec.Secrets = buildSecrets(state.secrets)
	}
	if state.dns != nil || state.dnsSearch != nil || state.dnsOptions != nil {
		dns := &swarm.DNSConfig{Search: state.dnsSearch, Options: state.dnsOptions}
		for _, item := range state.dns {
			if addr, err := parseAddr(item); err == nil {
				dns.Nameservers = append(dns.Nameservers, addr)
			}
		}
		containerSpec.DNSConfig = dns
	}
	if state.init != nil {
		containerSpec.Init = state.init
	}
	if state.capAdd != nil {
		containerSpec.CapabilityAdd = state.capAdd
	}
	if state.capDrop != nil {
		containerSpec.CapabilityDrop = state.capDrop
	}
	if state.labels != nil {
		spec.Labels = state.labels
	}
	if state.constraints != nil || state.replicasMaxPerNode != nil || state.placementPreferences != nil {
		if spec.TaskTemplate.Placement == nil {
			spec.TaskTemplate.Placement = &swarm.Placement{}
		}
		if state.constraints != nil {
			spec.TaskTemplate.Placement.Constraints = state.constraints
		}
		if state.replicasMaxPerNode != nil {
			spec.TaskTemplate.Placement.MaxReplicas = *state.replicasMaxPerNode
		}
		if state.placementPreferences != nil {
			spec.TaskTemplate.Placement.Preferences = buildPreferences(state.placementPreferences)
		}
	}
	if state.logDriver != nil || state.logDriverOptions != nil {
		driver := &swarm.Driver{}
		if state.logDriver != nil {
			driver.Name = *state.logDriver
		}
		driver.Options = state.logDriverOptions
		spec.TaskTemplate.LogDriver = driver
	}
	if state.restartPolicy != nil || state.restartPolicyDelay != nil || state.restartPolicyAttempts != nil || state.restartPolicyWindow != nil {
		policy := &swarm.RestartPolicy{}
		if state.restartPolicy != nil {
			policy.Condition = swarm.RestartPolicyCondition(*state.restartPolicy)
		}
		if state.restartPolicyDelay != nil {
			delay := time.Duration(*state.restartPolicyDelay)
			policy.Delay = &delay
		}
		policy.MaxAttempts = state.restartPolicyAttempts
		if state.restartPolicyWindow != nil {
			window := time.Duration(*state.restartPolicyWindow)
			policy.Window = &window
		}
		spec.TaskTemplate.RestartPolicy = policy
	}
	if state.limitCPU != nil || state.limitMemory != nil || state.reserveCPU != nil || state.reserveMemory != nil {
		if spec.TaskTemplate.Resources == nil {
			spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
		}
		if state.limitCPU != nil || state.limitMemory != nil {
			if spec.TaskTemplate.Resources.Limits == nil {
				spec.TaskTemplate.Resources.Limits = &swarm.Limit{}
			}
			if state.limitCPU != nil {
				spec.TaskTemplate.Resources.Limits.NanoCPUs = int64(*state.limitCPU * 1e9)
			}
			if state.limitMemory != nil {
				spec.TaskTemplate.Resources.Limits.MemoryBytes = *state.limitMemory
			}
		}
		if state.reserveCPU != nil || state.reserveMemory != nil {
			if spec.TaskTemplate.Resources.Reservations == nil {
				spec.TaskTemplate.Resources.Reservations = &swarm.Resources{}
			}
			if state.reserveCPU != nil {
				spec.TaskTemplate.Resources.Reservations.NanoCPUs = int64(*state.reserveCPU * 1e9)
			}
			if state.reserveMemory != nil {
				spec.TaskTemplate.Resources.Reservations.MemoryBytes = *state.reserveMemory
			}
		}
	}
	if state.forceUpdate != 0 {
		spec.TaskTemplate.ForceUpdate = state.forceUpdate
	}
	if state.networks != nil {
		spec.TaskTemplate.Networks = buildNetworks(state.networks)
	}
	switch state.mode {
	case "global":
		spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	case "replicated-job":
		spec.Mode = swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{TotalCompletions: state.replicas}}
	default:
		spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: state.replicas}}
	}
	if state.updateDelay != nil || state.updateParallelism != nil || state.updateFailureAction != nil || state.updateMonitor != nil || state.updateMaxFailureRatio != nil || state.updateOrder != nil {
		if spec.UpdateConfig == nil {
			spec.UpdateConfig = &swarm.UpdateConfig{}
		}
		if state.updateParallelism != nil {
			spec.UpdateConfig.Parallelism = *state.updateParallelism
		}
		if state.updateDelay != nil {
			spec.UpdateConfig.Delay = time.Duration(*state.updateDelay)
		}
		if state.updateFailureAction != nil {
			spec.UpdateConfig.FailureAction = swarm.FailureAction(*state.updateFailureAction)
		}
		if state.updateMonitor != nil {
			spec.UpdateConfig.Monitor = time.Duration(*state.updateMonitor)
		}
		if state.updateMaxFailureRatio != nil {
			spec.UpdateConfig.MaxFailureRatio = *state.updateMaxFailureRatio
		}
		if state.updateOrder != nil {
			spec.UpdateConfig.Order = swarm.UpdateOrder(*state.updateOrder)
		}
	}
	if state.rollbackConfig != nil {
		spec.RollbackConfig = updateConfigFromMap(state.rollbackConfig)
	}
	if state.publish != nil || state.endpointMode != nil {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		if state.endpointMode != nil {
			spec.EndpointSpec.Mode = swarm.ResolutionMode(*state.endpointMode)
		}
		if state.publish != nil {
			spec.EndpointSpec.Ports = buildPorts(state.publish)
		}
	}
}

func cloneSpec(spec swarm.ServiceSpec) swarm.ServiceSpec {
	raw, _ := json.Marshal(spec)
	var out swarm.ServiceSpec
	_ = json.Unmarshal(raw, &out)
	return out
}
