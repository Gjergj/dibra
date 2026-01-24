package user

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

	if req.UpdatePassword == "" {
		req.UpdatePassword = "always"
	}

	if req.UpdatePassword != "always" && req.UpdatePassword != "on_create" {
		return Response{Failed: true, Msg: "update_password must be 'always' or 'on_create'"}
	}

	if req.SSHKeyType == "" {
		req.SSHKeyType = "rsa"
	}

	userInfo, exists := getUserInfo(req.Name)

	switch req.State {
	case "absent":
		return removeUser(req, userInfo, exists)
	case "present":
		if exists {
			return modifyUser(req, userInfo)
		}
		return createUser(req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

type userInfoData struct {
	name    string
	uid     int
	gid     int
	comment string
	home    string
	shell   string
	groups  []string
}

func getUserInfo(username string) (*userInfoData, bool) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, false
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	info := &userInfoData{
		name:    u.Username,
		uid:     uid,
		gid:     gid,
		comment: u.Name,
		home:    u.HomeDir,
		groups:  getSupplementaryGroups(username),
	}

	info.shell = getUserShell(username)

	return info, true
}

func getUserShell(username string) string {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) >= 7 && parts[0] == username {
			return parts[6]
		}
	}
	return ""
}

func getSupplementaryGroups(username string) []string {
	output, err := exec.Command("id", "-Gn", username).Output()
	if err != nil {
		return nil
	}

	allGroups := strings.Fields(strings.TrimSpace(string(output)))
	if len(allGroups) <= 1 {
		return nil
	}
	return allGroups[1:]
}

func getPrimaryGroupName(gid int) string {
	g, err := user.LookupGroupId(strconv.Itoa(gid))
	if err != nil {
		return ""
	}
	return g.Name
}

func createUser(req Request) Response {
	args := []string{}

	if req.UID != nil {
		args = append(args, "-u", strconv.Itoa(*req.UID))
		if req.NonUnique {
			args = append(args, "-o")
		}
	}

	if req.Group != "" {
		args = append(args, "-g", req.Group)
	}

	if len(req.Groups) > 0 {
		args = append(args, "-G", strings.Join(req.Groups, ","))
	}

	if req.Shell != "" {
		args = append(args, "-s", req.Shell)
	}

	if req.Home != "" {
		args = append(args, "-d", req.Home)
	}

	createHome := true
	if req.CreateHome != nil {
		createHome = *req.CreateHome
	}

	if createHome {
		args = append(args, "-m")
		if req.Skeleton != "" {
			args = append(args, "-k", req.Skeleton)
		}
	} else {
		args = append(args, "-M")
	}

	if req.System {
		args = append(args, "-r")
	}

	if req.Comment != "" {
		args = append(args, "-c", req.Comment)
	}

	if req.Expires != nil {
		if *req.Expires < 0 {
			args = append(args, "-e", "")
		} else {
			expireDate := time.Unix(int64(*req.Expires), 0).Format("2006-01-02")
			args = append(args, "-e", expireDate)
		}
	}

	args = append(args, req.Name)

	cmd := exec.Command("useradd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("useradd failed: %v: %s", err, string(output))}
	}

	changed := true

	if req.Password != "" {
		if err := setPassword(req.Name, req.Password); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set password: %v", err)}
		}
	}

	if req.PasswordLock != nil {
		if err := setPasswordLock(req.Name, *req.PasswordLock); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to lock/unlock password: %v", err)}
		}
	}

	sshResp := Response{}
	if req.GenerateSSHKey {
		sshResp, err = generateSSHKey(req)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to generate SSH key: %v", err)}
		}
	}

	userInfo, _ := getUserInfo(req.Name)

	resp := Response{
		Changed:  changed,
		Name:     req.Name,
		State:    "present",
		System:   req.System,
		CreateHome: createHome,
	}

	if userInfo != nil {
		resp.UID = userInfo.uid
		resp.GID = userInfo.gid
		resp.Home = userInfo.home
		resp.Shell = userInfo.shell
		resp.Comment = userInfo.comment
		resp.Group = getPrimaryGroupName(userInfo.gid)
		if len(userInfo.groups) > 0 {
			resp.Groups = strings.Join(userInfo.groups, ",")
		}
	}

	if sshResp.SSHKeyFile != "" {
		resp.SSHKeyFile = sshResp.SSHKeyFile
		resp.SSHPublicKey = sshResp.SSHPublicKey
		resp.SSHFingerprint = sshResp.SSHFingerprint
	}

	return resp
}

