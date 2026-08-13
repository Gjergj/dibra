package docker_container

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

var comparisonKinds = map[string]string{
	"auto_remove": "value", "blkio_weight": "value", "capabilities": "set", "cap_drop": "set",
	"cgroup_parent": "value", "cgroupns_mode": "value", "command": "list", "cpu_period": "value",
	"cpu_quota": "value", "cpu_shares": "value", "cpus": "value", "cpuset_cpus": "value",
	"cpuset_mems": "value", "detach": "value", "device_cgroup_rules": "list", "device_read_bps": "set(dict)",
	"device_read_iops": "set(dict)", "device_requests": "set(dict)", "device_write_bps": "set(dict)", "device_write_iops": "set(dict)",
	"devices": "set(dict)", "dns_opts": "set", "dns_search_domains": "list", "dns_servers": "list",
	"domainname": "value", "entrypoint": "list", "env": "set", "etc_hosts": "set", "exposed_ports": "set",
	"groups": "set", "healthcheck": "dict", "hostname": "value", "image": "value", "init": "value",
	"interactive": "value", "ipc_mode": "value", "kernel_memory": "value", "labels": "dict", "links": "set",
	"log_driver": "value", "log_options": "dict", "mac_address": "value", "memory": "value",
	"memory_reservation": "value", "memory_swap": "value", "memory_swappiness": "value", "mounts": "set(dict)",
	"network_mode": "value", "networks": "set", "oom_killer": "value", "oom_score_adj": "value",
	"pid_mode": "value", "pids_limit": "value", "platform": "value", "privileged": "value",
	"publish_all_ports": "value", "published_ports": "dict", "read_only": "value", "restart_policy": "value",
	"restart_retries": "value", "runtime": "value", "security_opts": "set", "shm_size": "value",
	"stop_signal": "value", "stop_timeout": "value", "storage_opts": "dict", "sysctls": "dict", "tmpfs": "dict",
	"tty": "value", "ulimits": "set(dict)", "user": "value", "userns_mode": "value", "uts": "value",
	"volume_driver": "value", "volumes": "set", "volumes_from": "set", "working_dir": "value",
}

