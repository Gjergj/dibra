package ufw

type Request struct {
	// State commands
	State   string `json:"state,omitempty"`   // enabled, disabled, reloaded, reset
	Logging string `json:"logging,omitempty"` // on, off, low, medium, high, full

	// Default policy
	Default   string `json:"default,omitempty"` // allow, deny, reject (alias: policy)
	Policy    string `json:"policy,omitempty"`  // alias for default
	Direction string `json:"direction,omitempty"`

	// Rule commands
	Rule   string `json:"rule,omitempty"` // allow, deny, limit, reject
	Delete bool   `json:"delete"`
	Insert int    `json:"insert,omitempty"`

	// Positioning
	InsertRelativeTo string `json:"insert_relative_to,omitempty"` // zero, first-ipv4, last-ipv4, first-ipv6, last-ipv6

	// Interface options
	Interface    string `json:"interface,omitempty"`     // alias: if
	InterfaceIn  string `json:"interface_in,omitempty"`  // alias: if_in
	InterfaceOut string `json:"interface_out,omitempty"` // alias: if_out

	// Address/Port options
	FromIP   string `json:"from_ip,omitempty"`   // aliases: from, src
	FromPort string `json:"from_port,omitempty"` // source port
	ToIP     string `json:"to_ip,omitempty"`     // aliases: dest, to
	ToPort   string `json:"to_port,omitempty"`   // aliases: port

	// Protocol
	Proto string `json:"proto,omitempty"` // any, tcp, udp, ipv6, esp, ah, gre, igmp, vrrp

	// Application profile
	Name string `json:"name,omitempty"` // alias: app

	// Route
	Route bool `json:"route"`

	// Logging per rule
	Log bool `json:"log"`

	// Comment
	Comment string `json:"comment,omitempty"`

	// Aliases handling (populated by YAML unmarshaling)
	From string `json:"from,omitempty"` // alias for from_ip
	Src  string `json:"src,omitempty"`  // alias for from_ip
	Dest string `json:"dest,omitempty"` // alias for to_ip
	To   string `json:"to,omitempty"`   // alias for to_ip
	Port string `json:"port,omitempty"` // alias for to_port
	If   string `json:"if,omitempty"`   // alias for interface
	IfIn string `json:"if_in,omitempty"`  // alias for interface_in
	IfOut string `json:"if_out,omitempty"` // alias for interface_out
	App  string `json:"app,omitempty"`  // alias for name
	Protocol string `json:"protocol,omitempty"` // alias for proto
}

type Response struct {
	Changed  bool     `json:"changed"`
	Failed   bool     `json:"failed"`
	Msg      string   `json:"msg,omitempty"`
	Commands []string `json:"commands,omitempty"`
}
