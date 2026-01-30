package iptables

// Request represents the parameters for the iptables module
type Request struct {
	// Table to operate on (filter, nat, mangle, raw, security)
	Table string `json:"table,omitempty"`

	// Chain to operate on (INPUT, FORWARD, OUTPUT, PREROUTING, POSTROUTING, or custom)
	Chain string `json:"chain,omitempty"`

	// State: present or absent
	State string `json:"state,omitempty"`

	// Action: append or insert
	Action string `json:"action,omitempty"`

	// RuleNum: Insert rule at this position (only with action=insert)
	RuleNum int `json:"rule_num,omitempty"`

	// Protocol (tcp, udp, icmp, all, etc.)
	Protocol string `json:"protocol,omitempty"`

	// Source address/network
	Source string `json:"source,omitempty"`

	// Destination address/network
	Destination string `json:"destination,omitempty"`

	// Match extensions to use (e.g., state, conntrack, comment)
	Match []string `json:"match,omitempty"`

	// Jump target (ACCEPT, DROP, REJECT, LOG, DNAT, SNAT, MASQUERADE, etc.)
	Jump string `json:"jump,omitempty"`

	// Goto target (user-defined chain)
	Goto string `json:"goto,omitempty"`

	// Input interface
	InInterface string `json:"in_interface,omitempty"`

	// Output interface
	OutInterface string `json:"out_interface,omitempty"`

	// Source port (or range first:last)
	SourcePort string `json:"source_port,omitempty"`

	// Destination port (or range first:last)
	DestinationPort string `json:"destination_port,omitempty"`

	// Destination ports (multiple ports, uses multiport extension)
	DestinationPorts []string `json:"destination_ports,omitempty"`

	// Connection tracking states (NEW, ESTABLISHED, RELATED, INVALID, UNTRACKED, SNAT, DNAT)
	Ctstate []string `json:"ctstate,omitempty"`

	// Comment for the rule (uses comment extension)
	Comment string `json:"comment,omitempty"`

	// ICMP type (for icmp/icmpv6 protocol)
	IcmpType string `json:"icmp_type,omitempty"`

	// Fragment matching
	Fragment string `json:"fragment,omitempty"`

	// TCP flags matching
	TcpFlags *TcpFlags `json:"tcp_flags,omitempty"`

	// SYN flag matching: ignore, match, negate
	Syn string `json:"syn,omitempty"`

	// Limit rate (e.g., "5/second", "100/minute")
	Limit string `json:"limit,omitempty"`

	// Limit burst
	LimitBurst string `json:"limit_burst,omitempty"`

	// Log prefix (for LOG target)
	LogPrefix string `json:"log_prefix,omitempty"`

	// Log level (for LOG target)
	LogLevel string `json:"log_level,omitempty"`

	// Reject with (for REJECT target)
	RejectWith string `json:"reject_with,omitempty"`

	// DNAT destination
	ToDestination string `json:"to_destination,omitempty"`

	// SNAT source
	ToSource string `json:"to_source,omitempty"`

	// Port translation (for DNAT/SNAT/MASQUERADE)
	ToPorts string `json:"to_ports,omitempty"`

	// Gateway for TEE target
	Gateway string `json:"gateway,omitempty"`

	// Source IP range (uses iprange extension)
	SrcRange string `json:"src_range,omitempty"`

	// Destination IP range (uses iprange extension)
	DstRange string `json:"dst_range,omitempty"`

	// Set counters (packet:byte)
	SetCounters string `json:"set_counters,omitempty"`

	// DSCP mark value
	SetDscpMark string `json:"set_dscp_mark,omitempty"`

	// DSCP mark class
	SetDscpMarkClass string `json:"set_dscp_mark_class,omitempty"`

	// UID owner (for owner match)
	UidOwner string `json:"uid_owner,omitempty"`

	// GID owner (for owner match)
	GidOwner string `json:"gid_owner,omitempty"`

	// Match set (ipset name)
	MatchSet string `json:"match_set,omitempty"`

	// Match set flags (src, dst, src,dst, dst,src)
	MatchSetFlags string `json:"match_set_flags,omitempty"`

	// Flush chain/table
	Flush bool `json:"flush,omitempty"`

	// Set chain policy (ACCEPT, DROP, QUEUE, RETURN)
	Policy string `json:"policy,omitempty"`

	// Chain management (create/delete chains)
	ChainManagement bool `json:"chain_management,omitempty"`

	// IP version: ipv4, ipv6, both
	IPVersion string `json:"ip_version,omitempty"`

	// Wait for xtables lock (seconds, 0 means no wait)
	Wait int `json:"wait,omitempty"`

	// Numeric mode (skip DNS lookups)
	Numeric bool `json:"numeric,omitempty"`
}

// TcpFlags specifies TCP flag matching
type TcpFlags struct {
	Flags    []string `json:"flags,omitempty"`
	FlagsSet []string `json:"flags_set,omitempty"`
}

// Response represents the result of the iptables module execution
type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed,omitempty"`
	Msg     string `json:"msg,omitempty"`

	// Commands executed
	Commands []string `json:"commands,omitempty"`

	// State information
	State   string `json:"state,omitempty"`
	Table   string `json:"table,omitempty"`
	Chain   string `json:"chain,omitempty"`
	Rule    string `json:"rule,omitempty"`
	Policy  string `json:"policy,omitempty"`
	Flushed bool   `json:"flushed,omitempty"`
}
