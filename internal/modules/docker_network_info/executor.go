package docker_network_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

// Execute inspects a Docker network and returns its full configuration
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

	// Inspect network
	result, err := cli.NetworkInspect(ctx, req.Name, client.NetworkInspectOptions{Verbose: true})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "network not found",
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect network", req.Name, err).Error()}
	}
	inspect := result.Network

	// Convert to map for flexible JSON output
	network := map[string]interface{}{
		"id":          inspect.ID,
		"name":        inspect.Name,
		"created":     inspect.Created,
		"scope":       inspect.Scope,
		"driver":      inspect.Driver,
		"enable_ipv6": inspect.EnableIPv6,
		"internal":    inspect.Internal,
		"attachable":  inspect.Attachable,
		"ingress":     inspect.Ingress,
		"config_from": inspect.ConfigFrom.Network,
		"config_only": inspect.ConfigOnly,
		"labels":      inspect.Labels,
		"options":     inspect.Options,
	}

	// IPAM config
	if inspect.IPAM.Driver != "" || len(inspect.IPAM.Config) > 0 {
		ipamConfigs := make([]map[string]interface{}, len(inspect.IPAM.Config))
		for i, cfg := range inspect.IPAM.Config {
			ipamConfigs[i] = map[string]interface{}{
				"subnet":      cfg.Subnet,
				"gateway":     cfg.Gateway,
				"ip_range":    cfg.IPRange,
				"aux_address": cfg.AuxAddress,
			}
		}
		network["ipam"] = map[string]interface{}{
			"driver":  inspect.IPAM.Driver,
			"config":  ipamConfigs,
			"options": inspect.IPAM.Options,
		}
	}

	// Connected containers
	if len(inspect.Containers) > 0 {
		containers := make(map[string]interface{})
		for id, endpoint := range inspect.Containers {
			containers[id] = map[string]interface{}{
				"name":         endpoint.Name,
				"endpoint_id":  endpoint.EndpointID,
				"mac_address":  endpoint.MacAddress,
				"ipv4_address": endpoint.IPv4Address,
				"ipv6_address": endpoint.IPv6Address,
			}
		}
		network["containers"] = containers
	}

	// Peers (for overlay networks)
	if len(inspect.Peers) > 0 {
		peers := make([]map[string]interface{}, len(inspect.Peers))
		for i, peer := range inspect.Peers {
			peers[i] = map[string]interface{}{
				"name": peer.Name,
				"ip":   peer.IP,
			}
		}
		network["peers"] = peers
	}

	return Response{
		Changed: false, // Info modules never change anything
		Exists:  true,
		Network: network,
	}
}
