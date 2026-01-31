package iptables_state

// Request represents the parameters for the iptables_state module
type Request struct {
	// Path to save or restore iptables state
	Path string `json:"path"`

	// State: saved or restored
	State string `json:"state"`

	// Table to operate on (filter, nat, mangle, raw, security)
	// If not specified, all tables are saved/restored
	Table string `json:"table,omitempty"`

	// Save or restore packet and byte counters
	// When true, the module is not idempotent
	Counters bool `json:"counters,omitempty"`

	// For state=restored: if true, don't flush existing rules before restoring
	// Policies are still updated
	Noflush bool `json:"noflush,omitempty"`

	// IP version: ipv4 or ipv6
	IPVersion string `json:"ip_version,omitempty"`

	// Wait N seconds for the xtables lock
	Wait int `json:"wait,omitempty"`

	// Path to modprobe (optional)
	Modprobe string `json:"modprobe,omitempty"`
}

// Response represents the result of the iptables_state module execution
type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed,omitempty"`
	Msg     string `json:"msg,omitempty"`

	// Whether the state was successfully applied (for restored)
	Applied bool `json:"applied,omitempty"`

	// The iptables state when the module started
	InitialState []string `json:"initial_state,omitempty"`

	// The state that was saved to file (for state=saved)
	Saved []string `json:"saved,omitempty"`

	// The state that was restored from file (for state=restored)
	Restored []string `json:"restored,omitempty"`

	// Tables parsed from the initial state, keyed by table name
	Tables map[string][]string `json:"tables,omitempty"`
}
