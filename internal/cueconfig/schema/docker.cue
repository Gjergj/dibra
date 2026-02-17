package schema

#DockerContainer: {
	#DockerCommon
	name:              string
	image?:            string
	state?:            string
	command?:          _       // string or list
	entrypoint?:       _       // string or list
	args?:             [...string]
	env?:              {[string]: string}
	exposed_ports?:    [...string]
	ports?:            [...string]
	volumes?:          [...string]
	network_mode?:     string
	networks?:         [...#DockerNetworkEndpoint]
	networks_append:   bool | *false
	restart_policy?:   string
	auto_remove:       bool | *false
	privileged:        bool | *false
	user?:             string
	working_dir?:      string
	hostname?:         string
	domainname?:       string
	labels?:           {[string]: string}
	links?:            [...string]
	log_driver?:       string
	log_options?:      {[string]: string}
	cap_add?:          [...string]
	cap_drop?:         [...string]
	devices?:          [...string]
	healthcheck?:      #DockerHealthcheck
	init:              bool | *false
	tmpfs?:            [...string]
	shm_size?:         string
	ulimits?:          [...#DockerUlimit]
	sysctls?:          {[string]: string}
	security_opt?:     [...string]
	cpus?:             number
	memory?:           string
	memory_swap?:      string
	pids_limit?:       int
	comparisons?:      {[string]: string}
	recreate?:         _       // bool or string
	force_kill:        bool | *false
	keep_volumes:      bool | *false
	pull?:             _       // bool or string
	registry_username?: string
	registry_password?: string
}

#DockerImage: {
	#DockerCommon
	name:               string
	tag?:               string
	repository?:        string
	state?:             string
	source?:            string
	push:               bool | *false
	archive_path?:      string
	dockerfile?:        string
	build_path?:        string
	keep_image:         bool | *false
	pull?:              _       // string or bool
	force_pull:         bool | *false
	force_remove:       bool | *false
	force_tag:          bool | *false
	force_source:       bool | *false
	registry_username?: string
	registry_password?: string
}

#DockerNetwork: {
	#DockerCommon
	name:                 string
	state?:               string
	driver?:              string
	options?:             {[string]: string}
	ipam_config?:         [...#IPAMConfig]
	labels?:              {[string]: string}
	internal:             bool | *false
	attachable:           bool | *false
	scope?:               string
	force:                bool | *false
	connected?:           [...#NetworkConnectedContainer]
	appends:              bool | *false
	enable_ipv6:          bool | *false
	config_only:          bool | *false
	config_from?:         string
	ingress:              bool | *false
	ipam_driver?:         string
	ipam_driver_options?: {[string]: string}
}

#DockerVolume: {
	#DockerCommon
	name:            string
	state?:          string
	driver?:         string
	driver_options?: {[string]: string}
	labels?:         {[string]: string}
	recreate?:       string
	force:           bool | *false
}

#DockerPrune: {
	#DockerCommonLight
	containers:           bool | *false
	containers_filters?:  {[string]: string}
	images:               bool | *false
	images_filters?:      {[string]: string}
	networks:             bool | *false
	networks_filters?:    {[string]: string}
	volumes:              bool | *false
	volumes_filters?:     {[string]: string}
	builder:              bool | *false
	builder_cache_all:    bool | *false
	builder_cache_filters?: {[string]: string}
}

#DockerLogin: {
	#DockerCommonLight
	username:     string
	password:     string
	registry?:    string
	email?:       string
	config_path?: string
	state?:       string
	relogin:      bool | *false
}

#DockerSwarm: {
	#DockerCommonLight
	state?:             string
	advertise_addr?:    string
	listen_addr?:       string
	force_new_cluster:  bool | *false
	remote_addrs?:      [...string]
	join_token?:        string
	node_id?:           string
	force:              bool | *false
}

#DockerCompose: {
	#DockerCommonLight
	project_src:      string
	project_name?:    string
	files?:           [...string]
	state?:           string
	services?:        [...string]
	scale?:           {[string]: int}
	build:            bool | *false
	pull:             bool | *false
	remove_orphans:   bool | *false
	env?:             {[string]: string}
	profiles?:        [...string]
}

#DockerComposeV2Run: {
	#DockerCommonLight
	project_src:      string
	project_name?:    string
	files?:           [...string]
	service:          string
	argv?:            [...string]
	command?:         string
	build:            bool | *false
	cap_add?:         [...string]
	cap_drop?:        [...string]
	entrypoint?:      string
	interactive?:     bool
	labels?:          [...string]
	name?:            string
	no_deps:          bool | *false
	publish?:         [...string]
	quiet_pull:       bool | *false
	remove_orphans:   bool | *false
	cleanup:          bool | *false
	service_ports:    bool | *false
	use_aliases:      bool | *false
	volumes?:         [...string]
	chdir?:           string
	detach:           bool | *false
	user?:            string
	stdin?:           string
	stdin_add_newline?: bool
	strip_empty_ends?:  bool
	tty?:             bool
	env?:             {[string]: string}
	profiles?:        [...string]
}

#DockerSecret: {
	#DockerCommonLight
	name:        string
	data?:       string
	data_is_b64: bool | *false
	labels?:     {[string]: string}
	force:       bool | *false
	state?:      string
}

#DockerConfig: {
	#DockerCommonLight
	name:        string
	data?:       string
	data_is_b64: bool | *false
	labels?:     {[string]: string}
	force:       bool | *false
	state?:      string
}

#DockerStack: {
	#DockerCommonLight
	name:                 string
	compose_file?:        string
	state?:               string
	with_registry_auth:   bool | *false
	prune:                bool | *false
	resolve_image?:       string
}

#DockerContainerExec: {
	#DockerCommonLight
	container:         string
	argv?:             [...string]
	command?:          string
	chdir?:            string
	detach:            bool | *false
	user?:             string
	stdin?:            string
	stdin_add_newline: bool | *false
	strip_empty_ends:  bool | *false
	tty:               bool | *false
	env?:              {[string]: string}
}

#DockerContainerCopyInto: {
	#DockerCommonLight
	container:      string
	path?:          string
	content?:       string
	content_is_b64: bool | *false
	container_path: string
	follow:         bool | *false
	local_follow:   bool | *false
	owner_id?:      int
	group_id?:      int
	mode?:          string
	force?:         bool
}

#DockerImageBuild: {
	#DockerCommonLight
	name:        string
	tag?:        string
	path:        string
	dockerfile?: string
	cache_from?: [...string]
	pull:        bool | *false
	network?:    string
	nocache:     bool | *false
	etc_hosts?:  {[string]: string}
	args?:       {[string]: string}
	target?:     string
	platform?:   [...string]
	shm_size?:   string
	labels?:     {[string]: string}
	rebuild?:    string
	push:        bool | *false
}

#DockerImageLoad: {
	#DockerCommonLight
	path: string
}

#DockerImageExport: {
	#DockerCommonLight
	names?: [...string]
	name?:  string
	tag?:   string
	path:   string
	force:  bool | *false
}
