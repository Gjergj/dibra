package cron

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const ansibleMarker = "#Ansible: "

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

	if req.State == "present" && req.Job == "" && !req.Env {
		return Response{Failed: true, Msg: "job is required when state=present"}
	}

	if req.CronFile != "" && req.User == "" && req.State == "present" {
		return Response{Failed: true, Msg: "user is required when using cron_file with state=present"}
	}

	if req.SpecialTime != "" {
		validSpecialTimes := map[string]bool{
			"reboot": true, "yearly": true, "annually": true,
			"monthly": true, "weekly": true, "daily": true, "hourly": true,
		}
		if !validSpecialTimes[req.SpecialTime] {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid special_time: %s", req.SpecialTime)}
		}
	}

	if req.SpecialTime != "" && (req.Minute != "" || req.Hour != "" || req.Day != "" || req.Month != "" || req.Weekday != "") {
		if req.Minute != "*" || req.Hour != "*" || req.Day != "*" || req.Month != "*" || req.Weekday != "*" {
			return Response{Failed: true, Msg: "special_time cannot be combined with minute, hour, day, month, or weekday"}
		}
	}

	if (req.InsertAfter != "" || req.InsertBefore != "") && !req.Env {
		return Response{Failed: true, Msg: "insertafter and insertbefore are only valid with env=true"}
	}

	if req.InsertAfter != "" && req.InsertBefore != "" {
		return Response{Failed: true, Msg: "insertafter and insertbefore are mutually exclusive"}
	}

	ct := &cronTab{
		user:     req.User,
		cronFile: req.CronFile,
	}

	if err := ct.read(); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read crontab: %v", err)}
	}

	var backupFile string
	if req.Backup {
		backupFile = fmt.Sprintf("/tmp/crontab-backup-%d", time.Now().UnixNano())
		if err := ct.write(backupFile); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	changed := false

	if req.Env {
		if strings.Contains(req.Name, " ") {
			return Response{Failed: true, Msg: "invalid name for environment variable (contains spaces)"}
		}

		decl := fmt.Sprintf("%s=\"%s\"", req.Name, req.Job)
		oldDecl := ct.findEnv(req.Name)

		if req.State == "present" {
			if oldDecl == "" {
				ct.addEnv(decl, req.InsertAfter, req.InsertBefore)
				changed = true
			} else if oldDecl != decl {
				ct.updateEnv(req.Name, decl)
				changed = true
			}
		} else {
			if oldDecl != "" {
				ct.removeEnv(req.Name)
				changed = true
			}
		}
	} else {
		job := ct.getCronJob(req)

		if req.State == "present" {
			oldJob := ct.findJob(req.Name)
			if oldJob == "" {
				ct.addJob(req.Name, job)
				changed = true
			} else if oldJob != job {
				ct.updateJob(req.Name, job)
				changed = true
			}
		} else {
			oldJob := ct.findJob(req.Name)
			if oldJob != "" {
				ct.removeJob(req.Name)
				changed = true
			}
		}
	}

	if ct.cronFile != "" && ct.isEmpty() && req.State == "absent" {
		if err := ct.removeFile(); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove cron file: %v", err)}
		}
		if backupFile != "" && !changed {
			os.Remove(backupFile)
		}
		return Response{
			Changed:  true,
			CronFile: ct.cronFile,
			Msg:      "cron file removed (empty)",
		}
	}

	if changed {
		if err := ct.write(""); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write crontab: %v", err)}
		}
	}

	if backupFile != "" && !changed {
		os.Remove(backupFile)
		backupFile = ""
	}

	resp := Response{
		Changed:    changed,
		Jobs:       ct.getJobNames(),
		Envs:       ct.getEnvNames(),
		BackupFile: backupFile,
	}

	if ct.cronFile != "" {
		resp.CronFile = ct.cronFile
	}

	return resp
}

type cronTab struct {
	user     string
	cronFile string
	lines    []string
}

func (ct *cronTab) read() error {
	if ct.cronFile != "" {
		return ct.readFile()
	}
	return ct.readCrontab()
}

func (ct *cronTab) readFile() error {
	path := ct.cronFile
	if !filepath.IsAbs(path) {
		path = filepath.Join("/etc/cron.d", path)
	}
	ct.cronFile = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		ct.lines = []string{}
		return nil
	}
	if err != nil {
		return err
	}

	ct.lines = strings.Split(string(data), "\n")
	if len(ct.lines) > 0 && ct.lines[len(ct.lines)-1] == "" {
		ct.lines = ct.lines[:len(ct.lines)-1]
	}
	return nil
}

func (ct *cronTab) readCrontab() error {
	args := []string{"-l"}
	if ct.user != "" {
		currentUser, _ := user.Current()
		if currentUser == nil || currentUser.Username != ct.user {
			args = []string{"-u", ct.user, "-l"}
		}
	}

	cmd := exec.Command("crontab", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				ct.lines = []string{}
				return nil
			}
		}
		return fmt.Errorf("crontab -l failed: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	ct.lines = []string{}
	for i, line := range lines {
		if i < 3 && (strings.HasPrefix(line, "# DO NOT EDIT THIS FILE") ||
			strings.HasPrefix(line, "# (") ||
			strings.HasPrefix(line, "# m h")) {
			continue
		}
		if i == len(lines)-1 && line == "" {
			continue
		}
		ct.lines = append(ct.lines, line)
	}

	return nil
}

