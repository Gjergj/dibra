package docker_container

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

type comparisonResult struct {
	recreate bool
	update   bool
}

func compareContainer(req Request, desiredConfig *container.Config, desiredHost *container.HostConfig, existing container.InspectResponse, diff *docker.DiffBuilder) comparisonResult {
	result := comparisonResult{}
	compare := func(name string, supplied bool, desired, current any, mutable bool) {
		if !supplied || comparisonMode(req, name) == "ignore" {
			return
		}
		if compareValues(desired, current, comparisonMode(req, name), comparisonKinds[name]) {
			return
		}
		diff.Add(name, desired, current)
		if mutable {
			result.update = true
		} else {
			result.recreate = true
		}
	}

	config := existing.Config
	host := existing.HostConfig
	if config == nil || host == nil {
		diff.Add("container_config", "present", "missing")
		result.recreate = true
		return result
	}

	compare("command", req.Command != nil, desiredConfig.Cmd, config.Cmd, false)
	compare("entrypoint", req.Entrypoint != nil, desiredConfig.Entrypoint, config.Entrypoint, false)
	compare("env", req.Env != nil || req.argumentProvided("env_file", req.EnvFile != ""), desiredConfig.Env, config.Env, false)
	compare("hostname", req.argumentProvided("hostname", req.Hostname != ""), req.Hostname, config.Hostname, false)
	compare("domainname", req.argumentProvided("domainname", req.Domainname != ""), req.Domainname, config.Domainname, false)
	compare("user", req.argumentProvided("user", req.User != ""), req.User, config.User, false)
	compare("working_dir", req.argumentProvided("working_dir", req.WorkingDir != ""), req.WorkingDir, config.WorkingDir, false)
	compare("labels", req.Labels != nil, desiredConfig.Labels, config.Labels, false)
	compare("links", req.Links != nil, desiredHost.Links, host.Links, false)
	compare("stop_signal", req.argumentProvided("stop_signal", req.StopSignal != ""), req.StopSignal, config.StopSignal, false)
	compare("stop_timeout", req.StopTimeout != nil, desiredConfig.StopTimeout, config.StopTimeout, false)
	compare("tty", req.TTY != nil, desiredConfig.Tty, config.Tty, false)
	compare("interactive", req.Interactive != nil, desiredConfig.OpenStdin, config.OpenStdin, false)
	if req.Detach != nil {
		currentDetach := !(config.AttachStdout && config.AttachStderr)
		compare("detach", true, *req.Detach, currentDetach, false)
	}
	compare("healthcheck", req.Healthcheck != nil, desiredHealthComparison(req.Healthcheck, desiredConfig.Healthcheck), currentHealthComparison(config.Healthcheck), false)

	compare("auto_remove", req.AutoRemove != nil, desiredHost.AutoRemove, host.AutoRemove, false)
	compare("privileged", req.Privileged != nil, desiredHost.Privileged, host.Privileged, false)
	compare("read_only", req.ReadOnly != nil, desiredHost.ReadonlyRootfs, host.ReadonlyRootfs, false)
	compare("init", req.Init != nil, desiredHost.Init, host.Init, false)
	compare("network_mode", req.argumentProvided("network_mode", req.NetworkMode != ""), normalizeNetworkMode(string(desiredHost.NetworkMode)), normalizeNetworkMode(string(host.NetworkMode)), false)
	compare("capabilities", req.Capabilities != nil, normalizeCapabilities(desiredHost.CapAdd), normalizeCapabilities(host.CapAdd), false)
	compare("cap_drop", req.CapDrop != nil, normalizeCapabilities(desiredHost.CapDrop), normalizeCapabilities(host.CapDrop), false)
	compare("devices", req.Devices != nil, desiredHost.Devices, host.Devices, false)
	compare("device_cgroup_rules", req.DeviceCgroupRules != nil, desiredHost.DeviceCgroupRules, host.DeviceCgroupRules, false)
	compare("device_read_bps", req.DeviceReadBPS != nil, desiredHost.BlkioDeviceReadBps, host.BlkioDeviceReadBps, false)
	compare("device_write_bps", req.DeviceWriteBPS != nil, desiredHost.BlkioDeviceWriteBps, host.BlkioDeviceWriteBps, false)
	compare("device_read_iops", req.DeviceReadIOPS != nil, desiredHost.BlkioDeviceReadIOps, host.BlkioDeviceReadIOps, false)
	compare("device_write_iops", req.DeviceWriteIOPS != nil, desiredHost.BlkioDeviceWriteIOps, host.BlkioDeviceWriteIOps, false)
	compare("device_requests", req.DeviceRequests != nil, desiredHost.DeviceRequests, host.DeviceRequests, false)
	compare("dns_servers", req.DNSServers != nil, desiredHost.DNS, host.DNS, false)
	compare("dns_opts", req.DNSOptions != nil, desiredHost.DNSOptions, host.DNSOptions, false)
	compare("dns_search_domains", req.DNSSearchDomains != nil, desiredHost.DNSSearch, host.DNSSearch, false)
	compare("etc_hosts", req.EtcHosts != nil, desiredHost.ExtraHosts, host.ExtraHosts, false)
	compare("groups", req.Groups != nil, desiredHost.GroupAdd, host.GroupAdd, false)
	compare("ipc_mode", req.argumentProvided("ipc_mode", req.IPCMode != ""), string(desiredHost.IpcMode), string(host.IpcMode), false)
	compare("pid_mode", req.argumentProvided("pid_mode", req.PIDMode != ""), string(desiredHost.PidMode), string(host.PidMode), false)
	compare("uts", req.argumentProvided("uts", req.UTSMode != ""), string(desiredHost.UTSMode), string(host.UTSMode), false)
	compare("userns_mode", req.argumentProvided("userns_mode", req.UsernsMode != ""), string(desiredHost.UsernsMode), string(host.UsernsMode), false)
	compare("cgroupns_mode", req.argumentProvided("cgroupns_mode", req.CgroupnsMode != ""), string(desiredHost.CgroupnsMode), string(host.CgroupnsMode), false)
	compare("cgroup_parent", req.argumentProvided("cgroup_parent", req.CgroupParent != ""), desiredHost.CgroupParent, host.CgroupParent, false)
	compare("runtime", req.argumentProvided("runtime", req.Runtime != ""), desiredHost.Runtime, host.Runtime, false)
	compare("security_opts", req.SecurityOptions != nil, desiredHost.SecurityOpt, host.SecurityOpt, false)
	compare("storage_opts", req.StorageOptions != nil, desiredHost.StorageOpt, host.StorageOpt, false)
	compare("sysctls", req.Sysctls != nil, desiredHost.Sysctls, host.Sysctls, false)
	compare("tmpfs", req.Tmpfs != nil, desiredHost.Tmpfs, host.Tmpfs, false)
	compare("ulimits", req.Ulimits != nil, desiredHost.Ulimits, host.Ulimits, false)
	compare("volume_driver", req.argumentProvided("volume_driver", req.VolumeDriver != ""), desiredHost.VolumeDriver, host.VolumeDriver, false)
	compare("volumes_from", req.VolumesFrom != nil, desiredHost.VolumesFrom, host.VolumesFrom, false)
	if req.Volumes != nil && comparisonMode(req, "volumes") != "ignore" {
		mode := comparisonMode(req, "volumes")
		bindsEqual := compareValues(docker.NormalizeMounts(desiredHost.Binds), docker.NormalizeMounts(host.Binds), mode, "set")
		anonymousEqual := compareValues(desiredConfig.Volumes, config.Volumes, mode, "dict")
		if !bindsEqual || !anonymousEqual {
			diff.Add("volumes",
				map[string]any{"binds": desiredHost.Binds, "anonymous": desiredConfig.Volumes},
				map[string]any{"binds": host.Binds, "anonymous": config.Volumes})
			result.recreate = true
		}
	}
	compare("mounts", req.Mounts != nil, desiredMountComparison(req.Mounts), currentMountComparison(host.Mounts), false)
	compare("publish_all_ports", req.PublishAllPorts != nil, desiredHost.PublishAllPorts, host.PublishAllPorts, false)
	if req.PublishedPorts != nil && comparisonMode(req, "published_ports") != "ignore" {
		desiredPorts, currentPorts := toNatPortMap(desiredHost.PortBindings), toNatPortMap(host.PortBindings)
		matches := docker.PortBindingsContain(desiredPorts, currentPorts)
		if comparisonMode(req, "published_ports") == "strict" {
			matches = docker.ComparePortBindings(desiredPorts, currentPorts)
		}
		if !matches {
			diff.Add("published_ports", desiredHost.PortBindings, host.PortBindings)
			result.recreate = true
		}
	}
	compare("exposed_ports", req.ExposedPorts != nil || req.PublishedPorts != nil, portSetKeys(desiredConfig.ExposedPorts), portSetKeys(config.ExposedPorts), false)
	compare("log_driver", req.argumentProvided("log_driver", req.LogDriver != ""), desiredHost.LogConfig.Type, host.LogConfig.Type, false)
	compare("log_options", req.LogOptions != nil, desiredHost.LogConfig.Config, host.LogConfig.Config, false)
	if req.argumentProvided("mac_address", req.MacAddress != "") {
		currentMac := ""
		if existing.NetworkSettings != nil {
			if settings := existing.NetworkSettings.Networks[primaryNetworkName(req)]; settings != nil {
				currentMac = settings.MacAddress.String()
			}
		}
		compare("mac_address", true, normalizeMACAddress(req.MacAddress), normalizeMACAddress(currentMac), false)
	}

	compare("blkio_weight", req.BlkioWeight != nil, desiredHost.BlkioWeight, host.BlkioWeight, true)
	compare("cpu_period", req.CPUPeriod != nil, desiredHost.CPUPeriod, host.CPUPeriod, true)
	compare("cpu_quota", req.CPUQuota != nil, desiredHost.CPUQuota, host.CPUQuota, true)
	compare("cpu_shares", req.CPUShares != nil, desiredHost.CPUShares, host.CPUShares, true)
	compare("cpus", req.CPUs != nil, desiredHost.NanoCPUs, host.NanoCPUs, false)
	compare("cpuset_cpus", req.argumentProvided("cpuset_cpus", req.CPUSetCPUs != ""), desiredHost.CpusetCpus, host.CpusetCpus, true)
	compare("cpuset_mems", req.argumentProvided("cpuset_mems", req.CPUSetMems != ""), desiredHost.CpusetMems, host.CpusetMems, true)
	compare("memory", req.Memory != nil, desiredHost.Memory, host.Memory, true)
	compare("memory_reservation", req.MemoryReservation != nil, desiredHost.MemoryReservation, host.MemoryReservation, true)
	compare("memory_swap", req.MemorySwap != nil, desiredHost.MemorySwap, host.MemorySwap, true)
	compare("memory_swappiness", req.MemorySwappiness != nil, desiredHost.MemorySwappiness, host.MemorySwappiness, false)
	compare("oom_killer", req.OOMKiller != nil, boolValue(desiredHost.OomKillDisable), boolValue(host.OomKillDisable), false)
	compare("oom_score_adj", req.OOMScoreAdj != nil, desiredHost.OomScoreAdj, host.OomScoreAdj, false)
	compare("shm_size", req.ShmSize != nil, desiredHost.ShmSize, host.ShmSize, false)
	compare("pids_limit", req.PidsLimit != nil, desiredHost.PidsLimit, host.PidsLimit, false)
	if req.RestartPolicy != "" {
		compare("restart_policy", true, desiredHost.RestartPolicy.Name, host.RestartPolicy.Name, true)
	}
	if req.RestartRetries != nil {
		compare("restart_retries", true, desiredHost.RestartPolicy.MaximumRetryCount, host.RestartPolicy.MaximumRetryCount, true)
	}
	return result
}

