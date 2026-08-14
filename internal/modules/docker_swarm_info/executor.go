package docker_swarm_info

import (
	"context"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const notSwarmManagerMsg = "Error running docker swarm module: must run on swarm manager node"

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failed(false, false, false, docker.WrapError("create docker client", "", err).Error())
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return failed(false, false, false, docker.WrapError("get docker info", "", err).Error())
	}
	active := swarmNodeActive(infoResult.Info.Swarm)

	inspectResult, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{})
	if err != nil {
		return failed(true, active, false, notSwarmManagerMsg)
	}

	facts, err := docker.InspectionMap(inspectResult.Swarm)
	if err != nil {
		return failed(true, true, true, fmt.Sprintf("Error inspecting docker swarm: %v", err))
	}

	response := Response{
		CanTalkToDocker:    true,
		DockerSwarmActive:  true,
		DockerSwarmManager: true,
		SwarmFacts:         facts,
	}

	if req.Nodes {
		items, err := listNodes(ctx, cli, req)
		if err != nil {
			return listFailed(response, "nodes", err)
		}
		response.Nodes = &items
	}
	if req.Services {
		items, err := listServices(ctx, cli, req)
		if err != nil {
			return listFailed(response, "services", err)
		}
		response.Services = &items
	}
	if req.Tasks {
		items, err := listTasks(ctx, cli, req)
		if err != nil {
			return listFailed(response, "tasks", err)
		}
		response.Tasks = &items
	}
	if req.UnlockKey {
		key, err := cli.SwarmGetUnlockKey(ctx)
		if err != nil {
			return failed(true, true, true, fmt.Sprintf("Error inspecting docker swarm: %v", err))
		}
		if key.Key == "" {
			response.SwarmUnlockKey = (*string)(nil)
		} else {
			response.SwarmUnlockKey = key.Key
		}
	}
	return response
}

func listNodes(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.NodeList(ctx, client.NodeListOptions{Filters: req.NodesFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	items, err := inspectItems(result.Items)
	if err != nil || req.VerboseOutput {
		return items, err
	}
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, essentialNode(item))
	}
	return records, nil
}

func listServices(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.ServiceList(ctx, client.ServiceListOptions{Filters: req.ServicesFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	items, err := inspectItems(result.Items)
	if err != nil || req.VerboseOutput {
		return items, err
	}
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record := essentialService(item)
		if record["Mode"] == "Global" {
			record["Replicas"] = len(items)
		}
		records = append(records, record)
	}
	return records, nil
}

func listTasks(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.TaskList(ctx, client.TaskListOptions{Filters: req.TasksFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	items, err := inspectItems(result.Items)
	if err != nil || req.VerboseOutput {
		return items, err
	}
	hostnames, err := nodeHostnameIndex(ctx, cli)
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, essentialTask(item, hostnames))
	}
	return records, nil
}

func inspectItems(value any) ([]map[string]any, error) {
	items, err := docker.InspectionSlice(value)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []map[string]any{}, nil
	}
	return items, nil
}

func essentialNode(item map[string]any) map[string]any {
	managerStatus := any(nil)
	if status := nestedMap(item, "ManagerStatus"); status != nil {
		managerStatus = status["Reachability"]
		if leader, _ := status["Leader"].(bool); leader {
			managerStatus = "Leader"
		}
	}
	return map[string]any{
		"ID":            item["ID"],
		"Hostname":      nestedValue(item, "Description", "Hostname"),
		"Status":        nestedValue(item, "Status", "State"),
		"Availability":  nestedValue(item, "Spec", "Availability"),
		"ManagerStatus": managerStatus,
		"EngineVersion": nestedValue(item, "Description", "Engine", "EngineVersion"),
	}
}

func essentialService(item map[string]any) map[string]any {
	mode := nestedMap(item, "Spec", "Mode")
	record := map[string]any{
		"ID":       item["ID"],
		"Name":     nestedValue(item, "Spec", "Name"),
		"Image":    nestedValue(item, "Spec", "TaskTemplate", "ContainerSpec", "Image"),
		"Replicas": nil,
	}
	switch {
	case nestedMap(mode, "Replicated") != nil:
		record["Mode"] = "Replicated"
		record["Replicas"] = nestedValue(mode, "Replicated", "Replicas")
	case nestedMap(mode, "Global") != nil:
		record["Mode"] = "Global"
	}
	ports := nestedValue(item, "Spec", "EndpointSpec", "Ports")
	if ports == nil {
		ports = []any{}
	}
	record["Ports"] = ports
	return record
}

func essentialTask(item map[string]any, hostnames map[string]string) map[string]any {
	nodeID, _ := item["NodeID"].(string)
	hostname := hostnames[nodeID]
	status := nestedMap(item, "Status")
	containerID := nestedValue(status, "ContainerStatus", "ContainerID")
	if containerID == nil {
		containerID = ""
	}
	var taskError any
	if status != nil {
		if _, found := status["Err"]; found {
			taskError = status["Err"]
		}
	}
	return map[string]any{
		"ID":           item["ID"],
		"ContainerID":  containerID,
		"Image":        nestedValue(item, "Spec", "ContainerSpec", "Image"),
		"Node":         hostname,
		"DesiredState": item["DesiredState"],
		"CurrentState": nestedValue(status, "State"),
		"Error":        taskError,
	}
}

func nodeHostnameIndex(ctx context.Context, cli client.APIClient) (map[string]string, error) {
	result, err := cli.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil, err
	}
	hostnames := make(map[string]string, len(result.Items))
	for _, node := range result.Items {
		hostnames[node.ID] = node.Description.Hostname
	}
	return hostnames, nil
}

func swarmNodeActive(info swarm.Info) bool {
	if info.NodeID != "" {
		return true
	}
	switch info.LocalNodeState {
	case swarm.LocalNodeStateActive, swarm.LocalNodeStatePending, swarm.LocalNodeStateLocked:
		return true
	default:
		return false
	}
}

func failed(canTalk, active, manager bool, msg string) Response {
	return Response{
		Failed:             true,
		Msg:                msg,
		CanTalkToDocker:    canTalk,
		DockerSwarmActive:  active,
		DockerSwarmManager: manager,
	}
}

func listFailed(response Response, object string, err error) Response {
	response.Failed = true
	response.Msg = fmt.Sprintf("Error inspecting docker swarm for object '%s': %v", object, err)
	response.SwarmFacts = nil
	response.Nodes = nil
	response.Services = nil
	response.Tasks = nil
	response.SwarmUnlockKey = nil
	return response
}

func nestedMap(value any, keys ...string) map[string]any {
	current, _ := value.(map[string]any)
	for _, key := range keys {
		if current == nil {
			return nil
		}
		current, _ = current[key].(map[string]any)
	}
	return current
}

func nestedValue(value any, keys ...string) any {
	if len(keys) == 0 {
		return value
	}
	parent := nestedMap(value, keys[:len(keys)-1]...)
	if parent == nil {
		return nil
	}
	return parent[keys[len(keys)-1]]
}