func normalizeDefaults(req Request) Request {
	if req.State == "" {
		req.State = "started"
	}
	if req.ContainerDefaultBehavior == "" {
		req.ContainerDefaultBehavior = "no_defaults"
	}
	if req.CommandHandling == "" {
		req.CommandHandling = "correct"
	}
	if req.CommandHandling == "compatibility" && len(req.Entrypoint) == 0 {
		req.Entrypoint = nil
	}
	if req.ImageComparison == "" {
		req.ImageComparison = "desired-image"
	}
	if req.ImageLabelMismatch == "" {
		req.ImageLabelMismatch = "ignore"
	}
	if req.ImageNameMismatch == "" {
		req.ImageNameMismatch = "recreate"
	}
	if req.KeepVolumes == nil {
		req.KeepVolumes = boolPointer(true)
	}
	if req.NetworksCLICompatible == nil {
		req.NetworksCLICompatible = boolPointer(true)
	}
	if req.Pull == "" {
		req.Pull = PullMissing
	}
	if req.PullCheckModeBehavior == "" {
		req.PullCheckModeBehavior = "image_not_present"
	}
	if req.Recreate == "" {
		req.Recreate = RecreateAuto
	}
	if req.HealthyWaitTimeout == nil {
		value := 300.0
		req.HealthyWaitTimeout = &value
	} else if *req.HealthyWaitTimeout <= 0 {
		req.HealthyWaitTimeout = nil
	}
	if req.ContainerDefaultBehavior == "compatibility" {
		setDefaultBool(&req.AutoRemove, false)
		setDefaultBool(&req.Detach, true)
		setDefaultBool(&req.Init, false)
		setDefaultBool(&req.Interactive, false)
		setDefaultBool(&req.Paused, false)
		setDefaultBool(&req.Privileged, false)
		setDefaultBool(&req.ReadOnly, false)
		setDefaultBool(&req.TTY, false)
		if req.Memory == nil {
			value := "0"
			req.Memory = &value
		}
	}
	if req.NetworkMode == "" && len(req.Networks) > 0 && boolValue(req.NetworksCLICompatible) {
		req.NetworkMode = req.Networks[0].Name
		if req.providedArguments != nil {
			req.providedArguments["network_mode"] = true
		}
	}
	return req
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !oneOf(req.State, "absent", "present", "healthy", "started", "stopped") {
		return fmt.Errorf("state must be one of absent, present, healthy, started, or stopped")
	}
	if !oneOf(req.ContainerDefaultBehavior, "compatibility", "no_defaults") {
		return fmt.Errorf("container_default_behavior must be compatibility or no_defaults")
	}
	if !oneOf(req.CommandHandling, "compatibility", "correct") {
		return fmt.Errorf("command_handling must be compatibility or correct")
	}
	if !oneOf(req.ImageComparison, "desired-image", "current-image") {
		return fmt.Errorf("image_comparison must be desired-image or current-image")
	}
	if !oneOf(req.ImageLabelMismatch, "ignore", "fail") {
		return fmt.Errorf("image_label_mismatch must be ignore or fail")
	}
	if !oneOf(req.ImageNameMismatch, "ignore", "recreate") {
		return fmt.Errorf("image_name_mismatch must be ignore or recreate")
	}
	if !oneOf(string(req.Pull), string(PullNever), string(PullMissing), string(PullAlways)) {
		return fmt.Errorf("pull must be never, missing, or always")
	}
	if !oneOf(req.PullCheckModeBehavior, "image_not_present", "always") {
		return fmt.Errorf("pull_check_mode_behavior must be image_not_present or always")
	}
	if !oneOf(string(req.Recreate), string(RecreateAuto), string(RecreateAlways), string(RecreateNever)) {
		return fmt.Errorf("recreate must be a boolean or one of auto, always, never")
	}
	if req.RestartRetries != nil && req.RestartPolicy == "" {
		return fmt.Errorf("restart_policy is required when restart_retries is specified")
	}
	if req.RestartPolicy != "" && !oneOf(req.RestartPolicy, "no", "on-failure", "always", "unless-stopped") {
		return fmt.Errorf("restart_policy must be no, on-failure, always, or unless-stopped")
	}
	if req.LogOptions != nil && req.LogDriver == "" {
		return fmt.Errorf("log_driver is required when log_options is specified")
	}
	if req.CgroupnsMode != "" && !oneOf(req.CgroupnsMode, "host", "private") {
		return fmt.Errorf("cgroupns_mode must be host or private")
	}
	if req.BlkioWeight != nil && (*req.BlkioWeight < 10 || *req.BlkioWeight > 1000) {
		return fmt.Errorf("blkio_weight must be between 10 and 1000")
	}
	if req.MemorySwappiness != nil && (*req.MemorySwappiness < 0 || *req.MemorySwappiness > 100) {
		return fmt.Errorf("memory_swappiness must be between 0 and 100")
	}
	if req.DefaultHostIP != nil && *req.DefaultHostIP != "" {
		value := strings.Trim(*req.DefaultHostIP, "[]")
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("default_host_ip must be empty, an IPv4 address, or an IPv6 address")
		}
	}
	if err := validateComparisons(req.Comparisons); err != nil {
		return err
	}
	if req.State == "absent" {
		return nil
	}
	if req.Platform != "" {
		if _, err := parsePlatform(req.Platform, "", ""); err != nil {
			return err
		}
	}
	if req.MacAddress != "" {
		if _, err := net.ParseMAC(strings.ReplaceAll(req.MacAddress, "-", ":")); err != nil {
			return fmt.Errorf("invalid mac_address %q: %w", req.MacAddress, err)
		}
	}
	for _, desired := range req.Networks {
		if strings.TrimSpace(desired.Name) == "" {
			return fmt.Errorf("network name is required")
		}
		if _, err := endpointSettings(desired); err != nil {
			return err
		}
	}
	for _, mount := range req.Mounts {
		mountType := mount.Type
		if mountType == "" {
			mountType = "volume"
		}
		if mount.Source == "" && !oneOf(mountType, "tmpfs", "volume", "image", "cluster") {
			return fmt.Errorf("source must be specified for mount %q of type %q", mount.Target, mountType)
		}
		if mount.Consistency != "" && !oneOf(mount.Consistency, "default", "consistent", "cached", "delegated") {
			return fmt.Errorf("invalid mount consistency %q", mount.Consistency)
		}
		if mount.Propagation != "" && !oneOf(mount.Propagation, "private", "rprivate", "shared", "rshared", "slave", "rslave") {
			return fmt.Errorf("invalid mount propagation %q", mount.Propagation)
		}
	}
	if _, _, err := docker.BuildPortBindings(req.PublishedPorts); err != nil {
		return fmt.Errorf("invalid published port: %w", err)
	}
	for _, port := range req.ExposedPorts {
		if _, err := expandExposedPort(port); err != nil {
			return err
		}
	}
	if _, _, err := buildContainerConfig(req, nil); err != nil {
		return err
	}
	return nil
}

func validateComparisons(comparisons map[string]string) error {
	for key, mode := range comparisons {
		if key == "*" {
			if mode != "strict" && mode != "ignore" {
				return fmt.Errorf("comparisons wildcard only supports strict or ignore")
			}
			continue
		}
		kind, found := comparisonKinds[key]
		if !found {
			return fmt.Errorf("unknown module option %q in comparisons", key)
		}
		if !oneOf(mode, "strict", "ignore", "allow_more_present") {
			return fmt.Errorf("unknown comparison mode %q", mode)
		}
		if mode == "allow_more_present" && kind == "value" {
			return fmt.Errorf("option %q is a value, so its comparison cannot be allow_more_present", key)
		}
	}
	return nil
}

func parseByteSize(name string, value *string, unlimited bool) (int64, error) {
	if value == nil {
		return 0, nil
	}
	if unlimited && (*value == "-1" || *value == "unlimited") {
		return -1, nil
	}
	parsed, err := units.RAMInBytes(*value)
	if err != nil {
		return 0, fmt.Errorf("failed to convert %s to bytes: %w", name, err)
	}
	return parsed, nil
}

func parseDuration(name, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("cannot parse healthcheck.%s %q: %w", name, value, err)
	}
	if duration != 0 && duration < time.Millisecond {
		return 0, fmt.Errorf("healthcheck.%s must be at least 1ms", name)
	}
	return duration, nil
}

func parseUlimit(value string) (name string, soft, hard int64, err error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		return "", 0, 0, fmt.Errorf("invalid ulimit %q; expected name:soft[:hard]", value)
	}
	soft, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid ulimit %q: %w", value, err)
	}
	hard = soft
	if len(parts) == 3 {
		hard, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return "", 0, 0, fmt.Errorf("invalid ulimit %q: %w", value, err)
		}
	}
	return parts[0], soft, hard, nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func setDefaultBool(target **bool, value bool) {
	if *target == nil {
		*target = boolPointer(value)
	}
}

func boolPointer(value bool) *bool { return &value }
func boolValue(value *bool) bool   { return value != nil && *value }
