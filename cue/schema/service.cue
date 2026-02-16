package schema

#SystemdService: {
	name?:          string
	state?:         string
	enabled?:       bool
	masked?:        bool
	daemon_reload:  bool | *false
	daemon_reexec:  bool | *false
	scope?:         string
	no_block:       bool | *false
	force:          bool | *false
}

#Service: {
	name:       string
	state?:     string
	enabled?:   bool
	arguments?: string
	pattern?:   string
	sleep?:     int
	use?:       string
}

#ServiceFacts: {
}
