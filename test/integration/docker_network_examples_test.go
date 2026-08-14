//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// Ansible docs: https://docs.ansible.com/projects/ansible/latest/collections/community/docker/docker_network_module.html
// Already covered by docker_network_parity_test.go / phase4: create/absent lifecycle,
// connected replace + appends, labels, custom IPAM, IPv6, dual-stack IPAM, check/diff/force.
// This test adds the remaining docs snippets.

func TestPlaybook_DockerNetworkDocsExamples(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	remoteExec(t, client, "docker pull alpine:latest")

	t.Run("driver options bridge name", func(t *testing.T) {
		// Docs: Create a network with driver_options com.docker.network.bridge.name: net2
		name := "dibra-ex-network-two"
		bridge := "net2"
		remoteExec(t, client, "docker network rm "+name+" || true")
		defer remoteExec(t, client, "docker network rm "+name+" || true")

		created := runNetworkModule(t, client, "docs-bridge-name", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.name: `+bridge+`
`)
		if created["changed"] != true || created["failed"] == true {
			t.Fatalf("create = %#v", created)
		}
		inspect := dockerInspectNetwork(t, client, name)
		options, _ := inspect["Options"].(map[string]any)
		if fmt.Sprint(options["com.docker.network.bridge.name"]) != bridge {
			t.Fatalf("bridge name option = %#v", options)
		}
		iface := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" --format '{{index .Options \"com.docker.network.bridge.name\"}}'"))
		if iface != bridge {
			t.Fatalf("expected bridge %s, got %q", bridge, iface)
		}

		idem := runNetworkModule(t, client, "docs-bridge-name-idem", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.name: `+bridge+`
`)
		if idem["changed"] != false {
			t.Fatalf("idempotent = %#v", idem)
		}
	})

	t.Run("disconnect one container from a full list", func(t *testing.T) {
		// Docs: connected: "{{ fulllist|difference(['container_a']) }}"
		// Dibra module-arg rendering does not evaluate Jinja filters, so the
		// remaining names are supplied directly (same outcome).
		netName := "dibra-ex-docs-diff"
		a, b, c := "dibra-ex-docs-a", "dibra-ex-docs-b", "dibra-ex-docs-c"
		remoteExec(t, client, "docker rm -f "+a+" "+b+" "+c+" || true")
		remoteExec(t, client, "docker network rm "+netName+" || true")
		remoteExec(t, client, "docker run -d --name "+a+" alpine:latest sleep 3600")
		remoteExec(t, client, "docker run -d --name "+b+" alpine:latest sleep 3600")
		remoteExec(t, client, "docker run -d --name "+c+" alpine:latest sleep 3600")
		defer func() {
			remoteExec(t, client, "docker rm -f "+a+" "+b+" "+c+" || true")
			remoteExec(t, client, "docker network rm "+netName+" || true")
		}()

		_ = runNetworkModule(t, client, "docs-diff-all", `
      name: `+netName+`
      connected:
        - `+a+`
        - `+b+`
        - `+c+`
`)
		assertConnected(t, client, netName, a, b, c)

		removed := runNetworkModule(t, client, "docs-diff-one", `
      name: `+netName+`
      connected:
        - `+b+`
        - `+c+`
`)
		if removed["changed"] != true {
			t.Fatalf("difference disconnect = %#v", removed)
		}
		connected := remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
		if strings.Contains(connected, a) || !strings.Contains(connected, b) || !strings.Contains(connected, c) {
			t.Fatalf("after removing %s: %q", a, connected)
		}
	})

	t.Run("absent disconnects remaining containers", func(t *testing.T) {
		// Docs: Delete a network, disconnecting all containers
		netName := "dibra-ex-docs-absent"
		ctr := "dibra-ex-docs-absent-c"
		remoteExec(t, client, "docker rm -f "+ctr+" || true")
		remoteExec(t, client, "docker network rm "+netName+" || true")
		remoteExec(t, client, "docker run -d --name "+ctr+" alpine:latest sleep 3600")
		defer remoteExec(t, client, "docker rm -f "+ctr+" || true")
		defer remoteExec(t, client, "docker network rm "+netName+" || true")

		_ = runNetworkModule(t, client, "docs-absent-create", `
      name: `+netName+`
      connected:
        - `+ctr+`
`)
		assertConnected(t, client, netName, ctr)

		absent := runNetworkModule(t, client, "docs-absent", `
      name: `+netName+`
      state: absent
`)
		if absent["changed"] != true || absent["network"] != nil {
			t.Fatalf("absent = %#v", absent)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker network inspect "+netName+" >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatal("network still exists after absent")
		}
		networks := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+ctr)
		if strings.Contains(networks, netName) {
			t.Fatalf("container still attached: %s", networks)
		}
	})

	t.Run("aliases network_name incremental containers options", func(t *testing.T) {
		netName := "dibra-ex-docs-alias"
		c1, c2 := "dibra-ex-docs-alias-c1", "dibra-ex-docs-alias-c2"
		remoteExec(t, client, "docker rm -f "+c1+" "+c2+" || true")
		remoteExec(t, client, "docker network rm "+netName+" || true")
		remoteExec(t, client, "docker run -d --name "+c1+" alpine:latest sleep 3600")
		remoteExec(t, client, "docker run -d --name "+c2+" alpine:latest sleep 3600")
		defer func() {
			remoteExec(t, client, "docker rm -f "+c1+" "+c2+" || true")
			remoteExec(t, client, "docker network rm "+netName+" || true")
		}()

		created := runNetworkModule(t, client, "docs-alias-create", `
      network_name: `+netName+`
      options:
        com.docker.network.bridge.enable_icc: "true"
      containers:
        - `+c1+`
`)
		if created["changed"] != true {
			t.Fatalf("alias create = %#v", created)
		}
		assertConnected(t, client, netName, c1)
		inspect := dockerInspectNetwork(t, client, nameOr(created, netName))
		if inspect["Name"] != netName {
			t.Fatalf("Name = %#v", inspect["Name"])
		}

		appended := runNetworkModule(t, client, "docs-alias-incremental", `
      network_name: `+netName+`
      incremental: true
      containers:
        - `+c2+`
`)
		if appended["changed"] != true {
			t.Fatalf("incremental = %#v", appended)
		}
		assertConnected(t, client, netName, c1, c2)
	})
}

