//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerSwarmServiceParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-dss-%d", time.Now().UnixNano()%100000000)
	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-dss-*.json /tmp/.dibra-agent /tmp/dibra-dss-env-*")
	defer func() {
		mustRemote(t, client, "docker service ls -q | xargs -r docker service rm >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker config ls -q | xargs -r docker config rm >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker secret ls -q | xargs -r docker secret rm >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker network ls --filter driver=overlay -q | xargs -r docker network rm >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()
	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	mustRemote(t, client, "docker pull busybox:latest >/dev/null")

	templatePath := writeResultTemplate(t, "svc_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-dss-" + name + ".json"
		playbook := playbookHeader + `
  - name: Manage swarm service
    community.docker.docker_swarm_service:
` + arguments + `
    register: svc_result
` + taskOptions + `

  - name: Persist service result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			return nil, output
		}
		return readRemoteJSONMap(t, client, remotePath), output
	}
	success := func(t *testing.T, name, arguments, taskOptions string) map[string]any {
		t.Helper()
		result, output := run(t, name, arguments, taskOptions)
		if result == nil {
			t.Fatalf("%s failed: %s", name, output)
		}
		return result
	}
	absent := func(t *testing.T, name string) {
		t.Helper()
		success(t, name+"-absent", "      name: "+name+"\n      state: absent\n", "")
	}
	mustFail := func(t *testing.T, name, arguments, contains string) {
		t.Helper()
		_, output := run(t, name, arguments, "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, contains) {
			t.Fatalf("%s = %s", name, output)
		}
	}
	changed := func(t *testing.T, label string, results []map[string]any, want ...bool) {
		t.Helper()
		if len(results) != len(want) {
			t.Fatalf("%s: got %d results want %d", label, len(results), len(want))
		}
		for i := range want {
			if results[i]["changed"] != want[i] {
				t.Fatalf("%s[%d] changed=%v want %v result=%#v", label, i, results[i]["changed"], want[i], results[i])
			}
		}
	}
	base := func(name string) string {
		return "      name: " + name + "\n      image: alpine:latest\n      resolve_image: false\n"
	}

	t.Run("misc", func(t *testing.T) {
		_, output := run(t, "missing-name", "      state: present\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("missing name = %s", output)
		}
		missing := success(t, "absent-missing", "      name: non_existing_service\n      state: absent\n", "")
		if missing["changed"] != false {
			t.Fatalf("absent missing = %#v", missing)
		}

		name := prefix + "-misc"
		created := success(t, "misc-create", base(name)+"      endpoint_mode: dnsrr\n      args:\n        - sleep\n        - \"3600\"\n", "")
		if created["changed"] != true {
			t.Fatalf("create = %#v", created)
		}
		changedArgs := success(t, "misc-args", base(name)+"      args:\n        - sleep\n        - \"1800\"\n", "")
		facts := nestedMap(changedArgs, "swarm_service")
		if fmt.Sprint(facts["args"]) != fmt.Sprint([]any{"sleep", "1800"}) && !strings.Contains(fmt.Sprint(facts["args"]), "1800") {
			t.Fatalf("args = %#v", facts["args"])
		}
		rebuilt := success(t, "misc-global", base(name)+"      endpoint_mode: vip\n      mode: global\n      args:\n        - sleep\n        - \"1800\"\n", "")
		if rebuilt["rebuilt"] != true {
			t.Fatalf("rebuild = %#v", rebuilt)
		}
		published := success(t, "misc-publish", base(name)+`      mode: global
      args:
        - sleep
        - "1800"
      endpoint_mode: vip
      publish:
        - protocol: tcp
          published_port: 60001
          target_port: 60001
        - protocol: udp
          published_port: 60001
          target_port: 60001
`, "")
		if published["changed"] != true {
			t.Fatalf("publish = %#v", published)
		}
		removed := success(t, "misc-remove", "      name: "+name+"\n      state: absent\n", "")
		if removed["changed"] != true {
			t.Fatalf("remove = %#v", removed)
		}
	})

	t.Run("options", func(t *testing.T) {
		name := prefix + "-opts"
		defer absent(t, name)
		sleep := "      command: '/bin/sh -v -c \"sleep 10m\"'\n"

		args := success(t, "args-1", base(name)+"      args:\n        - sleep\n        - \"3600\"\n", "")
		argsIdem := success(t, "args-2", base(name)+"      args:\n        - sleep\n        - \"3600\"\n", "")
		argsChange := success(t, "args-3", base(name)+"      args:\n        - sleep\n        - \"3400\"\n", "")
		argsEmpty := success(t, "args-4", base(name)+"      args: []\n", "")
		argsEmptyIdem := success(t, "args-5", base(name)+"      args: []\n", "")
		if args["changed"] != true || argsIdem["changed"] != false || argsChange["changed"] != true || argsEmpty["changed"] != true || argsEmptyIdem["changed"] != false {
			t.Fatalf("args cycle changed=%v %v %v %v %v", args["changed"], argsIdem["changed"], argsChange["changed"], argsEmpty["changed"], argsEmptyIdem["changed"])
		}

		cmd := success(t, "cmd-1", base(name)+"      command: '/bin/sh -v -c \"sleep 10m\"'\n", "")
		cmdIdem := success(t, "cmd-2", base(name)+"      command: '/bin/sh -v -c \"sleep 10m\"'\n", "")
		cmdLess := success(t, "cmd-3", base(name)+"      command: '/bin/sh -c \"sleep 10m\"'\n", "")
		cmdList := success(t, "cmd-4", base(name)+"      command:\n        - /bin/sh\n        - -c\n        - sleep 10m\n", "")
		cmdEmpty := success(t, "cmd-5", base(name)+"      command: []\n", "")
		cmdEmptyIdem := success(t, "cmd-6", base(name)+"      command: []\n", "")
		changed(t, "command", []map[string]any{cmd, cmdIdem, cmdLess, cmdList, cmdEmpty, cmdEmptyIdem}, true, false, true, false, true, false)
		mustFail(t, "cmd-bool", base(name)+"      command: true\n", "Only string or list allowed")
		mustFail(t, "cmd-list-bool", base(name)+"      command:\n        - /bin/sh\n        - true\n", "All items in a command list need to be strings")

		labels := success(t, "clabels-1", base(name)+sleep+"      container_labels:\n        test_1: \"1\"\n        test_2: \"2\"\n", "")
		labelsIdem := success(t, "clabels-2", base(name)+sleep+"      container_labels:\n        test_1: \"1\"\n        test_2: \"2\"\n", "")
		labelsChange := success(t, "clabels-3", base(name)+sleep+"      container_labels:\n        test_1: \"1\"\n        test_2: \"3\"\n", "")
		labelsEmpty := success(t, "clabels-4", base(name)+sleep+"      container_labels: {}\n", "")
		labelsEmptyIdem := success(t, "clabels-5", base(name)+sleep+"      container_labels: {}\n", "")
		changed(t, "container_labels", []map[string]any{labels, labelsIdem, labelsChange, labelsEmpty, labelsEmptyIdem}, true, false, true, true, false)

		dns := success(t, "dns-1", base(name)+sleep+"      dns:\n        - 1.1.1.1\n        - 8.8.8.8\n", "")
		dnsIdem := success(t, "dns-2", base(name)+sleep+"      dns:\n        - 1.1.1.1\n        - 8.8.8.8\n", "")
		dnsOrder := success(t, "dns-3", base(name)+sleep+"      dns:\n        - 8.8.8.8\n        - 1.1.1.1\n", "")
		dnsChange := success(t, "dns-4", base(name)+sleep+"      dns:\n        - 8.8.8.8\n        - 9.9.9.9\n", "")
		dnsEmpty := success(t, "dns-5", base(name)+sleep+"      dns: []\n", "")
		dnsEmptyIdem := success(t, "dns-6", base(name)+sleep+"      dns: []\n", "")
		changed(t, "dns", []map[string]any{dns, dnsIdem, dnsOrder, dnsChange, dnsEmpty, dnsEmptyIdem}, true, false, true, true, true, false)

		dnsOpt := success(t, "dnsopt-1", base(name)+sleep+"      dns_options:\n        - \"timeout:10\"\n        - rotate\n", "")
		dnsOptIdem := success(t, "dnsopt-2", base(name)+sleep+"      dns_options:\n        - \"timeout:10\"\n        - rotate\n", "")
		dnsOptChange := success(t, "dnsopt-3", base(name)+sleep+"      dns_options:\n        - \"timeout:10\"\n        - no-check-names\n", "")
		dnsOptOrder := success(t, "dnsopt-4", base(name)+sleep+"      dns_options:\n        - no-check-names\n        - \"timeout:10\"\n", "")
		dnsOptEmpty := success(t, "dnsopt-5", base(name)+sleep+"      dns_options: []\n", "")
		dnsOptEmptyIdem := success(t, "dnsopt-6", base(name)+sleep+"      dns_options: []\n", "")
		changed(t, "dns_options", []map[string]any{dnsOpt, dnsOptIdem, dnsOptChange, dnsOptOrder, dnsOptEmpty, dnsOptEmptyIdem}, true, false, true, false, true, false)

		dnsSearch := success(t, "dnss-1", base(name)+sleep+"      dns_search:\n        - example.com\n        - example.org\n", "")
		dnsSearchIdem := success(t, "dnss-2", base(name)+sleep+"      dns_search:\n        - example.com\n        - example.org\n", "")
		dnsSearchOrder := success(t, "dnss-3", base(name)+sleep+"      dns_search:\n        - example.org\n        - example.com\n", "")
		dnsSearchChange := success(t, "dnss-4", base(name)+sleep+"      dns_search:\n        - ansible.com\n        - example.com\n", "")
		dnsSearchEmpty := success(t, "dnss-5", base(name)+sleep+"      dns_search: []\n", "")
		dnsSearchEmptyIdem := success(t, "dnss-6", base(name)+sleep+"      dns_search: []\n", "")
		changed(t, "dns_search", []map[string]any{dnsSearch, dnsSearchIdem, dnsSearchOrder, dnsSearchChange, dnsSearchEmpty, dnsSearchEmptyIdem}, true, false, true, true, true, false)

		endpoint := success(t, "ep-1", base(name)+sleep+"      endpoint_mode: dnsrr\n", "")
		endpointIdem := success(t, "ep-2", base(name)+sleep+"      endpoint_mode: dnsrr\n", "")
		endpointChange := success(t, "ep-3", base(name)+sleep+"      endpoint_mode: vip\n", "")
		changed(t, "endpoint_mode", []map[string]any{endpoint, endpointIdem, endpointChange}, true, false, true)

		env := success(t, "env-1", base(name)+sleep+"      env:\n        - TEST1=val1\n        - TEST2=val2\n", "")
		envDict := success(t, "env-2", base(name)+sleep+"      env:\n        TEST1: val1\n        TEST2: val2\n", "")
		envChange := success(t, "env-3", base(name)+sleep+"      env:\n        - TEST1=val1\n        - TEST2=val3\n", "")
		envOrder := success(t, "env-4", base(name)+sleep+"      env:\n        - TEST2=val3\n        - TEST1=val1\n", "")
		envEmpty := success(t, "env-5", base(name)+sleep+"      env: []\n", "")
		envEmptyIdem := success(t, "env-6", base(name)+sleep+"      env: []\n", "")
		changed(t, "env", []map[string]any{env, envDict, envChange, envOrder, envEmpty, envEmptyIdem}, true, false, true, false, true, false)
		mustFail(t, "env-bool", base(name)+sleep+"      env:\n        TEST1: true\n", "Non-string value found for env option")
		mustFail(t, "env-bad", base(name)+sleep+"      env:\n        - TEST1=val3\n        - TEST2\n", "Invalid environment variable found in list")

		mustRemote(t, client, "printf 'TEST3=val3\nTEST4=val4\n' > /tmp/dibra-dss-env-1")
		mustRemote(t, client, "printf 'TEST3=val5\nTEST5=val5\n' > /tmp/dibra-dss-env-2")
		envFiles := success(t, "envfiles-1", base(name)+sleep+"      env_files:\n        - /tmp/dibra-dss-env-1\n", "")
		envFilesIdem := success(t, "envfiles-2", base(name)+sleep+"      env_files:\n        - /tmp/dibra-dss-env-1\n", "")
		envFilesMore := success(t, "envfiles-3", base(name)+sleep+"      env_files:\n        - /tmp/dibra-dss-env-1\n        - /tmp/dibra-dss-env-2\n", "")
		envFilesOrder := success(t, "envfiles-4", base(name)+sleep+"      env_files:\n        - /tmp/dibra-dss-env-2\n        - /tmp/dibra-dss-env-1\n", "")
		envFilesOrderIdem := success(t, "envfiles-5", base(name)+sleep+"      env_files:\n        - /tmp/dibra-dss-env-2\n        - /tmp/dibra-dss-env-1\n", "")
		envFilesEmpty := success(t, "envfiles-6", base(name)+sleep+"      env_files: []\n", "")
		envFilesEmptyIdem := success(t, "envfiles-7", base(name)+sleep+"      env_files: []\n", "")
		changed(t, "env_files", []map[string]any{envFiles, envFilesIdem, envFilesMore, envFilesOrder, envFilesOrderIdem, envFilesEmpty, envFilesEmptyIdem}, true, false, true, true, false, true, false)

		force1 := success(t, "force-1", base(name)+sleep+"      args:\n        - sleep\n        - \"3600\"\n      force_update: true\n", "")
		force2 := success(t, "force-2", base(name)+sleep+"      args:\n        - sleep\n        - \"3600\"\n      force_update: true\n", "")
		changed(t, "force_update", []map[string]any{force1, force2}, true, true)

		groups := success(t, "groups-1", base(name)+sleep+"      groups:\n        - \"1234\"\n        - \"5678\"\n", "")
		groupsIdem := success(t, "groups-2", base(name)+sleep+"      groups:\n        - \"1234\"\n        - \"5678\"\n", "")
		groupsOrder := success(t, "groups-3", base(name)+sleep+"      groups:\n        - \"5678\"\n        - \"1234\"\n", "")
		groupsChange := success(t, "groups-4", base(name)+sleep+"      groups:\n        - \"1234\"\n", "")
		groupsEmpty := success(t, "groups-5", base(name)+sleep+"      groups: []\n", "")
		groupsEmptyIdem := success(t, "groups-6", base(name)+sleep+"      groups: []\n", "")
		changed(t, "groups", []map[string]any{groups, groupsIdem, groupsOrder, groupsChange, groupsEmpty, groupsEmptyIdem}, true, false, false, true, true, false)

		health := success(t, "hc-1", base(name)+sleep+`      healthcheck:
        test:
          - CMD
          - sleep
          - "1"
        timeout: 2s
        interval: 0h0m2s3ms4us
        retries: 2
        start_period: 20s
`, "")
		healthIdem := success(t, "hc-2", base(name)+sleep+`      healthcheck:
        test:
          - CMD
          - sleep
          - 1
        timeout: 2s
        interval: 0h0m2s3ms4us
        retries: 2
        start_period: 20s
`, "")
		healthChange := success(t, "hc-3", base(name)+sleep+`      healthcheck:
        test:
          - CMD
          - sleep
          - "1"
        timeout: 3s
        interval: 0h1m2s3ms4us
        retries: 3
`, "")
		healthNone := success(t, "hc-4", base(name)+sleep+"      healthcheck:\n        test:\n          - NONE\n", "")
		healthNoneIdem := success(t, "hc-5", base(name)+sleep+"      healthcheck:\n        test:\n          - NONE\n", "")
		healthString := success(t, "hc-6", base(name)+sleep+"      healthcheck:\n        test: sleep 1\n", "")
		healthStringIdem := success(t, "hc-7", base(name)+sleep+"      healthcheck:\n        test: sleep 1\n", "")
		healthEmpty := success(t, "hc-8", base(name)+sleep+"      healthcheck: {}\n", "")
		healthEmptyIdem := success(t, "hc-9", base(name)+sleep+"      healthcheck: {}\n", "")
		changed(t, "healthcheck", []map[string]any{health, healthIdem, healthChange, healthNone, healthNoneIdem, healthString, healthStringIdem, healthEmpty, healthEmptyIdem}, true, false, true, true, false, true, false, true, false)

		hostname := success(t, "hn-1", base(name)+sleep+"      hostname: me.example.com\n", "")
		hostnameIdem := success(t, "hn-2", base(name)+sleep+"      hostname: me.example.com\n", "")
		hostnameChange := success(t, "hn-3", base(name)+sleep+"      hostname: me.example.org\n", "")
		changed(t, "hostname", []map[string]any{hostname, hostnameIdem, hostnameChange}, true, false, true)

		hosts := success(t, "hosts-1", base(name)+sleep+"      hosts:\n        example.com: 1.2.3.4\n        example.org: 4.3.2.1\n", "")
		hostsIdem := success(t, "hosts-2", base(name)+sleep+"      hosts:\n        example.com: 1.2.3.4\n        example.org: 4.3.2.1\n", "")
		hostsChange := success(t, "hosts-3", base(name)+sleep+"      hosts:\n        example.com: 1.2.3.4\n", "")
		changed(t, "hosts", []map[string]any{hosts, hostsIdem, hostsChange}, true, false, true)

		absent(t, name)
		image := success(t, "image-1", base(name)+sleep, "")
		imageIdem := success(t, "image-2", base(name)+sleep, "")
		imageChange := success(t, "image-3", "      name: "+name+"\n      image: busybox:latest\n      resolve_image: false\n", "")
		changed(t, "image", []map[string]any{image, imageIdem, imageChange}, true, false, true)

		svcLabels := success(t, "slabels-1", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n"+sleep+"      labels:\n        test_1: \"1\"\n        test_2: \"2\"\n", "")
		svcLabelsIdem := success(t, "slabels-2", base(name)+sleep+"      labels:\n        test_1: \"1\"\n        test_2: \"2\"\n", "")
		svcLabelsChange := success(t, "slabels-3", base(name)+sleep+"      labels:\n        test_1: \"1\"\n        test_2: \"2\"\n        test_3: \"3\"\n", "")
		svcLabelsEmpty := success(t, "slabels-4", base(name)+sleep+"      labels: {}\n", "")
		svcLabelsEmptyIdem := success(t, "slabels-5", base(name)+sleep+"      labels: {}\n", "")
		changed(t, "labels", []map[string]any{svcLabels, svcLabelsIdem, svcLabelsChange, svcLabelsEmpty, svcLabelsEmptyIdem}, true, false, true, true, false)

		absent(t, name)
		mode := success(t, "mode-1", base(name)+sleep+"      mode: replicated\n      replicas: 1\n", "")
		modeIdem := success(t, "mode-2", base(name)+sleep+"      mode: replicated\n      replicas: 1\n", "")
		modeGlobal := success(t, "mode-3", base(name)+sleep+"      mode: global\n      replicas: 1\n", "")
		modeJob := success(t, "mode-4", base(name)+sleep+"      mode: replicated-job\n      replicas: 1\n", "")
		modeJobIdem := success(t, "mode-5", base(name)+sleep+"      mode: replicated-job\n      replicas: 1\n", "")
		modeBack := success(t, "mode-6", base(name)+sleep+"      mode: replicated\n      replicas: 1\n", "")
		changed(t, "mode", []map[string]any{mode, modeIdem, modeGlobal, modeJob, modeJobIdem, modeBack}, true, false, true, true, false, true)

		grace := success(t, "grace-1", base(name)+sleep+"      stop_grace_period: 60s\n", "")
		graceIdem := success(t, "grace-2", base(name)+sleep+"      stop_grace_period: 60s\n", "")
		graceChange := success(t, "grace-3", base(name)+sleep+"      stop_grace_period: 1m30s\n", "")
		changed(t, "stop_grace_period", []map[string]any{grace, graceIdem, graceChange}, true, false, true)

		signal := success(t, "sig-1", base(name)+sleep+"      stop_signal: \"30\"\n", "")
		signalIdem := success(t, "sig-2", base(name)+sleep+"      stop_signal: \"30\"\n", "")
		signalChange := success(t, "sig-3", base(name)+sleep+"      stop_signal: \"9\"\n", "")
		changed(t, "stop_signal", []map[string]any{signal, signalIdem, signalChange}, true, false, true)

		publish := success(t, "pub-1", base(name)+sleep+`      publish:
        - protocol: tcp
          published_port: 60001
          target_port: 60001
        - protocol: udp
          published_port: 60002
          target_port: 60002
`, "")
		publishIdem := success(t, "pub-2", base(name)+sleep+`      publish:
        - protocol: udp
          published_port: 60002
          target_port: 60002
        - published_port: 60001
          target_port: 60001
`, "")
		publishChange := success(t, "pub-3", base(name)+sleep+`      publish:
        - protocol: tcp
          published_port: 60002
          target_port: 60003
        - protocol: udp
          published_port: 60001
          target_port: 60001
`, "")
		publishHost := success(t, "pub-4", base(name)+sleep+`      publish:
        - protocol: tcp
          published_port: 60002
          target_port: 60003
          mode: host
        - protocol: udp
          published_port: 60001
          target_port: 60001
          mode: host
`, "")
		publishHostIdem := success(t, "pub-5", base(name)+sleep+`      publish:
        - protocol: udp
          published_port: 60001
          target_port: 60001
          mode: host
        - protocol: tcp
          published_port: 60002
          target_port: 60003
          mode: host
`, "")
		publishEmpty := success(t, "pub-6", base(name)+sleep+"      publish: []\n", "")
		publishEmptyIdem := success(t, "pub-7", base(name)+sleep+"      publish: []\n", "")
		publishBoth := success(t, "pub-8", base(name)+sleep+`      publish:
        - protocol: udp
          published_port: 60001
          target_port: 60001
          mode: host
        - protocol: tcp
          published_port: 60001
          target_port: 60001
          mode: host
`, "")
		publishOpen := success(t, "pub-9", base(name)+sleep+`      publish:
        - protocol: udp
          target_port: 60001
          mode: host
`, "")
		publishOpenIdem := success(t, "pub-10", base(name)+sleep+`      publish:
        - protocol: udp
          target_port: 60001
          mode: host
`, "")
		changed(t, "publish", []map[string]any{publish, publishIdem, publishChange, publishHost, publishHostIdem, publishEmpty, publishEmptyIdem, publishBoth, publishOpen, publishOpenIdem}, true, false, true, true, false, true, false, true, true, false)

		readOnly := success(t, "ro-1", base(name)+sleep+"      read_only: true\n", "")
		readOnlyIdem := success(t, "ro-2", base(name)+sleep+"      read_only: true\n", "")
		readOnlyChange := success(t, "ro-3", base(name)+sleep+"      read_only: false\n", "")
		changed(t, "read_only", []map[string]any{readOnly, readOnlyIdem, readOnlyChange}, true, false, true)

		replicas := success(t, "rep-1", base(name)+sleep+"      replicas: 2\n", "")
		replicasIdem := success(t, "rep-2", base(name)+sleep+"      replicas: 2\n", "")
		replicasChange := success(t, "rep-3", base(name)+sleep+"      replicas: 3\n", "")
		changed(t, "replicas", []map[string]any{replicas, replicasIdem, replicasChange}, true, false, true)

		absent(t, name)
		resolveFalse := success(t, "res-1", base(name)+sleep, "")
		resolveFalseIdem := success(t, "res-2", base(name)+sleep, "")
		resolveTrue := success(t, "res-3", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: true\n"+sleep, "")
		changed(t, "resolve_image", []map[string]any{resolveFalse, resolveFalseIdem, resolveTrue}, true, false, true)

		tty := success(t, "tty-1", base(name)+sleep+"      tty: true\n", "")
		ttyIdem := success(t, "tty-2", base(name)+sleep+"      tty: true\n", "")
		ttyChange := success(t, "tty-3", base(name)+sleep+"      tty: false\n", "")
		changed(t, "tty", []map[string]any{tty, ttyIdem, ttyChange}, true, false, true)

		user := success(t, "user-1", base(name)+sleep+"      user: operator\n", "")
		userIdem := success(t, "user-2", base(name)+sleep+"      user: operator\n", "")
		userChange := success(t, "user-3", base(name)+sleep+"      user: root\n", "")
		changed(t, "user", []map[string]any{user, userIdem, userChange}, true, false, true)

		absent(t, name)
		workdir := success(t, "wd-1", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      working_dir: /tmp\n", "")
		workdirIdem := success(t, "wd-2", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      working_dir: /tmp\n", "")
		workdirChange := success(t, "wd-3", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      working_dir: /\n", "")
		changed(t, "working_dir", []map[string]any{workdir, workdirIdem, workdirChange}, true, false, true)

		absent(t, name)
		init := success(t, "init-1", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n", "")
		initIdem := success(t, "init-2", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n", "")
		initChange := success(t, "init-3", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: false\n", "")
		changed(t, "init", []map[string]any{init, initIdem, initChange}, true, false, true)

		absent(t, name)
		caps := success(t, "caps-1", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n      cap_add:\n        - sys_time\n      cap_drop:\n        - all\n", "")
		capsIdem := success(t, "caps-2", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n      cap_add:\n        - sys_time\n      cap_drop:\n        - all\n", "    diff: true\n")
		capsLess := success(t, "caps-3", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n      cap_add: []\n      cap_drop:\n        - all\n", "    diff: true\n")
		capsChange := success(t, "caps-4", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: false\n      init: true\n      cap_add:\n        - setgid\n      cap_drop:\n        - all\n", "    diff: true\n")
		changed(t, "caps", []map[string]any{caps, capsIdem, capsLess, capsChange}, true, false, true, true)

		sysctls := success(t, "sys-1", base(name)+sleep+"      sysctls:\n        net.ipv4.ip_unprivileged_port_start: \"80\"\n", "")
		sysctlsIdem := success(t, "sys-2", base(name)+sleep+"      sysctls:\n        net.ipv4.ip_unprivileged_port_start: \"80\"\n", "")
		sysctlsChange := success(t, "sys-3", base(name)+sleep+"      sysctls:\n        net.ipv4.ip_unprivileged_port_start: \"1024\"\n", "")
		changed(t, "sysctls", []map[string]any{sysctls, sysctlsIdem, sysctlsChange}, true, false, true)
	})

	t.Run("resources", func(t *testing.T) {
		name := prefix + "-res"
		defer absent(t, name)
		sleep := "      command: '/bin/sh -v -c \"sleep 10m\"'\n"
		cpu := success(t, "cpu-1", base(name)+sleep+"      limits:\n        cpus: 1\n", "")
		cpuIdem := success(t, "cpu-2", base(name)+sleep+"      limits:\n        cpus: 1\n", "")
		cpuChange := success(t, "cpu-3", base(name)+sleep+"      limits:\n        cpus: 0.5\n", "")
		if cpu["changed"] != true || cpuIdem["changed"] != false || cpuChange["changed"] != true {
			t.Fatalf("limits.cpus = %v %v %v", cpu["changed"], cpuIdem["changed"], cpuChange["changed"])
		}
		mem := success(t, "mem-1", base(name)+sleep+"      limits:\n        memory: 64M\n", "")
		memIdem := success(t, "mem-2", base(name)+sleep+"      limits:\n        memory: 64M\n", "")
		if mem["changed"] != true || memIdem["changed"] != false {
			t.Fatalf("limits.memory = %v %v", mem["changed"], memIdem["changed"])
		}
		reserve := success(t, "rcpu-1", base(name)+sleep+"      reservations:\n        cpus: 0.25\n        memory: 32M\n", "")
		reserveIdem := success(t, "rcpu-2", base(name)+sleep+"      reservations:\n        cpus: 0.25\n        memory: 32M\n", "")
		if reserve["changed"] != true || reserveIdem["changed"] != false {
			t.Fatalf("reservations = %v %v", reserve["changed"], reserveIdem["changed"])
		}
	})

	t.Run("placement restart update rollback logging", func(t *testing.T) {
		name := prefix + "-cfg"
		defer absent(t, name)
		sleep := "      command: '/bin/sh -v -c \"sleep 10m\"'\n"
		place := success(t, "place-1", base(name)+sleep+"      placement:\n        constraints:\n          - node.role == manager\n        replicas_max_per_node: 1\n", "")
		placeIdem := success(t, "place-2", base(name)+sleep+"      placement:\n        constraints:\n          - node.role == manager\n        replicas_max_per_node: 1\n", "")
		placePrefs := success(t, "place-3", base(name)+sleep+"      placement:\n        preferences:\n          - spread: node.labels.test\n", "")
		placePrefsIdem := success(t, "place-4", base(name)+sleep+"      placement:\n        preferences:\n          - spread: node.labels.test\n", "")
		placePrefsChange := success(t, "place-5", base(name)+sleep+"      placement:\n        preferences:\n          - spread: node.labels.test2\n", "")
		placePrefsEmpty := success(t, "place-6", base(name)+sleep+"      placement:\n        preferences: []\n", "")
		placePrefsEmptyIdem := success(t, "place-7", base(name)+sleep+"      placement:\n        preferences: []\n", "")
		if place["changed"] != true || placeIdem["changed"] != false {
			t.Fatalf("placement = %v %v", place["changed"], placeIdem["changed"])
		}
		changed(t, "placement.preferences", []map[string]any{placePrefs, placePrefsIdem, placePrefsChange, placePrefsEmpty, placePrefsEmptyIdem}, true, false, true, true, false)
		restart := success(t, "rst-1", base(name)+sleep+"      restart_config:\n        condition: on-failure\n        delay: 5s\n        max_attempts: 3\n        window: 30s\n", "")
		restartIdem := success(t, "rst-2", base(name)+sleep+"      restart_config:\n        condition: on-failure\n        delay: 5s\n        max_attempts: 3\n        window: 30s\n", "")
		if restart["changed"] != true || restartIdem["changed"] != false {
			t.Fatalf("restart_config = %v %v", restart["changed"], restartIdem["changed"])
		}
		update := success(t, "upd-1", base(name)+sleep+"      update_config:\n        parallelism: 2\n        delay: 10s\n        failure_action: rollback\n        order: start-first\n", "")
		updateIdem := success(t, "upd-2", base(name)+sleep+"      update_config:\n        parallelism: 2\n        delay: 10s\n        failure_action: rollback\n        order: start-first\n", "")
		if update["changed"] != true || updateIdem["changed"] != false {
			t.Fatalf("update_config = %v %v", update["changed"], updateIdem["changed"])
		}
		rollback := success(t, "rb-1", base(name)+sleep+"      rollback_config:\n        parallelism: 1\n        delay: 5s\n        failure_action: pause\n", "")
		rollbackIdem := success(t, "rb-2", base(name)+sleep+"      rollback_config:\n        parallelism: 1\n        delay: 5s\n        failure_action: pause\n", "")
		if rollback["changed"] != true || rollbackIdem["changed"] != false {
			t.Fatalf("rollback_config = %v %v", rollback["changed"], rollbackIdem["changed"])
		}
		logging := success(t, "log-1", base(name)+sleep+"      logging:\n        driver: json-file\n        options:\n          max-size: 10m\n", "")
		loggingIdem := success(t, "log-2", base(name)+sleep+"      logging:\n        driver: json-file\n        options:\n          max-size: 10m\n", "")
		loggingChange := success(t, "log-3", base(name)+sleep+"      logging:\n        driver: json-file\n        options:\n          max-size: 20m\n", "")
		loggingEmpty := success(t, "log-4", base(name)+sleep+"      logging:\n        driver: json-file\n        options: {}\n", "")
		loggingEmptyIdem := success(t, "log-5", base(name)+sleep+"      logging:\n        driver: json-file\n        options: {}\n", "")
		if logging["changed"] != true || loggingIdem["changed"] != false {
			t.Fatalf("logging = %v %v", logging["changed"], loggingIdem["changed"])
		}
		changed(t, "logging.options", []map[string]any{loggingChange, loggingEmpty, loggingEmptyIdem}, true, true, false)
	})

	t.Run("networks mounts configs secrets", func(t *testing.T) {
		name := prefix + "-net"
		net1 := prefix + "-n1"
		net2 := prefix + "-n2"
		vol1 := prefix + "-v1"
		cfg1 := prefix + "-c1"
		sec1 := prefix + "-s1"
		defer func() {
			absent(t, name)
			mustRemote(t, client, "docker config rm "+cfg1+" >/dev/null 2>&1 || true")
			mustRemote(t, client, "docker secret rm "+sec1+" >/dev/null 2>&1 || true")
			mustRemote(t, client, "docker network rm "+net1+" "+net2+" >/dev/null 2>&1 || true")
			mustRemote(t, client, "docker volume rm "+vol1+" >/dev/null 2>&1 || true")
		}()
		mustRemote(t, client, "docker network create -d overlay --attachable "+net1)
		mustRemote(t, client, "docker network create -d overlay --attachable "+net2)
		mustRemote(t, client, "docker volume create "+vol1)
		mustRemote(t, client, "echo hello | docker config create "+cfg1+" -")
		mustRemote(t, client, "echo secret | docker secret create "+sec1+" -")
		sleep := "      command: '/bin/sh -v -c \"sleep 10m\"'\n"

		nets := success(t, "net-1", base(name)+sleep+"      networks:\n        - "+net1+"\n", "")
		netsIdem := success(t, "net-2", base(name)+sleep+"      networks:\n        - "+net1+"\n", "")
		netsChange := success(t, "net-3", base(name)+sleep+"      networks:\n        - name: "+net2+"\n          aliases:\n            - alias1\n", "")
		if nets["changed"] != true || netsIdem["changed"] != false || netsChange["changed"] != true {
			t.Fatalf("networks = %v %v %v", nets["changed"], netsIdem["changed"], netsChange["changed"])
		}

		mounts := success(t, "mnt-1", base(name)+sleep+"      mounts:\n        - source: "+vol1+"\n          target: /tmp/data\n          type: volume\n", "")
		mountsIdem := success(t, "mnt-2", base(name)+sleep+"      mounts:\n        - source: "+vol1+"\n          target: /tmp/data\n          type: volume\n", "")
		mountsRO := success(t, "mnt-ro", base(name)+sleep+"      mounts:\n        - source: "+vol1+"\n          target: /tmp/data\n          type: volume\n          readonly: true\n", "")
		tmpfs := success(t, "mnt-3", base(name)+sleep+"      mounts:\n        - type: tmpfs\n          target: /tmp/tmpfs\n          tmpfs_size: 64M\n          tmpfs_mode: \"1777\"\n", "")
		tmpfsIdem := success(t, "mnt-4", base(name)+sleep+"      mounts:\n        - type: tmpfs\n          target: /tmp/tmpfs\n          tmpfs_size: 64M\n          tmpfs_mode: \"1777\"\n", "")
		if mounts["changed"] != true || mountsIdem["changed"] != false || mountsRO["changed"] != true || tmpfs["changed"] != true || tmpfsIdem["changed"] != false {
			t.Fatalf("mounts = %v %v %v %v %v changes=%v tmpfsChanges=%v", mounts["changed"], mountsIdem["changed"], mountsRO["changed"], tmpfs["changed"], tmpfsIdem["changed"], mountsIdem["changes"], tmpfsIdem["changes"])
		}

		configs := success(t, "cfg-1", base(name)+sleep+"      configs:\n        - config_name: "+cfg1+"\n          filename: /tmp/"+cfg1+".txt\n          mode: \"0600\"\n", "")
		configsIdem := success(t, "cfg-2", base(name)+sleep+"      configs:\n        - config_name: "+cfg1+"\n          filename: /tmp/"+cfg1+".txt\n          mode: \"0600\"\n", "")
		configsMode := success(t, "cfg-3", base(name)+sleep+"      configs:\n        - config_name: "+cfg1+"\n          filename: /tmp/"+cfg1+".txt\n          mode: \"0777\"\n", "")
		if configs["changed"] != true || configsIdem["changed"] != false || configsMode["changed"] != true {
			t.Fatalf("configs = %v %v %v %#v", configs["changed"], configsIdem["changed"], configsMode["changed"], configs)
		}

		secrets := success(t, "sec-1", base(name)+sleep+"      secrets:\n        - secret_name: "+sec1+"\n          filename: /run/secrets/"+sec1+"\n          uid: \"1000\"\n          gid: \"1000\"\n          mode: \"0400\"\n", "")
		secretsIdem := success(t, "sec-2", base(name)+sleep+"      secrets:\n        - secret_name: "+sec1+"\n          filename: /run/secrets/"+sec1+"\n          uid: \"1000\"\n          gid: \"1000\"\n          mode: \"0400\"\n", "")
		secretsMode := success(t, "sec-3", base(name)+sleep+"      secrets:\n        - secret_name: "+sec1+"\n          filename: /run/secrets/"+sec1+"\n          uid: \"1000\"\n          gid: \"1000\"\n          mode: \"0444\"\n", "")
		if secrets["changed"] != true || secretsIdem["changed"] != false || secretsMode["changed"] != true {
			t.Fatalf("secrets = %v %v %v", secrets["changed"], secretsIdem["changed"], secretsMode["changed"])
		}
	})

	t.Run("check mode resolve_image and preserve omitted env", func(t *testing.T) {
		name := prefix + "-extra"
		defer absent(t, name)
		sleep := "      command: ['sleep', '3000']\n"
		check := success(t, "check-create", base(name)+sleep, "    check_mode: true\n    diff: true\n")
		if check["changed"] != true {
			t.Fatalf("check = %#v", check)
		}
		if strings.TrimSpace(mustRemote(t, client, "docker service ls --filter name="+name+" -q")) != "" {
			t.Fatal("check mode created a service")
		}
		created := success(t, "real-create", base(name)+sleep+"      env:\n        FOO: bar\n", "")
		if created["changed"] != true {
			t.Fatalf("create = %#v", created)
		}
		scale := success(t, "scale-omit-env", base(name)+sleep+"      replicas: 2\n", "")
		if scale["changed"] != true {
			t.Fatalf("scale = %#v", scale)
		}
		env := strings.TrimSpace(mustRemote(t, client, "docker service inspect "+name+" --format '{{range .Spec.TaskTemplate.ContainerSpec.Env}}{{.}}{{end}}'"))
		if !strings.Contains(env, "FOO=bar") {
			t.Fatalf("omitted env was not preserved: %s", env)
		}

		resolve := success(t, "resolve", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: true\n"+sleep+"      replicas: 2\n      env:\n        FOO: bar\n", "")
		image, _ := nestedMap(resolve, "swarm_service")["image"].(string)
		if !strings.Contains(image, "@sha256:") {
			t.Fatalf("resolve_image = %q result=%#v", image, resolve)
		}
		resolveIdem := success(t, "resolve-idem", "      name: "+name+"\n      image: alpine:latest\n      resolve_image: true\n"+sleep+"      replicas: 2\n      env:\n        FOO: bar\n", "")
		if resolveIdem["changed"] != false {
			t.Fatalf("resolve idem changed = %#v first=%#v", resolveIdem, resolve)
		}

		hostNet := success(t, "hostnet-1", base(name)+sleep+"      networks:\n        - host\n", "")
		hostNetIdem := success(t, "hostnet-2", base(name)+sleep+"      networks:\n        - host\n", "")
		if hostNet["changed"] != true || hostNetIdem["changed"] != false {
			t.Fatalf("host network = %v %v %#v", hostNet["changed"], hostNetIdem["changed"], hostNetIdem)
		}

		forceKeep := success(t, "force-keep-env", base(name)+sleep+"      force_update: true\n", "")
		if forceKeep["changed"] != true {
			t.Fatalf("force_update omit env = %#v", forceKeep)
		}
		envAfterForce := strings.TrimSpace(mustRemote(t, client, "docker service inspect "+name+" --format '{{range .Spec.TaskTemplate.ContainerSpec.Env}}{{.}}{{end}}'"))
		if !strings.Contains(envAfterForce, "FOO=bar") {
			t.Fatalf("force_update dropped env: %s", envAfterForce)
		}
	})
}
