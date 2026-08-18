//go:build integration

package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

const (
	tlsFixtureDir     = "/tmp/dibra-tls-nginx"
	tlsHTTPHost       = "tcp://127.0.0.1:2375"
	tlsHTTPSHost      = "tcp://127.0.0.1:2376"
	tlsServerName     = "daemon-tls.ansible.com"
	tlsCAPath         = tlsFixtureDir + "/certs/ca.pem"
	tlsClientCertPath = tlsFixtureDir + "/certs/cert.pem"
	tlsClientKeyPath  = tlsFixtureDir + "/certs/key.pem"
	tlsServerCertPath = tlsFixtureDir + "/certs/server.pem"
	tlsServerKeyPath  = tlsFixtureDir + "/certs/server-key.pem"
)

// TestPlaybook_DockerTLSConnectionParity stands up an nginx HTTP/HTTPS frontend
// in front of the pinned Engine 29.7.2 unix socket and independently ports the
// pinned community.docker generic_connection_tests target: unix, plaintext TCP,
// and TLS TCP must talk to the same daemon. Env/arg precedence against a dead
// TCP host remains in TestDockerConnectionEnvironmentFallbackAndArgumentPrecedence;
// this file performs a real TLS dial.
func TestPlaybook_DockerTLSConnectionParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	startTLSFixture(t, client)
	defer stopTLSFixture(t, client)
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	unixInfo := runTLSHostInfo(t, client, "unix", `
      containers: false
      docker_host: unix:///var/run/docker.sock
`)
	if unixInfo["can_talk_to_docker"] != true {
		t.Fatalf("unix host info = %#v", unixInfo)
	}

	t.Run("plaintext TCP matches unix socket", func(t *testing.T) {
		httpInfo := runTLSHostInfo(t, client, "http", `
      containers: false
      docker_host: `+tlsHTTPHost+`
`)
		assertSameDaemon(t, unixInfo, httpInfo)
	})

	t.Run("TLS TCP with hostname and CA matches unix socket", func(t *testing.T) {
		httpsInfo := runTLSHostInfo(t, client, "https", `
      containers: false
      docker_host: `+tlsHTTPSHost+`
      tls: true
      validate_certs: true
      tls_hostname: `+tlsServerName+`
      ca_path: `+tlsCAPath+`
`)
		assertSameDaemon(t, unixInfo, httpsInfo)
	})

	t.Run("TLS TCP derives hostname from docker_host", func(t *testing.T) {
		httpsInfo := runTLSHostInfo(t, client, "https-derived", `
      containers: false
      docker_host: `+tlsHTTPSHost+`
      tls: true
      validate_certs: true
      ca_path: `+tlsCAPath+`
`)
		assertSameDaemon(t, unixInfo, httpsInfo)
	})

	t.Run("wrong TLS hostname fails certificate verification", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: TLS with wrong hostname
    docker_host_info:
      containers: false
      docker_host: `+tlsHTTPSHost+`
      tls: true
      validate_certs: true
      tls_hostname: wrong.example
      ca_path: `+tlsCAPath+`
`)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("expected TLS hostname mismatch to fail: %s", output)
		}
	})

	t.Run("tls without validate_certs accepts a mismatched hostname", func(t *testing.T) {
		insecure := runTLSHostInfo(t, client, "https-insecure", `
      containers: false
      docker_host: `+tlsHTTPSHost+`
      tls: true
      validate_certs: false
      tls_hostname: wrong.example
`)
		assertSameDaemon(t, unixInfo, insecure)
	})

	t.Run("container API modules use the TLS frontend", func(t *testing.T) {
		const containerName = "dibra-tls-container-modules"
		remoteExec(t, client, "docker rm -f "+containerName+" || true")
		mustRemote(t, client, "docker run -d --name "+containerName+" alpine:latest sleep 300")
		defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

		common := map[string]any{
			"docker_host": tlsHTTPSHost, "api_version": "auto", "timeout": 30,
			"tls": true, "validate_certs": true, "tls_hostname": tlsServerName,
			"ca_path": tlsCAPath, "client_cert": tlsClientCertPath, "client_key": tlsClientKeyPath,
			"debug": true, "use_ssh_client": true,
		}
		args := func(specific map[string]any) map[string]any {
			result := make(map[string]any, len(common)+len(specific))
			for key, value := range common {
				result[key] = value
			}
			for key, value := range specific {
				result[key] = value
			}
			return result
		}

		info := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_info",
			args(map[string]any{"name": containerName}), "")
		if info["failed"] == true || info["exists"] != true {
			t.Fatalf("container_info TLS result = %#v", info)
		}
		container, _ := info["container"].(map[string]any)
		if container["Id"] == nil {
			t.Fatalf("container_info TLS result missing raw inspect: %#v", info)
		}
		wrongHostnameArgs := args(map[string]any{"name": containerName})
		wrongHostnameArgs["tls_hostname"] = "wrong.example"
		wrongHostname := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_info", wrongHostnameArgs, "")
		if wrongHostname["failed"] != true || !strings.Contains(resultString(wrongHostname, "msg"), "certificate") {
			t.Fatalf("container_info TLS hostname failure = %#v", wrongHostname)
		}

		execResult := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_exec",
			args(map[string]any{"container": containerName, "argv": []string{"echo", "tls-exec"}}), "")
		if execResult["failed"] == true || execResult["changed"] != true ||
			numberValue(execResult["rc"]) != 0 || execResult["stdout"] != "tls-exec" {
			t.Fatalf("container_exec TLS result = %#v", execResult)
		}

		copyArgs := args(map[string]any{
			"container": containerName, "content": "tls-copy",
			"container_path": "/tmp/tls-copy", "mode": "0644", "mode_parse": "modern",
		})
		copyResult := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_copy_into", copyArgs, "")
		if copyResult["failed"] == true || copyResult["changed"] != true {
			t.Fatalf("container_copy_into TLS result = %#v", copyResult)
		}
		copyAgain := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_copy_into", copyArgs, "")
		if copyAgain["failed"] == true || copyAgain["changed"] != false {
			t.Fatalf("container_copy_into TLS idempotency = %#v", copyAgain)
		}
		if got := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/tls-copy"); got != "tls-copy" {
			t.Fatalf("TLS copy content = %q", got)
		}

		tlsEnvironment := "DOCKER_HOST=" + tlsHTTPSHost + " " +
			"DOCKER_API_VERSION=auto DOCKER_TIMEOUT=30 DOCKER_TLS_VERIFY=true " +
			"DOCKER_CERT_PATH=" + tlsFixtureDir + "/certs " +
			"DOCKER_TLS_HOSTNAME=" + tlsServerName
		envInfo := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_info",
			map[string]any{"name": containerName}, tlsEnvironment)
		if envInfo["failed"] == true || envInfo["exists"] != true {
			t.Fatalf("container_info TLS environment result = %#v", envInfo)
		}
		envExec := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_exec",
			map[string]any{"container": containerName, "argv": []string{"true"}}, tlsEnvironment)
		if envExec["failed"] == true || numberValue(envExec["rc"]) != 0 {
			t.Fatalf("container_exec TLS environment result = %#v", envExec)
		}
		envCopy := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_copy_into",
			map[string]any{
				"container": containerName, "content": "tls-env-copy",
				"container_path": "/tmp/tls-env-copy", "mode": "0644", "mode_parse": "modern",
			}, tlsEnvironment)
		if envCopy["failed"] == true || envCopy["changed"] != true {
			t.Fatalf("container_copy_into TLS environment result = %#v", envCopy)
		}

		hostileEnvironment := "DOCKER_HOST=tcp://127.0.0.1:1 DOCKER_TLS=true DOCKER_TLS_VERIFY=true"
		unixArgs := map[string]any{
			"docker_host": "unix:///var/run/docker.sock",
			"tls":         false, "validate_certs": false,
		}
		infoOverride := make(map[string]any, len(unixArgs)+1)
		for key, value := range unixArgs {
			infoOverride[key] = value
		}
		infoOverride["name"] = containerName
		if response := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_info", infoOverride, hostileEnvironment); response["failed"] == true || response["exists"] != true {
			t.Fatalf("container_info explicit connection override = %#v", response)
		}
		execOverride := make(map[string]any, len(unixArgs)+2)
		for key, value := range unixArgs {
			execOverride[key] = value
		}
		execOverride["container"] = containerName
		execOverride["argv"] = []string{"true"}
		if response := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_exec", execOverride, hostileEnvironment); response["failed"] == true || numberValue(response["rc"]) != 0 {
			t.Fatalf("container_exec explicit connection override = %#v", response)
		}
		copyOverride := make(map[string]any, len(unixArgs)+5)
		for key, value := range unixArgs {
			copyOverride[key] = value
		}
		copyOverride["container"] = containerName
		copyOverride["content"] = "unix-override"
		copyOverride["container_path"] = "/tmp/unix-override"
		copyOverride["mode"] = "0644"
		copyOverride["mode_parse"] = "modern"
		if response := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_container_copy_into", copyOverride, hostileEnvironment); response["failed"] == true || response["changed"] != true {
			t.Fatalf("container_copy_into explicit connection override = %#v", response)
		}
	})

	t.Run("resource info API modules use the TLS frontend", func(t *testing.T) {
		const (
			networkName = "dibra-tls-network-info"
			volumeName  = "dibra-tls-volume-info"
		)
		remoteExec(t, client, "docker network rm "+networkName+" || true")
		remoteExec(t, client, "docker volume rm -f "+volumeName+" || true")
		mustRemote(t, client, "docker network create "+networkName)
		mustRemote(t, client, "docker volume create "+volumeName)
		defer remoteExec(t, client, "docker network rm "+networkName+" || true")
		defer remoteExec(t, client, "docker volume rm -f "+volumeName+" || true")

		common := map[string]any{
			"docker_host": tlsHTTPSHost, "api_version": "auto", "timeout": 30,
			"tls": true, "validate_certs": true, "tls_hostname": tlsServerName,
			"ca_path": tlsCAPath, "client_cert": tlsClientCertPath, "client_key": tlsClientKeyPath,
			"debug": true, "use_ssh_client": true,
		}
		arguments := func(name string) map[string]any {
			result := make(map[string]any, len(common)+1)
			for key, value := range common {
				result[key] = value
			}
			result["name"] = name
			return result
		}

		networkInfo := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_network_info", arguments(networkName), "")
		if networkInfo["failed"] == true || networkInfo["exists"] != true {
			t.Fatalf("network_info TLS result = %#v", networkInfo)
		}
		network, _ := networkInfo["network"].(map[string]any)
		if network["Name"] != networkName {
			t.Fatalf("network_info TLS inspection = %#v", network)
		}

		volumeInfo := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_volume_info", arguments(volumeName), "")
		if volumeInfo["failed"] == true || volumeInfo["exists"] != true {
			t.Fatalf("volume_info TLS result = %#v", volumeInfo)
		}
		volume, _ := volumeInfo["volume"].(map[string]any)
		if volume["Name"] != volumeName {
			t.Fatalf("volume_info TLS inspection = %#v", volume)
		}
	})

	t.Run("image API modules use the TLS frontend", func(t *testing.T) {
		const (
			taggedName   = "alpine:dibra-tls-image"
			registryName = "dibra-tls-image-registry"
			pushedName   = "127.0.0.1:5001/dibra-tls-image:v1"
			archivePath  = "/tmp/dibra-tls-image.tar"
		)
		remoteExec(t, client, "docker pull alpine:latest")
		remoteExec(t, client, "docker rm -f "+registryName+" || true")
		remoteExec(t, client, "docker image rm -f "+taggedName+" "+pushedName+" || true")
		remoteExec(t, client, "rm -f "+archivePath)
		defer remoteExec(t, client, "docker rm -f "+registryName+" || true")
		defer remoteExec(t, client, "docker image rm -f "+taggedName+" "+pushedName+" || true")
		defer remoteExec(t, client, "rm -f "+archivePath)

		common := map[string]any{
			"docker_host": tlsHTTPSHost, "api_version": "auto", "timeout": 30,
			"tls": true, "validate_certs": true, "tls_hostname": tlsServerName,
			"ca_path": tlsCAPath, "client_cert": tlsClientCertPath, "client_key": tlsClientKeyPath,
			"debug": true, "use_ssh_client": true,
		}
		args := func(specific map[string]any) map[string]any {
			result := make(map[string]any, len(common)+len(specific))
			for key, value := range common {
				result[key] = value
			}
			for key, value := range specific {
				result[key] = value
			}
			return result
		}
		run := func(module string, arguments map[string]any) map[string]any {
			t.Helper()
			result := runDockerAgentRequestWithEnvironment(t, client, module, arguments, "")
			if result["failed"] == true {
				t.Fatalf("%s over TLS failed: %#v", module, result)
			}
			return result
		}

		info := run("community.docker.docker_image_info", args(map[string]any{"name": []string{"alpine:latest"}}))
		if len(resultList(t, info, "images")) != 1 {
			t.Fatalf("image_info TLS result = %#v", info)
		}
		pull := run("community.docker.docker_image_pull", args(map[string]any{
			"name": "alpine", "tag": "latest", "pull": "not_present",
		}))
		if pull["changed"] != false {
			t.Fatalf("image_pull TLS result = %#v", pull)
		}
		tag := run("community.docker.docker_image_tag", args(map[string]any{
			"name": "alpine:latest", "repository": []string{taggedName},
		}))
		if tag["changed"] != true {
			t.Fatalf("image_tag TLS result = %#v", tag)
		}
		exported := run("community.docker.docker_image_export", args(map[string]any{
			"names": []string{"alpine:latest"}, "path": archivePath,
		}))
		if exported["changed"] != true {
			t.Fatalf("image_export TLS result = %#v", exported)
		}
		loaded := run("community.docker.docker_image_load", args(map[string]any{"path": archivePath}))
		if loaded["changed"] != true {
			t.Fatalf("image_load TLS result = %#v", loaded)
		}

		remoteExec(t, client, "docker run -d --name "+registryName+" -p 5001:5000 registry:2")
		remoteExec(t, client, "docker tag alpine:latest "+pushedName)
		pushed := run("community.docker.docker_image_push", args(map[string]any{"name": pushedName}))
		if pushed["changed"] != true {
			t.Fatalf("image_push TLS result = %#v", pushed)
		}
		removed := run("community.docker.docker_image_remove", args(map[string]any{
			"name": taggedName, "force": true,
		}))
		if removed["changed"] != true {
			t.Fatalf("image_remove TLS result = %#v", removed)
		}
		combined := run("community.docker.docker_image", args(map[string]any{
			"name": "alpine:latest", "source": "local",
		}))
		if combined["changed"] != false {
			t.Fatalf("docker_image TLS result = %#v", combined)
		}
		build := run("community.docker.docker_image_build", map[string]any{
			"name": "alpine:latest", "path": "/tmp", "rebuild": "never",
			"docker_host": tlsHTTPSHost, "api_version": "auto",
			"tls": true, "validate_certs": true, "tls_hostname": tlsServerName,
			"ca_path": tlsCAPath, "client_cert": tlsClientCertPath, "client_key": tlsClientKeyPath,
		})
		if build["changed"] != false {
			t.Fatalf("docker_image_build TLS result = %#v", build)
		}

		tlsEnvironment := "DOCKER_HOST=" + tlsHTTPSHost + " " +
			"DOCKER_API_VERSION=auto DOCKER_TIMEOUT=30 DOCKER_TLS_VERIFY=true " +
			"DOCKER_CERT_PATH=" + tlsFixtureDir + "/certs " +
			"DOCKER_TLS_HOSTNAME=" + tlsServerName
		envInfo := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_image_info",
			map[string]any{"name": []string{"alpine:latest"}}, tlsEnvironment)
		if envInfo["failed"] == true || len(resultList(t, envInfo, "images")) != 1 {
			t.Fatalf("image_info TLS environment result = %#v", envInfo)
		}
	})

	t.Run("DOCKER_CERT_PATH environment performs a real TLS dial", func(t *testing.T) {
		bootstrap := playbookHeader + `
  - name: Bootstrap Docker agent
    docker_host_info:
      containers: false
      docker_host: unix:///var/run/docker.sock
`
		if output := runPlaybook(t, bootstrap); strings.Contains(output, "FAILED") {
			t.Fatalf("failed to bootstrap Docker agent: %s", output)
		}
		request := `{"module":"community.docker.docker_host_info","args":{"containers":false},"check_mode":false,"diff":false}`
		command := "printf '%s' '" + request + "' | env " +
			"DOCKER_HOST=" + tlsHTTPSHost + " " +
			"DOCKER_TLS_VERIFY=true " +
			"DOCKER_CERT_PATH=" + tlsFixtureDir + "/certs " +
			"DOCKER_TLS_HOSTNAME=" + tlsServerName + " " +
			"/tmp/.dibra-agent"
		stdout, stderr, err := client.Run(command)
		if err != nil {
			t.Fatalf("TLS env agent invocation failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
			t.Fatalf("decode TLS env response %q: %v", stdout, err)
		}
		if response["failed"] == true || response["can_talk_to_docker"] != true {
			t.Fatalf("TLS env request failed: %#v", response)
		}
		assertSameDaemon(t, unixInfo, response)
	})

	t.Run("explicit unix arguments still override a live TLS environment", func(t *testing.T) {
		request := `{"module":"community.docker.docker_host_info","args":{"containers":false,"docker_host":"unix:///var/run/docker.sock","tls":false,"validate_certs":false},"check_mode":false,"diff":false}`
		command := "printf '%s' '" + request + "' | env " +
			"DOCKER_HOST=" + tlsHTTPSHost + " " +
			"DOCKER_TLS=true " +
			"DOCKER_TLS_VERIFY=true " +
			"DOCKER_CERT_PATH=" + tlsFixtureDir + "/certs " +
			"/tmp/.dibra-agent"
		stdout, stderr, err := client.Run(command)
		if err != nil {
			t.Fatalf("override agent invocation failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
			t.Fatalf("decode override response %q: %v", stdout, err)
		}
		if response["failed"] == true || response["can_talk_to_docker"] != true {
			t.Fatalf("unix override of TLS env failed: %#v", response)
		}
		assertSameDaemon(t, unixInfo, response)
	})

	t.Run("CLI-backed module uses the TLS frontend", func(t *testing.T) {
		remoteExec(t, client, "docker pull alpine:latest")
		writeRemoteFile(t, client, tlsFixtureDir+"/build/Dockerfile", "FROM alpine:latest\n")
		output := runPlaybook(t, playbookHeader+`
  - name: Inspect via TLS CLI
    community.docker.docker_image_build:
      name: alpine:latest
      path: `+tlsFixtureDir+`/build
      rebuild: never
      docker_host: `+tlsHTTPSHost+`
      tls: true
      validate_certs: true
      tls_hostname: `+tlsServerName+`
      ca_path: `+tlsCAPath+`
      client_cert: `+tlsClientCertPath+`
      client_key: `+tlsClientKeyPath+`
    register: tls_build_result

  - name: Persist CLI TLS result
    check_mode: false
    template:
      src: `+writeResultTemplate(t, "tls_build_result")+`
      dest: /tmp/dibra-tls-build.json
`)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("CLI TLS build lookup failed: %s", output)
		}
		result := readRemoteJSONMap(t, client, "/tmp/dibra-tls-build.json")
		if result["failed"] == true || result["changed"] == true {
			t.Fatalf("CLI TLS lookup = %#v", result)
		}
		image, _ := result["image"].(map[string]any)
		if image["Id"] == nil {
			t.Fatalf("CLI TLS lookup missing image: %#v", result)
		}
	})

	t.Run("unreachable TCP host cannot talk to docker", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Unreachable TCP daemon
    docker_host_info:
      docker_host: tcp://127.0.0.1:80
`)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("expected failure for tcp://127.0.0.1:80: %s", output)
		}
	})
}

