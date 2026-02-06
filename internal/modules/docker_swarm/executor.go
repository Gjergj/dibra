package docker_swarm

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
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

	info, err := cli.Info(ctx)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to get docker info: %v", err)}
	}

	isActive := info.Swarm.LocalNodeState == swarm.LocalNodeStateActive

	// === ABSENT (Leave) ===
	if state == "absent" {
		if !isActive {
			return Response{Changed: false, Msg: "node is not part of a swarm"}
		}

		err := cli.SwarmLeave(ctx, req.Force)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to leave swarm: %v", err)}
		}
		return Response{Changed: true, Msg: "left swarm"}
	}

	// === PRESENT (Init) ===
	if state == "present" {
		if isActive {
			// Already active.
			// Check if we need to update? For now, assume idempotent if active.
			// TODO: Check AdvertiseAddr, etc.

			// Return tokens
			inspect, err := cli.SwarmInspect(ctx)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect swarm: %v", err)}
			}

			return Response{
				Changed: false,
				Msg:     "already in a swarm",
				SwarmID: inspect.ID,
				NodeID:  info.Swarm.NodeID,
				JoinTokens: struct {
					Worker  string `json:"worker,omitempty"`
					Manager string `json:"manager,omitempty"`
				}{
					Worker:  inspect.JoinTokens.Worker,
					Manager: inspect.JoinTokens.Manager,
				},
			}
		}

		// Init Swarm
		listenAddr := req.ListenAddr
		if listenAddr == "" {
			listenAddr = "0.0.0.0:2377"
		}

		reqInit := swarm.InitRequest{
			ListenAddr:      listenAddr,
			AdvertiseAddr:   req.AdvertiseAddr,
			ForceNewCluster: req.ForceNewCluster,
		}

		swarmID, err := cli.SwarmInit(ctx, reqInit)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to init swarm: %v", err)}
		}

		// Get info again for NodeID and Tokens
		inspect, _ := cli.SwarmInspect(ctx)
		info, _ := cli.Info(ctx)

		return Response{
			Changed: true,
			Msg:     "swarm initialized",
			SwarmID: swarmID,
			NodeID:  info.Swarm.NodeID,
			JoinTokens: struct {
				Worker  string `json:"worker,omitempty"`
				Manager string `json:"manager,omitempty"`
			}{
				Worker:  inspect.JoinTokens.Worker,
				Manager: inspect.JoinTokens.Manager,
			},
		}
	}

	// === JOIN ===
	if state == "join" {
		if isActive {
			return Response{Changed: false, Msg: "already in a swarm"}
		}

		reqJoin := swarm.JoinRequest{
			ListenAddr:    req.ListenAddr,
			AdvertiseAddr: req.AdvertiseAddr,
			RemoteAddrs:   req.RemoteAddrs,
			JoinToken:     req.JoinToken,
		}

		err := cli.SwarmJoin(ctx, reqJoin)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to join swarm: %v", err)}
		}

		return Response{Changed: true, Msg: "joined swarm"}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
