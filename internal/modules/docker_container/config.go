package docker_container

import (
	"bufio"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/google/shlex"
	"github.com/moby/moby/api/types/blkiodev"
	"github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func buildContainerConfig(req Request, fileSystem docker.FileSystem) (*container.Config, *container.HostConfig, error) {
	environment, err := mergedEnvironment(req, fileSystem)
	if err != nil {
		return nil, nil, err
	}
	command, err := parseCommand(req.Command, req.CommandHandling)
	if err != nil {
		return nil, nil, err
	}
	entrypoint, err := parseEntrypoint(req.Entrypoint, req.CommandHandling)
	if err != nil {
		return nil, nil, err
	}
	normalizedVolumes, err := normalizeVolumes(req.Volumes, fileSystem)
	if err != nil {
		return nil, nil, err
	}
	req.Volumes = normalizedVolumes

	config := &container.Config{
		Image: req.Image, Hostname: req.Hostname, Domainname: req.Domainname, User: req.User,
		WorkingDir: req.WorkingDir, Labels: req.Labels, Env: environment, Entrypoint: entrypoint,
		StopSignal: req.StopSignal,
	}
	var hostConfigBinds []string
	config.Volumes, hostConfigBinds = splitVolumes(req.Volumes)
	if req.Command != nil {
		config.Cmd = command
	}
	if req.StopTimeout != nil {
		config.StopTimeout = req.StopTimeout
	}
	if req.TTY != nil {
		config.Tty = *req.TTY
	}
	if req.Interactive != nil {
		config.OpenStdin = *req.Interactive
	}
	// Upstream configures stdout/stderr attachment when detach is either false
	// or omitted in no-defaults mode. An omitted value still runs asynchronously;
	// it only affects the container's persisted Config attachment fields.
	if req.Detach == nil || !*req.Detach {
		config.AttachStdout = true
		config.AttachStderr = true
		if boolValue(req.Interactive) {
			config.AttachStdin = true
			config.StdinOnce = true
		}
	}

	portBindings, exposedPorts, err := docker.BuildPortBindings(req.PublishedPorts)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid published port: %w", err)
	}
	for _, specification := range req.ExposedPorts {
		ports, expandErr := expandExposedPort(specification)
		if expandErr != nil {
			return nil, nil, expandErr
		}
		for _, port := range ports {
			exposedPorts[nat.Port(port)] = struct{}{}
		}
	}
	config.ExposedPorts = toNetworkPortSet(exposedPorts)

	hostConfig := &container.HostConfig{
		Binds: hostConfigBinds, NetworkMode: container.NetworkMode(req.NetworkMode),
		PortBindings: toNetworkPortMap(portBindings), CapAdd: req.Capabilities, CapDrop: req.CapDrop,
		Links: normalizeContainerLinks(req.Name, req.Links), Sysctls: req.Sysctls, SecurityOpt: req.SecurityOptions,
		StorageOpt: req.StorageOptions, GroupAdd: req.Groups, DNSOptions: req.DNSOptions,
		DNSSearch: req.DNSSearchDomains, CgroupnsMode: container.CgroupnsMode(req.CgroupnsMode),
		IpcMode: container.IpcMode(req.IPCMode), PidMode: container.PidMode(req.PIDMode),
		UTSMode: container.UTSMode(req.UTSMode), UsernsMode: container.UsernsMode(req.UsernsMode),
		Runtime: req.Runtime, VolumeDriver: req.VolumeDriver, VolumesFrom: req.VolumesFrom,
	}
	hostConfig.CgroupParent = req.CgroupParent
	hostConfig.DeviceCgroupRules = req.DeviceCgroupRules
	if req.AutoRemove != nil {
		hostConfig.AutoRemove = *req.AutoRemove
	}
	if req.Privileged != nil {
		hostConfig.Privileged = *req.Privileged
	}
	if req.ReadOnly != nil {
		hostConfig.ReadonlyRootfs = *req.ReadOnly
	}
	if req.PublishAllPorts != nil {
		hostConfig.PublishAllPorts = *req.PublishAllPorts
	}
	if req.Init != nil {
		hostConfig.Init = req.Init
	}
	if req.BlkioWeight != nil {
		hostConfig.BlkioWeight = uint16(*req.BlkioWeight)
	}
	if req.CPUPeriod != nil {
		hostConfig.CPUPeriod = *req.CPUPeriod
	}
	if req.CPUQuota != nil {
		hostConfig.CPUQuota = *req.CPUQuota
	}
	if req.CPUShares != nil {
		hostConfig.CPUShares = *req.CPUShares
	}
	if req.CPUs != nil {
		hostConfig.NanoCPUs = int64(*req.CPUs * 1e9)
	}
	hostConfig.CpusetCpus = req.CPUSetCPUs
	hostConfig.CpusetMems = req.CPUSetMems
	if req.MemorySwappiness != nil {
		hostConfig.MemorySwappiness = req.MemorySwappiness
	}
	if req.OOMKiller != nil {
		hostConfig.OomKillDisable = req.OOMKiller
	}
	if req.OOMScoreAdj != nil {
		hostConfig.OomScoreAdj = *req.OOMScoreAdj
	}
	if req.PidsLimit != nil {
		hostConfig.PidsLimit = req.PidsLimit
	}
	if hostConfig.Memory, err = parseByteSize("memory", req.Memory, false); err != nil {
		return nil, nil, err
	}
	if hostConfig.MemoryReservation, err = parseByteSize("memory_reservation", req.MemoryReservation, false); err != nil {
		return nil, nil, err
	}
	if hostConfig.MemorySwap, err = parseByteSize("memory_swap", req.MemorySwap, true); err != nil {
		return nil, nil, err
	}
	// KernelMemory was removed from the modern Engine API. Keep accepting and
	// validating it for the pinned upstream contract, but fail explicitly when
	// a nonzero value would otherwise be silently discarded.
	if value, parseErr := parseByteSize("kernel_memory", req.KernelMemory, false); parseErr != nil {
		return nil, nil, parseErr
	} else if value != 0 {
		return nil, nil, fmt.Errorf("kernel_memory is not supported by the pinned Docker Engine API")
	}

	if req.RestartPolicy != "" {
		retries := 0
		if req.RestartRetries != nil {
			retries = *req.RestartRetries
		}
		hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(req.RestartPolicy), MaximumRetryCount: retries}
	}
	if req.LogDriver != "" {
		hostConfig.LogConfig = container.LogConfig{Type: req.LogDriver, Config: req.LogOptions}
	}
	for _, specification := range req.Devices {
		device, parseErr := parseDevice(specification)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		hostConfig.Devices = append(hostConfig.Devices, device)
	}
	if hostConfig.BlkioDeviceReadBps, err = buildBPSDevices(req.DeviceReadBPS); err != nil {
		return nil, nil, fmt.Errorf("device_read_bps: %w", err)
	}
	if hostConfig.BlkioDeviceWriteBps, err = buildBPSDevices(req.DeviceWriteBPS); err != nil {
		return nil, nil, fmt.Errorf("device_write_bps: %w", err)
	}
	if hostConfig.BlkioDeviceReadIOps, err = buildIOPSDevices(req.DeviceReadIOPS); err != nil {
		return nil, nil, fmt.Errorf("device_read_iops: %w", err)
	}
	if hostConfig.BlkioDeviceWriteIOps, err = buildIOPSDevices(req.DeviceWriteIOPS); err != nil {
		return nil, nil, fmt.Errorf("device_write_iops: %w", err)
	}
	for _, request := range req.DeviceRequests {
		count := 0
		if request.Count != nil {
			count = *request.Count
		}
		hostConfig.DeviceRequests = append(hostConfig.DeviceRequests, container.DeviceRequest{
			Driver: request.Driver, Count: count, DeviceIDs: request.DeviceIDs,
			Capabilities: request.Capabilities, Options: request.Options,
		})
	}
	for _, address := range req.DNSServers {
		parsed, parseErr := netip.ParseAddr(address)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("invalid DNS server %q: %w", address, parseErr)
		}
		hostConfig.DNS = append(hostConfig.DNS, parsed)
	}
	for host, address := range req.EtcHosts {
		hostConfig.ExtraHosts = append(hostConfig.ExtraHosts, host+":"+address)
	}
	sort.Strings(hostConfig.ExtraHosts)

	if req.Healthcheck != nil {
		config.Healthcheck, err = buildHealthcheck(req.Healthcheck)
		if err != nil {
			return nil, nil, err
		}
	}
	if len(req.Tmpfs) > 0 {
		hostConfig.Tmpfs = make(map[string]string, len(req.Tmpfs))
		for _, specification := range req.Tmpfs {
			path, options, _ := strings.Cut(specification, ":")
			if path == "" {
				return nil, nil, fmt.Errorf("tmpfs path cannot be empty")
			}
			hostConfig.Tmpfs[path] = options
		}
	}
	if req.ShmSize != nil {
		if hostConfig.ShmSize, err = parseByteSize("shm_size", req.ShmSize, false); err != nil {
			return nil, nil, err
		}
	}
	for _, specification := range req.Ulimits {
		name, soft, hard, parseErr := parseUlimit(specification)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		hostConfig.Ulimits = append(hostConfig.Ulimits, &units.Ulimit{Name: name, Soft: soft, Hard: hard})
	}
	for _, requestMount := range req.Mounts {
		mount, mountErr := buildMount(requestMount)
		if mountErr != nil {
			return nil, nil, mountErr
		}
		hostConfig.Mounts = append(hostConfig.Mounts, mount)
	}
	if err := validateMountTargets(req); err != nil {
		return nil, nil, err
	}
	return config, hostConfig, nil
}