func comparisonMode(req Request, name string) string {
	if value, found := req.Comparisons[name]; found {
		return value
	}
	if value, found := req.Comparisons["*"]; found {
		return value
	}
	if name == "stop_timeout" {
		return "ignore"
	}
	switch comparisonKinds[name] {
	case "set", "set(dict)", "dict":
		return "allow_more_present"
	default:
		return "strict"
	}
}

func compareValues(desired, current any, mode, kind string) bool {
	if mode == "ignore" {
		return true
	}
	desiredValue := normalizeJSONValue(desired)
	currentValue := normalizeJSONValue(current)
	if desiredValue == nil || currentValue == nil {
		if reflect.DeepEqual(desiredValue, currentValue) {
			return true
		}
		if kind == "value" {
			return false
		}
		if mode == "allow_more_present" && desiredValue == nil {
			return true
		}
		return collectionLength(nonNilValue(desiredValue, currentValue)) == 0
	}
	if kind == "value" {
		return reflect.DeepEqual(desiredValue, currentValue)
	}
	if kind == "list" {
		desiredList, desiredOK := desiredValue.([]any)
		currentList, currentOK := currentValue.([]any)
		if !desiredOK || !currentOK || mode == "strict" {
			return reflect.DeepEqual(desiredValue, currentValue)
		}
		index := 0
		for _, item := range desiredList {
			for index < len(currentList) && !reflect.DeepEqual(item, currentList[index]) {
				index++
			}
			if index == len(currentList) {
				return false
			}
			index++
		}
		return true
	}
	if kind == "dict" {
		desiredMap, desiredOK := desiredValue.(map[string]any)
		currentMap, currentOK := currentValue.(map[string]any)
		if !desiredOK || !currentOK {
			return reflect.DeepEqual(desiredValue, currentValue)
		}
		if mode == "strict" {
			return reflect.DeepEqual(desiredMap, currentMap)
		}
		return compareDictionarySubset(desiredMap, currentMap)
	}
	if kind == "set" {
		desiredSet := canonicalSet(desiredValue)
		currentSet := canonicalSet(currentValue)
		if mode == "strict" && len(desiredSet) != len(currentSet) {
			return false
		}
		for key := range desiredSet {
			if !currentSet[key] {
				return false
			}
		}
		return true
	}
	if kind == "set(dict)" {
		desiredList, desiredOK := desiredValue.([]any)
		currentList, currentOK := currentValue.([]any)
		if !desiredOK || !currentOK {
			return reflect.DeepEqual(desiredValue, currentValue)
		}
		contains := func(needle any, haystack []any) bool {
			needleMap, ok := needle.(map[string]any)
			if !ok {
				return false
			}
			for _, candidate := range haystack {
				candidateMap, ok := candidate.(map[string]any)
				if ok && compareDictionarySubset(needleMap, candidateMap) {
					return true
				}
			}
			return false
		}
		for _, item := range desiredList {
			if !contains(item, currentList) {
				return false
			}
		}
		if mode == "strict" {
			for _, item := range currentList {
				if !contains(item, desiredList) {
					return false
				}
			}
		}
		return true
	}
	return reflect.DeepEqual(desiredValue, currentValue)
}

