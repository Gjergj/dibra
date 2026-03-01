package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gjergjiramku/dibra/internal/version"

	"github.com/gjergjiramku/dibra/internal/modules/apt"
	"github.com/gjergjiramku/dibra/internal/modules/apt_key"
	"github.com/gjergjiramku/dibra/internal/modules/apt_repository"
	"github.com/gjergjiramku/dibra/internal/modules/blockinfile"
	"github.com/gjergjiramku/dibra/internal/modules/command"
	"github.com/gjergjiramku/dibra/internal/modules/copy"
	"github.com/gjergjiramku/dibra/internal/modules/cron"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_run"
	"github.com/gjergjiramku/dibra/internal/modules/docker_config"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_copy_into"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_host_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_build"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_export"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_load"
	"github.com/gjergjiramku/dibra/internal/modules/docker_login"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_prune"
	"github.com/gjergjiramku/dibra/internal/modules/docker_secret"
	"github.com/gjergjiramku/dibra/internal/modules/docker_stack"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_service"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_service_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_volume"
	"github.com/gjergjiramku/dibra/internal/modules/docker_volume_info"
	"github.com/gjergjiramku/dibra/internal/modules/file"
	"github.com/gjergjiramku/dibra/internal/modules/find"
	"github.com/gjergjiramku/dibra/internal/modules/gather_facts"
	"github.com/gjergjiramku/dibra/internal/modules/git"
	"github.com/gjergjiramku/dibra/internal/modules/group"
	"github.com/gjergjiramku/dibra/internal/modules/iptables"
	"github.com/gjergjiramku/dibra/internal/modules/iptables_state"
	"github.com/gjergjiramku/dibra/internal/modules/lineinfile"
	"github.com/gjergjiramku/dibra/internal/modules/ping"
	"github.com/gjergjiramku/dibra/internal/modules/reboot"
	"github.com/gjergjiramku/dibra/internal/modules/replace"
	"github.com/gjergjiramku/dibra/internal/modules/script"
	"github.com/gjergjiramku/dibra/internal/modules/service"
	"github.com/gjergjiramku/dibra/internal/modules/service_facts"
	"github.com/gjergjiramku/dibra/internal/modules/shell"
	"github.com/gjergjiramku/dibra/internal/modules/slurp"
	"github.com/gjergjiramku/dibra/internal/modules/stat"
	"github.com/gjergjiramku/dibra/internal/modules/systemd_service"
	"github.com/gjergjiramku/dibra/internal/modules/tempfile"
	"github.com/gjergjiramku/dibra/internal/modules/template"
	"github.com/gjergjiramku/dibra/internal/modules/ufw"
	"github.com/gjergjiramku/dibra/internal/modules/unarchive"
	"github.com/gjergjiramku/dibra/internal/modules/uri"
	"github.com/gjergjiramku/dibra/internal/modules/user"
)

type ModuleRequest struct {
	Module string          `json:"module"`
	Args   json.RawMessage `json:"args"`
}

