package docker_swarm_service

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx := context.Background()

	state := req.State
	if state == "" {
		state = "present"
	}

	// Helper to find service
	findService := func(name string) (swarm.Service, bool, error) {
		svc, _, err := cli.ServiceInspectWithRaw(ctx, name, types.ServiceInspectOptions{})
		if err != nil {
			if client.IsErrNotFound(err) {
				return swarm.Service{}, false, nil
			}
			return swarm.Service{}, false, err
		}
		return svc, true, nil
	}

	existing, exists, err := findService(req.Name)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect service: %v", err)}
	}

	if state == "absent" {
		if !exists {
			return Response{Changed: false, Msg: "service already absent"}
		}
		err := cli.ServiceRemove(ctx, req.Name)
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
				Protocol:      swarm.PortConfigProtocol(p.Protocol),
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

		// Update or Create
		if exists {
			// Idempotency: Comparing deep specs is hard.
			// We check critical differences or use ForceUpdate.
			needsUpdate := false

			// Simple Replicas check if Replicated mode
			if existing.Spec.Mode.Replicated != nil && spec.Mode.Replicated != nil {
				if *existing.Spec.Mode.Replicated.Replicas != *spec.Mode.Replicated.Replicas {
					needsUpdate = true
				}
			}

			// Check Image
			if existing.Spec.TaskTemplate.ContainerSpec.Image != req.Image {
				// Beware of digests in existing image string
				// Ideally resolve req.Image
				needsUpdate = true
			}

			if req.ForceUpdate {
				needsUpdate = true
				// To force update, usually we change the ForceUpdate param in TaskTemplate
				spec.TaskTemplate.ForceUpdate = uint64(time.Now().UnixNano())
			}

			if !needsUpdate {
				return Response{Changed: false, Msg: "service already present", ServiceID: existing.ID}
			}

			// Update
			// Must provide version
			spec.TaskTemplate.ForceUpdate = existing.Spec.TaskTemplate.ForceUpdate
			if req.ForceUpdate {
				spec.TaskTemplate.ForceUpdate++
			}

			resp, err := cli.ServiceUpdate(ctx, existing.ID, existing.Version, spec, types.ServiceUpdateOptions{})
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to update service: %v", err)}
			}
			// Warnings?
			_ = resp
			return Response{Changed: true, Msg: "service updated", ServiceID: existing.ID}
		}

		// Create
		resp, err := cli.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create service: %v", err)}
		}

		return Response{Changed: true, Msg: "service created", ServiceID: resp.ID}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
