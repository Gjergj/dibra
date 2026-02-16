package schema

#FilePerms: {
	mode?:  string
	owner?: string
	group?: string
}

#Host: {
	name:             string
	host:             string
	port:             int | *22
	user:             string
	password?:        string
	ssh_key_path?:    string
	become:           bool | *false
	become_password?: string
	groups:           [...string] | *[]
}

#Task: {
	name:      string
	register?: string
	vars?: {[string]: _}

	{ping:                     #Ping} |
	{apt:                      #Apt} |
	{apt_key:                  #AptKey} |
	{apt_repository:           #AptRepository} |
	{file:                     #File} |
	{copy:                     #Copy} |
	{fetch:                    #Fetch} |
	{uri:                      #URI} |
	{cron:                     #Cron} |
	{ufw:                      #UFW} |
	{user:                     #User} |
	{group:                    #Group} |
	{systemd_service:          #SystemdService} |
	{systemd:                  #SystemdService} |
	{service:                  #Service} |
	{service_facts:            #ServiceFacts} |
	{command:                  #Command} |
	{shell:                    #Shell} |
	{script:                   #Script} |
	{unarchive:                #Unarchive} |
	{template:                 #Template} |
	{slurp:                    #Slurp} |
	{tempfile:                 #Tempfile} |
	{find:                     #Find} |
	{reboot:                   #Reboot} |
	{git:                      #Git} |
	{lineinfile:               #Lineinfile} |
	{blockinfile:              #Blockinfile} |
	{replace:                  #Replace} |
	{iptables:                 #Iptables} |
	{iptables_state:           #IptablesState} |
	{docker_container:         #DockerContainer} |
	{docker_image:             #DockerImage} |
	{docker_network:           #DockerNetwork} |
	{docker_volume:            #DockerVolume} |
	{docker_prune:             #DockerPrune} |
	{docker_login:             #DockerLogin} |
	{docker_swarm:             #DockerSwarm} |
	{docker_swarm_service:     #DockerSwarmService} |
	{docker_node:              #DockerNode} |
	{docker_compose:           #DockerCompose} |
	{docker_compose_v2_run:    #DockerComposeV2Run} |
	{docker_secret:            #DockerSecret} |
	{docker_config:            #DockerConfig} |
	{docker_stack:             #DockerStack} |
	{docker_container_exec:    #DockerContainerExec} |
	{docker_container_copy_into: #DockerContainerCopyInto} |
	{docker_image_build:       #DockerImageBuild} |
	{docker_image_load:        #DockerImageLoad} |
	{docker_image_export:      #DockerImageExport} |
	{docker_container_info:    #DockerContainerInfo} |
	{docker_image_info:        #DockerImageInfo} |
	{docker_network_info:      #DockerNetworkInfo} |
	{docker_volume_info:       #DockerVolumeInfo} |
	{docker_host_info:         #DockerHostInfo} |
	{docker_swarm_info:        #DockerSwarmInfo} |
	{docker_swarm_service_info: #DockerSwarmServiceInfo} |
	{docker_node_info:         #DockerNodeInfo}
}

#Playbook: {
	hosts:       [...#Host]
	tasks:       [...#Task]
	vars?:       {[string]: _}
	vars_files?: [...string]
	vars_merge?: "replace" | "merge"
	inventory?:  string
}