func mergedEnvironment(req Request, fileSystem docker.FileSystem) ([]string, error) {
	values := make(map[string]string)
	if req.argumentProvided("env_file", req.EnvFile != "") && fileSystem != nil {
		reader, err := fileSystem.Open(req.EnvFile)
		if err != nil {
			return nil, fmt.Errorf("read env_file %q: %w", req.EnvFile, err)
		}
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if !found || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("invalid env_file line %q", line)
			}
			values[strings.TrimSpace(key)] = value
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read env_file %q: %w", req.EnvFile, err)
		}
	}
	for key, value := range req.Env {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result, nil
}

func parseCommand(value any, handling string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch command := value.(type) {
	case string:
		if command == "" && handling == "compatibility" {
			return nil, nil
		}
		parts, err := shlex.Split(command)
		if err != nil {
			return nil, fmt.Errorf("cannot parse command: %w", err)
		}
		return parts, nil
	case []string:
		if len(command) == 0 && handling == "compatibility" {
			return nil, nil
		}
		if handling == "compatibility" {
			parts, err := shlex.Split(strings.Join(command, " "))
			if err != nil {
				return nil, fmt.Errorf("cannot parse command: %w", err)
			}
			return parts, nil
		}
		return append([]string(nil), command...), nil
	case []any:
		result := make([]string, len(command))
		for index, item := range command {
			result[index] = fmt.Sprint(item)
		}
		if len(result) == 0 && handling == "compatibility" {
			return nil, nil
		}
		if handling == "compatibility" {
			parts, err := shlex.Split(strings.Join(result, " "))
			if err != nil {
				return nil, fmt.Errorf("cannot parse command: %w", err)
			}
			return parts, nil
		}
		return result, nil
	default:
		parts, err := shlex.Split(fmt.Sprint(value))
		if err != nil {
			return nil, fmt.Errorf("cannot parse command: %w", err)
		}
		return parts, nil
	}
}

