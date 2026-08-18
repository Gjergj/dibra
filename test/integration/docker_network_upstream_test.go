//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// Upstream community.docker 5.2.2 docker_network integration scenarios that
// were not already covered by docker_network_parity_test.go /
// docker_network_phase4_test.go / docker_network_examples_test.go.

func TestPlaybook_DockerNetworkUpstreamSubstring(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	superstring := "dibra-up-net-foobar"
	substring := "dibra-up-net-foo"
	remoteExec(t, client, "docker network rm "+substring+" "+superstring+" || true")
	defer remoteExec(t, client, "docker network rm "+substring+" "+superstring+" || true")

	first := runNetworkModule(t, client, "up-substr-1", `
      name: `+superstring+`
      state: present
`)
	if first["changed"] != true {
		t.Fatalf("superstring create = %#v", first)
	}
	second := runNetworkModule(t, client, "up-substr-2", `
      name: `+substring+`
      state: present
`)
	if second["changed"] != true {
		t.Fatalf("substring create = %#v", second)
	}
	if dockerInspectNetwork(t, client, superstring)["Name"] != superstring {
		t.Fatalf("superstring inspect name = %#v", dockerInspectNetwork(t, client, superstring)["Name"])
	}
	if dockerInspectNetwork(t, client, substring)["Name"] != substring {
		t.Fatalf("substring inspect name = %#v", dockerInspectNetwork(t, client, substring)["Name"])
	}
	if networkMap(t, first)["Id"] == networkMap(t, second)["Id"] {
		t.Fatal("substring lookup reused the superstring network")
	}
}

