package schema

#DockerCommon: {
	docker_host?:    string
	tls:             bool | *false
	validate_certs:  bool | *false
	ca_path?:        string
	client_cert?:    string
	client_key?:     string
	api_version?:    string
	timeout?:        int
	debug:           bool | *false
}

#DockerCommonLight: {
	docker_host?: string
	tls:          bool | *false
}

#DockerHealthcheck: {
	test:          [...string]
	interval?:     string
	timeout?:      string
	start_period?: string
	retries?:      int
}

#DockerUlimit: {
	name: string
	soft: int
	hard: int
}

#DockerNetworkEndpoint: {
	name:          string
	ipv4_address?: string
	ipv6_address?: string
	links?:        [...string]
	aliases?:      [...string]
}

#NetworkConnectedContainer: {
	name:          string
	ipv4_address?: string
	ipv6_address?: string
	aliases?:      [...string]
	links?:        [...string]
	driver_opts?:  {[string]: string}
}

#IPAMConfig: {
	subnet?:      string
	gateway?:     string
	ip_range?:    string
	aux_address?: {[string]: string}
}
