package docker_swarm

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	State           string   `json:"state"`
	AdvertiseAddr   string   `json:"advertise_addr"`
	ListenAddr      string   `json:"listen_addr"`
	Force           bool     `json:"force"`
	ForceNewCluster bool     `json:"force_new_cluster"`
	RemoteAddrs     []string `json:"remote_addrs"`
	JoinToken       string   `json:"join_token"`
	NodeID          string   `json:"node_id"`
	DataPathAddr    string   `json:"data_path_addr"`
	DataPathPort    *int     `json:"data_path_port"`

	DefaultAddrPool []string `json:"default_addr_pool"`
	SubnetSize      *int     `json:"subnet_size"`

	Name                       string            `json:"name"`
	Labels                     map[string]string `json:"labels"`
	SnapshotInterval           *int              `json:"snapshot_interval"`
	KeepOldSnapshots           *int              `json:"keep_old_snapshots"`
	LogEntriesForSlowFollowers *int              `json:"log_entries_for_slow_followers"`
	HeartbeatTick              *int              `json:"heartbeat_tick"`
	ElectionTick               *int              `json:"election_tick"`
	DispatcherHeartbeatPeriod  *int              `json:"dispatcher_heartbeat_period"`
	TaskHistoryRetentionLimit  *int              `json:"task_history_retention_limit"`
	NodeCertExpiry             *int              `json:"node_cert_expiry"`
	SigningCACert              string            `json:"signing_ca_cert"`
	SigningCAKey               string            `json:"signing_ca_key"`
	CAForceRotate              *int              `json:"ca_force_rotate"`
	AutolockManagers           *bool             `json:"autolock_managers"`
	RotateWorkerToken          bool              `json:"rotate_worker_token"`
	RotateManagerToken         bool              `json:"rotate_manager_token"`
}

type Diff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

type Response struct {
	Changed    bool           `json:"changed"`
	Failed     bool           `json:"failed"`
	Msg        string         `json:"msg,omitempty"`
	Actions    []string       `json:"actions,omitempty"`
	SwarmFacts map[string]any `json:"swarm_facts,omitempty"`
	Diff       *Diff          `json:"diff,omitempty"`
}
