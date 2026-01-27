package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gjergjiramku/goansible/internal/builder"
	"github.com/gjergjiramku/goansible/internal/config"
	"github.com/gjergjiramku/goansible/internal/ssh"
)

const (
	remoteAgentDir  = "/tmp"
	remoteAgentName = ".goansible-agent"
)

type ModuleRequest struct {
	Module string      `json:"module"`
	Args   interface{} `json:"args"`
}

type GenericResponse struct {
	Changed  bool     `json:"changed"`
	Failed   bool     `json:"failed"`
	Msg      string   `json:"msg,omitempty"`
	RC       int      `json:"rc"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Packages []string `json:"packages,omitempty"`
	KeyID    string   `json:"key_id,omitempty"`
	Filename string   `json:"filename,omitempty"`
}

func main() {
	configPath := flag.String("config", "playbook.yaml", "Path to playbook YAML file")
	forceUpload := flag.Bool("force-agent-upload", false, "Force upload of agent binary")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("Failed to load config: %v", err)
	}

	projectRoot := findProjectRoot()
	b := builder.New(projectRoot)

	fmt.Println("Building agent...")
	agentBinary, err := b.Build()
	if err != nil {
		fatal("Failed to build agent: %v", err)
	}
	fmt.Printf("Agent built: %s\n", agentBinary)

	for _, host := range cfg.Hosts {
		fmt.Printf("\n=== Host: %s (%s) ===\n", host.Name, host.Host)

		client, err := ssh.Connect(ssh.Config{
			Host:           host.Host,
			Port:           host.Port,
			User:           host.User,
			Password:       host.Password,
			SSHKeyPath:     host.SSHKeyPath,
			Become:         host.Become,
			BecomePassword: host.BecomePassword,
		})
		if err != nil {
			fmt.Printf("  ✗ Failed to connect: %v\n", err)
			continue
		}
		defer client.Close()

		remoteAgentPath := filepath.Join(remoteAgentDir, remoteAgentName)

		exists, _ := client.FileExists(remoteAgentPath)
		if !exists || *forceUpload {
			fmt.Println("  Uploading agent...")
			if err := client.UploadFile(agentBinary, remoteAgentPath); err != nil {
				fmt.Printf("  ✗ Failed to upload agent: %v\n", err)
				continue
			}
			_, _, err := client.RunWithSudo(fmt.Sprintf("chmod +x %s", remoteAgentPath))
			if err != nil {
				fmt.Printf("  ✗ Failed to chmod agent: %v\n", err)
				continue
			}
		} else {
			fmt.Println("  Agent already present (use --force-agent-upload to update)")
		}

		for _, task := range cfg.Tasks {
			fmt.Printf("  Task: %s\n", task.Name)

			var modReq ModuleRequest

			switch {
			case task.Apt != nil:
				modReq = ModuleRequest{
					Module: "apt",
					Args: map[string]interface{}{
						"packages":         task.Apt.GetPackages(),
						"state":            task.Apt.State,
						"update_cache":     task.Apt.UpdateCache,
						"cache_valid_time": task.Apt.CacheValidTime,
						"purge":            task.Apt.Purge,
						"autoremove":       task.Apt.Autoremove,
						"upgrade":          task.Apt.Upgrade,
					},
				}

			case task.AptKey != nil:
				modReq = ModuleRequest{
					Module: "apt_key",
					Args: map[string]interface{}{
						"url":     task.AptKey.URL,
						"data":    task.AptKey.Data,
						"file":    task.AptKey.File,
						"keyring": task.AptKey.Keyring,
						"id":      task.AptKey.ID,
						"state":   task.AptKey.State,
					},
				}

			case task.AptRepository != nil:
				modReq = ModuleRequest{
					Module: "apt_repository",
					Args: map[string]interface{}{
						"repo":         task.AptRepository.Repo,
						"state":        task.AptRepository.State,
						"filename":     task.AptRepository.Filename,
						"update_cache": task.AptRepository.UpdateCache,
					},
				}

			case task.File != nil:
				modReq = ModuleRequest{
					Module: "file",
					Args: map[string]interface{}{
						"path":    task.File.Path,
						"state":   task.File.State,
						"mode":    task.File.Mode,
						"owner":   task.File.Owner,
						"group":   task.File.Group,
						"src":     task.File.Src,
						"recurse": task.File.Recurse,
						"force":   task.File.Force,
						"follow":  task.File.Follow,
					},
				}

			case task.Fetch != nil:
				resp := executeFetch(client, remoteAgentPath, host.Name, task.Fetch, *verbose)
				printResponse(resp, *verbose)
				continue

			case task.URI != nil:
				modReq = ModuleRequest{
					Module: "uri",
					Args: map[string]interface{}{
						"url":              task.URI.URL,
						"method":           task.URI.Method,
						"body":             task.URI.Body,
						"body_format":      task.URI.BodyFormat,
						"headers":          task.URI.Headers,
						"status_code":      task.URI.StatusCode,
						"timeout":          task.URI.Timeout,
						"return_content":   task.URI.ReturnContent,
						"dest":             task.URI.Dest,
						"creates":          task.URI.Creates,
						"url_username":     task.URI.URLUsername,
						"url_password":     task.URI.URLPassword,
						"force_basic_auth": task.URI.ForceBasicAuth,
						"follow_redirects": task.URI.FollowRedirects,
						"validate_certs":   task.URI.ValidateCerts,
					},
				}

			case task.Cron != nil:
				modReq = ModuleRequest{
					Module: "cron",
					Args: map[string]interface{}{
						"name":         task.Cron.Name,
						"user":         task.Cron.User,
						"job":          task.Cron.Job,
						"state":        task.Cron.State,
						"minute":       task.Cron.Minute,
						"hour":         task.Cron.Hour,
						"day":          task.Cron.Day,
						"month":        task.Cron.Month,
						"weekday":      task.Cron.Weekday,
						"special_time": task.Cron.SpecialTime,
						"disabled":     task.Cron.Disabled,
						"backup":       task.Cron.Backup,
						"cron_file":    task.Cron.CronFile,
						"env":          task.Cron.Env,
						"insertafter":  task.Cron.InsertAfter,
						"insertbefore": task.Cron.InsertBefore,
					},
				}

			case task.UFW != nil:
				modReq = ModuleRequest{
					Module: "ufw",
					Args: map[string]interface{}{
						"state":              task.UFW.State,
						"logging":            task.UFW.Logging,
						"default":            task.UFW.Default,
						"policy":             task.UFW.Policy,
						"direction":          task.UFW.Direction,
						"rule":               task.UFW.Rule,
						"delete":             task.UFW.Delete,
						"insert":             task.UFW.Insert,
						"insert_relative_to": task.UFW.InsertRelativeTo,
						"interface":          task.UFW.Interface,
						"if":                 task.UFW.If,
						"interface_in":       task.UFW.InterfaceIn,
						"if_in":              task.UFW.IfIn,
						"interface_out":      task.UFW.InterfaceOut,
						"if_out":             task.UFW.IfOut,
						"from_ip":            task.UFW.FromIP,
						"from":               task.UFW.From,
						"src":                task.UFW.Src,
						"from_port":          task.UFW.FromPort,
						"to_ip":              task.UFW.ToIP,
						"dest":               task.UFW.Dest,
						"to":                 task.UFW.To,
						"to_port":            task.UFW.ToPort,
						"port":               task.UFW.Port,
						"proto":              task.UFW.Proto,
						"protocol":           task.UFW.Protocol,
						"name":               task.UFW.Name,
						"app":                task.UFW.App,
						"route":              task.UFW.Route,
						"log":                task.UFW.Log,
						"comment":            task.UFW.Comment,
					},
				}

			case task.User != nil:
				modReq = ModuleRequest{
					Module: "user",
					Args: map[string]interface{}{
						"name":               task.User.Name,
						"state":              task.User.State,
						"uid":                task.User.UID,
						"group":              task.User.Group,
						"groups":             task.User.Groups,
						"append":             task.User.Append,
						"shell":              task.User.Shell,
						"home":               task.User.Home,
						"create_home":        task.User.CreateHome,
						"move_home":          task.User.MoveHome,
						"system":             task.User.System,
						"password":           task.User.Password,
						"password_lock":      task.User.PasswordLock,
						"update_password":    task.User.UpdatePassword,
						"comment":            task.User.Comment,
						"expires":            task.User.Expires,
						"remove":             task.User.Remove,
						"force":              task.User.Force,
						"skeleton":           task.User.Skeleton,
						"non_unique":         task.User.NonUnique,
						"generate_ssh_key":   task.User.GenerateSSHKey,
						"ssh_key_bits":       task.User.SSHKeyBits,
						"ssh_key_type":       task.User.SSHKeyType,
						"ssh_key_file":       task.User.SSHKeyFile,
						"ssh_key_comment":    task.User.SSHKeyComment,
						"ssh_key_passphrase": task.User.SSHKeyPassphrase,
					},
				}

			case task.Group != nil:
				modReq = ModuleRequest{
					Module: "group",
					Args: map[string]interface{}{
						"name":       task.Group.Name,
						"state":      task.Group.State,
						"gid":        task.Group.GID,
						"system":     task.Group.System,
						"local":      task.Group.Local,
						"non_unique": task.Group.NonUnique,
						"force":      task.Group.Force,
					},
				}

			case task.Copy != nil:
				copyArgs := map[string]interface{}{
					"dest":       task.Copy.Dest,
					"mode":       task.Copy.Mode,
					"owner":      task.Copy.Owner,
					"group":      task.Copy.Group,
					"backup":     task.Copy.Backup,
					"force":      task.Copy.Force,
					"remote_src": task.Copy.RemoteSrc,
				}

				if task.Copy.Content != "" {
					copyArgs["content"] = task.Copy.Content
				} else if task.Copy.RemoteSrc {
					copyArgs["src"] = task.Copy.Src
				} else if task.Copy.Src != "" {
					localChecksum, err := sha1File(task.Copy.Src)
					if err != nil {
						fmt.Printf("    ✗ Failed to compute local checksum: %v\n", err)
						continue
					}

					remoteTmpPath := fmt.Sprintf("/tmp/.goansible-copy-%s", localChecksum[:12])
					if err := client.UploadFile(task.Copy.Src, remoteTmpPath); err != nil {
						fmt.Printf("    ✗ Failed to upload file: %v\n", err)
						continue
					}

					copyArgs["src"] = remoteTmpPath
					copyArgs["checksum"] = localChecksum
				}

				modReq = ModuleRequest{
					Module: "copy",
					Args:   copyArgs,
				}

			case task.SystemdService != nil || task.Systemd != nil:
				params := task.SystemdService
				if params == nil {
					params = task.Systemd
				}
				modReq = ModuleRequest{
					Module: "systemd_service",
					Args: map[string]interface{}{
						"name":          params.Name,
						"state":         params.State,
						"enabled":       params.Enabled,
						"masked":        params.Masked,
						"daemon_reload": params.DaemonReload,
						"daemon_reexec": params.DaemonReexec,
						"scope":         params.Scope,
						"no_block":      params.NoBlock,
						"force":         params.Force,
					},
				}

			case task.Service != nil:
				modReq = ModuleRequest{
					Module: "service",
					Args: map[string]interface{}{
						"name":      task.Service.Name,
						"state":     task.Service.State,
						"enabled":   task.Service.Enabled,
						"arguments": task.Service.Arguments,
						"pattern":   task.Service.Pattern,
						"sleep":     task.Service.Sleep,
						"use":       task.Service.Use,
					},
				}

			case task.ServiceFacts != nil:
				modReq = ModuleRequest{
					Module: "service_facts",
					Args:   map[string]interface{}{},
				}

			case task.Ping != nil:
				modReq = ModuleRequest{
					Module: "ping",
					Args: map[string]interface{}{
						"data": task.Ping.Data,
					},
				}

			case task.Command != nil:
				stdinAddNewline := true
				if task.Command.StdinAddNewline != nil {
					stdinAddNewline = *task.Command.StdinAddNewline
				}
				stripEmptyEnds := true
				if task.Command.StripEmptyEnds != nil {
					stripEmptyEnds = *task.Command.StripEmptyEnds
				}
				modReq = ModuleRequest{
					Module: "command",
					Args: map[string]interface{}{
						"cmd":               task.Command.Cmd,
						"argv":              task.Command.Argv,
						"chdir":             task.Command.Chdir,
						"creates":           task.Command.Creates,
						"removes":           task.Command.Removes,
						"stdin":             task.Command.Stdin,
						"stdin_add_newline": stdinAddNewline,
						"strip_empty_ends":  stripEmptyEnds,
					},
				}

			case task.Shell != nil:
				stdinAddNewline := true
				if task.Shell.StdinAddNewline != nil {
					stdinAddNewline = *task.Shell.StdinAddNewline
				}
				stripEmptyEnds := true
				if task.Shell.StripEmptyEnds != nil {
					stripEmptyEnds = *task.Shell.StripEmptyEnds
				}
				modReq = ModuleRequest{
					Module: "shell",
					Args: map[string]interface{}{
						"cmd":               task.Shell.Cmd,
						"chdir":             task.Shell.Chdir,
						"creates":           task.Shell.Creates,
						"removes":           task.Shell.Removes,
						"stdin":             task.Shell.Stdin,
						"stdin_add_newline": stdinAddNewline,
						"strip_empty_ends":  stripEmptyEnds,
						"executable":        task.Shell.Executable,
					},
				}

			case task.Unarchive != nil:
				unarchiveArgs := map[string]interface{}{
					"dest":       task.Unarchive.Dest,
					"remote_src": task.Unarchive.RemoteSrc,
					"creates":    task.Unarchive.Creates,
					"list_files": task.Unarchive.ListFiles,
					"exclude":    task.Unarchive.Exclude,
					"include":    task.Unarchive.Include,
					"keep_newer": task.Unarchive.KeepNewer,
					"extra_opts": task.Unarchive.ExtraOpts,
					"mode":       task.Unarchive.Mode,
					"owner":      task.Unarchive.Owner,
					"group":      task.Unarchive.Group,
				}

				if task.Unarchive.RemoteSrc {
					unarchiveArgs["src"] = task.Unarchive.Src
				} else if task.Unarchive.Src != "" {
					localChecksum, err := sha1File(task.Unarchive.Src)
					if err != nil {
						fmt.Printf("    ✗ Failed to compute local checksum: %v\n", err)
						continue
					}

					remoteTmpPath := fmt.Sprintf("/tmp/.goansible-unarchive-%s", localChecksum[:12])
					if err := client.UploadFile(task.Unarchive.Src, remoteTmpPath); err != nil {
						fmt.Printf("    ✗ Failed to upload archive: %v\n", err)
						continue
					}

					unarchiveArgs["src"] = remoteTmpPath
					unarchiveArgs["checksum"] = localChecksum
				}

				modReq = ModuleRequest{
					Module: "unarchive",
					Args:   unarchiveArgs,
				}

			default:
				fmt.Println("    ⚠ No module specified, skipping")
				continue
			}

			reqData, _ := json.Marshal(modReq)
			if *verbose {
				fmt.Printf("    Request: %s\n", string(reqData))
			}

			output, err := client.ExecuteAgent(remoteAgentPath, reqData)
			if err != nil {
				fmt.Printf("    ✗ Execution failed: %v\n", err)
				continue
			}

			outputStr := strings.TrimSpace(string(output))
			lines := strings.Split(outputStr, "\n")
			jsonOutput := lines[len(lines)-1]

			var resp GenericResponse
			if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
				fmt.Printf("    ✗ Failed to parse response: %v\n", err)
				fmt.Printf("    Raw output: %s\n", outputStr)
				continue
			}

			if resp.Failed {
				fmt.Printf("    ✗ FAILED: %s\n", resp.Msg)
				if *verbose && resp.Stderr != "" {
					fmt.Printf("    Stderr: %s\n", resp.Stderr)
				}
			} else if resp.Changed {
				fmt.Printf("    ✓ CHANGED")
				if len(resp.Packages) > 0 {
					fmt.Printf(" (packages: %s)", strings.Join(resp.Packages, ", "))
				}
				if resp.KeyID != "" {
					fmt.Printf(" (key: %s)", resp.KeyID)
				}
				if resp.Filename != "" {
					fmt.Printf(" (file: %s)", resp.Filename)
				}
				if resp.Msg != "" {
					fmt.Printf(" - %s", resp.Msg)
				}
				fmt.Println()
			} else {
				fmt.Printf("    ✓ OK (no changes)")
				if resp.Msg != "" {
					fmt.Printf(" - %s", resp.Msg)
				}
				fmt.Println()
			}

			if *verbose && resp.Stdout != "" {
				fmt.Printf("    Stdout: %s\n", truncate(resp.Stdout, 500))
			}
		}
	}

	fmt.Println("\nDone!")
}

func findProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	wd, _ := os.Getwd()
	return wd
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func executeFetch(client *ssh.Client, agentPath, hostName string, params *config.FetchParams, verbose bool) GenericResponse {
	failOnMissing := true
	if params.FailOnMissing != nil {
		failOnMissing = *params.FailOnMissing
	}
	validateChecksum := true
	if params.ValidateChecksum != nil {
		validateChecksum = *params.ValidateChecksum
	}

	statReq := ModuleRequest{
		Module: "stat",
		Args: map[string]interface{}{
			"path":   params.Src,
			"follow": true,
		},
	}
	reqData, _ := json.Marshal(statReq)
	if verbose {
		fmt.Printf("    Stat request: %s\n", string(reqData))
	}

	output, err := client.ExecuteAgent(agentPath, reqData)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to stat remote file: %v", err)}
	}

	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	jsonOutput := lines[len(lines)-1]

	var statResp struct {
		Failed bool   `json:"failed"`
		Msg    string `json:"msg"`
		Exists bool   `json:"exists"`
		Stat   *struct {
			IsDir    bool   `json:"isdir"`
			Checksum string `json:"checksum"`
		} `json:"stat"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &statResp); err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to parse stat response: %v", err)}
	}

	if statResp.Failed {
		return GenericResponse{Failed: true, Msg: statResp.Msg}
	}

	if !statResp.Exists {
		if failOnMissing {
			return GenericResponse{Failed: true, Msg: fmt.Sprintf("remote file does not exist: %s", params.Src)}
		}
		return GenericResponse{Changed: false, Msg: "file not found (fail_on_missing=false)"}
	}

	if statResp.Stat != nil && statResp.Stat.IsDir {
		return GenericResponse{Failed: true, Msg: "fetch does not support directories, use a loop instead"}
	}

	remoteChecksum := ""
	if statResp.Stat != nil {
		remoteChecksum = statResp.Stat.Checksum
	}

	var localDest string
	if params.Flat {
		localDest = params.Dest
		if strings.HasSuffix(params.Dest, "/") || isDir(params.Dest) {
			localDest = filepath.Join(params.Dest, filepath.Base(params.Src))
		}
	} else {
		localDest = filepath.Join(params.Dest, hostName, params.Src)
	}

	if localChecksum, err := sha1File(localDest); err == nil {
		if localChecksum == remoteChecksum {
			return GenericResponse{
				Changed: false,
				Msg:     "file already exists with matching checksum",
			}
		}
	}

	if verbose {
		fmt.Printf("    Downloading %s -> %s\n", params.Src, localDest)
	}

	if err := client.DownloadFile(params.Src, localDest); err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to download file: %v", err)}
	}

	if validateChecksum && remoteChecksum != "" {
		localChecksum, err := sha1File(localDest)
		if err != nil {
			return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to compute local checksum: %v", err)}
		}
		if localChecksum != remoteChecksum {
			os.Remove(localDest)
			return GenericResponse{Failed: true, Msg: fmt.Sprintf("checksum mismatch: expected %s, got %s", remoteChecksum, localChecksum)}
		}
	}

	return GenericResponse{
		Changed: true,
		Msg:     fmt.Sprintf("fetched to %s", localDest),
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printResponse(resp GenericResponse, verbose bool) {
	if resp.Failed {
		fmt.Printf("    ✗ FAILED: %s\n", resp.Msg)
		if verbose && resp.Stderr != "" {
			fmt.Printf("    Stderr: %s\n", resp.Stderr)
		}
	} else if resp.Changed {
		fmt.Printf("    ✓ CHANGED")
		if resp.Msg != "" {
			fmt.Printf(" - %s", resp.Msg)
		}
		fmt.Println()
	} else {
		fmt.Printf("    ✓ OK (no changes)")
		if resp.Msg != "" {
			fmt.Printf(" - %s", resp.Msg)
		}
		fmt.Println()
	}
}
