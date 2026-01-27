package group

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

func Execute(req Request) Response {
	if req.Name == "" {
		return Response{Failed: true, Msg: "name is required"}
	}

	if req.State == "" {
		req.State = "present"
	}

	if req.State != "present" && req.State != "absent" {
		return Response{Failed: true, Msg: "state must be 'present' or 'absent'"}
	}

	if req.NonUnique && req.GID == nil {
		return Response{Failed: true, Msg: "non_unique requires gid to be set"}
	}

	if req.Force && req.Local {
		return Response{Failed: true, Msg: "force and local are mutually exclusive"}
	}

	groupInfo, exists := getGroupInfo(req.Name, req.Local)

	switch req.State {
	case "absent":
		return removeGroup(req, groupInfo, exists)
	case "present":
		if exists {
			return modifyGroup(req, groupInfo)
		}
		return createGroup(req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

type groupInfoData struct {
	name   string
	gid    int
	system bool
}

func getGroupInfo(groupName string, local bool) (*groupInfoData, bool) {
	if local {
		return getLocalGroupInfo(groupName)
	}

	g, err := user.LookupGroup(groupName)
	if err != nil {
		return nil, false
	}

	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return nil, false
	}

	info := &groupInfoData{
		name:   g.Name,
		gid:    gid,
		system: gid < 1000,
	}

	return info, true
}

func getLocalGroupInfo(groupName string) (*groupInfoData, bool) {
	file, err := os.Open("/etc/group")
	if err != nil {
		return nil, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[0] == groupName {
			gid, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, false
			}
			return &groupInfoData{
				name:   parts[0],
				gid:    gid,
				system: gid < 1000,
			}, true
		}
	}

	return nil, false
}

func gidExists(gid int) bool {
	file, err := os.Open("/etc/group")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			existingGID, err := strconv.Atoi(parts[2])
			if err == nil && existingGID == gid {
				return true
			}
		}
	}

	return false
}

func createGroup(req Request) Response {
	cmd := "groupadd"
	if req.Local {
		cmd = "lgroupadd"
	}

	args := []string{}

	if req.GID != nil {
		if req.Local && gidExists(*req.GID) {
			return Response{Failed: true, Msg: fmt.Sprintf("gid %d already exists", *req.GID)}
		}
		args = append(args, "-g", strconv.Itoa(*req.GID))
		if req.NonUnique {
			args = append(args, "-o")
		}
	}

	if req.System {
		args = append(args, "-r")
	}

	args = append(args, req.Name)

	execCmd := exec.Command(cmd, args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("%s failed: %v: %s", cmd, err, string(output))}
	}

	groupInfo, _ := getGroupInfo(req.Name, req.Local)

	resp := Response{
		Changed: true,
		Name:    req.Name,
		State:   "present",
		System:  req.System,
	}

	if groupInfo != nil {
		resp.GID = groupInfo.gid
		resp.System = groupInfo.system
	}

	return resp
}

func modifyGroup(req Request, groupInfo *groupInfoData) Response {
	if req.GID == nil || *req.GID == groupInfo.gid {
		return Response{
			Changed: false,
			Name:    req.Name,
			GID:     groupInfo.gid,
			State:   "present",
			System:  groupInfo.system,
		}
	}

	cmd := "groupmod"
	if req.Local {
		cmd = "lgroupmod"
	}

	args := []string{"-g", strconv.Itoa(*req.GID)}

	if req.NonUnique {
		args = append(args, "-o")
	}

	args = append(args, req.Name)

	execCmd := exec.Command(cmd, args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("%s failed: %v: %s", cmd, err, string(output))}
	}

	groupInfo, _ = getGroupInfo(req.Name, req.Local)

	resp := Response{
		Changed: true,
		Name:    req.Name,
		State:   "present",
	}

	if groupInfo != nil {
		resp.GID = groupInfo.gid
		resp.System = groupInfo.system
	}

	return resp
}

func removeGroup(req Request, groupInfo *groupInfoData, exists bool) Response {
	if !exists {
		return Response{Changed: false, Name: req.Name, State: "absent", Msg: "group already absent"}
	}

	cmd := "groupdel"
	if req.Local {
		cmd = "lgroupdel"
	}

	args := []string{}

	if req.Force {
		args = append(args, "-f")
	}

	args = append(args, req.Name)

	execCmd := exec.Command(cmd, args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("%s failed: %v: %s", cmd, err, string(output))}
	}

	return Response{
		Changed: true,
		Name:    req.Name,
		State:   "absent",
		Msg:     "group removed",
	}
}