func parseEntrypoint(value []string, handling string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if handling == "compatibility" {
		if len(value) == 0 {
			return nil, nil
		}
		parts, err := shlex.Split(strings.Join(value, " "))
		if err != nil {
			return nil, fmt.Errorf("cannot parse entrypoint: %w", err)
		}
		return parts, nil
	}
	return append([]string(nil), value...), nil
}

func buildHealthcheck(value *Healthcheck) (*container.HealthConfig, error) {
	result := &container.HealthConfig{}
	var err error
	if result.Interval, err = parseDuration("interval", value.Interval); err != nil {
		return nil, err
	}
	if result.Timeout, err = parseDuration("timeout", value.Timeout); err != nil {
		return nil, err
	}
	if result.StartPeriod, err = parseDuration("start_period", value.StartPeriod); err != nil {
		return nil, err
	}
	if result.StartInterval, err = parseDuration("start_interval", value.StartInterval); err != nil {
		return nil, err
	}
	if value.Retries != nil {
		result.Retries = *value.Retries
	}
	if value.Test == nil {
		if !value.TestCLICompatible {
			result.Test = []string{"NONE"}
		}
	} else {
		switch test := value.Test.(type) {
		case string:
			result.Test = []string{"CMD-SHELL", test}
		case []string:
			result.Test = append([]string(nil), test...)
		case []any:
			for _, item := range test {
				result.Test = append(result.Test, fmt.Sprint(item))
			}
		default:
			return nil, fmt.Errorf("healthcheck.test must be a string or list")
		}
	}
	return result, nil
}