func runTLSHostInfo(t *testing.T, client *ssh.Client, suffix, arguments string) map[string]any {
	t.Helper()
	remotePath := "/tmp/dibra-tls-host-info-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "tls_host_info_result")
	playbook := playbookHeader + `
  - name: Inspect docker host over TLS fixture
    community.docker.docker_host_info:
` + arguments + `
    register: tls_host_info_result

  - name: Persist TLS host info
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s TLS host info failed: %s", suffix, output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}

func runDockerAgentRequestWithEnvironment(
	t *testing.T,
	client *ssh.Client,
	module string,
	args map[string]any,
	environment string,
) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"module": module, "args": args, "check_mode": false, "diff": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := "printf '%s' '" + string(request) + "' | "
	if environment != "" {
		command += "env " + environment + " "
	}
	command += "/tmp/.dibra-agent"
	stdout, stderr, err := client.Run(command)
	if err != nil {
		t.Fatalf("%s agent invocation failed: %v\nstdout: %s\nstderr: %s", module, err, stdout, stderr)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
		t.Fatalf("decode %s response %q: %v", module, stdout, err)
	}
	return response
}

func assertSameDaemon(t *testing.T, unixResult, other map[string]any) {
	t.Helper()
	if other["can_talk_to_docker"] != true {
		t.Fatalf("can_talk_to_docker = %#v", other)
	}
	unixInfo := sanitizeHostInfo(t, unixResult["host_info"])
	otherInfo := sanitizeHostInfo(t, other["host_info"])
	if unixInfo["ID"] != otherInfo["ID"] || unixInfo["ServerVersion"] != otherInfo["ServerVersion"] {
		t.Fatalf("daemon identity mismatch unix=%v other=%v", unixInfo["ID"], otherInfo["ID"])
	}
	if !reflect.DeepEqual(unixInfo, otherInfo) {
		t.Fatalf("sanitized host_info mismatch\nunix: %#v\nother: %#v", unixInfo, otherInfo)
	}
}

func sanitizeHostInfo(t *testing.T, raw any) map[string]any {
	t.Helper()
	info, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("host_info = %T, want object", raw)
	}
	sanitized := make(map[string]any, len(info))
	for key, value := range info {
		switch key {
		case "SystemTime", "NFd", "NGoroutines", "NEventsListener",
			"Containers", "ContainersPaused", "ContainersRunning", "ContainersStopped", "Images":
			continue
		default:
			sanitized[key] = value
		}
	}
	return sanitized
}

func startTLSFixture(t *testing.T, client *ssh.Client) {
	t.Helper()
	stopTLSFixture(t, client)
	material := generateTLSMaterial(t)
	remoteExec(t, client, "rm -rf "+tlsFixtureDir+" && mkdir -p "+tlsFixtureDir+"/certs "+tlsFixtureDir+"/build "+tlsFixtureDir+"/logs")
	writeRemoteFile(t, client, tlsCAPath, material.caPEM)
	writeRemoteFile(t, client, tlsClientCertPath, material.clientCertPEM)
	writeRemoteFile(t, client, tlsClientKeyPath, material.clientKeyPEM)
	writeRemoteFile(t, client, tlsServerCertPath, material.serverCertPEM)
	writeRemoteFile(t, client, tlsServerKeyPath, material.serverKeyPEM)
	writeRemoteFile(t, client, tlsFixtureDir+"/nginx.conf", tlsNginxConfig())

	start := remoteExec(t, client, "/usr/sbin/nginx -p "+tlsFixtureDir+" -c "+tlsFixtureDir+"/nginx.conf && echo started")
	if start != "started" {
		t.Fatalf("nginx did not start: %s\n%s", start, remoteExec(t, client, "cat "+tlsFixtureDir+"/logs/error.log 2>/dev/null || true"))
	}

	ready := remoteExec(t, client, "for i in $(seq 1 20); do curl -sf "+strings.Replace(tlsHTTPHost, "tcp://", "http://", 1)+"/version >/dev/null && echo ready && break; sleep 0.2; done")
	if ready != "ready" {
		t.Fatalf("HTTP frontend never became ready: %s\n%s", ready, remoteExec(t, client, "cat "+tlsFixtureDir+"/logs/error.log 2>/dev/null || true"))
	}
	httpsReady := remoteExec(t, client, "for i in $(seq 1 20); do curl -skf "+strings.Replace(tlsHTTPSHost, "tcp://", "https://", 1)+"/version >/dev/null && echo ready && break; sleep 0.2; done")
	if httpsReady != "ready" {
		t.Fatalf("HTTPS frontend never became ready: %s\n%s", httpsReady, remoteExec(t, client, "cat "+tlsFixtureDir+"/logs/error.log 2>/dev/null || true"))
	}
}

func stopTLSFixture(t *testing.T, client *ssh.Client) {
	t.Helper()
	_, _, _ = client.Run("if [ -f " + tlsFixtureDir + "/nginx.pid ]; then /usr/sbin/nginx -p " + tlsFixtureDir + " -c " + tlsFixtureDir + "/nginx.conf -s stop >/dev/null 2>&1; fi")
	_, _, _ = client.Run("if [ -f " + tlsFixtureDir + "/nginx.pid ]; then kill \"$(cat " + tlsFixtureDir + "/nginx.pid)\" >/dev/null 2>&1 || true; rm -f " + tlsFixtureDir + "/nginx.pid; fi")
}

func tlsNginxConfig() string {
	return `user root;
worker_processes 1;
error_log ` + tlsFixtureDir + `/logs/error.log info;
pid ` + tlsFixtureDir + `/nginx.pid;
daemon on;

events {
    worker_connections 32;
}

http {
    default_type application/octet-stream;
    access_log ` + tlsFixtureDir + `/logs/access.log;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        '' close;
    }

    upstream docker_engine {
        server unix:/var/run/docker.sock;
    }

    server {
        listen 127.0.0.1:2376 ssl;
        server_name ` + tlsServerName + `;
        ssl_certificate ` + tlsServerCertPath + `;
        ssl_certificate_key ` + tlsServerKeyPath + `;
        ssl_protocols TLSv1.2 TLSv1.3;

        location / {
            proxy_pass http://docker_engine;
            proxy_http_version 1.1;
            proxy_set_header Host $http_host;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_buffering off;
            client_max_body_size 0;
            chunked_transfer_encoding on;
        }
    }

    server {
        listen 127.0.0.1:2375;
        location / {
            proxy_pass http://docker_engine;
            proxy_http_version 1.1;
            proxy_set_header Host $http_host;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_buffering off;
            client_max_body_size 0;
            chunked_transfer_encoding on;
        }
    }
}
`
}

type tlsMaterial struct {
	caPEM         string
	serverCertPEM string
	serverKeyPEM  string
	clientCertPEM string
	clientKeyPEM  string
}

func generateTLSMaterial(t *testing.T) tlsMaterial {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Dibra Docker TLS test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: tlsServerName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{tlsServerName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "dibra-docker-tls-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	return tlsMaterial{
		caPEM:         encodeCertPEM(caDER),
		serverCertPEM: encodeCertPEM(serverDER),
		serverKeyPEM:  encodeKeyPEM(t, serverKey),
		clientCertPEM: encodeCertPEM(clientDER),
		clientKeyPEM:  encodeKeyPEM(t, clientKey),
	}
}

func encodeCertPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func encodeKeyPEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
