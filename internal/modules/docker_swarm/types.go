package docker_swarm

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	State           string   `json:"state"` // present (init), join, absent (leave)
	AdvertiseAddr   string   `json:"advertise_addr"`
	ListenAddr      string   `json:"listen_addr"`
	ForceNewCluster bool     `json:"force_new_cluster"`
	RemoteAddrs     []string `json:"remote_addrs"` // For joining
	JoinToken       string   `json:"join_token"`   // For joining
	NodeID          string   `json:"node_id"`      // For removing? usually absent just leaves local
	Force           bool     `json:"force"`        // Force leave
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	SwarmID    string `json:"swarm_id,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
	JoinTokens struct {
		Worker  string `json:"worker,omitempty"`
		Manager string `json:"manager,omitempty"`
	} `json:"join_tokens,omitempty"`
}
