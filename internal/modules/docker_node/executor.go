package docker_node

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx := context.Background()

	// 1. Identify the node
	var targetNode swarm.Node

	info, err := cli.Info(ctx)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to get info: %v", err)}
	}

	if !info.Swarm.ControlAvailable {
		return Response{Failed: true, Msg: "this node is not a swarm manager"}
	}

	if req.Self {
		nodeID := info.Swarm.NodeID
		node, _, err := cli.NodeInspectWithRaw(ctx, nodeID)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect self node: %v", err)}
		}
		targetNode = node
	} else if req.Hostname != "" {
		// Find node by hostname
		nodes, err := cli.NodeList(ctx, types.NodeListOptions{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to list nodes: %v", err)}
		}

		found := false
		for _, node := range nodes {
			if node.Description.Hostname == req.Hostname || node.ID == req.Hostname {
				targetNode = node
				found = true
				break
			}
		}
		if !found {
			return Response{Failed: true, Msg: fmt.Sprintf("node not found: %s", req.Hostname)}
		}
	} else {
		return Response{Failed: true, Msg: "must specify hostname or self=true"}
	}

	// 2. Prepare Update
	spec := targetNode.Spec
	changed := false

	// Availability
	if req.Availability != "" {
		newAvail := swarm.NodeAvailability(strings.ToLower(req.Availability))
		if spec.Availability != newAvail {
			spec.Availability = newAvail
			changed = true
		}
	}

	// Role
	if req.Role != "" {
		newRole := swarm.NodeRole(strings.ToLower(req.Role))
		if spec.Role != newRole {
			spec.Role = newRole
			changed = true
		}
	}

	// Labels
	if req.Labels != nil {
		if spec.Labels == nil {
			spec.Labels = make(map[string]string)
		}

		if req.LabelsState == "replace" {
			// Check if different
			// Simply assume changed if len differs or content differs
			// Or just set it and check deep equals?
			// Let's do a quick check
			isDiff := len(spec.Labels) != len(req.Labels)
			if !isDiff {
				for k, v := range req.Labels {
					if spec.Labels[k] != v {
						isDiff = true
						break
					}
				}
			}
			if isDiff {
				spec.Labels = req.Labels
				changed = true
			}
		} else {
			// Merge (default)
			for k, v := range req.Labels {
				if spec.Labels[k] != v {
					spec.Labels[k] = v
					changed = true
				}
			}
		}
	}

	if !changed {
		return Response{Changed: false, Msg: "no changes needed"}
	}

	// 3. Apply Update
	err = cli.NodeUpdate(ctx, targetNode.ID, targetNode.Version, spec)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to update node: %v", err)}
	}

	return Response{Changed: true, Msg: "node updated"}
}
