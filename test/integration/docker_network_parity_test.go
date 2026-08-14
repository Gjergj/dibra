//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerNetworkParityLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-parity-net-basic"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, name+"-create", `
      name: `+name+`
      state: present
`)
	if created["changed"] != true || created["failed"] == true {
		t.Fatalf("create = %#v", created)
	}
	if networkMap(t, created)["Name"] != name {
		t.Fatalf("created network = %#v", created["network"])
	}

	again := runNetworkModule(t, client, name+"-idem", `
      name: `+name+`
      state: present
`)
	if again["changed"] != false || again["failed"] == true {
		t.Fatalf("idempotent = %#v", again)
	}

	absent := runNetworkModule(t, client, name+"-absent", `
      name: `+name+`
      state: absent
`)
	if absent["changed"] != true || absent["failed"] == true || absent["network"] != nil {
		t.Fatalf("absent = %#v", absent)
	}

	absentAgain := runNetworkModule(t, client, name+"-absent-idem", `
      name: `+name+`
      state: absent
`)
	if absentAgain["changed"] != false || absentAgain["failed"] == true {
		t.Fatalf("absent idempotent = %#v", absentAgain)
	}
}

func TestPlaybook_DockerNetworkParityConnected(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	netName := "dibra-parity-net-conn"
	c1, c2, c3 := "dibra-parity-c1", "dibra-parity-c2", "dibra-parity-c3"
	remoteExec(t, client, "docker pull alpine:latest")
	remoteExec(t, client, "docker rm -f "+c1+" "+c2+" "+c3+" || true")
	remoteExec(t, client, "docker network rm "+netName+" || true")
	remoteExec(t, client, "docker run -d --name "+c1+" alpine:latest sleep 3600")
	remoteExec(t, client, "docker run -d --name "+c2+" alpine:latest sleep 3600")
	remoteExec(t, client, "docker run -d --name "+c3+" alpine:latest sleep 3600")
	defer func() {
		remoteExec(t, client, "docker rm -f "+c1+" "+c2+" "+c3+" || true")
		remoteExec(t, client, "docker network rm "+netName+" || true")
	}()

	_ = runNetworkModule(t, client, "conn-create", `
      name: `+netName+`
      state: present
`)
	first := runNetworkModule(t, client, "conn-1", `
      name: `+netName+`
      connected:
        - `+c1+`
`)
	if first["changed"] != true {
		t.Fatalf("connect first = %#v", first)
	}
	assertConnected(t, client, netName, c1)

	firstIdem := runNetworkModule(t, client, "conn-1-idem", `
      name: `+netName+`
      connected:
        - `+c1+`
`)
	if firstIdem["changed"] != false {
		t.Fatalf("connect first idempotent = %#v", firstIdem)
	}

	two := runNetworkModule(t, client, "conn-2", `
      name: `+netName+`
      connected:
        - `+c1+`
        - `+c2+`
`)
	if two["changed"] != true {
		t.Fatalf("connect two = %#v", two)
	}

	twoIdem := runNetworkModule(t, client, "conn-2-idem", `
      name: `+netName+`
      connected:
        - `+c1+`
        - `+c2+`
`)
	if twoIdem["changed"] != false {
		t.Fatalf("connect two idempotent = %#v", twoIdem)
	}

	appended := runNetworkModule(t, client, "conn-append", `
      name: `+netName+`
      appends: true
      connected:
        - `+c3+`
`)
	if appended["changed"] != true {
		t.Fatalf("appends = %#v", appended)
	}
	assertConnected(t, client, netName, c1, c2, c3)

	appendedIdem := runNetworkModule(t, client, "conn-append-idem", `
      name: `+netName+`
      appends: true
      connected:
        - `+c3+`
`)
	if appendedIdem["changed"] != false {
		t.Fatalf("appends idempotent = %#v", appendedIdem)
	}

	replaced := runNetworkModule(t, client, "conn-replace", `
      name: `+netName+`
      connected:
        - `+c2+`
        - `+c3+`
`)
	if replaced["changed"] != true {
		t.Fatalf("replace = %#v", replaced)
	}
	connected := remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	if strings.Contains(connected, c1) || !strings.Contains(connected, c2) || !strings.Contains(connected, c3) {
		t.Fatalf("connected after replace = %q", connected)
	}

	replacedIdem := runNetworkModule(t, client, "conn-replace-idem", `
      name: `+netName+`
      connected:
        - `+c2+`
        - `+c3+`
`)
	if replacedIdem["changed"] != false {
		t.Fatalf("replace idempotent = %#v", replacedIdem)
	}

	emptyConnected := runNetworkModule(t, client, "conn-empty", `
      name: `+netName+`
      connected: []
