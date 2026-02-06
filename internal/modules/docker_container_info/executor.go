package docker_container_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// Execute inspects a Docker container and returns its full configuration
func Execute(req Request) Response {
	if req.Name == "" {
		return Response{Failed: true, Msg: "name is required"}
	}

	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// Inspect container
	inspect, err := cli.ContainerInspect(ctx, req.Name)
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "container not found",
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect container", req.Name, err).Error()}
	}

	// Convert to map for flexible JSON output
	container := map[string]interface{}{
		"id":      inspect.ID,
		"name":    inspect.Name,
		"created": inspect.Created,
		"state": map[string]interface{}{
			"status":      inspect.State.Status,
			"running":     inspect.State.Running,
			"paused":      inspect.State.Paused,
			"restarting":  inspect.State.Restarting,
			"oom_killed":  inspect.State.OOMKilled,
			"dead":        inspect.State.Dead,
			"pid":         inspect.State.Pid,
			"exit_code":   inspect.State.ExitCode,
			"error":       inspect.State.Error,
			"started_at":  inspect.State.StartedAt,
			"finished_at": inspect.State.FinishedAt,
		},
		"image":         inspect.Image,
		"image_name":    inspect.Config.Image,
		"restart_count": inspect.RestartCount,
		"driver":        inspect.Driver,
		"platform":      inspect.Platform,
		"path":          inspect.Path,
		"args":          inspect.Args,
	}

	// Config
	if inspect.Config != nil {
		container["config"] = map[string]interface{}{
			"hostname":      inspect.Config.Hostname,
			"domainname":    inspect.Config.Domainname,
			"user":          inspect.Config.User,
			"attach_stdin":  inspect.Config.AttachStdin,
			"attach_stdout": inspect.Config.AttachStdout,
			"attach_stderr": inspect.Config.AttachStderr,
			"exposed_ports": inspect.Config.ExposedPorts,
			"tty":           inspect.Config.Tty,
			"open_stdin":    inspect.Config.OpenStdin,
			"stdin_once":    inspect.Config.StdinOnce,
			"env":           inspect.Config.Env,
			"cmd":           inspect.Config.Cmd,
			"image":         inspect.Config.Image,
			"volumes":       inspect.Config.Volumes,
			"working_dir":   inspect.Config.WorkingDir,
			"entrypoint":    inspect.Config.Entrypoint,
			"labels":        inspect.Config.Labels,
		}
		if inspect.Config.Healthcheck != nil {
			container["healthcheck"] = map[string]interface{}{
				"test":         inspect.Config.Healthcheck.Test,
				"interval":     inspect.Config.Healthcheck.Interval.String(),
				"timeout":      inspect.Config.Healthcheck.Timeout.String(),
				"start_period": inspect.Config.Healthcheck.StartPeriod.String(),
				"retries":      inspect.Config.Healthcheck.Retries,
			}
		}
	}

	// HostConfig
	if inspect.HostConfig != nil {
		hostConfig := map[string]interface{}{
			"binds":           inspect.HostConfig.Binds,
			"network_mode":    string(inspect.HostConfig.NetworkMode),
			"port_bindings":   inspect.HostConfig.PortBindings,
			"restart_policy":  inspect.HostConfig.RestartPolicy.Name,
			"auto_remove":     inspect.HostConfig.AutoRemove,
			"privileged":      inspect.HostConfig.Privileged,
			"publish_all":     inspect.HostConfig.PublishAllPorts,
			"readonly_rootfs": inspect.HostConfig.ReadonlyRootfs,
			"cap_add":         inspect.HostConfig.CapAdd,
			"cap_drop":        inspect.HostConfig.CapDrop,
			"devices":         inspect.HostConfig.Devices,
			"security_opt":    inspect.HostConfig.SecurityOpt,
			"sysctls":         inspect.HostConfig.Sysctls,
			"init":            inspect.HostConfig.Init,
		}
		// Resources
		hostConfig["cpu_shares"] = inspect.HostConfig.CPUShares
		hostConfig["memory"] = inspect.HostConfig.Memory
		hostConfig["memory_swap"] = inspect.HostConfig.MemorySwap
		hostConfig["nano_cpus"] = inspect.HostConfig.NanoCPUs
		hostConfig["pids_limit"] = inspect.HostConfig.PidsLimit
		if inspect.HostConfig.LogConfig.Type != "" {
			hostConfig["log_config"] = map[string]interface{}{
				"type":   inspect.HostConfig.LogConfig.Type,
				"config": inspect.HostConfig.LogConfig.Config,
			}
		}
		container["host_config"] = hostConfig
	}

	// NetworkSettings
	if inspect.NetworkSettings != nil {
		networks := make(map[string]interface{})
		for name, endpoint := range inspect.NetworkSettings.Networks {
			networks[name] = map[string]interface{}{
				"network_id":          endpoint.NetworkID,
				"endpoint_id":         endpoint.EndpointID,
				"gateway":             endpoint.Gateway,
				"ip_address":          endpoint.IPAddress,
				"ip_prefix_len":       endpoint.IPPrefixLen,
				"ipv6_gateway":        endpoint.IPv6Gateway,
				"global_ipv6_address": endpoint.GlobalIPv6Address,
				"mac_address":         endpoint.MacAddress,
				"aliases":             endpoint.Aliases,
			}
		}
		container["network_settings"] = map[string]interface{}{
			"bridge":      inspect.NetworkSettings.Bridge,
			"ports":       inspect.NetworkSettings.Ports,
			"networks":    networks,
			"ip_address":  inspect.NetworkSettings.IPAddress,
			"gateway":     inspect.NetworkSettings.Gateway,
			"mac_address": inspect.NetworkSettings.MacAddress,
		}
	}

	// Mounts
	if len(inspect.Mounts) > 0 {
		mounts := make([]map[string]interface{}, len(inspect.Mounts))
		for i, m := range inspect.Mounts {
			mounts[i] = map[string]interface{}{
				"type":        string(m.Type),
				"name":        m.Name,
				"source":      m.Source,
				"destination": m.Destination,
				"driver":      m.Driver,
				"mode":        m.Mode,
				"rw":          m.RW,
				"propagation": string(m.Propagation),
			}
		}
		container["mounts"] = mounts
	}

	return Response{
		Changed:   false, // Info modules never change anything
		Exists:    true,
		Container: container,
	}
}
