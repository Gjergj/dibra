package docker_host_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

// Execute retrieves Docker daemon information
func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	// Get server info
	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker info", "", err).Error()}
	}

	// Get server version
	version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker version", "", err).Error()}
	}

	hostInfo := convertInfoToMap(infoResult.Info, version)

	resp := Response{
		Changed:  false, // Info modules never change anything
		HostInfo: hostInfo,
	}

	// Get disk usage if requested
	if req.DiskUsage {
		du, err := cli.DiskUsage(ctx, client.DiskUsageOptions{Verbose: true})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("get disk usage", "", err).Error()}
		}
		resp.DiskUsage = convertDiskUsageToMap(du)
	}

	return resp
}

func convertInfoToMap(info system.Info, version client.ServerVersionResult) map[string]interface{} {
	hostInfo := map[string]interface{}{
		// Version info
		"server_version":  info.ServerVersion,
		"api_version":     version.APIVersion,
		"min_api_version": version.MinAPIVersion,
		"os":              version.Os,
		"arch":            version.Arch,
		"kernel_version":  info.KernelVersion,

		// System info
		"id":               info.ID,
		"name":             info.Name,
		"operating_system": info.OperatingSystem,
		"os_type":          info.OSType,
		"os_version":       info.OSVersion,
		"architecture":     info.Architecture,
		"ncpu":             info.NCPU,
		"mem_total":        info.MemTotal,

		// Docker info
		"docker_root_dir": info.DockerRootDir,
		"driver":          info.Driver,
		"driver_status":   info.DriverStatus,
		"logging_driver":  info.LoggingDriver,
		"cgroup_driver":   info.CgroupDriver,
		"cgroup_version":  info.CgroupVersion,

		// Container counts
		"containers":         info.Containers,
		"containers_running": info.ContainersRunning,
		"containers_paused":  info.ContainersPaused,
		"containers_stopped": info.ContainersStopped,

		// Image count
		"images": info.Images,

		// Swarm info
		"swarm": map[string]interface{}{
			"local_node_state":  string(info.Swarm.LocalNodeState),
			"control_available": info.Swarm.ControlAvailable,
			"node_id":           info.Swarm.NodeID,
			"node_addr":         info.Swarm.NodeAddr,
			"managers":          info.Swarm.Managers,
			"nodes":             info.Swarm.Nodes,
		},

		// Registry
		"registry_config": map[string]interface{}{
			"insecure_registries": info.RegistryConfig.InsecureRegistryCIDRs,
			"mirrors":             info.RegistryConfig.Mirrors,
		},

		// Security options
		"security_options": info.SecurityOptions,

		// Plugins
		"plugins": map[string]interface{}{
			"volume":  info.Plugins.Volume,
			"network": info.Plugins.Network,
			"log":     info.Plugins.Log,
		},

		// Runtime
		"runtimes":        info.Runtimes,
		"default_runtime": info.DefaultRuntime,

		// Features
		"live_restore_enabled": info.LiveRestoreEnabled,
		"experimental":         info.ExperimentalBuild,
	}

	return hostInfo
}

func convertDiskUsageToMap(du client.DiskUsageResult) map[string]interface{} {
	usage := map[string]interface{}{
		"layers_size": du.Images.TotalSize,
	}

	// Container usage
	if du.Containers.TotalCount > 0 {
		usage["containers"] = map[string]interface{}{
			"count":       du.Containers.TotalCount,
			"active":      du.Containers.ActiveCount,
			"total_size":  du.Containers.TotalSize,
			"reclaimable": du.Containers.Reclaimable,
		}
	}

	// Image usage
	if du.Images.TotalCount > 0 {
		usage["images"] = map[string]interface{}{
			"count":       du.Images.TotalCount,
			"active":      du.Images.ActiveCount,
			"total_size":  du.Images.TotalSize,
			"reclaimable": du.Images.Reclaimable,
		}
	}

	// Volume usage
	if du.Volumes.TotalCount > 0 {
		usage["volumes"] = map[string]interface{}{
			"count":       du.Volumes.TotalCount,
			"active":      du.Volumes.ActiveCount,
			"total_size":  du.Volumes.TotalSize,
			"reclaimable": du.Volumes.Reclaimable,
		}
	}

	// Build cache
	if du.BuildCache.TotalCount > 0 {
		usage["build_cache"] = map[string]interface{}{
			"count":       du.BuildCache.TotalCount,
			"active":      du.BuildCache.ActiveCount,
			"total_size":  du.BuildCache.TotalSize,
			"reclaimable": du.BuildCache.Reclaimable,
		}
	}

	return usage
}