func parseDevice(specification string) (container.DeviceMapping, error) {
	parts := strings.Split(specification, ":")
	if len(parts) > 3 || parts[0] == "" {
		return container.DeviceMapping{}, fmt.Errorf("invalid device %q", specification)
	}
	result := container.DeviceMapping{PathOnHost: parts[0], PathInContainer: parts[0], CgroupPermissions: "rwm"}
	if len(parts) >= 2 && parts[1] != "" {
		result.PathInContainer = parts[1]
	}
	if len(parts) == 3 && parts[2] != "" {
		for _, permission := range parts[2] {
			if !strings.ContainsRune("rwm", permission) {
				return container.DeviceMapping{}, fmt.Errorf("invalid device permissions %q", parts[2])
			}
		}
		result.CgroupPermissions = parts[2]
	}
	return result, nil
}

func buildBPSDevices(values []DeviceRate) ([]*blkiodev.ThrottleDevice, error) {
	result := make([]*blkiodev.ThrottleDevice, 0, len(values))
	for _, value := range values {
		if value.Path == "" || value.Rate == "" {
			return nil, fmt.Errorf("path and rate are required")
		}
		rate, err := units.RAMInBytes(value.Rate)
		if err != nil || rate <= 0 {
			return nil, fmt.Errorf("invalid rate %q", value.Rate)
		}
		result = append(result, &blkiodev.ThrottleDevice{Path: value.Path, Rate: uint64(rate)})
	}
	return result, nil
}

func buildIOPSDevices(values []DeviceIOPS) ([]*blkiodev.ThrottleDevice, error) {
	result := make([]*blkiodev.ThrottleDevice, 0, len(values))
	for _, value := range values {
		if value.Path == "" || value.Rate <= 0 {
			return nil, fmt.Errorf("path and a positive rate are required")
		}
		result = append(result, &blkiodev.ThrottleDevice{Path: value.Path, Rate: uint64(value.Rate)})
	}
	return result, nil
}