func portSetKeys(values network.PortSet) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value.String())
	}
	return result
}

func desiredMountComparison(values []Mount) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		mountType := value.Type
		if mountType == "" {
			mountType = "volume"
		}
		item := map[string]any{"type": mountType, "target": value.Target}
		if value.Source != "" {
			item["source"] = value.Source
		}
		if value.ReadOnly != nil {
			item["read_only"] = *value.ReadOnly
		}
		if value.Consistency != "" {
			item["consistency"] = value.Consistency
		}
		if value.Propagation != "" {
			item["propagation"] = value.Propagation
		}
		if value.NoCopy != nil {
			item["no_copy"] = *value.NoCopy
		}
		if value.Labels != nil {
			item["labels"] = mountOptionStrings(value.Labels)
		}
		if value.VolumeDriver != "" {
			item["volume_driver"] = value.VolumeDriver
			if value.VolumeOptions != nil {
				item["volume_options"] = mountOptionStrings(value.VolumeOptions)
			}
		}
		if value.TmpfsSize != "" {
			if parsed, err := parseByteSize("tmpfs_size", &value.TmpfsSize, false); err == nil {
				item["tmpfs_size"] = parsed
			}
		}
		if value.TmpfsMode != "" {
			if parsed, err := strconv.ParseUint(value.TmpfsMode, 8, 32); err == nil {
				item["tmpfs_mode"] = float64(parsed)
			}
		}
		if value.NonRecursive != nil {
			item["non_recursive"] = *value.NonRecursive
		}
		if value.CreateMountpoint != nil {
			item["create_mountpoint"] = *value.CreateMountpoint
		}
		if value.ReadOnlyNonRecursive != nil {
			item["read_only_non_recursive"] = *value.ReadOnlyNonRecursive
		}
		if value.ReadOnlyForceRecursive != nil {
			item["read_only_force_recursive"] = *value.ReadOnlyForceRecursive
		}
		if value.Subpath != "" {
			item["subpath"] = value.Subpath
		}
		if value.TmpfsOptions != nil {
			if options, err := buildTmpfsOptions(value.TmpfsOptions, value.Target); err == nil {
				item["tmpfs_options"] = options
			}
		}
		result = append(result, item)
	}
	return result
}