func TestPlaybook_DockerNetworkBlogBridge(t *testing.T) {
	// OneUptime: create-bridge-network.yml
	client := getClient(t)
	defer client.Close()
	name := "dibra-ex-app-network"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, "blog-bridge", `
      name: `+name+`
      driver: bridge
      state: present
`)
	if created["changed"] != true || created["failed"] == true {
		t.Fatalf("create = %#v", created)
	}
	network := networkMap(t, created)
	if network["Name"] != name {
		t.Fatalf("Name = %#v", network["Name"])
	}
	if fmt.Sprint(network["Driver"]) != "bridge" {
		t.Fatalf("Driver = %#v", network["Driver"])
	}
	subnet, gateway := ipamSubnetGateway(t, network)
	if subnet == "" || gateway == "" {
		t.Fatalf("expected IPAM subnet/gateway, got subnet=%q gateway=%q in %#v", subnet, gateway, network["IPAM"])
	}
	inspect := dockerInspectNetwork(t, client, name)
	if inspect["Name"] != name || fmt.Sprint(inspect["Driver"]) != "bridge" {
		t.Fatalf("inspect mismatch: %#v", inspect)
	}

	idem := runNetworkModule(t, client, "blog-bridge-idem", `
      name: `+name+`
      driver: bridge
      state: present
`)
	if idem["changed"] != false {
		t.Fatalf("idempotent = %#v", idem)
	}
}

