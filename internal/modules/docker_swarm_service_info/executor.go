package docker_swarm_service_info

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	if req.Name == "" {
		return Response{Failed: true, Msg: "service name is required"}
	}

	// Inspect service
	service, _, err := cli.ServiceInspectWithRaw(ctx, req.Name, types.ServiceInspectOptions{})
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

	// Get tasks for this service
	taskFilter := filters.NewArgs()
	taskFilter.Add("service", service.ID)
	tasks, err := cli.TaskList(ctx, types.TaskListOptions{Filters: taskFilter})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("list tasks", req.Name, err).Error()}
	}

	// Convert tasks to TaskInfo
	taskInfos := make([]TaskInfo, 0, len(tasks))
	for _, task := range tasks {
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