func modifyUser(req Request, userInfo *userInfoData) Response {
	changed := false
	args := []string{}

	if req.UID != nil && *req.UID != userInfo.uid {
		args = append(args, "-u", strconv.Itoa(*req.UID))
		if req.NonUnique {
			args = append(args, "-o")
		}
	}

	if req.Group != "" {
		targetGID, err := getGroupGID(req.Group)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("group not found: %s", req.Group)}
		}
		if targetGID != userInfo.gid {
			args = append(args, "-g", req.Group)
		}
	}

	if len(req.Groups) > 0 || (!req.Append && len(req.Groups) == 0) {
		currentGroups := userInfo.groups
		targetGroups := req.Groups

		needsChange := false
		if req.Append {
			for _, g := range targetGroups {
				if !contains(currentGroups, g) {
					needsChange = true
					break
				}
			}
		} else {
			if len(targetGroups) != len(currentGroups) {
				needsChange = true
			} else {
				for _, g := range targetGroups {
					if !contains(currentGroups, g) {
						needsChange = true
						break
					}
				}
				for _, g := range currentGroups {
					if !contains(targetGroups, g) {
						needsChange = true
						break
					}
				}
			}
		}

		if needsChange {
			if req.Append {
				args = append(args, "-a", "-G", strings.Join(targetGroups, ","))
			} else {
				args = append(args, "-G", strings.Join(targetGroups, ","))
			}
		}
	}

	if req.Shell != "" && req.Shell != userInfo.shell {
		args = append(args, "-s", req.Shell)
	}

	if req.Home != "" && req.Home != userInfo.home {
		args = append(args, "-d", req.Home)
		if req.MoveHome {
			args = append(args, "-m")
		}
	}

	if req.Comment != "" && req.Comment != userInfo.comment {
		args = append(args, "-c", req.Comment)
	}

	if req.Expires != nil {
		if *req.Expires < 0 {
			args = append(args, "-e", "")
		} else {
			expireDate := time.Unix(int64(*req.Expires), 0).Format("2006-01-02")
			args = append(args, "-e", expireDate)
		}
	}

	if len(args) > 0 {
		args = append(args, req.Name)
		cmd := exec.Command("usermod", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("usermod failed: %v: %s", err, string(output))}
		}
		changed = true
	}

	if req.Password != "" && req.UpdatePassword == "always" {
		currentHash := getPasswordHash(req.Name)
		if currentHash != req.Password {
			if err := setPassword(req.Name, req.Password); err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to set password: %v", err)}
			}
			changed = true
		}
	}

	if req.PasswordLock != nil {
		isLocked := isPasswordLocked(req.Name)
		if *req.PasswordLock != isLocked {
			if err := setPasswordLock(req.Name, *req.PasswordLock); err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to lock/unlock password: %v", err)}
			}
			changed = true
		}
	}

	sshResp := Response{}
	if req.GenerateSSHKey {
		var err error
		sshResp, err = generateSSHKey(req)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to generate SSH key: %v", err)}
		}
		if sshResp.Changed {
			changed = true
		}
	}

	userInfo, _ = getUserInfo(req.Name)

	resp := Response{
		Changed: changed,
		Name:    req.Name,
		State:   "present",
	}

	if userInfo != nil {
		resp.UID = userInfo.uid
		resp.GID = userInfo.gid
		resp.Home = userInfo.home
		resp.Shell = userInfo.shell
		resp.Comment = userInfo.comment
		resp.Group = getPrimaryGroupName(userInfo.gid)
		if len(userInfo.groups) > 0 {
			resp.Groups = strings.Join(userInfo.groups, ",")
		}
	}

	if sshResp.SSHKeyFile != "" {
		resp.SSHKeyFile = sshResp.SSHKeyFile
		resp.SSHPublicKey = sshResp.SSHPublicKey
		resp.SSHFingerprint = sshResp.SSHFingerprint
	}

	return resp
}