func TestPlaybook_DockerNetworkBlogCustomIPAM(t *testing.T) {
	// OneUptime: custom-ipam-network.yml
	client := getClient(t)
	defer client.Close()
	backend, frontend := "dibra-ex-backend-network", "dibra-ex-frontend-network"
	remoteExec(t, client, "docker network rm "+backend+" "+frontend+" || true")
	defer remoteExec(t, client, "docker network rm "+backend+" "+frontend+" || true")

	backendResult := runNetworkModule(t, client, "blog-ipam-backend", `
      name: `+backend+`
      driver: bridge
      ipam_config:
        - subnet: "172.20.0.0/16"
          gateway: "172.20.0.1"
          iprange: "172.20.1.0/24"
      state: present
`)
	if backendResult["changed"] != true {
		t.Fatalf("backend = %#v", backendResult)
	}
	backendNet := networkMap(t, backendResult)
	subnet, gateway := ipamSubnetGateway(t, backendNet)
	if subnet != "172.20.0.0/16" || gateway != "172.20.0.1" {
		t.Fatalf("backend IPAM subnet=%q gateway=%q", subnet, gateway)
	}
	if ipamField(t, backendNet, "IPRange") != "172.20.1.0/24" {
		t.Fatalf("backend iprange = %#v", backendNet["IPAM"])
	}

	frontendResult := runNetworkModule(t, client, "blog-ipam-frontend", `
      name: `+frontend+`
      driver: bridge
      ipam_config:
        - subnet: "172.21.0.0/16"
          gateway: "172.21.0.1"
      state: present
`)
	if frontendResult["changed"] != true {
		t.Fatalf("frontend = %#v", frontendResult)
	}
	frontendNet := networkMap(t, frontendResult)
	subnet, gateway = ipamSubnetGateway(t, frontendNet)
	if subnet != "172.21.0.0/16" || gateway != "172.21.0.1" {
		t.Fatalf("frontend IPAM subnet=%q gateway=%q", subnet, gateway)
	}

	backendIdem := runNetworkModule(t, client, "blog-ipam-backend-idem", `
      name: `+backend+`
      driver: bridge
      ipam_config:
        - subnet: "172.20.0.0/16"
          gateway: "172.20.0.1"
          iprange: "172.20.1.0/24"
      state: present
`)
	if backendIdem["changed"] != false {
		t.Fatalf("backend idempotent = %#v", backendIdem)
	}
}