func buildMount(value Mount) (mounttypes.Mount, error) {
	if value.Target == "" {
		return mounttypes.Mount{}, fmt.Errorf("mount target is required")
	}
	if value.Type == "" {
		value.Type = "volume"
	}
	if !oneOf(value.Type, "bind", "volume", "tmpfs", "npipe", "cluster", "image") {
		return mounttypes.Mount{}, fmt.Errorf("invalid mount type %q", value.Type)
	}
	result := mounttypes.Mount{Type: mounttypes.Type(value.Type), Source: value.Source, Target: value.Target, Consistency: mounttypes.Consistency(value.Consistency)}
	if value.ReadOnly != nil {
		result.ReadOnly = *value.ReadOnly
	}
	switch value.Type {
	case "bind":
		if value.Propagation != "" || value.NonRecursive != nil || value.CreateMountpoint != nil || value.ReadOnlyNonRecursive != nil || value.ReadOnlyForceRecursive != nil {
			result.BindOptions = &mounttypes.BindOptions{Propagation: mounttypes.Propagation(value.Propagation)}
			if value.NonRecursive != nil {
				result.BindOptions.NonRecursive = *value.NonRecursive
			}
			if value.CreateMountpoint != nil {
				result.BindOptions.CreateMountpoint = *value.CreateMountpoint
			}
			if value.ReadOnlyNonRecursive != nil {
				result.BindOptions.ReadOnlyNonRecursive = *value.ReadOnlyNonRecursive
			}
			if value.ReadOnlyForceRecursive != nil {
				result.BindOptions.ReadOnlyForceRecursive = *value.ReadOnlyForceRecursive
			}
		}
	case "volume":
		if value.NoCopy != nil || value.Labels != nil || value.Subpath != "" || value.VolumeDriver != "" {
			result.VolumeOptions = &mounttypes.VolumeOptions{Labels: value.Labels, Subpath: value.Subpath}
			if value.NoCopy != nil {
				result.VolumeOptions.NoCopy = *value.NoCopy
			}
			if value.VolumeDriver != "" {
				result.VolumeOptions.DriverConfig = &mounttypes.Driver{Name: value.VolumeDriver, Options: value.VolumeOptions}
			}
		}
	case "tmpfs":
		result.Source = ""
		options, err := buildTmpfsOptions(value.TmpfsOptions)
		if err != nil {
			return mounttypes.Mount{}, err
		}
		if value.TmpfsOptions != nil || value.TmpfsSize != "" || value.TmpfsMode != "" {
			result.TmpfsOptions = &mounttypes.TmpfsOptions{Options: options}
		}
		if value.TmpfsSize != "" {
			size, err := units.RAMInBytes(value.TmpfsSize)
			if err != nil {
				return mounttypes.Mount{}, fmt.Errorf("invalid mount tmpfs_size %q: %w", value.TmpfsSize, err)
			}
			result.TmpfsOptions.SizeBytes = size
		}
		if value.TmpfsMode != "" {
			mode, err := strconv.ParseUint(value.TmpfsMode, 8, 32)
			if err != nil {
				return mounttypes.Mount{}, fmt.Errorf("invalid mount tmpfs_mode %q: %w", value.TmpfsMode, err)
			}
			result.TmpfsOptions.Mode = os.FileMode(mode)
		}
	case "image":
		if value.Subpath != "" {
			result.ImageOptions = &mounttypes.ImageOptions{Subpath: value.Subpath}
		}
	}
	if err := validateMountOptionApplicability(value); err != nil {
		return mounttypes.Mount{}, err
	}
	return result, nil
}

func buildTmpfsOptions(values []map[string]*string) ([][]string, error) {
	result := make([][]string, 0, len(values))
	for index, value := range values {
		if len(value) != 1 {
			return nil, fmt.Errorf("tmpfs_options[%d] must be a one-element dictionary", index+1)
		}
		for key, optionValue := range value {
			if key == "" {
				return nil, fmt.Errorf("tmpfs_options[%d] key must not be empty", index+1)
			}
			item := []string{key}
			if optionValue != nil {
				item = append(item, *optionValue)
			}
			result = append(result, item)
		}
	}
	return result, nil
}

