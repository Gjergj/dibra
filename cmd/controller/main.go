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
	"time"

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

			case task.Script != nil:
				resp := executeScript(client, remoteAgentPath, task.Script, *verbose)
				printResponse(resp, *verbose)
				continue

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

			case task.Git != nil:
				gitArgs := map[string]interface{}{
					"repo":             task.Git.Repo,
					"dest":             task.Git.Dest,
					"version":          task.Git.Version,
					"remote":           task.Git.Remote,
					"force":            task.Git.Force,
					"bare":             task.Git.Bare,
					"track_submodules": task.Git.TrackSubmodules,
					"single_branch":    task.Git.SingleBranch,
					"accept_hostkey":   task.Git.AcceptHostkey,
					"accept_newhostkey": task.Git.AcceptNewhostkey,
					"key_file":         task.Git.KeyFile,
					"ssh_opts":         task.Git.SSHOpts,
					"refspec":          task.Git.Refspec,
					"executable":       task.Git.Executable,
					"separate_git_dir": task.Git.SeparateGitDir,
				}
				if task.Git.Clone != nil {
					gitArgs["clone"] = *task.Git.Clone
				}
				if task.Git.Update != nil {
					gitArgs["update"] = *task.Git.Update
				}
				if task.Git.Depth != nil {
					gitArgs["depth"] = *task.Git.Depth
				}
				if task.Git.Recursive != nil {
					gitArgs["recursive"] = *task.Git.Recursive
				}
				modReq = ModuleRequest{
					Module: "git",
					Args:   gitArgs,
				}

			case task.Lineinfile != nil:
				modReq = ModuleRequest{
					Module: "lineinfile",
					Args: map[string]interface{}{
						"path":          task.Lineinfile.Path,
						"line":          task.Lineinfile.Line,
						"regexp":        task.Lineinfile.Regexp,
						"search_string": task.Lineinfile.SearchString,
						"state":         task.Lineinfile.State,
						"backrefs":      task.Lineinfile.Backrefs,
						"insertafter":   task.Lineinfile.InsertAfter,
						"insertbefore":  task.Lineinfile.InsertBefore,
						"firstmatch":    task.Lineinfile.FirstMatch,
						"create":        task.Lineinfile.Create,
						"backup":        task.Lineinfile.Backup,
						"mode":          task.Lineinfile.Mode,
						"owner":         task.Lineinfile.Owner,
						"group":         task.Lineinfile.Group,
						"validate":      task.Lineinfile.Validate,
					},
				}

			case task.Blockinfile != nil:
				modReq = ModuleRequest{
					Module: "blockinfile",
					Args: map[string]interface{}{
						"path":            task.Blockinfile.Path,
						"block":           task.Blockinfile.Block,
						"marker":          task.Blockinfile.Marker,
						"marker_begin":    task.Blockinfile.MarkerBegin,
						"marker_end":      task.Blockinfile.MarkerEnd,
						"insertafter":     task.Blockinfile.InsertAfter,
						"insertbefore":    task.Blockinfile.InsertBefore,
						"state":           task.Blockinfile.State,
						"create":          task.Blockinfile.Create,
						"backup":          task.Blockinfile.Backup,
						"mode":            task.Blockinfile.Mode,
						"owner":           task.Blockinfile.Owner,
						"group":           task.Blockinfile.Group,
						"validate":        task.Blockinfile.Validate,
						"prepend_newline": task.Blockinfile.PrependNewline,
						"append_newline":  task.Blockinfile.AppendNewline,
					},
				}

			case task.Replace != nil:
				modReq = ModuleRequest{
					Module: "replace",
					Args: map[string]interface{}{
						"path":     task.Replace.Path,
						"regexp":   task.Replace.Regexp,
						"replace":  task.Replace.Replace,
						"after":    task.Replace.After,
						"before":   task.Replace.Before,
						"backup":   task.Replace.Backup,
						"mode":     task.Replace.Mode,
						"owner":    task.Replace.Owner,
						"group":    task.Replace.Group,
						"validate": task.Replace.Validate,
					},
				}

			case task.IptablesState != nil:
				modReq = ModuleRequest{
					Module: "iptables_state",
					Args: map[string]interface{}{
						"path":       task.IptablesState.Path,
						"state":      task.IptablesState.State,
						"table":      task.IptablesState.Table,
						"counters":   task.IptablesState.Counters,
						"noflush":    task.IptablesState.Noflush,
						"ip_version": task.IptablesState.IPVersion,
						"wait":       task.IptablesState.Wait,
						"modprobe":   task.IptablesState.Modprobe,
					},
				}

			case task.Iptables != nil:
				iptablesArgs := map[string]interface{}{
					"table":              task.Iptables.Table,
					"chain":              task.Iptables.Chain,
					"state":              task.Iptables.State,
					"action":             task.Iptables.Action,
					"rule_num":           task.Iptables.RuleNum,
					"protocol":           task.Iptables.Protocol,
					"source":             task.Iptables.Source,
					"destination":        task.Iptables.Destination,
					"match":              task.Iptables.Match,
					"jump":               task.Iptables.Jump,
					"goto":               task.Iptables.Goto,
					"in_interface":       task.Iptables.InInterface,
					"out_interface":      task.Iptables.OutInterface,
					"source_port":        task.Iptables.SourcePort,
					"destination_port":   task.Iptables.DestinationPort,
					"destination_ports":  task.Iptables.DestinationPorts,
					"ctstate":            task.Iptables.Ctstate,
					"comment":            task.Iptables.Comment,
					"icmp_type":          task.Iptables.IcmpType,
					"fragment":           task.Iptables.Fragment,
					"syn":                task.Iptables.Syn,
					"limit":              task.Iptables.Limit,
					"limit_burst":        task.Iptables.LimitBurst,
					"log_prefix":         task.Iptables.LogPrefix,
					"log_level":          task.Iptables.LogLevel,
					"reject_with":        task.Iptables.RejectWith,
					"to_destination":     task.Iptables.ToDestination,
					"to_source":          task.Iptables.ToSource,
					"to_ports":           task.Iptables.ToPorts,
					"gateway":            task.Iptables.Gateway,
					"src_range":          task.Iptables.SrcRange,
					"dst_range":          task.Iptables.DstRange,
					"set_counters":       task.Iptables.SetCounters,
					"set_dscp_mark":      task.Iptables.SetDscpMark,
					"set_dscp_mark_class": task.Iptables.SetDscpMarkClass,
					"uid_owner":          task.Iptables.UidOwner,
					"gid_owner":          task.Iptables.GidOwner,
					"match_set":          task.Iptables.MatchSet,
					"match_set_flags":    task.Iptables.MatchSetFlags,
					"flush":              task.Iptables.Flush,
					"policy":             task.Iptables.Policy,
					"chain_management":   task.Iptables.ChainManagement,
					"ip_version":         task.Iptables.IPVersion,
					"wait":               task.Iptables.Wait,
					"numeric":            task.Iptables.Numeric,
				}
				if task.Iptables.TcpFlags != nil {
					iptablesArgs["tcp_flags"] = map[string]interface{}{
						"flags":     task.Iptables.TcpFlags.Flags,
						"flags_set": task.Iptables.TcpFlags.FlagsSet,
					}
				}
				modReq = ModuleRequest{
					Module: "iptables",
					Args:   iptablesArgs,
				}

			case task.Tempfile != nil:
				tempfileArgs := map[string]interface{}{
					"path":   task.Tempfile.Path,
					"suffix": task.Tempfile.Suffix,
					"state":  task.Tempfile.State,
				}
				if task.Tempfile.Prefix != nil {
					tempfileArgs["prefix"] = *task.Tempfile.Prefix
				}
				modReq = ModuleRequest{
					Module: "tempfile",
					Args:   tempfileArgs,
				}

			case task.Reboot != nil:
				resp := executeReboot(client, remoteAgentPath, host, task.Reboot, *verbose)
				printResponse(resp, *verbose)
				continue

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

