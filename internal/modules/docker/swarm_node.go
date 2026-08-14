package docker

import (
	"strings"

	"github.com/moby/moby/client"
)

// NotSwarmManagerMsg is the pinned community.docker error when a Swarm module
// is invoked on a host that is not a manager.
const NotSwarmManagerMsg = "Error running docker swarm module: must run on swarm manager node"

// NodeInspection returns the raw Engine node object, applying the pinned
// leader-address workaround from community.docker's Swarm helpers.
func NodeInspection(result client.NodeInspectResult) (map[string]any, error) {
	var node map[string]any
	var err error
	if len(result.Raw) > 0 {
		node, err = DecodeInspection(result.Raw)
	} else {
		node, err = InspectionMap(result.Node)
	}
	if err != nil {
		return nil, err
	}
	ApplySwarmLeaderAddressWorkaround(node)
	return node, nil
}

// ApplySwarmLeaderAddressWorkaround copies a usable leader IP into Status.Addr
// when ManagerStatus.Addr is 0.0.0.0 or otherwise empty on IPv4. This matches
// the pinned community.docker get_node_inspect helper (moby/moby#35437).
func ApplySwarmLeaderAddressWorkaround(node map[string]any) {
	managerStatus, _ := node["ManagerStatus"].(map[string]any)
	if managerStatus == nil {
		return
	}
	leader, _ := managerStatus["Leader"].(bool)
	if !leader {
		return
	}
	status, _ := node["Status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
		node["Status"] = status
	}
	addr, _ := managerStatus["Addr"].(string)
	statusAddr, _ := status["Addr"].(string)
	if strings.Count(addr, ":") == 1 {
		host, _, _ := strings.Cut(addr, ":")
		if host != "" {
			status["Addr"] = host
			return
		}
	}
	status["Addr"] = statusAddr
}