func (ct *cronTab) write(backupPath string) error {
	content := ct.render()

	if backupPath != "" {
		return os.WriteFile(backupPath, []byte(content), 0644)
	}

	if ct.cronFile != "" {
		dir := filepath.Dir(ct.cronFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		return os.WriteFile(ct.cronFile, []byte(content), 0644)
	}

	tmpFile, err := os.CreateTemp("", "crontab-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	args := []string{tmpFile.Name()}
	if ct.user != "" {
		currentUser, _ := user.Current()
		if currentUser == nil || currentUser.Username != ct.user {
			args = []string{"-u", ct.user, tmpFile.Name()}
		}
	}

	cmd := exec.Command("crontab", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab install failed: %w: %s", err, string(output))
	}

	return nil
}

func (ct *cronTab) render() string {
	if len(ct.lines) == 0 {
		return ""
	}
	result := strings.Join(ct.lines, "\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func (ct *cronTab) isEmpty() bool {
	for _, line := range ct.lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}

func (ct *cronTab) removeFile() error {
	if ct.cronFile == "" {
		return nil
	}
	return os.Remove(ct.cronFile)
}

func (ct *cronTab) getCronJob(req Request) string {
	var job string

	if req.SpecialTime != "" {
		job = fmt.Sprintf("@%s %s", req.SpecialTime, req.Job)
	} else {
		minute := req.Minute
		hour := req.Hour
		day := req.Day
		month := req.Month
		weekday := req.Weekday

		if minute == "" {
			minute = "*"
		}
		if hour == "" {
			hour = "*"
		}
		if day == "" {
			day = "*"
		}
		if month == "" {
			month = "*"
		}
		if weekday == "" {
			weekday = "*"
		}

		if ct.cronFile != "" && req.User != "" {
			job = fmt.Sprintf("%s %s %s %s %s %s %s", minute, hour, day, month, weekday, req.User, req.Job)
		} else {
			job = fmt.Sprintf("%s %s %s %s %s %s", minute, hour, day, month, weekday, req.Job)
		}
	}

	if req.Disabled {
		job = "#" + job
	}

	return job
}

func (ct *cronTab) findJob(name string) string {
	marker := ansibleMarker + name
	for i, line := range ct.lines {
		if line == marker && i+1 < len(ct.lines) {
			return ct.lines[i+1]
		}
	}
	return ""
}

func (ct *cronTab) addJob(name, job string) {
	ct.lines = append(ct.lines, ansibleMarker+name, job)
}

func (ct *cronTab) updateJob(name, job string) {
	marker := ansibleMarker + name
	for i, line := range ct.lines {
		if line == marker && i+1 < len(ct.lines) {
			ct.lines[i+1] = job
			return
		}
	}
}

func (ct *cronTab) removeJob(name string) {
	marker := ansibleMarker + name
	newLines := []string{}
	skip := false
	for _, line := range ct.lines {
		if line == marker {
			skip = true
			continue
		}
		if skip {
			skip = false
			continue
		}
		newLines = append(newLines, line)
	}
	ct.lines = newLines
}

func (ct *cronTab) findEnv(name string) string {
	pattern := regexp.MustCompile(fmt.Sprintf(`^%s\s*=`, regexp.QuoteMeta(name)))
	for _, line := range ct.lines {
		if pattern.MatchString(line) {
			return line
		}
	}
	return ""
}

func (ct *cronTab) addEnv(decl, insertAfter, insertBefore string) {
	if insertAfter == "" && insertBefore == "" {
		ct.lines = append([]string{decl}, ct.lines...)
		return
	}

	newLines := []string{}
	inserted := false

	for _, line := range ct.lines {
		if insertBefore != "" && strings.HasPrefix(line, insertBefore+"=") && !inserted {
			newLines = append(newLines, decl)
			inserted = true
		}
		newLines = append(newLines, line)
		if insertAfter != "" && strings.HasPrefix(line, insertAfter+"=") && !inserted {
			newLines = append(newLines, decl)
			inserted = true
		}
	}

	if !inserted {
		newLines = append([]string{decl}, ct.lines...)
	}

	ct.lines = newLines
}

func (ct *cronTab) updateEnv(name, decl string) {
	pattern := regexp.MustCompile(fmt.Sprintf(`^%s\s*=`, regexp.QuoteMeta(name)))
	for i, line := range ct.lines {
		if pattern.MatchString(line) {
			ct.lines[i] = decl
			return
		}
	}
}

func (ct *cronTab) removeEnv(name string) {
	pattern := regexp.MustCompile(fmt.Sprintf(`^%s\s*=`, regexp.QuoteMeta(name)))
	newLines := []string{}
	for _, line := range ct.lines {
		if !pattern.MatchString(line) {
			newLines = append(newLines, line)
		}
	}
	ct.lines = newLines
}

func (ct *cronTab) getJobNames() []string {
	names := []string{}
	scanner := bufio.NewScanner(strings.NewReader(strings.Join(ct.lines, "\n")))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ansibleMarker) {
			names = append(names, strings.TrimPrefix(line, ansibleMarker))
		}
	}
	return names
}

func (ct *cronTab) getEnvNames() []string {
	names := []string{}
	envPattern := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	for _, line := range ct.lines {
		if matches := envPattern.FindStringSubmatch(line); matches != nil {
			names = append(names, matches[1])
		}
	}
	return names
}