func executeScript(client *ssh.Client, agentPath string, params *config.ScriptParams, verbose bool) GenericResponse {
	if params.Cmd == "" {
		return GenericResponse{Failed: true, Msg: "no script path specified (cmd parameter required)"}
	}

	scriptPath, scriptArgs := parseScriptCmd(params.Cmd)

	localFile, err := os.Stat(scriptPath)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to stat local script: %v", err)}
	}
	if localFile.IsDir() {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("script path is a directory: %s", scriptPath)}
	}

	localChecksum, err := sha1File(scriptPath)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to compute script checksum: %v", err)}
	}

	remoteTmpPath := fmt.Sprintf("/tmp/.goansible-script-%s", localChecksum[:12])

	if verbose {
		fmt.Printf("    Uploading script %s -> %s\n", scriptPath, remoteTmpPath)
	}

	if err := client.UploadFile(scriptPath, remoteTmpPath); err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to upload script: %v", err)}
	}

	_, _, err = client.RunWithSudo(fmt.Sprintf("chmod +x %s", remoteTmpPath))
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to chmod script: %v", err)}
	}

	stripEmptyEnds := true
	if params.StripEmptyEnds != nil {
		stripEmptyEnds = *params.StripEmptyEnds
	}

	scriptReq := ModuleRequest{
		Module: "script",
		Args: map[string]interface{}{
			"script_path":      remoteTmpPath,
			"args":             scriptArgs,
			"chdir":            params.Chdir,
			"creates":          params.Creates,
			"removes":          params.Removes,
			"executable":       params.Executable,
			"strip_empty_ends": stripEmptyEnds,
		},
	}
	reqData, _ := json.Marshal(scriptReq)
	if verbose {
		fmt.Printf("    Script request: %s\n", string(reqData))
	}

	output, err := client.ExecuteAgent(agentPath, reqData)
	if err != nil {
		cleanupScript(client, remoteTmpPath)
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to execute script: %v", err)}
	}

	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	jsonOutput := lines[len(lines)-1]

	var resp GenericResponse
	if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
		cleanupScript(client, remoteTmpPath)
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to parse script response: %v", err)}
	}

	cleanupScript(client, remoteTmpPath)

	return resp
}

