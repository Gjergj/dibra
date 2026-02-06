package docker_host_info

import (
	"github.com/docker/docker/api/types"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// Execute retrieves Docker daemon information
func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// Get server info
	info, err := cli.Info(ctx)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker info", "", err).Error()}
	}

	// Get server version
	version, err := cli.ServerVersion(ctx)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker version", "", err).Error()}
	}

	hostInfo := convertInfoToMap(info, version)

	resp := Response{
		Changed:  false, // Info modules never change anything
		HostInfo: hostInfo,
	}

	// Get disk usage if requested
	if req.DiskUsage {
		du, err := cli.DiskUsage(ctx, types.DiskUsageOptions{})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("get disk usage", "", err).Error()}
		}
		resp.DiskUsage = convertDiskUsageToMap(du)
	}

	return resp
}

func convertInfoToMap(info types.Info, version types.Version) map[string]interface{} {
	hostInfo := map[string]interface{}{
		// Version info
		"server_version":  info.ServerVersion,
		"api_version":     version.APIVersion,
		"min_api_version": version.MinAPIVersion,
		"git_commit":      version.GitCommit,
		"go_version":      version.GoVersion,
		"os":              version.Os,
		"arch":            version.Arch,
		"kernel_version":  version.KernelVersion,
		"build_time":      version.BuildTime,

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

func convertDiskUsageToMap(du types.DiskUsage) map[string]interface{} {
	usage := map[string]interface{}{
		"layers_size": du.LayersSize,
	}

	// Container usage
	if len(du.Containers) > 0 {
		var totalSize int64
		for _, c := range du.Containers {
			totalSize += c.SizeRw
		}
		usage["containers"] = map[string]interface{}{
			"count":      len(du.Containers),
			"total_size": totalSize,
		}
	}

	// Image usage
	if len(du.Images) > 0 {
		var totalSize, sharedSize int64
		for _, img := range du.Images {
			totalSize += img.Size
			sharedSize += img.SharedSize
		}
		usage["images"] = map[string]interface{}{
			"count":       len(du.Images),
			"total_size":  totalSize,
			"shared_size": sharedSize,
		}
	}

	// Volume usage
	if len(du.Volumes) > 0 {
		var totalSize int64
		for _, v := range du.Volumes {
			if v.UsageData != nil {
				totalSize += v.UsageData.Size
			}
		}
		usage["volumes"] = map[string]interface{}{
			"count":      len(du.Volumes),
			"total_size": totalSize,
		}
	}

	// Build cache
	if len(du.BuildCache) > 0 {
		var totalSize int64
		for _, bc := range du.BuildCache {
			totalSize += bc.Size
		}
		usage["build_cache"] = map[string]interface{}{
			"count":      len(du.BuildCache),
			"total_size": totalSize,
		}
	}

	return usage
}