func main() {
	// Check for --version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("dibra-agent %s (commit: %s, built: %s)\n", version.Version, version.Commit, version.Date)
		os.Exit(0)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(fmt.Sprintf("failed to read stdin: %v", err))
		return
	}

	jsonStart := bytes.IndexByte(input, '{')
	if jsonStart == -1 {
		writeError("no JSON object found in input")
		return
	}
	input = input[jsonStart:]

	var modReq ModuleRequest
	if err := json.Unmarshal(input, &modReq); err != nil {
		writeError(fmt.Sprintf("failed to parse module request: %v", err))
		return
	}

	switch modReq.Module {
	case "apt":
		var req apt.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt request: %v", err))
			return
		}
		writeJSON(apt.Execute(req))

	case "apt_key":
		var req apt_key.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt_key request: %v", err))
			return
		}
		writeJSON(apt_key.Execute(req))

	case "apt_repository":
		var req apt_repository.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt_repository request: %v", err))
			return
		}
		writeJSON(apt_repository.Execute(req))

	case "file":
		var req file.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse file request: %v", err))
			return
		}
		writeJSON(file.Execute(req))

	case "copy":
		var req copy.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse copy request: %v", err))
			return
		}
		writeJSON(copy.Execute(req))

	case "template":
		var req template.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse template request: %v", err))
			return
		}
		writeJSON(template.Execute(req))

	case "stat":
		var req stat.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse stat request: %v", err))
			return
		}
		writeJSON(stat.Execute(req))

	case "uri":
		var req uri.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse uri request: %v", err))
			return
		}
		writeJSON(uri.Execute(req))

	case "cron":
		var req cron.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse cron request: %v", err))
			return
		}
		writeJSON(cron.Execute(req))

	case "ufw":
		var req ufw.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse ufw request: %v", err))
			return
		}
		writeJSON(ufw.Execute(req))

	case "user":
		var req user.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse user request: %v", err))
			return
		}
		writeJSON(user.Execute(req))

	case "group":
		var req group.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse group request: %v", err))
			return
		}
		writeJSON(group.Execute(req))

	case "systemd_service", "systemd":
		var req systemd_service.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse systemd_service request: %v", err))
			return
		}
		writeJSON(systemd_service.Execute(req))

	case "service":
		var req service.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse service request: %v", err))
			return
		}
		writeJSON(service.Execute(req))

	case "service_facts":
		var req service_facts.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse service_facts request: %v", err))
			return
		}
		writeJSON(service_facts.Execute(req))

	case "gather_facts":
		var req gather_facts.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse gather_facts request: %v", err))
			return
		}
		writeJSON(gather_facts.Execute(req))

	case "ping":
		var req ping.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse ping request: %v", err))
			return
		}
		writeJSON(ping.Execute(req))

	case "slurp":
		var req slurp.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse slurp request: %v", err))
			return
		}
		writeJSON(slurp.Execute(req))

	case "reboot":
		var req reboot.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse reboot request: %v", err))
			return
		}
		writeJSON(reboot.Execute(req))

	case "command":
		var req command.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse command request: %v", err))
			return
		}
		writeJSON(command.Execute(req))

	case "shell":
		var req shell.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse shell request: %v", err))
			return
		}
		writeJSON(shell.Execute(req))

	case "script":
		var req script.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse script request: %v", err))
			return
		}
		writeJSON(script.Execute(req))

	case "unarchive":
		var req unarchive.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse unarchive request: %v", err))
			return
		}
		writeJSON(unarchive.Execute(req))

	case "git":
		var req git.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse git request: %v", err))
			return
		}
		writeJSON(git.Execute(req))

	case "lineinfile":
		var req lineinfile.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse lineinfile request: %v", err))
			return
		}
		writeJSON(lineinfile.Execute(req))

	case "blockinfile":
		var req blockinfile.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse blockinfile request: %v", err))
			return
		}
		writeJSON(blockinfile.Execute(req))

	case "replace":
		var req replace.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse replace request: %v", err))
			return
		}
		writeJSON(replace.Execute(req))

	case "iptables":
		var req iptables.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse iptables request: %v", err))
			return
		}
		writeJSON(iptables.Execute(req))

	case "iptables_state":
		var req iptables_state.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse iptables_state request: %v", err))
			return
		}
		writeJSON(iptables_state.Execute(req))

	case "tempfile":
		var req tempfile.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse tempfile request: %v", err))
			return
		}
		writeJSON(tempfile.Execute(req))

	case "docker_container":
		var req docker_container.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_container request: %v", err))
			return
		}
		writeJSON(docker_container.Execute(req))

	case "docker_image":
		var req docker_image.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_image request: %v", err))
			return
		}
		writeJSON(docker_image.Execute(req))

	case "docker_network":
		var req docker_network.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_network request: %v", err))
			return
		}
		writeJSON(docker_network.Execute(req))

	case "docker_volume":
		var req docker_volume.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_volume request: %v", err))
			return
		}
		writeJSON(docker_volume.Execute(req))

	case "docker_prune":
		var req docker_prune.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_prune request: %v", err))
			return
		}
		writeJSON(docker_prune.Execute(req))

	case "docker_login":
		var req docker_login.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_login request: %v", err))
			return
		}
		writeJSON(docker_login.Execute(req))

	case "docker_swarm":
		var req docker_swarm.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_swarm request: %v", err))
			return
		}
		writeJSON(docker_swarm.Execute(req))

	case "docker_swarm_service":
		var req docker_swarm_service.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_swarm_service request: %v", err))
			return
		}
		writeJSON(docker_swarm_service.Execute(req))

	case "docker_node":
		var req docker_node.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_node request: %v", err))
			return
		}
		writeJSON(docker_node.Execute(req))

	case "docker_compose":
		var req docker_compose.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_compose request: %v", err))
			return
		}
		writeJSON(docker_compose.Execute(req))

	case "docker_compose_v2_run":
		var req docker_compose_v2_run.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_compose_v2_run request: %v", err))
			return
		}
		writeJSON(docker_compose_v2_run.Execute(req))

	case "docker_secret":
		var req docker_secret.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_secret request: %v", err))
			return
		}
		writeJSON(docker_secret.Execute(req))

	case "docker_config":
		var req docker_config.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_config request: %v", err))
			return
		}
		writeJSON(docker_config.Execute(req))

	case "docker_stack":
		var req docker_stack.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_stack request: %v", err))
			return
		}
		writeJSON(docker_stack.Execute(req))

	case "docker_container_exec":
		var req docker_container_exec.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_container_exec request: %v", err))
			return
		}
		writeJSON(docker_container_exec.Execute(req))

	case "docker_container_copy_into":
		var req docker_container_copy_into.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_container_copy_into request: %v", err))
			return
		}
		writeJSON(docker_container_copy_into.Execute(req))

	case "docker_image_build":
		var req docker_image_build.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_image_build request: %v", err))
			return
		}
		writeJSON(docker_image_build.Execute(req))

	case "docker_image_load":
		var req docker_image_load.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_image_load request: %v", err))
			return
		}
		writeJSON(docker_image_load.Execute(req))

	case "docker_image_export":
		var req docker_image_export.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_image_export request: %v", err))
			return
		}
		writeJSON(docker_image_export.Execute(req))

	case "docker_container_info":
		var req docker_container_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_container_info request: %v", err))
			return
		}
		writeJSON(docker_container_info.Execute(req))

	case "docker_image_info":
		var req docker_image_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_image_info request: %v", err))
			return
		}
		writeJSON(docker_image_info.Execute(req))

	case "docker_network_info":
		var req docker_network_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_network_info request: %v", err))
			return
		}
		writeJSON(docker_network_info.Execute(req))

	case "docker_volume_info":
		var req docker_volume_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_volume_info request: %v", err))
			return
		}
		writeJSON(docker_volume_info.Execute(req))

	case "docker_host_info":
		var req docker_host_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_host_info request: %v", err))
			return
		}
		writeJSON(docker_host_info.Execute(req))

	case "docker_swarm_info":
		var req docker_swarm_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_swarm_info request: %v", err))
			return
		}
		writeJSON(docker_swarm_info.Execute(req))

	case "docker_swarm_service_info":
		var req docker_swarm_service_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_swarm_service_info request: %v", err))
			return
		}
		writeJSON(docker_swarm_service_info.Execute(req))

	case "docker_node_info":
		var req docker_node_info.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse docker_node_info request: %v", err))
			return
		}
		writeJSON(docker_node_info.Execute(req))

	case "find":
		var req find.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse find request: %v", err))
			return
		}
		writeJSON(find.Execute(req))

	default:
		writeError(fmt.Sprintf("unknown module: %s", modReq.Module))
	}
}

func writeError(msg string) {
	resp := map[string]interface{}{
		"failed": true,
		"msg":    msg,
	}
	writeJSON(resp)
}

func writeJSON(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
