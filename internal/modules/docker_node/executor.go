package docker_node

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	if err := validateRequest(req); err != nil {
		return failedResponse(err.Error())
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	if _, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{}); err != nil {
		return failedResponse(docker.NotSwarmManagerMsg)
	}

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return failedResponse(fmt.Sprintf("Failed to get node information for %v", err))
	}

	targetID := strings.TrimSpace(req.Hostname)
	if req.Self {
		targetID = infoResult.Info.Swarm.NodeID
		if targetID == "" {
			return failedResponse("Failed to get node information.")
		}
	}

	if down, err := localNodeIsDown(ctx, cli, infoResult.Info.Swarm.NodeID); err != nil {
		return failedResponse(fmt.Sprintf("Error while reading from Swarm manager: %v", err))
	} else if down {
		return failedResponse("Can not update the node. The node is down.")
	}

	node, inspection, err := inspectNode(ctx, cli, targetID)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error while reading from Swarm manager: %v", err))
	}
	if node.ID == "" {
		return failedResponse("This node is not part of a swarm.")
	}

	spec, changed := desiredSpec(req, node)
	if !changed {
		return Response{Node: inspection}
	}

	if !state.CheckMode {
		if err := updateNode(ctx, cli, dependencies, node.ID, spec); err != nil {
			return failedResponse(fmt.Sprintf("Failed to update node : %v", err))
		}
	}

	updated, err := inspectNodeMap(ctx, cli, node.ID)
	if err != nil {
		return failedResponse(fmt.Sprintf("Failed to get node information for %v", err))
	}
	return Response{Changed: true, Node: updated}
}

func localNodeIsDown(ctx context.Context, cli client.APIClient, nodeID string) (bool, error) {
	if nodeID == "" {
		return false, nil
	}
	result, err := cli.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
	if err != nil {
		return false, err
	}
	return result.Node.Status.State == swarm.NodeStateDown, nil
}

func validateRequest(req Request) error {
	if !req.Self && strings.TrimSpace(req.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	switch strings.ToLower(strings.TrimSpace(req.LabelsState)) {
	case "", "merge", "replace":
	default:
		return fmt.Errorf("labels_state must be merge or replace")
	}
	switch strings.ToLower(strings.TrimSpace(req.Availability)) {
	case "", "active", "pause", "drain":
	default:
		return fmt.Errorf("availability must be active, pause, or drain")
	}
	switch strings.ToLower(strings.TrimSpace(req.Role)) {
	case "", "manager", "worker":
	default:
		return fmt.Errorf("role must be manager or worker")
	}
	return nil
}

func desiredSpec(req Request, node swarm.Node) (swarm.NodeSpec, bool) {
	spec := node.Spec
	changed := false

	if req.Role != "" {
		role := swarm.NodeRole(strings.ToLower(req.Role))
		if spec.Role != role {
			spec.Role = role
			changed = true
		}
	}
	if req.Availability != "" {
		availability := swarm.NodeAvailability(strings.ToLower(req.Availability))
		if spec.Availability != availability {
			spec.Availability = availability
			changed = true
		}
	}

	labels := docker.NormalizeLabels(spec.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	state := strings.ToLower(strings.TrimSpace(req.LabelsState))
	if state == "" {
		state = "merge"
	}

	switch state {
	case "replace":
		desired := map[string]string{}
		if req.Labels != nil {
			desired = docker.NormalizeLabels(req.Labels)
			if desired == nil {
				desired = map[string]string{}
			}
		}
		if !docker.CompareMaps(labels, desired) {
			labels = desired
			changed = true
		}
	default:
		if req.Labels != nil {
			for key, value := range req.Labels {
				if labels[key] != value {
					labels[key] = value
					changed = true
				}
			}
		}
		for _, key := range req.LabelsToRemove {
			if labelAssignedInRequest(req.Labels, key) {
				continue
			}
			if _, found := labels[key]; found {
				delete(labels, key)
				changed = true
			}
		}
	}

	spec.Labels = labels
	return spec, changed
}

func labelAssignedInRequest(labels LabelMap, key string) bool {
	if labels == nil {
		return false
	}
	value, found := labels[key]
	return found && value != ""
}

func inspectNode(ctx context.Context, cli client.APIClient, id string) (swarm.Node, map[string]any, error) {
	result, err := cli.NodeInspect(ctx, id, client.NodeInspectOptions{})
	if err != nil {
		return swarm.Node{}, nil, err
	}
	inspection, err := docker.NodeInspection(result)
	if err != nil {
		return swarm.Node{}, nil, err
	}
	return result.Node, inspection, nil
}

func inspectNodeMap(ctx context.Context, cli client.APIClient, id string) (map[string]any, error) {
	_, inspection, err := inspectNode(ctx, cli, id)
	return inspection, err
}

func updateNode(ctx context.Context, cli client.APIClient, dependencies docker.Dependencies, nodeID string, spec swarm.NodeSpec) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := cli.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
		if err != nil {
			return err
		}
		_, err = cli.NodeUpdate(ctx, nodeID, client.NodeUpdateOptions{
			Version: result.Node.Version,
			Spec:    spec,
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if !strings.Contains(strings.ToLower(err.Error()), "update out of sequence") &&
			!strings.Contains(strings.ToLower(err.Error()), "version") {
			return err
		}
		dependencies.Clock.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func failedResponse(msg string) Response {
	return Response{Failed: true, Msg: msg}
}