func currentMountComparison(values []mounttypes.Mount) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item := map[string]any{
			"type": string(value.Type), "source": value.Source, "target": value.Target,
			"read_only": value.ReadOnly, "consistency": string(value.Consistency),
		}
		if value.BindOptions != nil {
			item["propagation"] = string(value.BindOptions.Propagation)
			item["non_recursive"] = value.BindOptions.NonRecursive
			item["create_mountpoint"] = value.BindOptions.CreateMountpoint
			item["read_only_non_recursive"] = value.BindOptions.ReadOnlyNonRecursive
			item["read_only_force_recursive"] = value.BindOptions.ReadOnlyForceRecursive
		}
		if value.VolumeOptions != nil {
			item["no_copy"] = value.VolumeOptions.NoCopy
			item["labels"] = value.VolumeOptions.Labels
			item["subpath"] = value.VolumeOptions.Subpath
			if value.VolumeOptions.DriverConfig != nil {
				item["volume_driver"] = value.VolumeOptions.DriverConfig.Name
				item["volume_options"] = value.VolumeOptions.DriverConfig.Options
			}
		}
		if value.TmpfsOptions != nil {
			item["tmpfs_size"] = value.TmpfsOptions.SizeBytes
			item["tmpfs_mode"] = float64(value.TmpfsOptions.Mode)
			item["tmpfs_options"] = value.TmpfsOptions.Options
		}
		if value.ImageOptions != nil {
			item["subpath"] = value.ImageOptions.Subpath
		}
		result = append(result, item)
	}
	return result
}

