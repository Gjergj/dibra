package docker_swarm_service

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	// Helper to find service
	findService := func(name string) (swarm.Service, bool, error) {
		result, err := cli.ServiceInspect(ctx, name, client.ServiceInspectOptions{})
		if err != nil {
			if docker.IsNotFoundError(err) {
				return swarm.Service{}, false, nil
			}
			return swarm.Service{}, false, err
		}
		return result.Service, true, nil
	}

	existing, exists, err := findService(req.Name)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect service: %v", err)}
	}

	if state == "absent" {
		if !exists {
			return Response{Changed: false, Msg: "service already absent"}
		}
		_, err := cli.ServiceRemove(ctx, req.Name, client.ServiceRemoveOptions{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove service: %v", err)}
		}
		return Response{Changed: true, Msg: "service removed"}
	}

	if state == "present" {
		// Prepare Scale
		var replicas *uint64
		if req.Replicas != nil {
			replicas = req.Replicas
		} else {
			defaultReplicas := uint64(1)
			replicas = &defaultReplicas
		}

		// Prepare Ports
		ports := []swarm.PortConfig{}
		for _, p := range req.Publish {
			mode := swarm.PortConfigPublishMode(p.Mode)
			if mode == "" {
				mode = swarm.PortConfigPublishModeIngress
			}
			ports = append(ports, swarm.PortConfig{
				Protocol:      network.IPProtocol(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   mode,
			})
		}

		// Prepare Networks
		networks := []swarm.NetworkAttachmentConfig{}
		for _, n := range req.Networks {
			networks = append(networks, swarm.NetworkAttachmentConfig{Target: n})
		}

		// Prepare ContainerSpec
		containerSpec := &swarm.ContainerSpec{
			Image: req.Image,
			Command: func() []string {
				if req.Command == nil {
					return nil
				}
				if s, ok := req.Command.(string); ok {
					return []string{s}
				}
				if sl, ok := req.Command.([]interface{}); ok {
					strs := make([]string, len(sl))
					for i, v := range sl {
						strs[i] = fmt.Sprint(v)
					}
					return strs
				}
				return nil
			}(),
			Args: req.Args,
			Env: func() []string {
				env := []string{}
				for k, v := range req.Env {
					env = append(env, fmt.Sprintf("%s=%s", k, v))
				}
				return env
			}(),
			Labels: req.Labels,
		}

		// Convert config references
		var configRefs []*swarm.ConfigReference
		for _, cfg := range req.Configs {
			mode := cfg.Mode
			if mode == 0 {
				mode = 0444 // Default mode for configs
			}
			fileName := cfg.FileName
			if fileName == "" {
				fileName = cfg.ConfigName
			}
			configRef := &swarm.ConfigReference{
				ConfigName: cfg.ConfigName,
				File: &swarm.ConfigReferenceFileTarget{
					Name: fileName,
					UID:  cfg.UID,
					GID:  cfg.GID,
					Mode: os.FileMode(mode),
				},
			}
			if cfg.ConfigID != "" {
				configRef.ConfigID = cfg.ConfigID
			}
			configRefs = append(configRefs, configRef)
		}
		containerSpec.Configs = configRefs

		// Convert secret references
		var secretRefs []*swarm.SecretReference
		for _, sec := range req.Secrets {
			mode := sec.Mode
			if mode == 0 {
				mode = 0444 // Default mode for secrets
			}
			fileName := sec.FileName
			if fileName == "" {
				fileName = sec.SecretName
			}
			secretRef := &swarm.SecretReference{
				SecretName: sec.SecretName,
				File: &swarm.SecretReferenceFileTarget{
					Name: fileName,
					UID:  sec.UID,
					GID:  sec.GID,
					Mode: os.FileMode(mode),
				},
			}
			if sec.SecretID != "" {
				secretRef.SecretID = sec.SecretID
			}
			secretRefs = append(secretRefs, secretRef)
		}
		containerSpec.Secrets = secretRefs

		// Healthcheck
		if req.Healthcheck != nil {
			hc := &container.HealthConfig{
				Test: req.Healthcheck.Test,
			}
			if req.Healthcheck.Interval != "" {
				interval, _ := time.ParseDuration(req.Healthcheck.Interval)
				hc.Interval = interval
			}
			if req.Healthcheck.Timeout != "" {
				timeout, _ := time.ParseDuration(req.Healthcheck.Timeout)
				hc.Timeout = timeout
			}
			if req.Healthcheck.StartPeriod != "" {
				startPeriod, _ := time.ParseDuration(req.Healthcheck.StartPeriod)
				hc.StartPeriod = startPeriod
			}
			if req.Healthcheck.Retries > 0 {
				hc.Retries = req.Healthcheck.Retries
			}
			containerSpec.Healthcheck = hc
		}

		// DNS configuration
		if len(req.DNS) > 0 || len(req.DNSSearch) > 0 || len(req.DNSOptions) > 0 {
			nameservers, err := parseDNSAddresses(req.DNS)
			if err != nil {
				return Response{Failed: true, Msg: err.Error()}
			}
			containerSpec.DNSConfig = &swarm.DNSConfig{
				Nameservers: nameservers,
				Search:      req.DNSSearch,
				Options:     req.DNSOptions,
			}
		}

		// Extra hosts
		containerSpec.Hosts = req.Hosts

		// Mounts
		if len(req.Mounts) > 0 {
			mounts := make([]mount.Mount, 0, len(req.Mounts))
			for _, m := range req.Mounts {
				mnt := mount.Mount{
					Type:     mount.Type(m.Type),
					Source:   m.Source,
					Target:   m.Target,
					ReadOnly: m.ReadOnly,
				}
				if m.Consistency != "" {
					mnt.Consistency = mount.Consistency(m.Consistency)
				}
				// Bind options
				if m.Type == "bind" && m.BindPropagation != "" {
					mnt.BindOptions = &mount.BindOptions{
						Propagation: mount.Propagation(m.BindPropagation),
					}
				}
				// Volume options
				if m.Type == "volume" {
					mnt.VolumeOptions = &mount.VolumeOptions{
						NoCopy: m.VolumeNoCopy,
					}
					if m.VolumeLabels != nil {
						mnt.VolumeOptions.Labels = m.VolumeLabels
					}
					if m.VolumeDriver != "" || len(m.VolumeOptions) > 0 {
						mnt.VolumeOptions.DriverConfig = &mount.Driver{
							Name:    m.VolumeDriver,
							Options: m.VolumeOptions,
						}
					}
				}
				// Tmpfs options
				if m.Type == "tmpfs" {
					mnt.TmpfsOptions = &mount.TmpfsOptions{
						SizeBytes: m.TmpfsSize,
						Mode:      os.FileMode(m.TmpfsMode),
					}
				}
				mounts = append(mounts, mnt)
			}
			containerSpec.Mounts = mounts
		}

		// Prepare TaskSpec
		taskSpec := swarm.TaskSpec{
			ContainerSpec: containerSpec,
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{
					NanoCPUs:    int64(req.LimitCPU * 1e9),
					MemoryBytes: req.LimitMemory,
				},
			},
			Networks: networks,
			Placement: &swarm.Placement{
				Constraints: req.Constraint,
			},
		}

		// Mode (Replicated)
		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: req.Labels,
			},
			TaskTemplate: taskSpec,
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: replicas,
				},
			},
			EndpointSpec: &swarm.EndpointSpec{
				Ports: ports,
			},
		}

		// Update configuration
		if req.UpdateDelay != "" || req.UpdateParallelism > 0 || req.UpdateFailureAction != "" || req.UpdateOrder != "" {
			updateConfig := &swarm.UpdateConfig{}
			if req.UpdateDelay != "" {
				delay, _ := time.ParseDuration(req.UpdateDelay)
				updateConfig.Delay = delay
			}
			if req.UpdateParallelism > 0 {
				updateConfig.Parallelism = req.UpdateParallelism
			}
			if req.UpdateFailureAction != "" {
				updateConfig.FailureAction = swarm.FailureAction(req.UpdateFailureAction)
			}
			if req.UpdateOrder != "" {
				updateConfig.Order = swarm.UpdateOrder(req.UpdateOrder)
			}
			if req.UpdateMonitor != "" {
				monitor, _ := time.ParseDuration(req.UpdateMonitor)
				updateConfig.Monitor = monitor
			}
			if req.MaxFailureRatio > 0 {
				updateConfig.MaxFailureRatio = req.MaxFailureRatio
			}
			spec.UpdateConfig = updateConfig
		}

		// Rollback configuration
		if req.RollbackDelay != "" || req.RollbackParallelism > 0 || req.RollbackFailureAction != "" || req.RollbackOrder != "" {
			rollbackConfig := &swarm.UpdateConfig{}
			if req.RollbackDelay != "" {
				delay, _ := time.ParseDuration(req.RollbackDelay)
				rollbackConfig.Delay = delay
			}
			if req.RollbackParallelism > 0 {
				rollbackConfig.Parallelism = req.RollbackParallelism
			}
			if req.RollbackFailureAction != "" {
				rollbackConfig.FailureAction = swarm.FailureAction(req.RollbackFailureAction)
			}
			if req.RollbackOrder != "" {
				rollbackConfig.Order = swarm.UpdateOrder(req.RollbackOrder)
			}
			if req.RollbackMonitor != "" {
				monitor, _ := time.ParseDuration(req.RollbackMonitor)
				rollbackConfig.Monitor = monitor
			}
			if req.RollbackMaxFailureRatio > 0 {
				rollbackConfig.MaxFailureRatio = req.RollbackMaxFailureRatio
			}
			spec.RollbackConfig = rollbackConfig
		}

		// Update or Create
		if exists {
			// Idempotency: Deep comparison
			diffBuilder := docker.NewDiffBuilder()

			// Check replicas
			if spec.Mode.Replicated != nil && existing.Spec.Mode.Replicated != nil {
				if *existing.Spec.Mode.Replicated.Replicas != *spec.Mode.Replicated.Replicas {
					diffBuilder.Add("replicas", *spec.Mode.Replicated.Replicas, *existing.Spec.Mode.Replicated.Replicas)
				}
			}

			// Check image (normalize - existing may have digest)
			existingImage := existing.Spec.TaskTemplate.ContainerSpec.Image
			desiredImage := req.Image
			// Strip digest from existing for comparison if desired doesn't have one
			if !strings.Contains(desiredImage, "@") && strings.Contains(existingImage, "@") {
				existingImage = strings.Split(existingImage, "@")[0]
			}
			if existingImage != desiredImage {
				diffBuilder.Add("image", desiredImage, existingImage)
			}

			// Check command
			if !docker.CompareStringSlicesOrdered(containerSpec.Command, existing.Spec.TaskTemplate.ContainerSpec.Command) {
				diffBuilder.Add("command", containerSpec.Command, existing.Spec.TaskTemplate.ContainerSpec.Command)
			}

			// Check args
			if !docker.CompareStringSlicesOrdered(containerSpec.Args, existing.Spec.TaskTemplate.ContainerSpec.Args) {
				diffBuilder.Add("args", containerSpec.Args, existing.Spec.TaskTemplate.ContainerSpec.Args)
			}

			// Check environment (order-independent)
			if !docker.CompareStringSlices(containerSpec.Env, existing.Spec.TaskTemplate.ContainerSpec.Env) {
				diffBuilder.Add("env", containerSpec.Env, existing.Spec.TaskTemplate.ContainerSpec.Env)
			}

			// Check labels
			if !docker.CompareMaps(containerSpec.Labels, existing.Spec.TaskTemplate.ContainerSpec.Labels) {
				diffBuilder.Add("container_labels", containerSpec.Labels, existing.Spec.TaskTemplate.ContainerSpec.Labels)
			}
			if !docker.CompareMaps(spec.Annotations.Labels, existing.Spec.Annotations.Labels) {
				diffBuilder.Add("service_labels", spec.Annotations.Labels, existing.Spec.Annotations.Labels)
			}

			// Check networks (simplified comparison)
			existingNetworks := make([]string, 0, len(existing.Spec.TaskTemplate.Networks))
			for _, n := range existing.Spec.TaskTemplate.Networks {
				existingNetworks = append(existingNetworks, n.Target)
			}
			desiredNetworks := make([]string, 0, len(networks))
			for _, n := range networks {
				desiredNetworks = append(desiredNetworks, n.Target)
			}
			if !docker.CompareStringSlices(existingNetworks, desiredNetworks) {
				diffBuilder.Add("networks", desiredNetworks, existingNetworks)
			}

			// Check ports (simplified)
			if len(spec.EndpointSpec.Ports) != len(existing.Spec.EndpointSpec.Ports) {
				diffBuilder.Add("ports", spec.EndpointSpec.Ports, existing.Spec.EndpointSpec.Ports)
			} else {
				for i, p := range spec.EndpointSpec.Ports {
					ep := existing.Spec.EndpointSpec.Ports[i]
					if p.TargetPort != ep.TargetPort || p.PublishedPort != ep.PublishedPort || p.Protocol != ep.Protocol {
						diffBuilder.Add("ports", spec.EndpointSpec.Ports, existing.Spec.EndpointSpec.Ports)
						break
					}
				}
			}

			// Force update if requested
			if req.ForceUpdate {
				diffBuilder.Add("force_update", existing.Spec.TaskTemplate.ForceUpdate+1, existing.Spec.TaskTemplate.ForceUpdate)
			}

			if !diffBuilder.HasDiffs() {
				return Response{Changed: false, Msg: "service already present", ServiceID: existing.ID}
			}

			// Update
			// Must provide version
			spec.TaskTemplate.ForceUpdate = existing.Spec.TaskTemplate.ForceUpdate
			if req.ForceUpdate {
				spec.TaskTemplate.ForceUpdate++
			}

			resp, err := cli.ServiceUpdate(ctx, existing.ID, client.ServiceUpdateOptions{Version: existing.Version, Spec: spec})
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to update service: %v", err)}
			}
			// Warnings?
			_ = resp

			return Response{Changed: true, Msg: "service updated", ServiceID: existing.ID, Diff: diffBuilder.DiffMap()}
		}

		// Create
		resp, err := cli.ServiceCreate(ctx, client.ServiceCreateOptions{Spec: spec})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create service: %v", err)}
		}

		return Response{Changed: true, Msg: "service created", ServiceID: resp.ID}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}

func parseDNSAddresses(values []string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("invalid DNS nameserver %q: %w", value, err)
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}