func removeUser(req Request, userInfo *userInfoData, exists bool) Response {
	if !exists {
		return Response{Changed: false, Name: req.Name, State: "absent", Msg: "user already absent"}
	}

	args := []string{}

	if req.Remove {
		args = append(args, "-r")
	}

	if req.Force {
		args = append(args, "-f")
	}

	args = append(args, req.Name)

	cmd := exec.Command("userdel", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("userdel failed: %v: %s", err, string(output))}
	}

	return Response{
		Changed: true,
		Name:    req.Name,
		State:   "absent",
		Msg:     "user removed",
	}
}

func setPassword(username, hash string) error {
	cmd := exec.Command("chpasswd", "-e")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s\n", username, hash))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

func getPasswordHash(username string) string {
	output, err := exec.Command("getent", "shadow", username).Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(string(output), ":")
	if len(parts) >= 2 {
		return strings.TrimPrefix(parts[1], "!")
	}
	return ""
}

func isPasswordLocked(username string) bool {
	output, err := exec.Command("passwd", "-S", username).Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(output))
	if len(fields) >= 2 {
		return fields[1] == "L" || fields[1] == "LK"
	}
	return false
}

func setPasswordLock(username string, lock bool) error {
	var cmd *exec.Cmd
	if lock {
		cmd = exec.Command("usermod", "-L", username)
	} else {
		cmd = exec.Command("usermod", "-U", username)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

func getGroupGID(groupName string) (int, error) {
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func generateSSHKey(req Request) (Response, error) {
	userInfo, exists := getUserInfo(req.Name)
	if !exists {
		return Response{}, fmt.Errorf("user does not exist: %s", req.Name)
	}

	keyFile := req.SSHKeyFile
	if keyFile == "" {
		keyFile = ".ssh/id_rsa"
	}

	if !filepath.IsAbs(keyFile) {
		keyFile = filepath.Join(userInfo.home, keyFile)
	}

	if _, err := os.Stat(keyFile); err == nil {
		if !req.Force {
			pubKeyData, _ := os.ReadFile(keyFile + ".pub")
			fingerprint := getSSHFingerprint(keyFile + ".pub")
			return Response{
				Changed:        false,
				SSHKeyFile:     keyFile,
				SSHPublicKey:   strings.TrimSpace(string(pubKeyData)),
				SSHFingerprint: fingerprint,
			}, nil
		}
		os.Remove(keyFile)
		os.Remove(keyFile + ".pub")
	}

	sshDir := filepath.Dir(keyFile)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return Response{}, fmt.Errorf("failed to create .ssh directory: %w", err)
	}

	if err := os.Chown(sshDir, userInfo.uid, userInfo.gid); err != nil {
		return Response{}, fmt.Errorf("failed to chown .ssh directory: %w", err)
	}

	comment := req.SSHKeyComment
	if comment == "" {
		hostname, _ := os.Hostname()
		comment = fmt.Sprintf("ansible-generated on %s", hostname)
	}

	args := []string{"-t", req.SSHKeyType, "-f", keyFile, "-C", comment}

	if req.SSHKeyBits > 0 {
		args = append(args, "-b", strconv.Itoa(req.SSHKeyBits))
	}

	if req.SSHKeyPassphrase == "" {
		args = append(args, "-N", "")
	} else {
		args = append(args, "-N", req.SSHKeyPassphrase)
	}

	cmd := exec.Command("ssh-keygen", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{}, fmt.Errorf("ssh-keygen failed: %v: %s", err, string(output))
	}

	if err := os.Chown(keyFile, userInfo.uid, userInfo.gid); err != nil {
		return Response{}, fmt.Errorf("failed to chown private key: %w", err)
	}
	if err := os.Chmod(keyFile, 0600); err != nil {
		return Response{}, fmt.Errorf("failed to chmod private key: %w", err)
	}
	if err := os.Chown(keyFile+".pub", userInfo.uid, userInfo.gid); err != nil {
		return Response{}, fmt.Errorf("failed to chown public key: %w", err)
	}

	pubKeyData, _ := os.ReadFile(keyFile + ".pub")
	fingerprint := getSSHFingerprint(keyFile + ".pub")

	return Response{
		Changed:        true,
		SSHKeyFile:     keyFile,
		SSHPublicKey:   strings.TrimSpace(string(pubKeyData)),
		SSHFingerprint: fingerprint,
	}, nil
}

func getSSHFingerprint(pubKeyFile string) string {
	output, err := exec.Command("ssh-keygen", "-lf", pubKeyFile).Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) >= 2 {
		return fields[1]
	}
	return ""
}