func desiredHealthComparison(request *Healthcheck, value *container.HealthConfig) map[string]any {
	if request == nil || value == nil {
		return nil
	}
	result := make(map[string]any)
	if request.Test != nil || !request.TestCLICompatible {
		result["Test"] = value.Test
	}
	if request.Interval != "" {
		result["Interval"] = value.Interval
	}
	if request.Timeout != "" {
		result["Timeout"] = value.Timeout
	}
	if request.StartPeriod != "" {
		result["StartPeriod"] = value.StartPeriod
	}
	if request.StartInterval != "" {
		result["StartInterval"] = value.StartInterval
	}
	if request.Retries != nil {
		result["Retries"] = value.Retries
	}
	return result
}

func currentHealthComparison(value *container.HealthConfig) map[string]any {
	if value == nil {
		return nil
	}
	return map[string]any{
		"Test": value.Test, "Interval": value.Interval, "Timeout": value.Timeout,
		"StartPeriod": value.StartPeriod, "StartInterval": value.StartInterval, "Retries": value.Retries,
	}
}

func compareDictionarySubset(desired, current map[string]any) bool {
	for key, value := range desired {
		if currentValue, found := current[key]; !found || !reflect.DeepEqual(value, currentValue) {
			return false
		}
	}
	return true
}

