package schema

#TcpFlags: {
	flags?:     [...string]
	flags_set?: [...string]
}

#Iptables: {
	table?:              string
	chain?:              string
	state?:              string
	action?:             string
	rule_num?:           int
	protocol?:           string
	source?:             string
	destination?:        string
	match?:              [...string]
	jump?:               string
	goto?:               string
	in_interface?:       string
	out_interface?:      string
	source_port?:        string
	destination_port?:   string
	destination_ports?:  [...string]
	ctstate?:            [...string]
	comment?:            string
	icmp_type?:          string
	fragment?:           string
	tcp_flags?:          #TcpFlags
	syn?:                string
	limit?:              string
	limit_burst?:        string
	log_prefix?:         string
	log_level?:          string
	reject_with?:        string
	to_destination?:     string
	to_source?:          string
	to_ports?:           string
	gateway?:            string
	src_range?:          string
	dst_range?:          string
	set_counters?:       string
	set_dscp_mark?:      string
	set_dscp_mark_class?: string
	uid_owner?:          string
	gid_owner?:          string
	match_set?:          string
	match_set_flags?:    string
	flush:               bool | *false
	policy?:             string
	chain_management:    bool | *false
	ip_version?:         string
	wait?:               int
	numeric:             bool | *false
}

#IptablesState: {
	path:        string
	state:       string
	table?:      string
	counters:    bool | *false
	noflush:     bool | *false
	ip_version?: string
	wait?:       int
	modprobe?:   string
}
