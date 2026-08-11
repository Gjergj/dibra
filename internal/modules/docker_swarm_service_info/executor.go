package docker_swarm_service_info

import (
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
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	if req.Name == "" {
		return Response{Failed: true, Msg: "service name is required"}
	}

	// Inspect service
	serviceResult, err := cli.ServiceInspect(ctx, req.Name, client.ServiceInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "service not found",
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect service", req.Name, err).Error()}
	}
	service := serviceResult.Service

	// Get tasks for this service
	taskFilter := client.Filters{}
	taskFilter.Add("service", service.ID)
	tasks, err := cli.TaskList(ctx, client.TaskListOptions{Filters: taskFilter})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("list tasks", req.Name, err).Error()}
	}

	// Convert tasks to TaskInfo
	taskInfos := make([]TaskInfo, 0, len(tasks.Items))
	for _, task := range tasks.Items {
		taskInfos = append(taskInfos, TaskInfo{
			ID:           task.ID,
			NodeID:       task.NodeID,
			Status:       task.Status,
			DesiredState: task.DesiredState,
			Slot:         task.Slot,
		})
	}

	return Response{
		Changed:   false,
		Exists:    true,
		ServiceID: service.ID,
		Service:   service,
		Tasks:     taskInfos,
	}
}