func validateMountOptionApplicability(value Mount) error {
	invalid := func(condition bool, option string, allowed ...string) error {
		if condition && !oneOf(value.Type, allowed...) {
			return fmt.Errorf("mount option %s is not valid for type %s", option, value.Type)
		}
		return nil
	}
	checks := []struct {
		condition bool
		option    string
		allowed   []string
	}{
		{value.Propagation != "", "propagation", []string{"bind"}}, {value.NonRecursive != nil, "non_recursive", []string{"bind"}},
		{value.CreateMountpoint != nil, "create_mountpoint", []string{"bind"}}, {value.ReadOnlyNonRecursive != nil, "read_only_non_recursive", []string{"bind"}},
		{value.ReadOnlyForceRecursive != nil, "read_only_force_recursive", []string{"bind"}}, {value.NoCopy != nil, "no_copy", []string{"volume"}},
		{value.Labels != nil, "labels", []string{"volume"}}, {value.VolumeDriver != "", "volume_driver", []string{"volume"}},
		{value.VolumeOptions != nil, "volume_options", []string{"volume"}}, {value.TmpfsSize != "", "tmpfs_size", []string{"tmpfs"}},
		{value.TmpfsMode != "", "tmpfs_mode", []string{"tmpfs"}}, {value.TmpfsOptions != nil, "tmpfs_options", []string{"tmpfs"}},
		{value.Subpath != "", "subpath", []string{"volume", "image"}},
	}
	for _, check := range checks {
		if err := invalid(check.condition, check.option, check.allowed...); err != nil {
			return err
		}
	}
	return nil
}

func validateMountTargets(req Request) error {
	seen := make(map[string]string)
	for _, value := range req.Mounts {
		if previous := seen[value.Target]; previous != "" {
			return fmt.Errorf("mount point %q appears twice in %s", value.Target, previous)
		}
		seen[value.Target] = "mounts"
	}
	for _, value := range req.Volumes {
		target := volumeTarget(value)
		if previous := seen[target]; previous != "" {
			return fmt.Errorf("mount point %q appears both in volumes and %s", target, previous)
		}
		seen[target] = "volumes"
	}
	return nil
}

func volumeTarget(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) == 2 && isVolumePermission(parts[1]) {
		return parts[0]
	}
	if len(parts) >= 2 {
		return parts[1]
	}
	return parts[0]
}

func splitVolumes(values []string) (map[string]struct{}, []string) {
	var anonymous map[string]struct{}
	binds := make([]string, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) == 1 || (len(parts) == 2 && isVolumePermission(parts[1])) {
			if anonymous == nil {
				anonymous = make(map[string]struct{})
			}
			anonymous[value] = struct{}{}
			continue
		}
		binds = append(binds, value)
	}
	return anonymous, binds
}

func normalizeVolumes(values []string, fileSystem docker.FileSystem) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) == 0 || len(parts) > 3 || parts[0] == "" {
			return nil, fmt.Errorf("invalid volume %q", value)
		}
		if len(parts) == 1 || (len(parts) == 2 && isVolumePermission(parts[1])) {
			result = append(result, value)
			continue
		}
		if parts[1] == "" {
			return nil, fmt.Errorf("invalid volume %q: container path is required", value)
		}
		if len(parts) == 3 && !isVolumePermission(parts[2]) {
			return nil, fmt.Errorf("found invalid volumes mode %q", parts[2])
		}
		source := parts[0]
		if source == "~" || strings.HasPrefix(source, "~/") {
			if fileSystem == nil {
				result = append(result, value)
				continue
			}
			home, err := fileSystem.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("expand volume source %q: %w", source, err)
			}
			suffix := ""
			if source != "~" {
				suffix = strings.TrimPrefix(source, "~/")
			}
			source = strings.TrimRight(home, "/")
			if suffix != "" {
				source += "/" + suffix
			}
		} else if strings.HasPrefix(source, ".") {
			if fileSystem == nil {
				result = append(result, value)
				continue
			}
			absolute, err := fileSystem.Abs(source)
			if err != nil {
				return nil, fmt.Errorf("expand volume source %q: %w", source, err)
			}
			source = absolute
		}
		mode := "rw"
		if len(parts) == 3 {
			mode = parts[2]
		}
		result = append(result, source+":"+parts[1]+":"+mode)
	}
	return result, nil
}

func isVolumePermission(value string) bool {
	for _, item := range strings.Split(value, ",") {
		if !oneOf(item, "ro", "rw", "z", "Z", "consistent", "delegated", "cached", "rprivate", "private", "rshared", "shared", "rslave", "slave", "nocopy") {
			return false
		}
	}
	return value != ""
}

