package schema

#DockerContainerInfo: {
	#DockerCommon
	name: string
}

#DockerImageInfo: {
	#DockerCommon
	name: string
}

#DockerNetworkInfo: {
	#DockerCommon
	name: string
}

#DockerVolumeInfo: {
	#DockerCommon
	name: string
}

#DockerHostInfo: {
	#DockerCommon
	containers: bool | *false
	images:     bool | *false
	volumes:    bool | *false
	disk_usage: bool | *false
}

#DockerSwarmInfo: {
	#DockerCommon
	nodes:   bool | *false
	verbose: bool | *false
}

#DockerSwarmServiceInfo: {
	#DockerCommonLight
	name: string
}

#DockerNodeInfo: {
	#DockerCommonLight
	name?: string
	self:  bool | *false
}