func TestPlaybook_DockerNetworkBlogIsolationAndConnect(t *testing.T) {
	// OneUptime: multi-network-setup.yml + manage-container-networks.yml + network-info.yml
	// Images nginx:1.25 / myapp-api:latest / postgres:16 / redis:7 are substituted with
	// alpine:latest. Host port mappings 80/443 are omitted to avoid colliding with the
	// integration container. Task no_log is not a Dibra keyword.
	client := getClient(t)
	defer client.Close()
	remoteExec(t, client, "docker pull alpine:latest")

	frontend, backend, data, monitoring := "dibra-ex-frontend", "dibra-ex-backend", "dibra-ex-data", "dibra-ex-monitoring"
	nginx, api, postgres, redis, worker := "dibra-ex-nginx", "dibra-ex-api", "dibra-ex-postgres", "dibra-ex-redis", "dibra-ex-worker"
	cleanup := func() {
		remoteExec(t, client, "docker rm -f "+nginx+" "+api+" "+postgres+" "+redis+" "+worker+" || true")
		remoteExec(t, client, "docker network rm "+frontend+" "+backend+" "+data+" "+monitoring+" || true")
	}
	cleanup()
	defer cleanup()

	infoTemplate := writeResultTemplate(t, "net_info")
	playbook := playbookHeader + `
  - name: Create frontend network
    community.docker.docker_network:
      name: ` + frontend + `
      driver: bridge
      ipam_config:
        - subnet: "172.30.0.0/24"

  - name: Create backend network
    community.docker.docker_network:
      name: ` + backend + `
      driver: bridge
      ipam_config:
        - subnet: "172.31.0.0/24"

  - name: Create data network
    community.docker.docker_network:
      name: ` + data + `
      driver: bridge
      ipam_config:
        - subnet: "172.32.0.0/24"
      internal: true

  - name: Create monitoring network
    community.docker.docker_network:
      name: ` + monitoring + `
      driver: bridge

  - name: Start nginx on frontend network
    community.docker.docker_container:
      name: ` + nginx + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]
      networks:
        - name: ` + frontend + `

  - name: Start API server on frontend and data networks
    community.docker.docker_container:
      name: ` + api + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]
      networks:
        - name: ` + frontend + `
          aliases:
            - api-server
        - name: ` + data + `
          aliases:
            - api

  - name: Start PostgreSQL on data network only
    community.docker.docker_container:
      name: ` + postgres + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]
      networks:
        - name: ` + data + `
          aliases:
            - db
            - database

  - name: Start Redis on data network
    community.docker.docker_container:
      name: ` + redis + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]
      networks:
        - name: ` + data + `
          aliases:
            - cache

  - name: Start worker
    community.docker.docker_container:
      name: ` + worker + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]

  - name: Connect API to the monitoring network
    community.docker.docker_container:
      name: ` + api + `
      image: alpine:latest
      state: started
      command: ["sleep", "3600"]
      networks:
        - name: ` + frontend + `
        - name: ` + data + `
        - name: ` + monitoring + `
      comparisons:
        networks: allow_more_present

  - name: Connect the worker to the data network
    community.docker.docker_network:
      name: ` + data + `
      connected:
        - ` + worker + `
      appends: true
      state: present

  - name: Get info about the data network
    community.docker.docker_network_info:
      name: ` + data + `
    register: net_info

  - name: Persist network info
    template:
      src: ` + infoTemplate + `
      dest: /tmp/dibra-ex-data-info.json
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("isolation playbook failed: %s", output)
	}

	assertNetworkSubnet(t, client, frontend, "172.30.0.0/24")
	assertNetworkSubnet(t, client, backend, "172.31.0.0/24")
	assertNetworkSubnet(t, client, data, "172.32.0.0/24")
	internal := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+data+" --format '{{.Internal}}'"))
	if internal != "true" {
		t.Fatalf("data network Internal=%q", internal)
	}

	nginxNets := containerNetworks(t, client, nginx)
	if _, ok := nginxNets[frontend]; !ok {
		t.Fatalf("nginx missing frontend: %#v", nginxNets)
	}
	if _, ok := nginxNets[data]; ok {
		t.Fatalf("nginx should not be on data: %#v", nginxNets)
	}

	apiNets := containerNetworks(t, client, api)
	for _, name := range []string{frontend, data, monitoring} {
		if _, ok := apiNets[name]; !ok {
			t.Fatalf("api missing %s: %#v", name, apiNets)
		}
	}
	assertAliasesContain(t, apiNets[frontend], "api-server")
	assertAliasesContain(t, apiNets[data], "api")

	postgresNets := containerNetworks(t, client, postgres)
	if _, ok := postgresNets[data]; !ok {
		t.Fatalf("postgres missing data: %#v", postgresNets)
	}
	if _, ok := postgresNets[frontend]; ok {
		t.Fatalf("postgres should not be on frontend: %#v", postgresNets)
	}
	assertAliasesContain(t, postgresNets[data], "db", "database")

	redisNets := containerNetworks(t, client, redis)
	if _, ok := redisNets[data]; !ok {
		t.Fatalf("redis missing data: %#v", redisNets)
	}
	assertAliasesContain(t, redisNets[data], "cache")

	assertConnected(t, client, data, worker, api, postgres, redis)

	info := readRemoteJSONMap(t, client, "/tmp/dibra-ex-data-info.json")
	if info["exists"] != true || info["changed"] != false {
		t.Fatalf("network info = %#v", info)
	}
	infoNet, _ := info["network"].(map[string]any)
	if infoNet["Name"] != data || fmt.Sprint(infoNet["Internal"]) != "true" {
		t.Fatalf("info network = %#v", infoNet)
	}
	containers, _ := infoNet["Containers"].(map[string]any)
	if len(containers) < 4 {
		t.Fatalf("expected >=4 connected containers, got %#v", containers)
	}
}