func expandExposedPort(value string) ([]string, error) {
	protocol := "tcp"
	port := strings.TrimSpace(value)
	if base, suffix, found := strings.Cut(port, "/"); found {
		port, protocol = base, suffix
	}
	if !oneOf(protocol, "tcp", "udp", "sctp") {
		return nil, fmt.Errorf("invalid exposed port protocol %q", protocol)
	}
	ports, err := docker.ExpandPortRange(port)
	if err != nil {
		return nil, fmt.Errorf("cannot parse exposed port %q: %w", value, err)
	}
	result := make([]string, len(ports))
	for index, item := range ports {
		result[index] = item + "/" + protocol
	}
	return result, nil
}

func endpointSettings(value Network) (*network.EndpointSettings, error) {
	result := &network.EndpointSettings{Aliases: value.Aliases, Links: normalizeEndpointLinks(value.Links), DriverOpts: value.DriverOpts}
	if value.GWPriority != nil {
		result.GwPriority = *value.GWPriority
	}
	if value.MacAddress != "" {
		address, err := net.ParseMAC(value.MacAddress)
		if err != nil {
			return nil, fmt.Errorf("invalid network MAC address %q: %w", value.MacAddress, err)
		}
		result.MacAddress = network.HardwareAddr(address)
	}
	if value.IPv4Address != "" || value.IPv6Address != "" {
		result.IPAMConfig = &network.EndpointIPAMConfig{}
	}
	if value.IPv4Address != "" {
		address, err := netip.ParseAddr(value.IPv4Address)
		if err != nil || !address.Is4() {
			return nil, fmt.Errorf("invalid IPv4 address %q", value.IPv4Address)
		}
		result.IPAMConfig.IPv4Address = address
	}
	if value.IPv6Address != "" {
		address, err := netip.ParseAddr(value.IPv6Address)
		if err != nil || !address.Is6() {
			return nil, fmt.Errorf("invalid IPv6 address %q", value.IPv6Address)
		}
		result.IPAMConfig.IPv6Address = address
	}
	return result, nil
}

func normalizeContainerLinks(containerName string, values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		source, alias, found := strings.Cut(value, ":")
		if !found {
			alias = source
		}
		result = append(result, "/"+strings.TrimPrefix(source, "/")+":/"+strings.TrimPrefix(containerName, "/")+"/"+strings.TrimPrefix(alias, "/"))
	}
	sort.Strings(result)
	return result
}

func normalizeEndpointLinks(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		source, alias, found := strings.Cut(value, ":")
		if !found {
			alias = source
		}
		result = append(result, source+":"+alias)
	}
	sort.Strings(result)
	return result
}

func toNetworkPortMap(input nat.PortMap) network.PortMap {
	result := make(network.PortMap, len(input))
	for port, bindings := range input {
		key, err := network.ParsePort(string(port))
		if err != nil {
			continue
		}
		for _, binding := range bindings {
			var hostIP netip.Addr
			if binding.HostIP != "" {
				hostIP, err = netip.ParseAddr(binding.HostIP)
				if err != nil {
					continue
				}
			}
			result[key] = append(result[key], network.PortBinding{HostIP: hostIP, HostPort: binding.HostPort})
		}
	}
	return result
}

func toNetworkPortSet(input nat.PortSet) network.PortSet {
	result := make(network.PortSet, len(input))
	for port := range input {
		if key, err := network.ParsePort(string(port)); err == nil {
			result[key] = struct{}{}
		}
	}
	return result
}

func toNatPortMap(input network.PortMap) nat.PortMap {
	result := make(nat.PortMap, len(input))
	for port, bindings := range input {
		for _, binding := range bindings {
			hostIP := ""
			if binding.HostIP.IsValid() {
				hostIP = binding.HostIP.String()
			}
			result[nat.Port(port.String())] = append(result[nat.Port(port.String())], nat.PortBinding{HostIP: hostIP, HostPort: binding.HostPort})
		}
	}
	return result
}
