package docker_network

import (
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	// Helper to find network
	findNetwork := func(name string) (types.NetworkResource, bool, error) {
		// Used Inspect by Name as it's most reliable for exact match
		net, err := cli.NetworkInspect(ctx, name, types.NetworkInspectOptions{})
		if err != nil {
			if client.IsErrNotFound(err) {
				return types.NetworkResource{}, false, nil
			}
			return types.NetworkResource{}, false, err
		}
		return net, true, nil
	}

	existing, exists, err := findNetwork(req.Name)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect network: %v", err)}
	}

	if state == "absent" {
		if !exists {
			return Response{Changed: false, Msg: "network already absent"}
		}
		// Remove
		err := cli.NetworkRemove(ctx, req.Name)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove network: %v", err)}
		}
		return Response{Changed: true, Msg: "network removed", NetworkID: existing.ID}
	}

	if state == "present" {
		if exists {
			// Idempotency check?
			// For now, simpler check: if exists, we assume OK unless force=true
			if !req.Force {
				return Response{Changed: false, Msg: "network already exists", NetworkID: existing.ID}
			}
			// Force recreate
			err := cli.NetworkRemove(ctx, req.Name)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to remove existing network for recreation: %v", err)}
			}
		}

		// Create
		opts := types.NetworkCreate{
			Driver:     req.Driver,
			Options:    req.Options,
			Internal:   req.Internal,
			Attachable: req.Attachable,
			Labels:     req.Labels,
			Scope:      req.Scope,
		}

		if len(req.IPAMConfig) > 0 {
			ipamConfigs := []network.IPAMConfig{}
			for _, cfg := range req.IPAMConfig {
				ipamConfigs = append(ipamConfigs, network.IPAMConfig{
					Subnet:  cfg.Subnet,
					Gateway: cfg.Gateway,
					IPRange: cfg.IPRange,
				})
			}
			opts.IPAM = &network.IPAM{
				Config: ipamConfigs,
			}
		}

		resp, err := cli.NetworkCreate(ctx, req.Name, opts)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create network: %v", err)}
		}

		return Response{Changed: true, Msg: "network created", NetworkID: resp.ID}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