func TestPlaybook_DockerNetworkBlogLabeled(t *testing.T) {
	// OneUptime: labeled-network.yml
	client := getClient(t)
	defer client.Close()
	name := "dibra-ex-monitored-network"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, "blog-labeled", `
      name: `+name+`
      driver: bridge
      labels:
        environment: "production"
        team: "backend"
        managed_by: "ansible"
      driver_options:
        com.docker.network.bridge.name: "br-monitored"
        com.docker.network.bridge.enable_icc: "true"
        com.docker.network.bridge.enable_ip_masquerade: "true"
      ipam_config:
        - subnet: "172.25.0.0/24"
          gateway: "172.25.0.1"
      state: present
`)
	if created["changed"] != true {
		t.Fatalf("create = %#v", created)
	}
	inspect := dockerInspectNetwork(t, client, name)
	labels, _ := inspect["Labels"].(map[string]any)
	if fmt.Sprint(labels["environment"]) != "production" || fmt.Sprint(labels["team"]) != "backend" || fmt.Sprint(labels["managed_by"]) != "ansible" {
		t.Fatalf("labels = %#v", labels)
	}
	options, _ := inspect["Options"].(map[string]any)
	if fmt.Sprint(options["com.docker.network.bridge.name"]) != "br-monitored" {
		t.Fatalf("options = %#v", options)
	}
	if fmt.Sprint(options["com.docker.network.bridge.enable_icc"]) != "true" {
		t.Fatalf("enable_icc = %#v", options)
	}
	if fmt.Sprint(options["com.docker.network.bridge.enable_ip_masquerade"]) != "true" {
		t.Fatalf("masquerade = %#v", options)
	}
	subnet, gateway := ipamSubnetGateway(t, inspect)
	if subnet != "172.25.0.0/24" || gateway != "172.25.0.1" {
		t.Fatalf("IPAM subnet=%q gateway=%q", subnet, gateway)
	}

	idem := runNetworkModule(t, client, "blog-labeled-idem", `
      name: `+name+`
      driver: bridge
      labels:
        environment: "production"
        team: "backend"
        managed_by: "ansible"
      driver_options:
        com.docker.network.bridge.name: "br-monitored"
        com.docker.network.bridge.enable_icc: "true"
        com.docker.network.bridge.enable_ip_masquerade: "true"
      ipam_config:
        - subnet: "172.25.0.0/24"
          gateway: "172.25.0.1"
      state: present
`)
	if idem["changed"] != false {
		t.Fatalf("idempotent = %#v", idem)
	}
}

