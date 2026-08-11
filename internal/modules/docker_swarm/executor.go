package docker_swarm

import (
	"fmt"

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
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to get docker info: %v", err)}
	}
	info := infoResult.Info

	isActive := info.Swarm.LocalNodeState == swarm.LocalNodeStateActive

	// === ABSENT (Leave) ===
	if state == "absent" {
		if !isActive {
			return Response{Changed: false, Msg: "node is not part of a swarm"}
		}

		_, err := cli.SwarmLeave(ctx, client.SwarmLeaveOptions{Force: req.Force})
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
			inspectResult, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{})
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect swarm: %v", err)}
			}
			inspect := inspectResult.Swarm

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

		reqInit := client.SwarmInitOptions{
			ListenAddr:      listenAddr,
			AdvertiseAddr:   req.AdvertiseAddr,
			ForceNewCluster: req.ForceNewCluster,
		}

		initResult, err := cli.SwarmInit(ctx, reqInit)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to init swarm: %v", err)}
		}

		// Get info again for NodeID and Tokens
		inspectResult, _ := cli.SwarmInspect(ctx, client.SwarmInspectOptions{})
		infoResult, _ = cli.Info(ctx, client.InfoOptions{})
		inspect := inspectResult.Swarm
		info = infoResult.Info

		return Response{
			Changed: true,
			Msg:     "swarm initialized",
			SwarmID: inspect.ID,
			NodeID:  initResult.NodeID,
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

		reqJoin := client.SwarmJoinOptions{
			ListenAddr:    req.ListenAddr,
			AdvertiseAddr: req.AdvertiseAddr,
			RemoteAddrs:   req.RemoteAddrs,
			JoinToken:     req.JoinToken,
		}

		_, err := cli.SwarmJoin(ctx, reqJoin)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to join swarm: %v", err)}
		}

		return Response{Changed: true, Msg: "joined swarm"}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