func parseScriptCmd(cmd string) (scriptPath string, args string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", ""
	}

	if cmd[0] == '"' || cmd[0] == '\'' {
		quoteChar := cmd[0]
		endIdx := strings.IndexByte(cmd[1:], quoteChar)
		if endIdx != -1 {
			scriptPath = cmd[1 : endIdx+1]
			args = strings.TrimSpace(cmd[endIdx+2:])
			return
		}
	}

	parts := strings.SplitN(cmd, " ", 2)
	scriptPath = parts[0]
	if len(parts) > 1 {
		args = parts[1]
	}
	return
}

func cleanupScript(client *ssh.Client, remotePath string) {
	client.Run(fmt.Sprintf("rm -f %s", remotePath))
}

func executeReboot(client *ssh.Client, agentPath string, host config.Host, params *config.RebootParams, verbose bool) GenericResponse {
	startTime := time.Now()

	rebootTimeout := params.RebootTimeout
	if rebootTimeout == 0 {
		rebootTimeout = 600
	}
	connectTimeout := params.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = 30
	}
	testCommand := params.TestCommand
	if testCommand == "" {
		testCommand = "whoami"
	}
	bootTimeCommand := params.BootTimeCommand
	if bootTimeCommand == "" {
		bootTimeCommand = "cat /proc/sys/kernel/random/boot_id"
	}
	msg := params.Msg
	if msg == "" {
		msg = "Reboot initiated by GoAnsible"
	}
	searchPaths := params.SearchPaths
	if len(searchPaths) == 0 {
		searchPaths = []string{"/sbin", "/bin", "/usr/sbin", "/usr/bin", "/usr/local/sbin"}
	}

	bootTimeReq := ModuleRequest{
		Module: "shell",
		Args: map[string]interface{}{
			"cmd": bootTimeCommand,
		},
	}
	reqData, _ := json.Marshal(bootTimeReq)
	if verbose {
		fmt.Printf("    Getting boot time: %s\n", bootTimeCommand)
	}

	output, err := client.ExecuteAgent(agentPath, reqData)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to get boot time: %v", err)}
	}

	var bootTimeResp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
		Failed bool   `json:"failed"`
		Msg    string `json:"msg"`
	}
	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	jsonOutput := lines[len(lines)-1]

	if err := json.Unmarshal([]byte(jsonOutput), &bootTimeResp); err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to parse boot time response: %v", err)}
	}
	if bootTimeResp.Failed || bootTimeResp.RC != 0 {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to get boot time: %s", bootTimeResp.Msg)}
	}

	previousBootTime := strings.TrimSpace(bootTimeResp.Stdout)
	if verbose {
		fmt.Printf("    Previous boot time: %s\n", previousBootTime)
	}

	if params.PreRebootDelay > 0 {
		if verbose {
			fmt.Printf("    Waiting %d seconds before reboot...\n", params.PreRebootDelay)
		}
		time.Sleep(time.Duration(params.PreRebootDelay) * time.Second)
	}

	rebootCmd := params.RebootCommand
	if rebootCmd == "" {
		for _, path := range searchPaths {
			checkReq := ModuleRequest{
				Module: "shell",
				Args: map[string]interface{}{
					"cmd": fmt.Sprintf("test -x %s/shutdown && echo found", path),
				},
			}
			reqData, _ := json.Marshal(checkReq)
			output, err := client.ExecuteAgent(agentPath, reqData)
			if err == nil {
				outputStr := strings.TrimSpace(string(output))
				lines := strings.Split(outputStr, "\n")
				jsonOutput := lines[len(lines)-1]
				var resp struct {
					Stdout string `json:"stdout"`
					RC     int    `json:"rc"`
				}
				if json.Unmarshal([]byte(jsonOutput), &resp) == nil && resp.RC == 0 && strings.Contains(resp.Stdout, "found") {
					rebootCmd = fmt.Sprintf("%s/shutdown -r +0 \"%s\"", path, msg)
					break
				}
			}
		}
		if rebootCmd == "" {
			for _, path := range searchPaths {
				checkReq := ModuleRequest{
					Module: "shell",
					Args: map[string]interface{}{
						"cmd": fmt.Sprintf("test -x %s/reboot && echo found", path),
					},
				}
				reqData, _ := json.Marshal(checkReq)
				output, err := client.ExecuteAgent(agentPath, reqData)
				if err == nil {
					outputStr := strings.TrimSpace(string(output))
					lines := strings.Split(outputStr, "\n")
					jsonOutput := lines[len(lines)-1]
					var resp struct {
						Stdout string `json:"stdout"`
						RC     int    `json:"rc"`
					}
					if json.Unmarshal([]byte(jsonOutput), &resp) == nil && resp.RC == 0 && strings.Contains(resp.Stdout, "found") {
						rebootCmd = fmt.Sprintf("%s/reboot", path)
						break
					}
				}
			}
		}
		if rebootCmd == "" {
			return GenericResponse{Failed: true, Msg: "shutdown/reboot command not found in search paths"}
		}
	}

	if verbose {
		fmt.Printf("    Executing reboot command: %s\n", rebootCmd)
	}

	rebootReq := ModuleRequest{
		Module: "shell",
		Args: map[string]interface{}{
			"cmd": rebootCmd,
		},
	}
	reqData, _ = json.Marshal(rebootReq)

	_, _ = client.ExecuteAgent(agentPath, reqData)

	client.Close()

	if params.PostRebootDelay > 0 {
		if verbose {
			fmt.Printf("    Waiting %d seconds after reboot command...\n", params.PostRebootDelay)
		}
		time.Sleep(time.Duration(params.PostRebootDelay) * time.Second)
	}

	if verbose {
		fmt.Println("    Waiting for system to come back up...")
	}

	deadline := time.Now().Add(time.Duration(rebootTimeout) * time.Second)
	var newClient *ssh.Client

	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		var err error
		newClient, err = ssh.Connect(ssh.Config{
			Host:           host.Host,
			Port:           host.Port,
			User:           host.User,
			Password:       host.Password,
			SSHKeyPath:     host.SSHKeyPath,
			Become:         host.Become,
			BecomePassword: host.BecomePassword,
		})
		if err != nil {
			if verbose {
				fmt.Printf("    Reconnect attempt failed: %v\n", err)
			}
			continue
		}

		bootTimeReq := ModuleRequest{
			Module: "shell",
			Args: map[string]interface{}{
				"cmd": bootTimeCommand,
			},
		}
		reqData, _ := json.Marshal(bootTimeReq)
		output, err := newClient.ExecuteAgent(agentPath, reqData)
		if err != nil {
			newClient.Close()
			continue
		}

		outputStr := strings.TrimSpace(string(output))
		lines := strings.Split(outputStr, "\n")
		jsonOutput := lines[len(lines)-1]

		var resp struct {
			Stdout string `json:"stdout"`
			RC     int    `json:"rc"`
		}
		if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
			newClient.Close()
			continue
		}

		currentBootTime := strings.TrimSpace(resp.Stdout)
		if currentBootTime == "" || currentBootTime == previousBootTime {
			if verbose {
				fmt.Printf("    Boot time unchanged (%s), still waiting...\n", currentBootTime)
			}
			newClient.Close()
			continue
		}

		if verbose {
			fmt.Printf("    New boot time: %s\n", currentBootTime)
		}

		testReq := ModuleRequest{
			Module: "shell",
			Args: map[string]interface{}{
				"cmd": testCommand,
			},
		}
		reqData, _ = json.Marshal(testReq)
		output, err = newClient.ExecuteAgent(agentPath, reqData)
		newClient.Close()

		if err != nil {
			continue
		}

		outputStr = strings.TrimSpace(string(output))
		lines = strings.Split(outputStr, "\n")
		jsonOutput = lines[len(lines)-1]

		var testResp struct {
			RC     int  `json:"rc"`
			Failed bool `json:"failed"`
		}
		if err := json.Unmarshal([]byte(jsonOutput), &testResp); err != nil || testResp.Failed || testResp.RC != 0 {
			continue
		}

		elapsed := int(time.Since(startTime).Seconds())
		return GenericResponse{
			Changed: true,
			Msg:     fmt.Sprintf("system rebooted successfully (elapsed: %ds)", elapsed),
		}
	}

	elapsed := int(time.Since(startTime).Seconds())
	return GenericResponse{
		Failed: true,
		Msg:    fmt.Sprintf("timed out waiting for system to reboot (timeout=%ds, elapsed=%ds)", rebootTimeout, elapsed),
	}
}
