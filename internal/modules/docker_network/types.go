package docker_network

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name       string            `json:"name"`
	State      string            `json:"state"` // present, absent
	Driver     string            `json:"driver"`
	Options    map[string]string `json:"options"`
	IPAMConfig []IPAMConfig      `json:"ipam_config"`
	Labels     map[string]string `json:"labels"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	Scope      string            `json:"scope"`
	Force      bool              `json:"force"`

	// Phase 4.1: Connected Containers Management
	Connected []ConnectedContainer `json:"connected,omitempty"` // Containers to connect to this network
	Appends   bool                 `json:"appends,omitempty"`   // Don't disconnect existing containers

	// Phase 4.2: Additional Options
	EnableIPv6 bool   `json:"enable_ipv6,omitempty"` // Enable IPv6 on the network
	ConfigOnly bool   `json:"config_only,omitempty"` // Create a config-only network (for swarm)
	ConfigFrom string `json:"config_from,omitempty"` // Name of network to copy config from
	Ingress    bool   `json:"ingress,omitempty"`     // Create an ingress network (swarm)

	// Phase 4.2: IPAM Driver
	IPAMDriver        string            `json:"ipam_driver,omitempty"`
	IPAMDriverOptions map[string]string `json:"ipam_driver_options,omitempty"`
}

// ConnectedContainer represents a container to connect to the network with optional endpoint settings
type ConnectedContainer struct {
	Name        string   `json:"name"`                    // Container name or ID
	IPv4Address string   `json:"ipv4_address,omitempty"`  // Static IPv4 address
	IPv6Address string   `json:"ipv6_address,omitempty"`  // Static IPv6 address
	Aliases     []string `json:"aliases,omitempty"`       // Network aliases
	Links       []string `json:"links,omitempty"`         // Legacy links
	DriverOpts  map[string]string `json:"driver_opts,omitempty"` // Per-container driver options
}

type IPAMConfig struct {
	Subnet     string            `json:"subnet"`
	Gateway    string            `json:"gateway"`
	IPRange    string            `json:"ip_range"`
	AuxAddress map[string]string `json:"aux_address,omitempty"` // Auxiliary addresses
}

type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	NetworkID string                 `json:"network_id,omitempty"`
	Diff      map[string]interface{} `json:"diff,omitempty"` // Phase 4.3: Diff reporting
}
