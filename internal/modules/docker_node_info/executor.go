package docker_node_info

import (
	"context"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	if _, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{}); err != nil {
		return failedResponse(docker.NotSwarmManagerMsg)
	}

	nodes, err := collectNodes(ctx, cli, req)
	if err != nil {
		return failedResponse(err.Error())
	}
	if nodes == nil {
		nodes = []map[string]any{}
	}
	return Response{Nodes: nodes}
}

func collectNodes(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	if req.Self {
		infoResult, err := cli.Info(ctx, client.InfoOptions{})
		if err != nil {
			return nil, fmt.Errorf("Failed to get node information for %v", err)
		}
		nodeID := infoResult.Info.Swarm.NodeID
		if nodeID == "" {
			return nil, fmt.Errorf("Failed to get node information.")
		}
		node, err := inspectNode(ctx, cli, nodeID, false)
		if err != nil {
			return nil, err
		}
		return []map[string]any{node}, nil
	}

	if req.Name == nil {
		listed, err := cli.NodeList(ctx, client.NodeListOptions{})
		if err != nil {
			return nil, fmt.Errorf("Error while reading from Swarm manager: %v", err)
		}
		nodes := make([]map[string]any, 0, len(listed.Items))
		for _, item := range listed.Items {
			node, err := inspectNode(ctx, cli, item.ID, false)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
		return nodes, nil
	}

	nodes := make([]map[string]any, 0, len(req.Name))
	for _, name := range req.Name {
		node, err := inspectNode(ctx, cli, name, true)
		if err != nil {
			return nil, err
		}
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func inspectNode(ctx context.Context, cli client.APIClient, id string, skipMissing bool) (map[string]any, error) {
	result, err := cli.NodeInspect(ctx, id, client.NodeInspectOptions{})
	if err != nil {
		if skipMissing && docker.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("Error while reading from Swarm manager: %v", err)
	}
	node, err := docker.NodeInspection(result)
	if err != nil {
		return nil, fmt.Errorf("Error inspecting swarm node: %v", err)
	}
	return node, nil
}

func failedResponse(msg string) Response {
	return Response{Failed: true, Msg: msg, Nodes: []map[string]any{}}
}
