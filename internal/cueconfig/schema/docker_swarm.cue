package schema

#PortPublish: {
	published_port: int
	target_port:    int
	protocol?:      string
	mode?:          string
}

#ServiceConfigReference: {
	config_name: string
	config_id?:  string
	filename?:   string
	uid?:        string
	gid?:        string
	mode?:       int
}

#ServiceSecretReference: {
	secret_name: string
	secret_id?:  string
	filename?:   string
	uid?:        string
	gid?:        string
	mode?:       int
}

#ServiceHealthcheck: {
	test?:         [...string]
	interval?:     string
	timeout?:      string
	start_period?: string
	retries?:      int
}

#ServiceMount: {
	type?:             string
	source?:           string
	target?:           string
	read_only:         bool | *false
	consistency?:      string
	bind_propagation?: string
	volume_driver?:    string
	volume_labels?:    {[string]: string}
	volume_options?:   {[string]: string}
	volume_no_copy:    bool | *false
	tmpfs_size?:       int
	tmpfs_mode?:       int
}

#DockerSwarmService: {
	#DockerCommonLight
	name:                    string
	image:                   string
	state?:                  string
	replicas?:               int
	args?:                   [...string]
	command?:                _       // string or list
	env?:                    {[string]: string}
	publish?:                [...#PortPublish]
	networks?:               [...string]
	labels?:                 {[string]: string}
	limit_cpu?:              number
	limit_memory?:           int
	constraint?:             [...string]
	restart_policy?:         string
	force_update:            bool | *false
	configs?:                [...#ServiceConfigReference]
	secrets?:                [...#ServiceSecretReference]
	update_delay?:           string
	update_parallelism?:     int
	update_failure_action?:  string
	update_order?:           string
	update_monitor?:         string
	max_failure_ratio?:      number
	rollback_delay?:         string
	rollback_parallelism?:   int
	rollback_failure_action?: string
	rollback_order?:         string
	rollback_monitor?:       string
	rollback_max_failure_ratio?: number
	healthcheck?:            #ServiceHealthcheck
	dns?:                    [...string]
	dns_search?:             [...string]
	dns_options?:            [...string]
	hosts?:                  [...string]
	mounts?:                 [...#ServiceMount]
}

#DockerNode: {
	#DockerCommonLight
	hostname?:         string
	self:              bool | *false
	availability?:     string
	role?:             string
	labels?:           {[string]: string}
	labels_state?:     string
	labels_to_remove?: [...string]
}
