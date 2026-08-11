package docker_swarm_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

// Execute retrieves Docker Swarm information
func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// Get server info to check swarm status
	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("get docker info", "", err).Error()}
	}
	info := infoResult.Info

	// Check if in swarm mode
	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return Response{
			Changed:   false,
			InSwarm:   false,
			IsManager: false,
			Msg:       "not part of a swarm",
		}
	}

	resp := Response{
		Changed:   false,
		InSwarm:   true,
		IsManager: info.Swarm.ControlAvailable,
	}

	// Basic swarm info from docker info
	swarmInfo := map[string]interface{}{
		"node_id":           info.Swarm.NodeID,
		"node_addr":         info.Swarm.NodeAddr,
		"local_node_state":  string(info.Swarm.LocalNodeState),
		"control_available": info.Swarm.ControlAvailable,
		"managers":          info.Swarm.Managers,
		"nodes":             info.Swarm.Nodes,
	}

	// If manager, get detailed swarm inspect
	if info.Swarm.ControlAvailable {
		swarmInspectResult, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("inspect swarm", "", err).Error()}
		}
		swarmInspect := swarmInspectResult.Swarm

		swarmInfo["id"] = swarmInspect.ID
		swarmInfo["created_at"] = swarmInspect.CreatedAt
		swarmInfo["updated_at"] = swarmInspect.UpdatedAt
		swarmInfo["version"] = swarmInspect.Version.Index
		swarmInfo["spec"] = map[string]interface{}{
			"name":   swarmInspect.Spec.Annotations.Name,
			"labels": swarmInspect.Spec.Annotations.Labels,
			"orchestration": map[string]interface{}{
				"task_history_retention_limit": swarmInspect.Spec.Orchestration.TaskHistoryRetentionLimit,
			},
			"raft": map[string]interface{}{
				"snapshot_interval":              swarmInspect.Spec.Raft.SnapshotInterval,
				"keep_old_snapshots":             swarmInspect.Spec.Raft.KeepOldSnapshots,
				"log_entries_for_slow_followers": swarmInspect.Spec.Raft.LogEntriesForSlowFollowers,
				"election_tick":                  swarmInspect.Spec.Raft.ElectionTick,
				"heartbeat_tick":                 swarmInspect.Spec.Raft.HeartbeatTick,
			},
			"dispatcher": map[string]interface{}{
				"heartbeat_period": swarmInspect.Spec.Dispatcher.HeartbeatPeriod,
			},
			"ca_config": map[string]interface{}{
				"node_cert_expiry": swarmInspect.Spec.CAConfig.NodeCertExpiry,
			},
			"encryption_config": map[string]interface{}{
				"auto_lock_managers": swarmInspect.Spec.EncryptionConfig.AutoLockManagers,
			},
		}

		// Join tokens (only for managers)
		swarmInfo["join_tokens"] = map[string]interface{}{
			"worker":  swarmInspect.JoinTokens.Worker,
			"manager": swarmInspect.JoinTokens.Manager,
		}

		// TLS info
		if swarmInspect.TLSInfo.TrustRoot != "" {
			swarmInfo["tls_info"] = map[string]interface{}{
				"trust_root":             swarmInspect.TLSInfo.TrustRoot,
				"cert_issuer_subject":    swarmInspect.TLSInfo.CertIssuerSubject,
				"cert_issuer_public_key": swarmInspect.TLSInfo.CertIssuerPublicKey,
			}
		}

		// Root rotation status
		if swarmInspect.RootRotationInProgress {
			swarmInfo["root_rotation_in_progress"] = true
		}

		// Get nodes if requested
		if req.Nodes {
			nodes, err := cli.NodeList(ctx, client.NodeListOptions{})
			if err != nil {
				return Response{Failed: true, Msg: docker.WrapError("list nodes", "", err).Error()}
			}

			nodeList := make([]map[string]interface{}, len(nodes.Items))
			for i, node := range nodes.Items {
				nodeList[i] = convertNodeToMap(node, req.Verbose)
			}
			resp.Nodes = nodeList
		}
	}

	resp.SwarmInfo = swarmInfo
	return resp
}

func convertNodeToMap(node swarm.Node, verbose bool) map[string]interface{} {
	nodeMap := map[string]interface{}{
		"id":           node.ID,
		"hostname":     node.Description.Hostname,
		"role":         string(node.Spec.Role),
		"availability": string(node.Spec.Availability),
		"status": map[string]interface{}{
			"state":   string(node.Status.State),
			"message": node.Status.Message,
			"addr":    node.Status.Addr,
		},
		"manager_status": nil,
	}

	if node.ManagerStatus != nil {
		nodeMap["manager_status"] = map[string]interface{}{
			"leader":       node.ManagerStatus.Leader,
			"reachability": string(node.ManagerStatus.Reachability),
			"addr":         node.ManagerStatus.Addr,
		}
	}

	if verbose {
		nodeMap["version"] = node.Version.Index
		nodeMap["created_at"] = node.CreatedAt
		nodeMap["updated_at"] = node.UpdatedAt
		nodeMap["labels"] = node.Spec.Labels
		nodeMap["description"] = map[string]interface{}{
			"platform": map[string]interface{}{
				"architecture": node.Description.Platform.Architecture,
				"os":           node.Description.Platform.OS,
			},
			"resources": map[string]interface{}{
				"nano_cpus":    node.Description.Resources.NanoCPUs,
				"memory_bytes": node.Description.Resources.MemoryBytes,
			},
			"engine": map[string]interface{}{
				"engine_version": node.Description.Engine.EngineVersion,
				"labels":         node.Description.Engine.Labels,
				"plugins":        node.Description.Engine.Plugins,
			},
		}
		if node.Description.TLSInfo.TrustRoot != "" {
			nodeMap["tls_info"] = map[string]interface{}{
				"trust_root":             node.Description.TLSInfo.TrustRoot,
				"cert_issuer_subject":    node.Description.TLSInfo.CertIssuerSubject,
				"cert_issuer_public_key": node.Description.TLSInfo.CertIssuerPublicKey,
			}
		}
	}

	return nodeMap
}