`)
	if emptyConnected["changed"] != false {
		t.Fatalf("empty connected should copy current names: %#v", emptyConnected)
	}
	connected = remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	if strings.Contains(connected, c1) || !strings.Contains(connected, c2) || !strings.Contains(connected, c3) {
		t.Fatalf("empty connected mutated attachments: %q", connected)
	}
}

func TestPlaybook_DockerNetworkParityOptions(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-parity-net-opts"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	internal := runNetworkModule(t, client, "internal-1", `
      name: `+name+`
      internal: true
`)
	if internal["changed"] != true {
		t.Fatalf("internal create = %#v", internal)
	}
	internalIdem := runNetworkModule(t, client, "internal-2", `
      name: `+name+`
      internal: true
`)
	if internalIdem["changed"] != false {
		t.Fatalf("internal idempotent = %#v", internalIdem)
	}
	internalChange := runNetworkModule(t, client, "internal-3", `
      name: `+name+`
      internal: false
`)
	if internalChange["changed"] != true {
		t.Fatalf("internal change = %#v", internalChange)
	}

	_ = runNetworkModule(t, client, "opts-cleanup", `
      name: `+name+`
      state: absent
      force: true
`)

	driverOpts := runNetworkModule(t, client, "driver-opts-1", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.enable_icc: "false"
`)
	if driverOpts["changed"] != true {
		t.Fatalf("driver_options create = %#v", driverOpts)
	}
	driverOptsStringIdem := runNetworkModule(t, client, "driver-opts-2", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.enable_icc: "false"
`)
	if driverOptsStringIdem["changed"] != false {
		t.Fatalf("driver_options string idempotent = %#v", driverOptsStringIdem)
	}
	driverOptsIdem := runNetworkModule(t, client, "driver-opts-3", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.enable_icc: false
`)
	if driverOptsIdem["changed"] != false {
		t.Fatalf("driver_options boolean idempotent = %#v", driverOptsIdem)
	}
	driverOptsChange := runNetworkModule(t, client, "driver-opts-4", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.enable_icc: "true"
`)
	if driverOptsChange["changed"] != true {
		t.Fatalf("driver_options change = %#v", driverOptsChange)
	}
	driverOptsTrueIdem := runNetworkModule(t, client, "driver-opts-5", `
      name: `+name+`
      driver_options:
        com.docker.network.bridge.enable_icc: true
`)
	if driverOptsTrueIdem["changed"] != false {
		t.Fatalf("driver_options true boolean idempotent = %#v", driverOptsTrueIdem)
	}

	_ = runNetworkModule(t, client, "labels-cleanup", `
      name: `+name+`
      state: absent
      force: true
`)

	labels := runNetworkModule(t, client, "labels-1", `
      name: `+name+`
      labels:
        ansible.test.1: hello
        ansible.test.2: world
`)
	if labels["changed"] != true {
		t.Fatalf("labels create = %#v", labels)
	}
	labelsIdem := runNetworkModule(t, client, "labels-2", `
      name: `+name+`
      labels:
        ansible.test.2: world
        ansible.test.1: hello
`)
	if labelsIdem["changed"] != false {
		t.Fatalf("labels idempotent = %#v", labelsIdem)
	}
	labelsLess := runNetworkModule(t, client, "labels-3", `
      name: `+name+`
      labels:
        ansible.test.1: hello
`)
	if labelsLess["changed"] != false {
		t.Fatalf("labels subset = %#v", labelsLess)
	}
	labelsMore := runNetworkModule(t, client, "labels-4", `
      name: `+name+`
      labels:
        ansible.test.1: hello
        ansible.test.3: ansible
`)
	if labelsMore["changed"] != true {
		t.Fatalf("labels more = %#v", labelsMore)
	}

	_ = runNetworkModule(t, client, "ipv6-cleanup", `
      name: `+name+`
      state: absent
      force: true
`)
	ipv6 := runNetworkModule(t, client, "ipv6-1", `
      name: `+name+`
      enable_ipv6: true
`)
	if ipv6["changed"] != true {
		t.Fatalf("enable_ipv6 = %#v", ipv6)
	}
	if fmt.Sprint(networkMap(t, ipv6)["EnableIPv6"]) != "true" {
		t.Fatalf("EnableIPv6 = %#v", ipv6["network"])
	}
}

func TestPlaybook_DockerNetworkParityIPAM(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-parity-net-ipam"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	created := runNetworkModule(t, client, "ipam-1", `
      name: `+name+`
      ipam_config:
        - subnet: 10.77.120.0/24
          gateway: 10.77.120.2
          iprange: 10.77.120.0/26
          aux_addresses:
            host1: 10.77.120.3
            host2: 10.77.120.4
`)
	if created["changed"] != true {
		t.Fatalf("ipam create = %#v", created)
	}
	idem := runNetworkModule(t, client, "ipam-2", `
      name: `+name+`
      ipam_config:
        - subnet: 10.77.120.0/24
          gateway: 10.77.120.2
          iprange: 10.77.120.0/26
          aux_addresses:
            host1: 10.77.120.3
            host2: 10.77.120.4
`)
	if idem["changed"] != false {
		t.Fatalf("ipam idempotent = %#v", idem)
	}

	changed := runNetworkModuleWithArgs(t, client, "ipam-3", `
      name: `+name+`
      ipam_config:
        - subnet: 10.77.121.0/24
          gateway: 10.77.121.2
          iprange: 10.77.121.0/26
          aux_addresses:
            host1: 10.77.121.3
`, "--diff")
	if changed["changed"] != true {
		t.Fatalf("ipam change = %#v", changed)
	}
	before, after := networkDiffMaps(t, changed)
	before, after = networkConfigDiff(before, after)
	for _, key := range []string{
		"ipam_config[0].subnet",
		"ipam_config[0].gateway",
		"ipam_config[0].iprange",
		"ipam_config[0].aux_addresses",
	} {
		if _, ok := before[key]; !ok {
			t.Fatalf("missing %s in before: %#v", key, before)
		}
		if _, ok := after[key]; !ok {
			t.Fatalf("missing %s in after: %#v", key, after)
		}
	}
	if len(before) != 4 || len(after) != 4 {
		t.Fatalf("ipam diff key count = before %#v after %#v", before, after)
	}

	subset := runNetworkModule(t, client, "ipam-4", `
      name: `+name+`
      ipam_config:
        - subnet: 10.77.121.0/24
`)
	if subset["changed"] != false {
		t.Fatalf("ipam subset = %#v", subset)
	}

	_ = runNetworkModule(t, client, "ipam-cleanup", `
      name: `+name+`
      state: absent
`)

	ipv6 := runNetworkModule(t, client, "ipam-v6-1", `
      name: `+name+`
      enable_ipv6: true
      ipam_config:
        - subnet: fdd1:ac8c:0557:7ce0::/64
`)
	if ipv6["changed"] != true {
		t.Fatalf("ipv6 ipam create = %#v", ipv6)
	}
	ipv6Idem := runNetworkModule(t, client, "ipam-v6-2", `
      name: `+name+`
      enable_ipv6: true
      ipam_config:
        - subnet: fdd1:ac8c:0557:7ce0::/64
`)
	if ipv6Idem["changed"] != false {
		t.Fatalf("ipv6 ipam idempotent = %#v", ipv6Idem)
	}
	ipv6Change := runNetworkModuleWithArgs(t, client, "ipam-v6-3", `
      name: `+name+`
      enable_ipv6: true
      ipam_config:
        - subnet: fdd1:ac8c:0557:7ce1::/64
`, "--diff")
	if ipv6Change["changed"] != true {
		t.Fatalf("ipv6 ipam change = %#v", ipv6Change)
	}
	before, after = networkDiffMaps(t, ipv6Change)
	before, after = networkConfigDiff(before, after)
	if len(before) != 1 || before["ipam_config[0].subnet"] == after["ipam_config[0].subnet"] {
		t.Fatalf("ipv6 ipam diff = before %#v after %#v", before, after)
	}

	invalid := playbookHeader + `
  - name: Invalid CIDR
    community.docker.docker_network:
      name: ` + name + `
      enable_ipv6: true
      ipam_config:
        - subnet: "fdd1:ac8c:0557:7ce1::"
`
	output := runPlaybook(t, invalid)
	if !strings.Contains(output, "FAILED") || !strings.Contains(output, `"fdd1:ac8c:0557:7ce1::" is not a valid CIDR`) {
		t.Fatalf("invalid CIDR output = %s", output)
	}

	_ = runNetworkModule(t, client, "ipam-v6-cleanup", `
      name: `+name+`
      state: absent
`)

	dual := runNetworkModule(t, client, "ipam-dual-1", `
      name: `+name+`
      enable_ipv6: true
      ipam_config:
        - subnet: 10.77.122.0/24
        - subnet: fdd1:ac8c:0557:7ce2::/64
`)
	if dual["changed"] != true {
		t.Fatalf("dual ipam create = %#v", dual)
	}
	reordered := runNetworkModule(t, client, "ipam-dual-2", `
      name: `+name+`
      enable_ipv6: true
      ipam_config:
        - subnet: fdd1:ac8c:0557:7ce2::/64
        - subnet: 10.77.122.0/24
`)
	if reordered["changed"] != false {
		t.Fatalf("dual ipam reorder = %#v", reordered)
	}
	disableV6 := runNetworkModuleWithArgs(t, client, "ipam-dual-3", `
      name: `+name+`
      enable_ipv6: false
      ipam_config:
        - subnet: 10.77.122.0/24
`, "--diff")
	if disableV6["changed"] != true {
		t.Fatalf("disable ipv6 = %#v", disableV6)
	}
	before, after = networkDiffMaps(t, disableV6)
	before, after = networkConfigDiff(before, after)
	if len(before) != 1 || fmt.Sprint(before["enable_ipv6"]) == fmt.Sprint(after["enable_ipv6"]) {
		t.Fatalf("enable_ipv6 diff = before %#v after %#v", before, after)
	}
}

func TestPlaybook_DockerNetworkParityCheckAndForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	name := "dibra-parity-net-check"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")

	playbook := playbookHeader + `
  - name: Check-mode create
    community.docker.docker_network:
      name: ` + name + `
      state: present
`
	output := runPlaybookWithArgs(t, playbook, "--check")
	if strings.Contains(output, "FAILED") || strings.Contains(output, "SKIPPED") {
		t.Fatalf("check-mode playbook failed: %s", output)
	}
	if strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" >/dev/null 2>&1; echo $?")) == "0" {
		t.Fatal("check mode created the network")
	}

	created := runNetworkModule(t, client, "check-create", `
      name: `+name+`
      state: present
      driver: bridge
`)
	if created["changed"] != true {
		t.Fatalf("real create = %#v", created)
	}
	origID := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" --format '{{.Id}}'"))

	forced := runNetworkModule(t, client, "force-recreate", `
      name: `+name+`
      state: present
      driver: bridge
      force: true
`)
	if forced["changed"] != true {
		t.Fatalf("force recreate = %#v", forced)
	}
	newID := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" --format '{{.Id}}'"))
	if origID == newID {
		t.Fatal("force did not recreate the network")
	}

	diffPlaybook := playbookHeader + `
  - name: Diff-mode present
    community.docker.docker_network:
      name: ` + name + `
      internal: true
    register: network_result

  - name: Persist result
    template:
      src: ` + writeResultTemplate(t, "network_result") + `
      dest: /tmp/dibra-network-diff.json
`
	diffOutput := runPlaybookWithArgs(t, diffPlaybook, "--diff")
	if strings.Contains(diffOutput, "FAILED") {
		t.Fatalf("diff-mode playbook failed: %s", diffOutput)
	}
	result := readRemoteJSONMap(t, client, "/tmp/dibra-network-diff.json")
	if result["changed"] != true {
		t.Fatalf("diff recreate = %#v", result)
	}
	diff, _ := result["diff"].(map[string]any)
	if diff == nil {
		t.Fatalf("missing diff: %#v", result)
	}
	if _, ok := diff["differences"]; ok {
		t.Fatalf("diff included differences list: %#v", diff)
	}
	before, _ := diff["before"].(map[string]any)
	after, _ := diff["after"].(map[string]any)
	if before == nil || after == nil {
		t.Fatalf("diff missing before/after: %#v", diff)
	}
}

func runNetworkModule(t *testing.T, client *ssh.Client, resultName, arguments string) map[string]any {
	t.Helper()
	return runNetworkModuleWithArgs(t, client, resultName, arguments)
}

func runNetworkModuleWithArgs(t *testing.T, client *ssh.Client, resultName, arguments string, extraArgs ...string) map[string]any {
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
		t.Fatalf("%s network playbook failed: %s", resultName, output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}

func networkDiffMaps(t *testing.T, result map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	diff, _ := result["diff"].(map[string]any)
	if diff == nil {
		t.Fatalf("expected diff: %#v", result)
	}
	if _, ok := diff["differences"]; ok {
		t.Fatalf("diff included differences list: %#v", diff)
	}
	before, _ := diff["before"].(map[string]any)
	after, _ := diff["after"].(map[string]any)
	if before == nil || after == nil {
		t.Fatalf("diff missing before/after: %#v", diff)
	}
	return before, after
}

func networkConfigDiff(before, after map[string]any) (map[string]any, map[string]any) {
	strip := func(in map[string]any) map[string]any {
		out := make(map[string]any, len(in))
		for key, value := range in {
			if key == "exists" {
				continue
			}
			out[key] = value
		}
		return out
	}
	return strip(before), strip(after)
}

func networkMap(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	network, ok := result["network"].(map[string]any)
	if !ok {
		t.Fatalf("network = %T, want object in %#v", result["network"], result)
	}
	return network
}

func assertConnected(t *testing.T, client *ssh.Client, network string, names ...string) {
	t.Helper()
	connected := remoteExec(t, client, "docker network inspect "+network+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	for _, name := range names {
		if !strings.Contains(connected, name) {
			t.Fatalf("expected %s connected to %s, got %q", name, network, connected)
		}
	}
}