func TestPlaybook_DockerNetworkUpstreamAttachable(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-up-net-attach"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, "up-attach-1", `
      name: `+name+`
      attachable: true
`)
	if created["changed"] != true {
		t.Fatalf("attachable create = %#v", created)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, name)["Attachable"]) != "true" {
		t.Fatalf("Attachable after create = %#v", dockerInspectNetwork(t, client, name)["Attachable"])
	}

	idem := runNetworkModule(t, client, "up-attach-2", `
      name: `+name+`
      attachable: true
`)
	if idem["changed"] != false {
		t.Fatalf("attachable idempotent = %#v", idem)
	}

	changed := runNetworkModule(t, client, "up-attach-3", `
      name: `+name+`
      attachable: false
`)
	if changed["changed"] != true {
		t.Fatalf("attachable change = %#v", changed)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, name)["Attachable"]) != "false" {
		t.Fatalf("Attachable after change = %#v", dockerInspectNetwork(t, client, name)["Attachable"])
	}
}

func TestPlaybook_DockerNetworkUpstreamScope(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-up-net-scope"
	leaveSwarm(t, client, name)
	defer leaveSwarm(t, client, name)

	created := runNetworkModule(t, client, "up-scope-1", `
      name: `+name+`
      driver: bridge
      scope: local
`)
	if created["changed"] != true {
		t.Fatalf("scope local create = %#v", created)
	}
	inspect := dockerInspectNetwork(t, client, name)
	if fmt.Sprint(inspect["Scope"]) != "local" || fmt.Sprint(inspect["Driver"]) != "bridge" {
		t.Fatalf("local inspect = %#v", inspect)
	}

	idem := runNetworkModule(t, client, "up-scope-2", `
      name: `+name+`
      driver: bridge
      scope: local
`)
	if idem["changed"] != false {
		t.Fatalf("scope local idempotent = %#v", idem)
	}

	initSwarmOrSkip(t, client)

	changed := runNetworkModule(t, client, "up-scope-3", `
      name: `+name+`
      driver: overlay
      scope: swarm
`)
	if changed["changed"] != true {
		t.Fatalf("scope swarm change = %#v", changed)
	}
	inspect = dockerInspectNetwork(t, client, name)
	if fmt.Sprint(inspect["Driver"]) != "overlay" {
		t.Fatalf("overlay driver after scope change = %#v", inspect["Driver"])
	}
}

func TestPlaybook_DockerNetworkUpstreamOverlay(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	overlayName := "dibra-up-net-overlay"
	ingressName := "dibra-up-net-ingress"
	leaveSwarm(t, client, overlayName, ingressName)
	defer leaveSwarm(t, client, overlayName, ingressName)

	initSwarmOrSkip(t, client)

	created := runNetworkModule(t, client, "up-overlay-1", `
      name: `+overlayName+`
      driver: overlay
      driver_options:
        com.docker.network.driver.overlay.vxlanid_list: "257"
`)
	if created["changed"] != true {
		t.Fatalf("overlay create = %#v", created)
	}
	inspect := dockerInspectNetwork(t, client, overlayName)
	if fmt.Sprint(inspect["Driver"]) != "overlay" {
		t.Fatalf("overlay driver = %#v", inspect["Driver"])
	}
	options, _ := inspect["Options"].(map[string]any)
	if fmt.Sprint(options["com.docker.network.driver.overlay.vxlanid_list"]) != "257" {
		t.Fatalf("vxlanid_list = %#v", options)
	}

	idem := runNetworkModule(t, client, "up-overlay-2", `
      name: `+overlayName+`
      driver: overlay
      driver_options:
        com.docker.network.driver.overlay.vxlanid_list: "257"
`)
	if idem["changed"] != false {
		t.Fatalf("overlay idempotent = %#v", idem)
	}

	toBridge := runNetworkModule(t, client, "up-overlay-3", `
      name: `+overlayName+`
      driver: bridge
`)
	if toBridge["changed"] != true {
		t.Fatalf("overlay to bridge = %#v", toBridge)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, overlayName)["Driver"]) != "bridge" {
		t.Fatalf("driver after overlay change = %#v", dockerInspectNetwork(t, client, overlayName)["Driver"])
	}

	_ = runNetworkModule(t, client, "up-overlay-cleanup", `
      name: `+overlayName+`
      state: absent
      force: true
`)
	_ = runNetworkModule(t, client, "up-ingress-default-absent", `
      name: ingress
      state: absent
`)

	ingress := runNetworkModule(t, client, "up-ingress-1", `
      name: `+ingressName+`
      driver: overlay
      ingress: true
`)
	if ingress["changed"] != true {
		t.Fatalf("ingress create = %#v", ingress)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, ingressName)["Ingress"]) != "true" {
		t.Fatalf("Ingress after create = %#v", dockerInspectNetwork(t, client, ingressName)["Ingress"])
	}

	ingressIdem := runNetworkModule(t, client, "up-ingress-2", `
      name: `+ingressName+`
      driver: overlay
      ingress: true
`)
	if ingressIdem["changed"] != false {
		t.Fatalf("ingress idempotent = %#v", ingressIdem)
	}

	ingressChange := runNetworkModule(t, client, "up-ingress-3", `
      name: `+ingressName+`
      driver: overlay
      ingress: false
`)
	if ingressChange["changed"] != true {
		t.Fatalf("ingress change = %#v", ingressChange)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, ingressName)["Ingress"]) != "false" {
		t.Fatalf("Ingress after change = %#v", dockerInspectNetwork(t, client, ingressName)["Ingress"])
	}
}

func TestPlaybook_DockerNetworkUpstreamIPAMDriverOptions(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-up-net-ipam-opts"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created, output := runNetworkModuleAllowFail(t, client, "up-ipam-opts-1", `
      name: `+name+`
      ipam_driver: default
      ipam_driver_options:
        a: b
`)
	if created == nil {
		directName := name + "-direct"
		remoteExec(t, client, "docker network rm "+directName+" || true")
		defer remoteExec(t, client, "docker network rm "+directName+" || true")
		stdout, stderr, err := client.Run("docker network create --ipam-driver default --ipam-opt a=b " + directName)
		directOutput := stdout + stderr
		if err == nil || !strings.Contains(strings.ToLower(directOutput), "invalid") ||
			!strings.Contains(strings.ToLower(output), "invalid") {
			t.Fatalf("Dibra rejected IPAM driver options without matching Engine rejection:\nDibra: %s\nDocker CLI: %s", output, directOutput)
		}
		t.Logf("Engine 29.7.2 rejects arbitrary default-driver IPAM options through both Dibra and Docker CLI: %s", strings.TrimSpace(directOutput))
		return
	}
	if created["changed"] != true {
		t.Fatalf("ipam_driver_options create = %#v", created)
	}

	idem, output := runNetworkModuleAllowFail(t, client, "up-ipam-opts-2", `
      name: `+name+`
      ipam_driver: default
      ipam_driver_options:
        a: b
`, "--diff")
	if idem == nil {
		t.Fatalf("Engine accepted IPAM driver options during create but rejected idempotency: %s", output)
	}
	if idem["changed"] != false {
		t.Fatalf("ipam_driver_options idempotent = %#v", idem)
	}

	changed, output := runNetworkModuleAllowFail(t, client, "up-ipam-opts-3", `
      name: `+name+`
      ipam_driver: default
      ipam_driver_options:
        a: c
`, "--diff")
	if changed == nil {
		t.Fatalf("Engine accepted IPAM driver options during create but rejected update: %s", output)
	}
	if changed["changed"] != true {
		t.Fatalf("ipam_driver_options change = %#v", changed)
	}
	before, after := networkDiffMaps(t, changed)
	if _, ok := before["ipam_driver_options"]; !ok {
		t.Fatalf("missing ipam_driver_options in before: %#v", before)
	}
	if fmt.Sprint(after["ipam_driver_options"]) == fmt.Sprint(before["ipam_driver_options"]) {
		t.Fatalf("ipam_driver_options diff unchanged: before %#v after %#v", before, after)
	}
}

func TestPlaybook_DockerNetworkUpstreamMacvlanDualIPv4(t *testing.T) {
	// Upstream skips this block inside Docker (needs a parent iface). Dibra's
	// Engine 29.7.2 host already creates macvlan without a parent, so we try
	// that first and skip only with Engine evidence.
	client := getClient(t)
	defer client.Close()
	name := "dibra-up-net-macvlan"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created, output := runNetworkModuleAllowFail(t, client, "up-macvlan-1", `
      name: `+name+`
      driver: macvlan
      ipam_config:
        - subnet: 10.78.120.0/24
        - subnet: 10.78.121.0/24
`)
	if created == nil {
		parent := strings.TrimSpace(remoteExec(t, client, "ip -o -4 route show default 2>/dev/null | awk '{print $5; exit}'"))
		if parent == "" {
			t.Skipf("macvlan dual IPv4 without parent failed and no default iface (upstream skips in Docker): %s", output)
		}
		created, output = runNetworkModuleAllowFail(t, client, "up-macvlan-1-parent", `
      name: `+name+`
      driver: macvlan
      driver_options:
        parent: `+parent+`
      ipam_config:
        - subnet: 10.78.120.0/24
        - subnet: 10.78.121.0/24
`)
		if created == nil {
			t.Skipf("macvlan dual IPv4 with parent %s failed on Engine 29.7.2 (upstream skips when virtualization_type=docker): %s", parent, output)
		}
	}
	if created["changed"] != true {
		t.Fatalf("macvlan dual create = %#v", created)
	}
	if fmt.Sprint(dockerInspectNetwork(t, client, name)["Driver"]) != "macvlan" {
		t.Fatalf("driver = %#v", dockerInspectNetwork(t, client, name)["Driver"])
	}

	parentArgs := ""
	options, _ := dockerInspectNetwork(t, client, name)["Options"].(map[string]any)
	if parent, _ := options["parent"].(string); parent != "" {
		parentArgs = `
      driver_options:
        parent: ` + parent
	}

	idem := runNetworkModule(t, client, "up-macvlan-2", `
      name: `+name+`
      driver: macvlan`+parentArgs+`
      ipam_config:
        - subnet: 10.78.121.0/24
        - subnet: 10.78.120.0/24
`)
	if idem["changed"] != false {
		t.Fatalf("macvlan dual reorder = %#v", idem)
	}

	changed := runNetworkModuleWithArgs(t, client, "up-macvlan-3", `
      name: `+name+`
      driver: macvlan`+parentArgs+`
      ipam_config:
        - subnet: 10.78.120.0/24
        - subnet: 10.78.122.0/24
`, "--diff")
	if changed["changed"] != true {
		t.Fatalf("macvlan dual change = %#v", changed)
	}
	before, after := networkDiffMaps(t, changed)
	before, after = networkConfigDiff(before, after)
	if len(before) != 1 || before["ipam_config[1].subnet"] == after["ipam_config[1].subnet"] {
		t.Fatalf("expected one IPAM difference, got before %#v after %#v", before, after)
	}

	subset := runNetworkModule(t, client, "up-macvlan-4", `
      name: `+name+`
      driver: macvlan`+parentArgs+`
      ipam_config:
        - subnet: 10.78.122.0/24
`)
	if subset["changed"] != false {
		t.Fatalf("macvlan dual subset = %#v", subset)
	}
}

func initSwarmOrSkip(t *testing.T, client *ssh.Client) {
	t.Helper()
	init := playbookHeader + `
  - name: Init Swarm
    docker_swarm:
      state: present
      advertise_addr: 127.0.0.1
`
	if output := runPlaybook(t, init); strings.Contains(output, "FAILED") {
		t.Skipf("swarm init failed: %s", output)
	}
	state := strings.TrimSpace(remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'"))
	if state != "active" {
		t.Skipf("swarm not active after init, got %q", state)
	}
}

func leaveSwarm(t *testing.T, client *ssh.Client, networks ...string) {
	t.Helper()
	if len(networks) > 0 {
		remoteExec(t, client, "docker network rm "+strings.Join(networks, " ")+" || true")
	}
	remoteExec(t, client, "docker swarm leave --force || true")
}

func runNetworkModuleAllowFail(t *testing.T, client *ssh.Client, resultName, arguments string, extraArgs ...string) (map[string]any, string) {
	t.Helper()
	remotePath := "/tmp/dibra-network-" + resultName + ".json"
	templatePath := writeResultTemplate(t, "network_result")
	playbook := playbookHeader + `
  - name: Manage network
    community.docker.docker_network:
` + arguments + `
    register: network_result

  - name: Persist network result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extraArgs...)
	if strings.Contains(output, "FAILED") {
		return nil, output
	}
	return readRemoteJSONMap(t, client, remotePath), output
}
