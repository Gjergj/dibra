package docker_node

import (
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
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
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	// 1. Identify the node
	var targetNode swarm.Node

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get info", "", err).Error()}
	}
	info := infoResult.Info

	if !info.Swarm.ControlAvailable {
		return Response{Failed: true, Msg: "this node is not a swarm manager; docker_node can only be run on manager nodes"}
	}

	if req.Self {
		nodeID := info.Swarm.NodeID
		result, err := cli.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("inspect node", nodeID, err).Error()}
		}
		targetNode = result.Node
	} else if req.Hostname != "" {
		// Find node by hostname
		nodes, err := cli.NodeList(ctx, client.NodeListOptions{})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("list nodes", "", err).Error()}
		}

		found := false
		for _, node := range nodes.Items {
			if node.Description.Hostname == req.Hostname || node.ID == req.Hostname {
				targetNode = node
				found = true
				break
			}
		}
		if !found {
			return Response{Failed: true, Msg: docker.WrapError("find node", req.Hostname, nil).Error() + ": not found"}
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

	// Remove specified labels
	for _, label := range req.LabelsToRemove {
		if _, exists := spec.Labels[label]; exists {
			delete(spec.Labels, label)
			changed = true
		}
	}

	if !changed {
		return Response{Changed: false, Msg: "no changes needed"}
	}

	// 3. Apply Update with retry for version conflicts
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Re-fetch node to get current version
			result, inspectErr := cli.NodeInspect(ctx, targetNode.ID, client.NodeInspectOptions{})
			err = inspectErr
			if err != nil {
				return Response{Failed: true, Msg: docker.WrapError("inspect node", targetNode.ID, err).Error()}
			}
			targetNode = result.Node
		}

		_, err = cli.NodeUpdate(ctx, targetNode.ID, client.NodeUpdateOptions{Version: targetNode.Version, Spec: spec})
		if err == nil {
			return Response{Changed: true, Msg: "node updated"}
		}

		lastErr = err
		// Check if it's a version conflict error
		if !strings.Contains(err.Error(), "update out of sequence") &&
			!strings.Contains(err.Error(), "version") {
			break // Not a version conflict, don't retry
		}
		dependencies.Clock.Sleep(100 * time.Millisecond)
	}

	return Response{Failed: true, Msg: docker.WrapError("update node", targetNode.ID, lastErr).Error()}
}
