package docker_node_info

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
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

	// Get docker info for self lookup and manager check
	info, err := cli.Info(ctx)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker info", "", err).Error()}
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return Response{
			Changed: false,
			Exists:  false,
			Msg:     "not part of a swarm",
		}
	}

	var targetNodeID string

	if req.Self {
		targetNodeID = info.Swarm.NodeID
	} else if req.Name != "" {
		// Need to be a manager to look up other nodes
		if !info.Swarm.ControlAvailable {
			return Response{
				Failed: true,
				Msg:    "looking up nodes by name requires manager access",
			}
		}

		// Find node by hostname or ID
		nodes, err := cli.NodeList(ctx, types.NodeListOptions{})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("list nodes", "", err).Error()}
		}

		for _, node := range nodes {
			if node.Description.Hostname == req.Name || node.ID == req.Name {
				targetNodeID = node.ID
				break
			}
		}

		if targetNodeID == "" {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "node not found",
			}
		}
	} else {
		// Default to self
		targetNodeID = info.Swarm.NodeID
	}

	// Inspect node
	node, _, err := cli.NodeInspectWithRaw(ctx, targetNodeID)
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "node not found",
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect node", targetNodeID, err).Error()}
	}

	// Get tasks for this node (only if we're a manager)
	var taskInfos []TaskInfo
	if info.Swarm.ControlAvailable {
		taskFilter := filters.NewArgs()
		taskFilter.Add("node", targetNodeID)
		tasks, err := cli.TaskList(ctx, types.TaskListOptions{Filters: taskFilter})
		if err == nil {
			taskInfos = make([]TaskInfo, 0, len(tasks))
			for _, task := range tasks {
				taskInfos = append(taskInfos, TaskInfo{
					ID:           task.ID,
					ServiceID:    task.ServiceID,
					Status:       task.Status,
					DesiredState: task.DesiredState,
					Slot:         task.Slot,
				})
			}
		}
	}

	return Response{
		Changed: false,
		Exists:  true,
		NodeID:  node.ID,
		Node:    node,
		Tasks:   taskInfos,
	}
}