func TestPlaybook_DockerNetworkBlogCleanup(t *testing.T) {
	// OneUptime: cleanup-networks.yml
	// ignore_errors is not a Dibra task keyword; networks are created first so absent succeeds.
	client := getClient(t)
	defer client.Close()
	staging := "dibra-ex-staging-network"
	oldFrontend, oldBackend, testNet := "dibra-ex-old-frontend", "dibra-ex-old-backend", "dibra-ex-test-network"
	pruneTarget := "dibra-ex-prune-unused"
	remoteExec(t, client, "docker network rm "+staging+" "+oldFrontend+" "+oldBackend+" "+testNet+" "+pruneTarget+" || true")
	defer remoteExec(t, client, "docker network rm "+staging+" "+oldFrontend+" "+oldBackend+" "+testNet+" "+pruneTarget+" || true")

	_ = runNetworkModule(t, client, "blog-cleanup-staging", `
      name: `+staging+`
      state: present
`)
	forced := runNetworkModule(t, client, "blog-cleanup-staging-absent", `
      name: `+staging+`
      state: absent
      force: true
`)
	if forced["changed"] != true {
		t.Fatalf("force absent = %#v", forced)
	}

	pruneTemplate := writeResultTemplate(t, "prune_result")
	playbook := playbookHeader + `
  - name: Create old frontend
    community.docker.docker_network:
      name: ` + oldFrontend + `
      state: present

  - name: Create old backend
    community.docker.docker_network:
      name: ` + oldBackend + `
      state: present

  - name: Create test network
    community.docker.docker_network:
      name: ` + testNet + `
      state: present

  - name: Remove old project networks
    community.docker.docker_network:
      name: "{{ item }}"
      state: absent
    loop:
      - ` + oldFrontend + `
      - ` + oldBackend + `
      - ` + testNet + `

  - name: Create unused network for prune
    community.docker.docker_network:
      name: ` + pruneTarget + `
      state: present

  - name: Prune unused networks
    community.docker.docker_prune:
      networks: true
    register: prune_result

  - name: Persist prune result
    template:
      src: ` + pruneTemplate + `
      dest: /tmp/dibra-ex-prune-networks.json
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("cleanup playbook failed: %s", output)
	}
	for _, name := range []string{oldFrontend, oldBackend, testNet, pruneTarget} {
		if strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatalf("network %s still exists", name)
		}
	}
	prune := readRemoteJSONMap(t, client, "/tmp/dibra-ex-prune-networks.json")
	networks, _ := prune["networks"].([]any)
	if prune["failed"] == true || len(networks) == 0 {
		t.Fatalf("expected pruned networks, got %#v", prune)
	}
}

func TestPlaybook_DockerNetworkBlogMicroservices(t *testing.T) {
	// OneUptime: microservice-networks.yml
	// labels|combine is expanded because module-arg rendering does not run Jinja filters.
	// internal is selected with when: because templated booleans stringify in module args.
	client := getClient(t)
	defer client.Close()
	publicNet, mesh, dataTier, monitoring := "dibra-ex-public-ingress", "dibra-ex-service-mesh", "dibra-ex-data-tier", "dibra-ex-monitoring-tier"
	remoteExec(t, client, "docker network rm "+publicNet+" "+mesh+" "+dataTier+" "+monitoring+" || true")
	defer remoteExec(t, client, "docker network rm "+publicNet+" "+mesh+" "+dataTier+" "+monitoring+" || true")

	playbook := playbookWithVars(`
  networks:
    - name: `+publicNet+`
      subnet: "172.40.0.0/24"
      internal: false
      labels:
        tier: "ingress"
    - name: `+mesh+`
      subnet: "172.41.0.0/24"
      internal: false
      labels:
        tier: "services"
    - name: `+dataTier+`
      subnet: "172.42.0.0/24"
      internal: true
      labels:
        tier: "data"
    - name: `+monitoring+`
      subnet: "172.43.0.0/24"
      internal: false
      labels:
        tier: "monitoring"
`, `
  - name: Create public application networks
    community.docker.docker_network:
      name: "{{ item.name }}"
      driver: bridge
      ipam_config:
        - subnet: "{{ item.subnet }}"
      internal: false
      labels:
        tier: "{{ item.labels.tier }}"
        managed_by: ansible
      state: present
    loop: "{{ networks }}"
    when: "not item.internal"

  - name: Create internal application networks
    community.docker.docker_network:
      name: "{{ item.name }}"
      driver: bridge
      ipam_config:
        - subnet: "{{ item.subnet }}"
      internal: true
      labels:
        tier: "{{ item.labels.tier }}"
        managed_by: ansible
      state: present
    loop: "{{ networks }}"
    when: "item.internal"
`)
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("microservices playbook failed: %s", output)
	}

	cases := []struct {
		name     string
		subnet   string
		internal string
		tier     string
	}{
		{publicNet, "172.40.0.0/24", "false", "ingress"},
		{mesh, "172.41.0.0/24", "false", "services"},
		{dataTier, "172.42.0.0/24", "true", "data"},
		{monitoring, "172.43.0.0/24", "false", "monitoring"},
	}
	for _, tc := range cases {
		inspect := dockerInspectNetwork(t, client, tc.name)
		assertNetworkSubnet(t, client, tc.name, tc.subnet)
		if fmt.Sprint(inspect["Internal"]) != tc.internal {
			t.Fatalf("%s Internal=%v want %s", tc.name, inspect["Internal"], tc.internal)
		}
		labels, _ := inspect["Labels"].(map[string]any)
		if fmt.Sprint(labels["tier"]) != tc.tier || fmt.Sprint(labels["managed_by"]) != "ansible" {
			t.Fatalf("%s labels = %#v", tc.name, labels)
		}
	}

	again := runPlaybook(t, playbook)
	if strings.Contains(again, "FAILED") {
		t.Fatalf("microservices idempotent failed: %s", again)
	}
	if strings.Contains(again, "CHANGED") {
		t.Fatalf("microservices second run was not idempotent: %s", again)
	}
}

func TestPlaybook_DockerNetworkOverlayIngress(t *testing.T) {
	// Blog describes overlay/ingress as Swarm network types without YAML.
	// Ansible docs list ingress as a module option. Covered here when Swarm init works.
	client := getClient(t)
	defer client.Close()
	overlayName := "dibra-ex-overlay"
	ingressName := "dibra-ex-custom-ingress"
	leaveSwarm := func() {
		remoteExec(t, client, "docker network rm "+overlayName+" "+ingressName+" || true")
		remoteExec(t, client, "docker swarm leave --force || true")
	}
	leaveSwarm()
	defer leaveSwarm()

	init := playbookHeader + `
  - name: Init Swarm
    docker_swarm:
      state: present
      advertise_addr: 127.0.0.1
`
	if output := runPlaybook(t, init); strings.Contains(output, "FAILED") {
		t.Skipf("swarm init failed, skipping overlay/ingress: %s", output)
	}
	state := strings.TrimSpace(remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'"))
	if state != "active" {
		t.Skipf("swarm not active after init, got %q", state)
	}

	overlay := runNetworkModule(t, client, "overlay-create", `
      name: `+overlayName+`
      driver: overlay
`)
	if overlay["failed"] == true {
		t.Fatalf("overlay create = %#v", overlay)
	}
	if overlay["changed"] != true {
		t.Fatalf("overlay create unchanged: %#v", overlay)
	}
	inspect := dockerInspectNetwork(t, client, overlayName)
	if fmt.Sprint(inspect["Driver"]) != "overlay" {
		t.Fatalf("overlay driver = %#v", inspect["Driver"])
	}
	idem := runNetworkModule(t, client, "overlay-idem", `
      name: `+overlayName+`
      driver: overlay
`)
	if idem["changed"] != false {
		t.Fatalf("overlay idempotent = %#v", idem)
	}

	_ = runNetworkModule(t, client, "ingress-default-absent", `
      name: ingress
      state: absent
`)
	created := runNetworkModule(t, client, "ingress-create", `
      name: `+ingressName+`
      driver: overlay
      ingress: true
`)
	if created["failed"] == true {
		t.Fatalf("ingress create = %#v", created)
	}
	if created["changed"] != true {
		t.Fatalf("ingress create unchanged: %#v", created)
	}
	ingressInspect := dockerInspectNetwork(t, client, ingressName)
	if fmt.Sprint(ingressInspect["Ingress"]) != "true" {
		t.Fatalf("Ingress = %#v", ingressInspect)
	}
	ingressIdem := runNetworkModule(t, client, "ingress-idem", `
      name: `+ingressName+`
      driver: overlay
      ingress: true
`)
	if ingressIdem["changed"] != false {
		t.Fatalf("ingress idempotent = %#v", ingressIdem)
	}
}

func TestPlaybook_DockerNetworkBlogMacvlan(t *testing.T) {
	// Blog lists macvlan as a network type without YAML. Engine 29.7.2 accepts a
	// macvlan network with an explicit subnet and no parent interface.
	client := getClient(t)
	defer client.Close()
	name := "dibra-ex-macvlan"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, "blog-macvlan", `
      name: `+name+`
      driver: macvlan
      ipam_config:
        - subnet: "172.44.0.0/24"
      state: present
`)
	if created["changed"] != true || created["failed"] == true {
		t.Fatalf("macvlan create = %#v", created)
	}
	inspect := dockerInspectNetwork(t, client, name)
	if fmt.Sprint(inspect["Driver"]) != "macvlan" {
		t.Fatalf("driver = %#v", inspect["Driver"])
	}
	subnet, _ := ipamSubnetGateway(t, inspect)
	if subnet != "172.44.0.0/24" {
		t.Fatalf("macvlan subnet = %q", subnet)
	}

	idem := runNetworkModule(t, client, "blog-macvlan-idem", `
      name: `+name+`
      driver: macvlan
      ipam_config:
        - subnet: "172.44.0.0/24"
      state: present
`)
	if idem["changed"] != false {
		t.Fatalf("macvlan idempotent = %#v", idem)
	}
}

func TestPlaybook_DockerNetworkUnsupportedDrivers(t *testing.T) {
	// Blog lists host/none as Docker network types but provides no YAML.
	// Engine 29.7.2 rejects a second host network and has no none plugin.
	client := getClient(t)
	defer client.Close()

	hostOut, hostErr, _ := client.Run("docker network create -d host dibra-ex-host-probe 2>&1")
	remoteExec(t, client, "docker network rm dibra-ex-host-probe || true")
	noneOut, noneErr, _ := client.Run("docker network create -d none dibra-ex-none-probe 2>&1")
	remoteExec(t, client, "docker network rm dibra-ex-none-probe || true")

	evidence := strings.TrimSpace(hostOut + "\n" + hostErr + "\n" + noneOut + "\n" + noneErr)
	t.Skipf("host/none cannot be created as custom networks on Engine 29.7.2: %s", evidence)
}

func playbookWithVars(varsBlock, tasks string) string {
	return `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

vars:
` + varsBlock + `
tasks:
` + tasks
}

func dockerInspectNetwork(t *testing.T, client *ssh.Client, name string) map[string]any {
	t.Helper()
	raw, stderr, err := client.Run("docker network inspect " + name)
	if err != nil {
		t.Fatalf("docker network inspect %s: %v: %s", name, err, stderr)
	}
	var inspected []map[string]any
	if err := json.Unmarshal([]byte(raw), &inspected); err != nil || len(inspected) != 1 {
		t.Fatalf("decode docker network inspect %s: %v\n%s", name, err, raw)
	}
	return inspected[0]
}

func ipamSubnetGateway(t *testing.T, network map[string]any) (string, string) {
	t.Helper()
	return ipamField(t, network, "Subnet"), ipamField(t, network, "Gateway")
}

func ipamField(t *testing.T, network map[string]any, field string) string {
	t.Helper()
	ipam, _ := network["IPAM"].(map[string]any)
	if ipam == nil {
		return ""
	}
	config, _ := ipam["Config"].([]any)
	if len(config) == 0 {
		return ""
	}
	first, _ := config[0].(map[string]any)
	if first == nil || first[field] == nil {
		return ""
	}
	return fmt.Sprint(first[field])
}

func assertNetworkSubnet(t *testing.T, client *ssh.Client, name, subnet string) {
	t.Helper()
	inspect := dockerInspectNetwork(t, client, name)
	got := ipamField(t, inspect, "Subnet")
	if got != subnet {
		t.Fatalf("%s subnet = %q, want %q", name, got, subnet)
	}
}

func containerNetworks(t *testing.T, client *ssh.Client, container string) map[string]map[string]any {
	t.Helper()
	raw, stderr, err := client.Run("docker inspect --format '{{json .NetworkSettings.Networks}}' " + container)
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", container, err, stderr)
	}
	var networks map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &networks); err != nil {
		t.Fatalf("decode networks for %s: %v\n%s", container, err, raw)
	}
	return networks
}

func assertAliasesContain(t *testing.T, endpoint map[string]any, aliases ...string) {
	t.Helper()
	if endpoint == nil {
		t.Fatal("missing network endpoint")
	}
	raw := fmt.Sprint(endpoint["Aliases"])
	for _, alias := range aliases {
		if !strings.Contains(raw, alias) {
			t.Fatalf("expected alias %q in %#v", alias, endpoint["Aliases"])
		}
	}
}

func nameOr(result map[string]any, fallback string) string {
	network, _ := result["network"].(map[string]any)
	if network != nil {
		if name, _ := network["Name"].(string); name != "" {
			return name
		}
	}
	return fallback
}