func nonNilValue(first, second any) any {
	if first != nil {
		return first
	}
	return second
}

func collectionLength(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case map[string]any:
		return len(typed)
	default:
		return -1
	}
}

func normalizeJSONValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return value
	}
	return normalized
}

func canonicalSet(value any) map[string]bool {
	result := make(map[string]bool)
	list, ok := value.([]any)
	if !ok {
		if value != nil {
			data, _ := json.Marshal(value)
			result[string(data)] = true
		}
		return result
	}
	for _, item := range list {
		data, _ := json.Marshal(item)
		result[string(data)] = true
	}
	return result
}

func normalizeNetworkMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "default" || mode == "bridge" {
		return "bridge"
	}
	return mode
}

func normalizeCapabilities(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimPrefix(strings.ToUpper(value), "CAP_")
	}
	return result
}

func normalizeMACAddress(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "-", ":"))
}

func networkDifferences(req Request, existing container.InspectResponse, diff *docker.DiffBuilder) (connect []Network, disconnect []string, err error) {
	if req.Networks == nil || comparisonMode(req, "networks") == "ignore" {
		return nil, nil, nil
	}
	current := map[string]any{}
	if existing.NetworkSettings != nil {
		for name, settings := range existing.NetworkSettings.Networks {
			current[name] = settings
		}
	}
	desiredNames := make(map[string]bool, len(req.Networks))
	for _, desired := range req.Networks {
		if desired.Name == "" {
			return nil, nil, fmt.Errorf("network name is required")
		}
		desiredNames[desired.Name] = true
		settings, found := current[desired.Name]
		if !found || !networkMatches(desired, settings) {
			connect = append(connect, desired)
			diff.Add("network."+desired.Name, desired, settings)
		}
	}
	if comparisonMode(req, "networks") == "strict" {
		for name := range current {
			if !desiredNames[name] {
				disconnect = append(disconnect, name)
				diff.Add("network."+name, nil, current[name])
			}
		}
		sort.Strings(disconnect)
	}
	return connect, disconnect, nil
}

func networkDifferencesForCreate(req Request, existing container.InspectResponse, diff *docker.DiffBuilder) (connect []Network, disconnect []string, err error) {
	if comparisonMode(req, "networks") != "ignore" {
		return networkDifferences(req, existing, diff)
	}
	comparisons := make(map[string]string, len(req.Comparisons)+1)
	for name, mode := range req.Comparisons {
		comparisons[name] = mode
	}
	comparisons["networks"] = "allow_more_present"
	req.Comparisons = comparisons
	return networkDifferences(req, existing, diff)
}

func networkMatches(desired Network, current any) bool {
	settings, ok := current.(*network.EndpointSettings)
	if !ok || settings == nil {
		return false
	}
	if desired.IPv4Address != "" && (settings.IPAMConfig == nil || docker.NormalizeEndpointAddress(desired.IPv4Address) != docker.NormalizeEndpointAddress(settings.IPAMConfig.IPv4Address.String())) {
		return false
	}
	if desired.IPv6Address != "" && (settings.IPAMConfig == nil || docker.NormalizeEndpointAddress(desired.IPv6Address) != docker.NormalizeEndpointAddress(settings.IPAMConfig.IPv6Address.String())) {
		return false
	}
	if desired.Aliases != nil && !compareValues(desired.Aliases, settings.Aliases, "allow_more_present", "set") {
		return false
	}
	if desired.Links != nil && !compareValues(normalizeEndpointLinks(desired.Links), settings.Links, "allow_more_present", "set") {
		return false
	}
	if desired.MacAddress != "" && normalizeMACAddress(desired.MacAddress) != normalizeMACAddress(settings.MacAddress.String()) {
		return false
	}
	return true
}
