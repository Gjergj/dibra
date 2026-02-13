package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/agent"
	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/inventory"
	"github.com/gjergjiramku/dibra/internal/secrets"
	"github.com/gjergjiramku/dibra/internal/secrets/bitwarden"
	"github.com/gjergjiramku/dibra/internal/secrets/onepassword"
	"github.com/gjergjiramku/dibra/internal/ssh"
	"github.com/gjergjiramku/dibra/internal/template"
	"github.com/gjergjiramku/dibra/internal/vars"
	"github.com/gjergjiramku/dibra/internal/version"
	"gopkg.in/yaml.v3"
)

const (
	remoteAgentDir  = "/tmp"
	remoteAgentName = ".dibra-agent"
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
	showVersion := flag.Bool("version", false, "Print version and exit")
	agentPath := flag.String("agent-path", "", "Path to a pre-built agent binary")
	agentBuild := flag.Bool("agent-build", false, "Build agent from source (requires Go)")
	inventoryPath := flag.String("i", "", "Path to YAML inventory file")
	inventoryPathLong := flag.String("inventory", "", "Path to YAML inventory file")
	extraVars := flag.String("e", "", "Extra variables (key=value or @file.yaml)")
	extraVarsLong := flag.String("extra-vars", "", "Extra variables (key=value or @file.yaml)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("dibra %s (commit: %s, built: %s)\n", version.Version, version.Commit, version.Date)
		os.Exit(0)
	}

	if *agentPath != "" && *agentBuild {
		fatal("--agent-path and --agent-build are mutually exclusive")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("Failed to load config: %v", err)
	}

	rawExtra := *extraVars
	if rawExtra == "" {
		rawExtra = *extraVarsLong
	}
	extraVarsMap, err := parseExtraVars(rawExtra)
	if err != nil {
		fatal("Failed to parse extra vars: %v", err)
	}

	invPath := *inventoryPath
	if invPath == "" {
		invPath = *inventoryPathLong
	}
	if invPath == "" && cfg.Inventory != "" {
		invPath = cfg.Inventory
		if !filepath.IsAbs(invPath) {
			invPath = filepath.Join(filepath.Dir(*configPath), invPath)
		}
	}

	var inv *inventory.Inventory
	if invPath != "" {
		if len(cfg.Hosts) > 0 {
			fatal("Cannot use both inventory (-i) and playbook hosts; remove one")
		}
		inv, err = inventory.Load(invPath)
		if err != nil {
			fatal("Failed to load inventory: %v", err)
		}

		secretsResolver := secrets.NewResolver()
		secretsResolver.Register("bw", bitwarden.NewProvider())
		secretsResolver.Register("op", onepassword.NewProvider())
		if err := inv.ResolveSecrets(secretsResolver); err != nil {
			fatal("Failed to resolve inventory secrets: %v", err)
		}

		if err := inv.ResolveTemplates(); err != nil {
			fatal("Failed to resolve inventory templates: %v", err)
		}

		cfg.Hosts, err = inv.HostsAsConfig()
		if err != nil {
			fatal("Failed to convert inventory hosts: %v", err)
		}
	}

	resolverMode := agent.ModeAuto
	if *agentPath != "" {
		resolverMode = agent.ModePath
	} else if *agentBuild {
		resolverMode = agent.ModeBuild
	}

	agentResolver := agent.NewResolver(agent.Options{
		Mode:        resolverMode,
		AgentPath:   *agentPath,
		Version:     version.Version,
		ProjectRoot: findProjectRoot(),
	})

	hostInfos := make([]vars.HostInfo, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		hostInfos = append(hostInfos, vars.HostInfo{Name: host.Name, Groups: host.Groups})
	}

	baseDir := filepath.Dir(*configPath)
	inventoryBaseDir := baseDir
	if inv != nil {
		inventoryBaseDir = inv.BaseDir
	}
	varsResolver := vars.Resolver{
		MergeStrategy: vars.MergeStrategy(cfg.VarsMerge),
		InventoryDir:  inventoryBaseDir,
	}

	var extraGroupNames []string
	if inv != nil {
		for gName := range inv.Groups {
			extraGroupNames = append(extraGroupNames, gName)
		}
	}
	inventoryVars, err := varsResolver.LoadInventoryVars(hostInfos, extraGroupNames)
	if err != nil {
		fatal("Failed to load inventory vars: %v", err)
	}

	if inv != nil {
		for _, hostname := range inv.HostNames() {
			invHostVars := inv.EffectiveVarsForHost(hostname)
			cleanVars := map[string]interface{}{}
			for k, v := range invHostVars {
				if !strings.HasPrefix(k, "dibra_") {
					cleanVars[k] = v
				}
			}
			if len(cleanVars) > 0 {
				if inventoryVars.Host[hostname] == nil {
					inventoryVars.Host[hostname] = map[string]interface{}{}
				}
				for k, v := range cleanVars {
					if _, exists := inventoryVars.Host[hostname][k]; !exists {
						inventoryVars.Host[hostname][k] = v
					}
				}
			}
		}
	}

	playVars := cfg.Vars
	if len(cfg.VarsFiles) > 0 {
		varsFromFiles, err := vars.LoadVarsFiles(baseDir, cfg.VarsFiles, vars.MergeStrategy(cfg.VarsMerge))
		if err != nil {
			fatal("Failed to load vars_files: %v", err)
		}
		playVars = vars.MergeMaps(playVars, varsFromFiles, vars.MergeStrategy(cfg.VarsMerge))
	}

	{
		sr := secrets.NewResolver()
		sr.Register("bw", bitwarden.NewProvider())
		sr.Register("op", onepassword.NewProvider())
		resolved, err := sr.ResolveMap(playVars)
		if err != nil {
			fatal("Failed to resolve secrets in play vars: %v", err)
		}
		playVars = resolved
	}

	renderImportPath := func(s string) (string, error) {
		ctx := make(map[string]interface{})
		for k, v := range playVars {
			ctx[k] = v
		}
		for k, v := range extraVarsMap {
			ctx[k] = v
		}
		return vars.RenderString(s, ctx)
	}
	cfg.Tasks, err = config.ExpandImportTasks(cfg.Tasks, baseDir, renderImportPath)
	if err != nil {
		fatal("Failed to expand import_tasks: %v", err)
	}

	var groupsMap map[string][]string
	if inv != nil {
		groupsMap = inv.GroupMembers()
	} else {
		groupsMap = map[string][]string{}
		for _, hostInfo := range hostInfos {
			for _, group := range hostInfo.Groups {
				if group == "" {
					continue
				}
				groupsMap[group] = append(groupsMap[group], hostInfo.Name)
			}
		}
	}

	hostRuntimeVars := make(map[string]map[string]interface{})
	for _, host := range cfg.Hosts {
		hostRuntimeVars[host.Name] = make(map[string]interface{})
	}

	for _, host := range cfg.Hosts {
		// Create a context for rendering host connection parameters
		connectCtx := make(map[string]interface{})
		for k, v := range playVars {
			connectCtx[k] = v
		}
		for k, v := range extraVarsMap {
			connectCtx[k] = v
		}

		// Render connection parameters
		host.Host, _ = vars.RenderString(host.Host, connectCtx)
		host.User, _ = vars.RenderString(host.User, connectCtx)
		host.Password, _ = vars.RenderString(host.Password, connectCtx)
		host.SSHKeyPath, _ = vars.RenderString(host.SSHKeyPath, connectCtx)
		host.BecomePassword, _ = vars.RenderString(host.BecomePassword, connectCtx)

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

		fmt.Println("  Resolving agent binary...")
		agentBinary, err := agentResolver.Resolve(client)
		if err != nil {
			fmt.Printf("  ✗ Failed to resolve agent: %v\n", err)
			continue
		}
		if *verbose {
			fmt.Printf("  Agent binary: %s\n", agentBinary)
		}

		needsUpload := *forceUpload
		if !needsUpload {
			exists, _ := client.FileExists(remoteAgentPath)
			if !exists {
				needsUpload = true
			} else {
				remoteVersion, err := agent.CheckRemoteAgentVersion(client, remoteAgentPath)
				if err != nil {
					needsUpload = true
					if *verbose {
						fmt.Printf("  Could not check remote agent version: %v\n", err)
					}
				} else {
					localVersion := strings.TrimPrefix(version.Version, "v")
					if remoteVersion != localVersion {
						needsUpload = true
						if *verbose {
							fmt.Printf("  Remote agent version %q != local %q, uploading\n", remoteVersion, localVersion)
						}
					}
				}
			}
		}

		if needsUpload {
			fmt.Println("  Uploading agent...")
			if host.Become && host.User != "root" {
				_, _, err := client.RunWithSudo(fmt.Sprintf("rm -f %s", remoteAgentPath))
				if err != nil {
					fmt.Printf("  ✗ Failed to remove agent: %v\n", err)
					continue
				}
			}
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
			fmt.Println("  Agent up-to-date on remote")
		}

		taskQueue := make([]config.Task, len(cfg.Tasks))
		copy(taskQueue, cfg.Tasks)

		for taskIdx := 0; taskIdx < len(taskQueue); taskIdx++ {
			task := taskQueue[taskIdx]
			fmt.Printf("  Task: %s\n", task.Name)

			if task.Register != "" {
				if err := validateVarName(task.Register); err != nil {
					fmt.Printf("    ✗ %v\n", err)
					continue
				}
			}

			taskHostvars, err := buildHostvarsForTask(hostInfos, varsResolver, inventoryVars, playVars, task.Vars, extraVarsMap, hostRuntimeVars, groupsMap)
			if err != nil {
				fmt.Printf("    ✗ Failed to build hostvars: %v\n", err)
				continue
			}

			resolved, err := varsResolver.ResolveHostVars(vars.ResolveRequest{
				Host: vars.HostInfo{
					Name:   host.Name,
					Groups: host.Groups,
				},
				InventoryVars: inventoryVars,
				PlayVars:      playVars,
				TaskVars:      task.Vars,
				ExtraVars:     extraVarsMap,
				RuntimeVars:   hostRuntimeVars[host.Name],
			})
			if err != nil {
				fmt.Printf("    ✗ Failed to resolve vars: %v\n", err)
				continue
			}

			hostvarsContext := map[string]interface{}{}
			for name, hv := range taskHostvars {
				hostvarsContext[name] = hv
			}
			groupsContext := map[string]interface{}{}
			for groupName, members := range groupsMap {
				membersAny := make([]interface{}, len(members))
				for i, m := range members {
					membersAny[i] = m
				}
				groupsContext[groupName] = membersAny
			}
			for name, ctx := range taskHostvars {
				ctx["hostvars"] = hostvarsContext
				ctx["groups"] = groupsContext
				ctx["inventory_hostname"] = name
			}
			hostContext := taskHostvars[host.Name]
			groupNamesAny := make([]interface{}, len(host.Groups))
			for i, g := range host.Groups {
				groupNamesAny[i] = g
			}
			hostContext["group_names"] = groupNamesAny
			hostContext["vars"] = resolved.Namespaces
			flattened := hostContext

			var modReq ModuleRequest

			switch {
			case task.Apt != nil:
				args := map[string]interface{}{
					"packages":         task.Apt.GetPackages(),
					"state":            task.Apt.State,
					"update_cache":     task.Apt.UpdateCache,
					"cache_valid_time": task.Apt.CacheValidTime,
					"purge":            task.Apt.Purge,
					"autoremove":       task.Apt.Autoremove,
					"upgrade":          task.Apt.Upgrade,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "apt", Args: renderedArgs}

			case task.AptKey != nil:
				args := map[string]interface{}{
					"url":     task.AptKey.URL,
					"data":    task.AptKey.Data,
					"file":    task.AptKey.File,
					"keyring": task.AptKey.Keyring,
					"id":      task.AptKey.ID,
					"state":   task.AptKey.State,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "apt_key", Args: renderedArgs}

			case task.AptRepository != nil:
				args := map[string]interface{}{
					"repo":         task.AptRepository.Repo,
					"state":        task.AptRepository.State,
					"filename":     task.AptRepository.Filename,
					"update_cache": task.AptRepository.UpdateCache,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "apt_repository", Args: renderedArgs}

			case task.File != nil:
				args := map[string]interface{}{
					"path":    task.File.Path,
					"state":   task.File.State,
					"mode":    task.File.Mode,
					"owner":   task.File.Owner,
					"group":   task.File.Group,
					"src":     task.File.Src,
					"recurse": task.File.Recurse,
					"force":   task.File.Force,
					"follow":  task.File.Follow,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "file", Args: renderedArgs}

			case task.Fetch != nil:
				renderedFetch, err := renderFetchParams(task.Fetch, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render fetch params: %v\n", err)
					continue
				}
				resp := executeFetch(client, remoteAgentPath, host.Name, renderedFetch, *verbose)
				printResponse(resp, *verbose)
				if task.Register != "" {
					result := genericResponseToMap(resp)
					localDest := computeFetchDest(renderedFetch, host.Name)
					result["dest"] = localDest
					result["src"] = renderedFetch.Src
					if !resp.Failed {
						if checksum, err := sha1File(localDest); err == nil {
							result["checksum"] = checksum
						}
					}
					registerResult(hostRuntimeVars, host.Name, task, result)
				}
				continue

			case task.URI != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "uri", Args: renderedArgs}

			case task.Cron != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "cron", Args: renderedArgs}

			case task.UFW != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "ufw", Args: renderedArgs}

			case task.User != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "user", Args: renderedArgs}

			case task.Group != nil:
				args := map[string]interface{}{
					"name":       task.Group.Name,
					"state":      task.Group.State,
					"gid":        task.Group.GID,
					"system":     task.Group.System,
					"local":      task.Group.Local,
					"non_unique": task.Group.NonUnique,
					"force":      task.Group.Force,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "group", Args: renderedArgs}

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

					remoteTmpPath := fmt.Sprintf("/tmp/.dibra-copy-%s", localChecksum[:12])
					if err := client.UploadFile(task.Copy.Src, remoteTmpPath); err != nil {
						fmt.Printf("    ✗ Failed to upload file: %v\n", err)
						continue
					}

					copyArgs["src"] = remoteTmpPath
					copyArgs["checksum"] = localChecksum
				}

				renderedArgs, err := renderArgs(copyArgs, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "copy", Args: renderedArgs}

			case task.Template != nil:
				templateDest := task.Template.Dest
				if templateDest == "" {
					fmt.Println("    ✗ template: dest is required")
					continue
				}

				templateDest, err = vars.RenderString(templateDest, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render template dest: %v\n", err)
					continue
				}

				force := true
				if task.Template.Force != nil {
					force = *task.Template.Force
				}

				srcPath := task.Template.Src
				if srcPath == "" {
					fmt.Println("    ✗ template: src is required")
					continue
				}

				srcPath, err = vars.RenderString(srcPath, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render template src: %v\n", err)
					continue
				}

				resolveDir := baseDir
				if task.SourceDir != "" {
					resolveDir = task.SourceDir
				}

				if !filepath.IsAbs(srcPath) {
					srcPath = filepath.Join(resolveDir, srcPath)
				}
				srcPath = filepath.Clean(srcPath)

				if _, err := os.Stat(srcPath); err != nil {
					fmt.Printf("    ✗ template: failed to read src %q: %v\n", srcPath, err)
					continue
				}

				templateContext := make(map[string]interface{})
				for k, v := range flattened {
					templateContext[k] = v
				}

				templateDestPath := templateDest
				if strings.HasSuffix(templateDestPath, "/") {
					trimmed := strings.TrimRight(templateDestPath, "/")
					if trimmed == "" {
						templateDestPath = "/" + filepath.Base(srcPath)
					} else {
						templateDestPath = filepath.Join(trimmed, filepath.Base(srcPath))
					}
				}

				options := template.Options{
					NewlineSequence:     task.Template.NewlineSequence,
					VariableStartString: task.Template.VariableStartString,
					VariableEndString:   task.Template.VariableEndString,
					BlockStartString:    task.Template.BlockStartString,
					BlockEndString:      task.Template.BlockEndString,
					CommentStartString:  task.Template.CommentStartString,
					CommentEndString:    task.Template.CommentEndString,
					TrimBlocks:          task.Template.TrimBlocks,
					LstripBlocks:        task.Template.LstripBlocks,
				}

				meta := template.Metadata{
					HostName: host.Name,
					DestPath: templateDestPath,
				}

				rendered, _, err := template.RenderFile(srcPath, templateContext, meta, options)
				if err != nil {
					fmt.Printf("    ✗ template: render failed: %v\n", err)
					continue
				}

				if task.Template.NewlineSequence != "" {
					rendered = normalizeNewlines(rendered, task.Template.NewlineSequence)
				}

				templateArgs := map[string]interface{}{
					"src":      srcPath,
					"dest":     templateDest,
					"mode":     task.Template.Mode,
					"owner":    task.Template.Owner,
					"group":    task.Template.Group,
					"backup":   task.Template.Backup,
					"force":    force,
					"follow":   task.Template.Follow,
					"validate": task.Template.Validate,
				}

				renderedArgs, err := renderArgs(templateArgs, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render template args: %v\n", err)
					continue
				}
				renderedArgs["content"] = rendered

				modReq = ModuleRequest{Module: "template", Args: renderedArgs}

			case task.SystemdService != nil || task.Systemd != nil:
				params := task.SystemdService
				if params == nil {
					params = task.Systemd
				}
				args := map[string]interface{}{
					"name":          params.Name,
					"state":         params.State,
					"enabled":       params.Enabled,
					"masked":        params.Masked,
					"daemon_reload": params.DaemonReload,
					"daemon_reexec": params.DaemonReexec,
					"scope":         params.Scope,
					"no_block":      params.NoBlock,
					"force":         params.Force,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "systemd_service", Args: renderedArgs}

			case task.Service != nil:
				args := map[string]interface{}{
					"name":      task.Service.Name,
					"state":     task.Service.State,
					"enabled":   task.Service.Enabled,
					"arguments": task.Service.Arguments,
					"pattern":   task.Service.Pattern,
					"sleep":     task.Service.Sleep,
					"use":       task.Service.Use,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "service", Args: renderedArgs}

			case task.ServiceFacts != nil:
				modReq = ModuleRequest{
					Module: "service_facts",
					Args:   map[string]interface{}{},
				}

			case task.Ping != nil:
				args := map[string]interface{}{"data": task.Ping.Data}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "ping", Args: renderedArgs}

			case task.Slurp != nil:
				src := task.Slurp.Src
				if src == "" {
					src = task.Slurp.Path
				}
				args := map[string]interface{}{"src": src}
				renderedArgs, err := renderArgs(args, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render args: %v\n", err)
					continue
				}
				modReq = ModuleRequest{Module: "slurp", Args: renderedArgs}

			case task.Command != nil:
				stdinAddNewline := true
				if task.Command.StdinAddNewline != nil {
					stdinAddNewline = *task.Command.StdinAddNewline
				}
				stripEmptyEnds := true
				if task.Command.StripEmptyEnds != nil {
					stripEmptyEnds = *task.Command.StripEmptyEnds
				}
				args := map[string]interface{}{
					"cmd":               task.Command.Cmd,
					"argv":              task.Command.Argv,
					"chdir":             task.Command.Chdir,
					"creates":           task.Command.Creates,
					"removes":           task.Command.Removes,
					"stdin":             task.Command.Stdin,
					"stdin_add_newline": stdinAddNewline,
					"strip_empty_ends":  stripEmptyEnds,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "command", Args: renderedArgs}

			case task.Shell != nil:
				stdinAddNewline := true
				if task.Shell.StdinAddNewline != nil {
					stdinAddNewline = *task.Shell.StdinAddNewline
				}
				stripEmptyEnds := true
				if task.Shell.StripEmptyEnds != nil {
					stripEmptyEnds = *task.Shell.StripEmptyEnds
				}
				args := map[string]interface{}{
					"cmd":               task.Shell.Cmd,
					"chdir":             task.Shell.Chdir,
					"creates":           task.Shell.Creates,
					"removes":           task.Shell.Removes,
					"stdin":             task.Shell.Stdin,
					"stdin_add_newline": stdinAddNewline,
					"strip_empty_ends":  stripEmptyEnds,
					"executable":        task.Shell.Executable,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "shell", Args: renderedArgs}

			case task.Script != nil:
				renderedScript, err := renderScriptParams(task.Script, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render script params: %v\n", err)
					continue
				}
				resp, rawJSON := executeScript(client, remoteAgentPath, renderedScript, *verbose)
				printResponse(resp, *verbose)
				if task.Register != "" {
					var fullResult map[string]interface{}
					if rawJSON != "" {
						err = json.Unmarshal([]byte(rawJSON), &fullResult)
						if err != nil {
							fmt.Printf("    ✗ Failed to parse script response: %v\n", err)
							continue
						}
					}
					if fullResult == nil {
						fullResult = genericResponseToMap(resp)
					}
					registerResult(hostRuntimeVars, host.Name, task, fullResult)
				}
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

					remoteTmpPath := fmt.Sprintf("/tmp/.dibra-unarchive-%s", localChecksum[:12])
					if err := client.UploadFile(task.Unarchive.Src, remoteTmpPath); err != nil {
						fmt.Printf("    ✗ Failed to upload archive: %v\n", err)
						continue
					}

					unarchiveArgs["src"] = remoteTmpPath
					unarchiveArgs["checksum"] = localChecksum
				}

				renderedArgs, err := renderArgs(unarchiveArgs, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "unarchive", Args: renderedArgs}

			case task.Git != nil:
				gitArgs := map[string]interface{}{
					"repo":              task.Git.Repo,
					"dest":              task.Git.Dest,
					"version":           task.Git.Version,
					"remote":            task.Git.Remote,
					"force":             task.Git.Force,
					"bare":              task.Git.Bare,
					"track_submodules":  task.Git.TrackSubmodules,
					"single_branch":     task.Git.SingleBranch,
					"accept_hostkey":    task.Git.AcceptHostkey,
					"accept_newhostkey": task.Git.AcceptNewhostkey,
					"key_file":          task.Git.KeyFile,
					"ssh_opts":          task.Git.SSHOpts,
					"refspec":           task.Git.Refspec,
					"executable":        task.Git.Executable,
					"separate_git_dir":  task.Git.SeparateGitDir,
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
				renderedArgs, err := renderArgs(gitArgs, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "git", Args: renderedArgs}

			case task.Lineinfile != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "lineinfile", Args: renderedArgs}

			case task.Blockinfile != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "blockinfile", Args: renderedArgs}

			case task.Replace != nil:
				args := map[string]interface{}{
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
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "replace", Args: renderedArgs}

			case task.IptablesState != nil:
				args := map[string]interface{}{
					"path":       task.IptablesState.Path,
					"state":      task.IptablesState.State,
					"table":      task.IptablesState.Table,
					"counters":   task.IptablesState.Counters,
					"noflush":    task.IptablesState.Noflush,
					"ip_version": task.IptablesState.IPVersion,
					"wait":       task.IptablesState.Wait,
					"modprobe":   task.IptablesState.Modprobe,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "iptables_state", Args: renderedArgs}

			case task.Iptables != nil:
				iptablesArgs := map[string]interface{}{
					"table":               task.Iptables.Table,
					"chain":               task.Iptables.Chain,
					"state":               task.Iptables.State,
					"action":              task.Iptables.Action,
					"rule_num":            task.Iptables.RuleNum,
					"protocol":            task.Iptables.Protocol,
					"source":              task.Iptables.Source,
					"destination":         task.Iptables.Destination,
					"match":               task.Iptables.Match,
					"jump":                task.Iptables.Jump,
					"goto":                task.Iptables.Goto,
					"in_interface":        task.Iptables.InInterface,
					"out_interface":       task.Iptables.OutInterface,
					"source_port":         task.Iptables.SourcePort,
					"destination_port":    task.Iptables.DestinationPort,
					"destination_ports":   task.Iptables.DestinationPorts,
					"ctstate":             task.Iptables.Ctstate,
					"comment":             task.Iptables.Comment,
					"icmp_type":           task.Iptables.IcmpType,
					"fragment":            task.Iptables.Fragment,
					"syn":                 task.Iptables.Syn,
					"limit":               task.Iptables.Limit,
					"limit_burst":         task.Iptables.LimitBurst,
					"log_prefix":          task.Iptables.LogPrefix,
					"log_level":           task.Iptables.LogLevel,
					"reject_with":         task.Iptables.RejectWith,
					"to_destination":      task.Iptables.ToDestination,
					"to_source":           task.Iptables.ToSource,
					"to_ports":            task.Iptables.ToPorts,
					"gateway":             task.Iptables.Gateway,
					"src_range":           task.Iptables.SrcRange,
					"dst_range":           task.Iptables.DstRange,
					"set_counters":        task.Iptables.SetCounters,
					"set_dscp_mark":       task.Iptables.SetDscpMark,
					"set_dscp_mark_class": task.Iptables.SetDscpMarkClass,
					"uid_owner":           task.Iptables.UidOwner,
					"gid_owner":           task.Iptables.GidOwner,
					"match_set":           task.Iptables.MatchSet,
					"match_set_flags":     task.Iptables.MatchSetFlags,
					"flush":               task.Iptables.Flush,
					"policy":              task.Iptables.Policy,
					"chain_management":    task.Iptables.ChainManagement,
					"ip_version":          task.Iptables.IPVersion,
					"wait":                task.Iptables.Wait,
					"numeric":             task.Iptables.Numeric,
				}
				if task.Iptables.TcpFlags != nil {
					iptablesArgs["tcp_flags"] = map[string]interface{}{
						"flags":     task.Iptables.TcpFlags.Flags,
						"flags_set": task.Iptables.TcpFlags.FlagsSet,
					}
				}
				renderedArgs, err := renderArgs(iptablesArgs, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "iptables", Args: renderedArgs}

			case task.Tempfile != nil:
				tempfileArgs := map[string]interface{}{
					"path":   task.Tempfile.Path,
					"suffix": task.Tempfile.Suffix,
					"state":  task.Tempfile.State,
				}
				if task.Tempfile.Prefix != nil {
					tempfileArgs["prefix"] = *task.Tempfile.Prefix
				}
				renderedArgs, err := renderArgs(tempfileArgs, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "tempfile", Args: renderedArgs}

			case task.Find != nil:
				paths := task.Find.Paths
				if len(paths) == 0 {
					paths = task.Find.Path
				}
				if len(paths) == 0 {
					paths = task.Find.Name
				}
				patterns := task.Find.Patterns
				if len(patterns) == 0 {
					patterns = task.Find.Pattern
				}
				excludes := task.Find.Excludes
				if len(excludes) == 0 {
					excludes = task.Find.Exclude
				}
				exactMode := true
				if task.Find.ExactMode != nil {
					exactMode = *task.Find.ExactMode
				}
				args := map[string]interface{}{
					"paths":              paths,
					"patterns":           patterns,
					"excludes":           excludes,
					"contains":           task.Find.Contains,
					"read_whole_file":    task.Find.ReadWholeFile,
					"file_type":          task.Find.FileType,
					"age":                task.Find.Age,
					"age_stamp":          task.Find.AgeStamp,
					"size":               task.Find.Size,
					"recurse":            task.Find.Recurse,
					"hidden":             task.Find.Hidden,
					"follow":             task.Find.Follow,
					"get_checksum":       task.Find.GetChecksum,
					"checksum_algorithm": task.Find.ChecksumAlgorithm,
					"use_regex":          task.Find.UseRegex,
					"depth":              task.Find.Depth,
					"mode":               task.Find.Mode,
					"exact_mode":         exactMode,
					"limit":              task.Find.Limit,
				}
				renderedArgs, err := renderArgs(args, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render args: %v\n", err)
					continue
				}
				modReq = ModuleRequest{Module: "find", Args: renderedArgs}

			case task.DockerContainer != nil:
				args := map[string]interface{}{
					"name":            task.DockerContainer.Name,
					"image":           task.DockerContainer.Image,
					"state":           task.DockerContainer.State,
					"command":         task.DockerContainer.Command,
					"entrypoint":      task.DockerContainer.Entrypoint,
					"args":            task.DockerContainer.Args,
					"env":             task.DockerContainer.Env,
					"exposed_ports":   task.DockerContainer.ExposedPorts,
					"ports":           task.DockerContainer.Ports,
					"volumes":         task.DockerContainer.Volumes,
					"network_mode":    task.DockerContainer.NetworkMode,
					"networks":        task.DockerContainer.Networks,
					"networks_append": task.DockerContainer.NetworksAppend,
					"restart_policy":  task.DockerContainer.RestartPolicy,
					"auto_remove":     task.DockerContainer.AutoRemove,
					"privileged":      task.DockerContainer.Privileged,
					"user":            task.DockerContainer.User,
					"working_dir":     task.DockerContainer.WorkingDir,
					"hostname":        task.DockerContainer.Hostname,
					"domainname":      task.DockerContainer.Domainname,
					"labels":          task.DockerContainer.Labels,
					"links":           task.DockerContainer.Links,
					"log_driver":      task.DockerContainer.LogDriver,
					"log_options":     task.DockerContainer.LogOptions,
					// Tier 1 options
					"cap_add":     task.DockerContainer.CapAdd,
					"cap_drop":    task.DockerContainer.CapDrop,
					"devices":     task.DockerContainer.Devices,
					"healthcheck": task.DockerContainer.Healthcheck,
					"init":        task.DockerContainer.Init,
					"tmpfs":       task.DockerContainer.Tmpfs,
					"shm_size":    task.DockerContainer.ShmSize,
					// Tier 2 options
					"ulimits":      task.DockerContainer.Ulimits,
					"sysctls":      task.DockerContainer.Sysctls,
					"security_opt": task.DockerContainer.SecurityOpt,
					"cpus":         task.DockerContainer.CPUs,
					"memory":       task.DockerContainer.Memory,
					"memory_swap":  task.DockerContainer.MemorySwap,
					"pids_limit":   task.DockerContainer.PidsLimit,
					// Idempotency control
					"comparisons":  task.DockerContainer.Comparisons,
					"recreate":     task.DockerContainer.Recreate,
					"force_kill":   task.DockerContainer.ForceKill,
					"keep_volumes": task.DockerContainer.KeepVolumes,
					// Pull behavior
					"pull": task.DockerContainer.Pull,
					// Registry auth
					"registry_username": task.DockerContainer.RegistryUsername,
					"registry_password": task.DockerContainer.RegistryPassword,
					// Common options
					"docker_host":    task.DockerContainer.DockerHost,
					"tls":            task.DockerContainer.TLS,
					"validate_certs": task.DockerContainer.ValidateCerts,
					"ca_path":        task.DockerContainer.CAPath,
					"client_cert":    task.DockerContainer.ClientCert,
					"client_key":     task.DockerContainer.ClientKey,
					"api_version":    task.DockerContainer.APIVersion,
					"timeout":        task.DockerContainer.Timeout,
					"debug":          task.DockerContainer.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_container", Args: renderedArgs}

			case task.DockerImage != nil:
				args := map[string]interface{}{
					"name":              task.DockerImage.Name,
					"tag":               task.DockerImage.Tag,
					"repository":        task.DockerImage.Repository,
					"state":             task.DockerImage.State,
					"source":            task.DockerImage.Source,
					"push":              task.DockerImage.Push,
					"archive_path":      task.DockerImage.ArchivePath,
					"dockerfile":        task.DockerImage.DockerFile,
					"build.path":        task.DockerImage.BuildPath,
					"keep_image":        task.DockerImage.KeepImage,
					"pull":              task.DockerImage.Pull,
					"force_pull":        task.DockerImage.ForcePull,
					"force_remove":      task.DockerImage.ForceRemove,
					"force_tag":         task.DockerImage.ForceTag,
					"force_source":      task.DockerImage.ForceSource,
					"registry_username": task.DockerImage.RegistryUsername,
					"registry_password": task.DockerImage.RegistryPassword,
					"docker_host":       task.DockerImage.DockerHost,
					"tls":               task.DockerImage.TLS,
					"validate_certs":    task.DockerImage.ValidateCerts,
					"ca_path":           task.DockerImage.CAPath,
					"client_cert":       task.DockerImage.ClientCert,
					"client_key":        task.DockerImage.ClientKey,
					"api_version":       task.DockerImage.APIVersion,
					"timeout":           task.DockerImage.Timeout,
					"debug":             task.DockerImage.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_image", Args: renderedArgs}

			case task.DockerNetwork != nil:
				// Convert config types to module request args
				ipamConfigs := []map[string]interface{}{}
				for _, cfg := range task.DockerNetwork.IPAMConfig {
					ipamCfg := map[string]interface{}{
						"subnet":   cfg.Subnet,
						"gateway":  cfg.Gateway,
						"ip_range": cfg.IPRange,
					}
					if len(cfg.AuxAddress) > 0 {
						ipamCfg["aux_address"] = cfg.AuxAddress
					}
					ipamConfigs = append(ipamConfigs, ipamCfg)
				}

				// Convert connected containers
				connected := []map[string]interface{}{}
				for _, c := range task.DockerNetwork.Connected {
					connCfg := map[string]interface{}{
						"name": c.Name,
					}
					if c.IPv4Address != "" {
						connCfg["ipv4_address"] = c.IPv4Address
					}
					if c.IPv6Address != "" {
						connCfg["ipv6_address"] = c.IPv6Address
					}
					if len(c.Aliases) > 0 {
						connCfg["aliases"] = c.Aliases
					}
					if len(c.Links) > 0 {
						connCfg["links"] = c.Links
					}
					if len(c.DriverOpts) > 0 {
						connCfg["driver_opts"] = c.DriverOpts
					}
					connected = append(connected, connCfg)
				}

				args := map[string]interface{}{
					"name":                task.DockerNetwork.Name,
					"state":               task.DockerNetwork.State,
					"driver":              task.DockerNetwork.Driver,
					"options":             task.DockerNetwork.Options,
					"ipam_config":         ipamConfigs,
					"labels":              task.DockerNetwork.Labels,
					"internal":            task.DockerNetwork.Internal,
					"attachable":          task.DockerNetwork.Attachable,
					"scope":               task.DockerNetwork.Scope,
					"force":               task.DockerNetwork.Force,
					"connected":           connected,
					"appends":             task.DockerNetwork.Appends,
					"enable_ipv6":         task.DockerNetwork.EnableIPv6,
					"config_only":         task.DockerNetwork.ConfigOnly,
					"config_from":         task.DockerNetwork.ConfigFrom,
					"ingress":             task.DockerNetwork.Ingress,
					"ipam_driver":         task.DockerNetwork.IPAMDriver,
					"ipam_driver_options": task.DockerNetwork.IPAMDriverOptions,
					"docker_host":         task.DockerNetwork.DockerHost,
					"tls":                 task.DockerNetwork.TLS,
					"validate_certs":      task.DockerNetwork.ValidateCerts,
					"ca_path":             task.DockerNetwork.CAPath,
					"client_cert":         task.DockerNetwork.ClientCert,
					"client_key":          task.DockerNetwork.ClientKey,
					"api_version":         task.DockerNetwork.APIVersion,
					"timeout":             task.DockerNetwork.Timeout,
					"debug":               task.DockerNetwork.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_network", Args: renderedArgs}

			case task.DockerVolume != nil:
				args := map[string]interface{}{
					"name":           task.DockerVolume.Name,
					"state":          task.DockerVolume.State,
					"driver":         task.DockerVolume.Driver,
					"driver_options": task.DockerVolume.DriverOptions,
					"labels":         task.DockerVolume.Labels,
					"recreate":       task.DockerVolume.Recreate,
					"force":          task.DockerVolume.Force,
					"docker_host":    task.DockerVolume.DockerHost,
					"tls":            task.DockerVolume.TLS,
					"validate_certs": task.DockerVolume.ValidateCerts,
					"ca_path":        task.DockerVolume.CAPath,
					"client_cert":    task.DockerVolume.ClientCert,
					"client_key":     task.DockerVolume.ClientKey,
					"api_version":    task.DockerVolume.APIVersion,
					"timeout":        task.DockerVolume.Timeout,
					"debug":          task.DockerVolume.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_volume", Args: renderedArgs}

			case task.DockerPrune != nil:
				args := map[string]interface{}{
					"containers":           task.DockerPrune.Containers,
					"containers_filters":   task.DockerPrune.ContainersFilters,
					"images":               task.DockerPrune.Images,
					"images_filters":       task.DockerPrune.ImagesFilters,
					"networks":             task.DockerPrune.Networks,
					"networks_filters":     task.DockerPrune.NetworksFilters,
					"volumes":              task.DockerPrune.Volumes,
					"volumes_filters":      task.DockerPrune.VolumesFilters,
					"builder":              task.DockerPrune.Builder,
					"builder_cache_all":    task.DockerPrune.BuilderCacheAll,
					"builder_cache_filters": task.DockerPrune.BuilderCacheFilters,
					"docker_host":          task.DockerPrune.DockerHost,
					"tls":                  task.DockerPrune.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_prune", Args: renderedArgs}

			case task.DockerLogin != nil:
				args := map[string]interface{}{
					"username":    task.DockerLogin.Username,
					"password":    task.DockerLogin.Password,
					"registry":    task.DockerLogin.Registry,
					"email":       task.DockerLogin.Email,
					"config_path": task.DockerLogin.ConfigPath,
					"state":       task.DockerLogin.State,
					"relogin":     task.DockerLogin.Relogin,
					"docker_host": task.DockerLogin.DockerHost,
					"tls":         task.DockerLogin.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_login", Args: renderedArgs}

			case task.DockerSwarm != nil:
				args := map[string]interface{}{
					"state":             task.DockerSwarm.State,
					"advertise_addr":    task.DockerSwarm.AdvertiseAddr,
					"listen_addr":       task.DockerSwarm.ListenAddr,
					"force_new_cluster": task.DockerSwarm.ForceNewCluster,
					"remote_addrs":      task.DockerSwarm.RemoteAddrs,
					"join_token":        task.DockerSwarm.JoinToken,
					"node_id":           task.DockerSwarm.NodeID,
					"force":             task.DockerSwarm.Force,
					"docker_host":       task.DockerSwarm.DockerHost,
					"tls":               task.DockerSwarm.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_swarm", Args: renderedArgs}

			case task.DockerSwarmService != nil:
				// Convert PortPublish params
				publishes := []map[string]interface{}{}
				for _, p := range task.DockerSwarmService.Publish {
					publishes = append(publishes, map[string]interface{}{
						"published_port": p.PublishedPort,
						"target_port":    p.TargetPort,
						"protocol":       p.Protocol,
						"mode":           p.Mode,
					})
				}

				args := map[string]interface{}{
					"name":           task.DockerSwarmService.Name,
					"image":          task.DockerSwarmService.Image,
					"state":          task.DockerSwarmService.State,
					"replicas":       task.DockerSwarmService.Replicas,
					"args":           task.DockerSwarmService.Args,
					"command":        task.DockerSwarmService.Command,
					"env":            task.DockerSwarmService.Env,
					"publish":        publishes,
					"networks":       task.DockerSwarmService.Networks,
					"labels":         task.DockerSwarmService.Labels,
					"limit_cpu":      task.DockerSwarmService.LimitCPU,
					"limit_memory":   task.DockerSwarmService.LimitMemory,
					"constraint":     task.DockerSwarmService.Constraint,
					"restart_policy": task.DockerSwarmService.RestartPolicy,
					"force_update":   task.DockerSwarmService.ForceUpdate,
					// Phase 6.1: Configs and Secrets
					"configs": task.DockerSwarmService.Configs,
					"secrets": task.DockerSwarmService.Secrets,
					// Phase 6.2: Update/Rollback config
					"update_delay":               task.DockerSwarmService.UpdateDelay,
					"update_parallelism":         task.DockerSwarmService.UpdateParallelism,
					"update_failure_action":      task.DockerSwarmService.UpdateFailureAction,
					"update_order":               task.DockerSwarmService.UpdateOrder,
					"update_monitor":             task.DockerSwarmService.UpdateMonitor,
					"max_failure_ratio":          task.DockerSwarmService.MaxFailureRatio,
					"rollback_delay":             task.DockerSwarmService.RollbackDelay,
					"rollback_parallelism":       task.DockerSwarmService.RollbackParallelism,
					"rollback_failure_action":    task.DockerSwarmService.RollbackFailureAction,
					"rollback_order":             task.DockerSwarmService.RollbackOrder,
					"rollback_monitor":           task.DockerSwarmService.RollbackMonitor,
					"rollback_max_failure_ratio": task.DockerSwarmService.RollbackMaxFailureRatio,
					// Phase 6.3: Additional options
					"healthcheck": task.DockerSwarmService.Healthcheck,
					"dns":         task.DockerSwarmService.DNS,
					"dns_search":  task.DockerSwarmService.DNSSearch,
					"dns_options": task.DockerSwarmService.DNSOptions,
					"hosts":       task.DockerSwarmService.Hosts,
					"mounts":      task.DockerSwarmService.Mounts,
					"docker_host": task.DockerSwarmService.DockerHost,
					"tls":         task.DockerSwarmService.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_swarm_service", Args: renderedArgs}

			case task.DockerNode != nil:
				args := map[string]interface{}{
					"hostname":         task.DockerNode.Hostname,
					"self":             task.DockerNode.Self,
					"availability":     task.DockerNode.Availability,
					"role":             task.DockerNode.Role,
					"labels":           task.DockerNode.Labels,
					"labels_state":     task.DockerNode.LabelsState,
					"labels_to_remove": task.DockerNode.LabelsToRemove,
					"docker_host":      task.DockerNode.DockerHost,
					"tls":              task.DockerNode.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_node", Args: renderedArgs}

			case task.DockerCompose != nil:
				args := map[string]interface{}{
					"project_src":    task.DockerCompose.ProjectSrc,
					"project_name":   task.DockerCompose.ProjectName,
					"files":          task.DockerCompose.Files,
					"state":          task.DockerCompose.State,
					"services":       task.DockerCompose.Services,
					"scale":          task.DockerCompose.Scale,
					"build":          task.DockerCompose.Build,
					"pull":           task.DockerCompose.Pull,
					"remove_orphans": task.DockerCompose.RemoveOrphans,
					"env":            task.DockerCompose.Env,
					"profiles":       task.DockerCompose.Profiles,
					"docker_host":    task.DockerCompose.DockerHost,
					"tls":            task.DockerCompose.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_compose", Args: renderedArgs}

			case task.DockerComposeV2Run != nil:
				args := map[string]interface{}{
					"project_src":       task.DockerComposeV2Run.ProjectSrc,
					"project_name":      task.DockerComposeV2Run.ProjectName,
					"files":             task.DockerComposeV2Run.Files,
					"service":           task.DockerComposeV2Run.Service,
					"argv":              task.DockerComposeV2Run.Argv,
					"command":           task.DockerComposeV2Run.Command,
					"build":             task.DockerComposeV2Run.Build,
					"cap_add":           task.DockerComposeV2Run.CapAdd,
					"cap_drop":          task.DockerComposeV2Run.CapDrop,
					"entrypoint":        task.DockerComposeV2Run.EntryPoint,
					"interactive":       task.DockerComposeV2Run.Interactive,
					"labels":            task.DockerComposeV2Run.Labels,
					"name":              task.DockerComposeV2Run.Name,
					"no_deps":           task.DockerComposeV2Run.NoDeps,
					"publish":           task.DockerComposeV2Run.Publish,
					"quiet_pull":        task.DockerComposeV2Run.QuietPull,
					"remove_orphans":    task.DockerComposeV2Run.RemoveOrphans,
					"cleanup":           task.DockerComposeV2Run.Cleanup,
					"service_ports":     task.DockerComposeV2Run.ServicePorts,
					"use_aliases":       task.DockerComposeV2Run.UseAliases,
					"volumes":           task.DockerComposeV2Run.Volumes,
					"chdir":             task.DockerComposeV2Run.Chdir,
					"detach":            task.DockerComposeV2Run.Detach,
					"user":              task.DockerComposeV2Run.User,
					"stdin":             task.DockerComposeV2Run.Stdin,
					"stdin_add_newline": task.DockerComposeV2Run.StdinAddNewline,
					"strip_empty_ends":  task.DockerComposeV2Run.StripEmptyEnds,
					"tty":               task.DockerComposeV2Run.TTY,
					"env":               task.DockerComposeV2Run.Env,
					"profiles":          task.DockerComposeV2Run.Profiles,
					"docker_host":       task.DockerComposeV2Run.DockerHost,
					"tls":               task.DockerComposeV2Run.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_compose_v2_run", Args: renderedArgs}

			case task.DockerSecret != nil:
				args := map[string]interface{}{
					"name":        task.DockerSecret.Name,
					"data":        task.DockerSecret.Data,
					"data_is_b64": task.DockerSecret.DataIsB64,
					"labels":      task.DockerSecret.Labels,
					"force":       task.DockerSecret.Force,
					"state":       task.DockerSecret.State,
					"docker_host": task.DockerSecret.DockerHost,
					"tls":         task.DockerSecret.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_secret", Args: renderedArgs}

			case task.DockerConfig != nil:
				args := map[string]interface{}{
					"name":        task.DockerConfig.Name,
					"data":        task.DockerConfig.Data,
					"data_is_b64": task.DockerConfig.DataIsB64,
					"labels":      task.DockerConfig.Labels,
					"force":       task.DockerConfig.Force,
					"state":       task.DockerConfig.State,
					"docker_host": task.DockerConfig.DockerHost,
					"tls":         task.DockerConfig.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_config", Args: renderedArgs}

			case task.DockerStack != nil:
				args := map[string]interface{}{
					"name":               task.DockerStack.Name,
					"compose_file":       task.DockerStack.ComposeFile,
					"state":              task.DockerStack.State,
					"with_registry_auth": task.DockerStack.WithRegistryAuth,
					"prune":              task.DockerStack.Prune,
					"resolve_image":      task.DockerStack.ResolveImage,
					"docker_host":        task.DockerStack.DockerHost,
					"tls":                task.DockerStack.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_stack", Args: renderedArgs}

			case task.DockerContainerExec != nil:
				args := map[string]interface{}{
					"container":         task.DockerContainerExec.Container,
					"argv":              task.DockerContainerExec.Argv,
					"command":           task.DockerContainerExec.Command,
					"chdir":             task.DockerContainerExec.Chdir,
					"detach":            task.DockerContainerExec.Detach,
					"user":              task.DockerContainerExec.User,
					"stdin":             task.DockerContainerExec.Stdin,
					"stdin_add_newline": task.DockerContainerExec.StdinAddNewline,
					"strip_empty_ends":  task.DockerContainerExec.StripEmptyEnds,
					"tty":               task.DockerContainerExec.TTY,
					"env":               task.DockerContainerExec.Env,
					"docker_host":       task.DockerContainerExec.DockerHost,
					"tls":               task.DockerContainerExec.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_container_exec", Args: renderedArgs}
			case task.DockerContainerCopyInto != nil:
				args := map[string]interface{}{
					"container":      task.DockerContainerCopyInto.Container,
					"path":           task.DockerContainerCopyInto.Path,
					"content":        task.DockerContainerCopyInto.Content,
					"content_is_b64": task.DockerContainerCopyInto.ContentIsB64,
					"container_path": task.DockerContainerCopyInto.ContainerPath,
					"follow":         task.DockerContainerCopyInto.Follow,
					"local_follow":   task.DockerContainerCopyInto.LocalFollow,
					"owner_id":       task.DockerContainerCopyInto.OwnerID,
					"group_id":       task.DockerContainerCopyInto.GroupID,
					"mode":           task.DockerContainerCopyInto.Mode,
					"force":          task.DockerContainerCopyInto.Force,
					"docker_host":    task.DockerContainerCopyInto.DockerHost,
					"tls":            task.DockerContainerCopyInto.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_container_copy_into", Args: renderedArgs}

			case task.DockerImageBuild != nil:
				args := map[string]interface{}{
					"name":        task.DockerImageBuild.Name,
					"tag":         task.DockerImageBuild.Tag,
					"path":        task.DockerImageBuild.Path,
					"dockerfile":  task.DockerImageBuild.Dockerfile,
					"cache_from":  task.DockerImageBuild.CacheFrom,
					"pull":        task.DockerImageBuild.Pull,
					"network":     task.DockerImageBuild.Network,
					"nocache":     task.DockerImageBuild.NoCache,
					"etc_hosts":   task.DockerImageBuild.EtcHosts,
					"args":        task.DockerImageBuild.Args,
					"target":      task.DockerImageBuild.Target,
					"platform":    task.DockerImageBuild.Platform,
					"shm_size":    task.DockerImageBuild.ShmSize,
					"labels":      task.DockerImageBuild.Labels,
					"rebuild":     task.DockerImageBuild.Rebuild,
					"push":        task.DockerImageBuild.Push,
					"docker_host": task.DockerImageBuild.DockerHost,
					"tls":         task.DockerImageBuild.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_image_build", Args: renderedArgs}

			case task.DockerImageLoad != nil:
				args := map[string]interface{}{
					"path":        task.DockerImageLoad.Path,
					"docker_host": task.DockerImageLoad.DockerHost,
					"tls":         task.DockerImageLoad.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_image_load", Args: renderedArgs}

			case task.DockerImageExport != nil:
				// Handle names/name alias
				names := task.DockerImageExport.Names
				if len(names) == 0 && task.DockerImageExport.Name != "" {
					names = []string{task.DockerImageExport.Name}
				}
				args := map[string]interface{}{
					"names":       names,
					"tag":         task.DockerImageExport.Tag,
					"path":        task.DockerImageExport.Path,
					"force":       task.DockerImageExport.Force,
					"docker_host": task.DockerImageExport.DockerHost,
					"tls":         task.DockerImageExport.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_image_export", Args: renderedArgs}

			case task.DockerContainerInfo != nil:
				args := map[string]interface{}{
					"name":           task.DockerContainerInfo.Name,
					"docker_host":    task.DockerContainerInfo.DockerHost,
					"tls":            task.DockerContainerInfo.TLS,
					"validate_certs": task.DockerContainerInfo.ValidateCerts,
					"ca_path":        task.DockerContainerInfo.CAPath,
					"client_cert":    task.DockerContainerInfo.ClientCert,
					"client_key":     task.DockerContainerInfo.ClientKey,
					"api_version":    task.DockerContainerInfo.APIVersion,
					"timeout":        task.DockerContainerInfo.Timeout,
					"debug":          task.DockerContainerInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_container_info", Args: renderedArgs}

			case task.DockerImageInfo != nil:
				args := map[string]interface{}{
					"name":           task.DockerImageInfo.Name,
					"docker_host":    task.DockerImageInfo.DockerHost,
					"tls":            task.DockerImageInfo.TLS,
					"validate_certs": task.DockerImageInfo.ValidateCerts,
					"ca_path":        task.DockerImageInfo.CAPath,
					"client_cert":    task.DockerImageInfo.ClientCert,
					"client_key":     task.DockerImageInfo.ClientKey,
					"api_version":    task.DockerImageInfo.APIVersion,
					"timeout":        task.DockerImageInfo.Timeout,
					"debug":          task.DockerImageInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_image_info", Args: renderedArgs}

			case task.DockerNetworkInfo != nil:
				args := map[string]interface{}{
					"name":           task.DockerNetworkInfo.Name,
					"docker_host":    task.DockerNetworkInfo.DockerHost,
					"tls":            task.DockerNetworkInfo.TLS,
					"validate_certs": task.DockerNetworkInfo.ValidateCerts,
					"ca_path":        task.DockerNetworkInfo.CAPath,
					"client_cert":    task.DockerNetworkInfo.ClientCert,
					"client_key":     task.DockerNetworkInfo.ClientKey,
					"api_version":    task.DockerNetworkInfo.APIVersion,
					"timeout":        task.DockerNetworkInfo.Timeout,
					"debug":          task.DockerNetworkInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_network_info", Args: renderedArgs}

			case task.DockerVolumeInfo != nil:
				args := map[string]interface{}{
					"name":           task.DockerVolumeInfo.Name,
					"docker_host":    task.DockerVolumeInfo.DockerHost,
					"tls":            task.DockerVolumeInfo.TLS,
					"validate_certs": task.DockerVolumeInfo.ValidateCerts,
					"ca_path":        task.DockerVolumeInfo.CAPath,
					"client_cert":    task.DockerVolumeInfo.ClientCert,
					"client_key":     task.DockerVolumeInfo.ClientKey,
					"api_version":    task.DockerVolumeInfo.APIVersion,
					"timeout":        task.DockerVolumeInfo.Timeout,
					"debug":          task.DockerVolumeInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_volume_info", Args: renderedArgs}

			case task.DockerHostInfo != nil:
				args := map[string]interface{}{
					"containers":     task.DockerHostInfo.Containers,
					"images":         task.DockerHostInfo.Images,
					"volumes":        task.DockerHostInfo.Volumes,
					"disk_usage":     task.DockerHostInfo.DiskUsage,
					"docker_host":    task.DockerHostInfo.DockerHost,
					"tls":            task.DockerHostInfo.TLS,
					"validate_certs": task.DockerHostInfo.ValidateCerts,
					"ca_path":        task.DockerHostInfo.CAPath,
					"client_cert":    task.DockerHostInfo.ClientCert,
					"client_key":     task.DockerHostInfo.ClientKey,
					"api_version":    task.DockerHostInfo.APIVersion,
					"timeout":        task.DockerHostInfo.Timeout,
					"debug":          task.DockerHostInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_host_info", Args: renderedArgs}

			case task.DockerSwarmInfo != nil:
				args := map[string]interface{}{
					"nodes":          task.DockerSwarmInfo.Nodes,
					"verbose":        task.DockerSwarmInfo.Verbose,
					"docker_host":    task.DockerSwarmInfo.DockerHost,
					"tls":            task.DockerSwarmInfo.TLS,
					"validate_certs": task.DockerSwarmInfo.ValidateCerts,
					"ca_path":        task.DockerSwarmInfo.CAPath,
					"client_cert":    task.DockerSwarmInfo.ClientCert,
					"client_key":     task.DockerSwarmInfo.ClientKey,
					"api_version":    task.DockerSwarmInfo.APIVersion,
					"timeout":        task.DockerSwarmInfo.Timeout,
					"debug":          task.DockerSwarmInfo.Debug,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_swarm_info", Args: renderedArgs}

			case task.DockerSwarmServiceInfo != nil:
				args := map[string]interface{}{
					"name":        task.DockerSwarmServiceInfo.Name,
					"docker_host": task.DockerSwarmServiceInfo.DockerHost,
					"tls":         task.DockerSwarmServiceInfo.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_swarm_service_info", Args: renderedArgs}

			case task.DockerNodeInfo != nil:
				args := map[string]interface{}{
					"name":        task.DockerNodeInfo.Name,
					"self":        task.DockerNodeInfo.Self,
					"docker_host": task.DockerNodeInfo.DockerHost,
					"tls":         task.DockerNodeInfo.TLS,
				}
				renderedArgs, err := renderArgs(args, flattened)

				if err != nil {

					fmt.Printf("    ✗ Failed to render args: %v\n", err)

					continue

				}

				modReq = ModuleRequest{Module: "docker_node_info", Args: renderedArgs}

			case task.Reboot != nil:
				renderedReboot, err := renderRebootParams(task.Reboot, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render reboot params: %v\n", err)
					continue
				}
				resp := executeReboot(client, remoteAgentPath, host, renderedReboot, *verbose)
				printResponse(resp, *verbose)
				if task.Register != "" {
					result := genericResponseToMap(resp)
					result["rebooted"] = !resp.Failed
					registerResult(hostRuntimeVars, host.Name, task, result)
				}
				continue

			case task.IncludeTasks != nil:
				filePath := task.IncludeTasks.File
				if filePath == "" {
					fmt.Println("    ✗ include_tasks: file path is required")
					continue
				}

				renderedPath, err := vars.RenderString(filePath, flattened)
				if err != nil {
					fmt.Printf("    ✗ Failed to render include_tasks path: %v\n", err)
					continue
				}

				resolveDir := baseDir
				if task.SourceDir != "" {
					resolveDir = task.SourceDir
				}

				if !filepath.IsAbs(renderedPath) {
					renderedPath = filepath.Join(resolveDir, renderedPath)
				}
				renderedPath = filepath.Clean(renderedPath)

				data, err := os.ReadFile(renderedPath)
				if err != nil {
					fmt.Printf("    ✗ include_tasks: failed to read %q: %v\n", renderedPath, err)
					continue
				}

				var includedTasks []config.Task
				dec := yaml.NewDecoder(bytes.NewReader(data))
				if err := dec.Decode(&includedTasks); err != nil {
					fmt.Printf("    ✗ include_tasks: failed to parse %q: %v\n", renderedPath, err)
					continue
				}

				includeBaseDir := filepath.Dir(renderedPath)
				for i := range includedTasks {
					includedTasks[i].SourceDir = includeBaseDir
				}

				if len(task.Vars) > 0 {
					for i := range includedTasks {
						if includedTasks[i].Vars == nil {
							includedTasks[i].Vars = make(map[string]interface{})
						}
						merged := make(map[string]interface{})
						for k, v := range task.Vars {
							merged[k] = v
						}
						for k, v := range includedTasks[i].Vars {
							merged[k] = v
						}
						includedTasks[i].Vars = merged
					}
				}

				includedTasks, err = config.ExpandImportTasks(includedTasks, includeBaseDir, renderImportPath)
				if err != nil {
					fmt.Printf("    ✗ include_tasks: failed to expand nested imports in %q: %v\n", renderedPath, err)
					continue
				}

				tail := make([]config.Task, len(taskQueue[taskIdx+1:]))
				copy(tail, taskQueue[taskIdx+1:])
				taskQueue = append(taskQueue[:taskIdx+1], includedTasks...)
				taskQueue = append(taskQueue, tail...)

				fmt.Printf("    ✓ included %d task(s) from %s\n", len(includedTasks), filepath.Base(renderedPath))
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
				if task.Register != "" {
					registerResult(hostRuntimeVars, host.Name, task, map[string]interface{}{
						"unreachable": true,
						"failed":      true,
						"msg":         fmt.Sprintf("Execution failed: %v", err),
					})
				}
				continue
			}

			outputStr := strings.TrimSpace(string(output))
			lines := strings.Split(outputStr, "\n")
			jsonOutput := lines[len(lines)-1]

			var resp GenericResponse
			if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
				fmt.Printf("    ✗ Failed to parse response: %v\n", err)
				fmt.Printf("    Raw output: %s\n", outputStr)
				if task.Register != "" {
					registerResult(hostRuntimeVars, host.Name, task, map[string]interface{}{
						"failed": true,
						"msg":    fmt.Sprintf("Failed to parse response: %v", err),
					})
				}
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

			if task.Register != "" {
				var fullResult map[string]interface{}
				if err := json.Unmarshal([]byte(jsonOutput), &fullResult); err != nil {
					fullResult = genericResponseToMap(resp)
				}
				registerResult(hostRuntimeVars, host.Name, task, fullResult)
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

func normalizeNewlines(input string, sequence string) string {
	if sequence == "" {
		return input
	}
	cleaned := strings.Trim(sequence, "\"'")
	if cleaned == "\\r\\n" {
		cleaned = "\r\n"
	} else if cleaned == "\\r" {
		cleaned = "\r"
	} else if cleaned == "\\n" {
		cleaned = "\n"
	}
	cleaned = strings.ReplaceAll(cleaned, "\\r", "\r")
	cleaned = strings.ReplaceAll(cleaned, "\\n", "\n")
	if cleaned == "" {
		cleaned = "\n"
	}
	base := strings.ReplaceAll(input, "\r\n", "\n")
	base = strings.ReplaceAll(base, "\r", "\n")
	if cleaned == "\n" {
		return base
	}
	return strings.ReplaceAll(base, "\n", cleaned)
}

var validVarNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateVarName(name string) error {
	if !validVarNameRE.MatchString(name) {
		return fmt.Errorf("invalid variable name %q: must start with a letter or underscore and contain only letters, digits, and underscores", name)
	}
	return nil
}

func genericResponseToMap(resp GenericResponse) map[string]interface{} {
	result := map[string]interface{}{
		"changed": resp.Changed,
		"failed":  resp.Failed,
		"rc":      resp.RC,
		"msg":     resp.Msg,
		"stdout":  resp.Stdout,
		"stderr":  resp.Stderr,
	}
	result["stdout_lines"] = splitToInterfaceSlice(resp.Stdout)
	result["stderr_lines"] = splitToInterfaceSlice(resp.Stderr)
	return result
}

func splitToInterfaceSlice(s string) []interface{} {
	if s == "" {
		return []interface{}{}
	}
	parts := strings.Split(s, "\n")
	out := make([]interface{}, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

func normalizeRegisteredResult(result map[string]interface{}) map[string]interface{} {
	if _, ok := result["changed"]; !ok {
		result["changed"] = false
	}
	if _, ok := result["failed"]; !ok {
		result["failed"] = false
	}
	if _, ok := result["skipped"]; !ok {
		result["skipped"] = false
	}
	if _, ok := result["msg"]; !ok {
		result["msg"] = ""
	}
	if _, ok := result["stdout"]; !ok {
		result["stdout"] = ""
	}
	if _, ok := result["stderr"]; !ok {
		result["stderr"] = ""
	}
	if _, ok := result["rc"]; !ok {
		result["rc"] = 0
	}
	if rc, ok := result["rc"].(float64); ok {
		result["rc"] = int(rc)
	}
	if _, ok := result["attempts"]; !ok {
		result["attempts"] = 1
	}
	if _, ok := result["retries"]; !ok {
		result["retries"] = 0
	}
	if _, ok := result["stdout_lines"]; !ok {
		stdout, _ := result["stdout"].(string)
		result["stdout_lines"] = splitToInterfaceSlice(stdout)
	}
	if _, ok := result["stderr_lines"]; !ok {
		stderr, _ := result["stderr"].(string)
		result["stderr_lines"] = splitToInterfaceSlice(stderr)
	}
	return result
}

func computeFetchDest(params *config.FetchParams, hostName string) string {
	if params.Flat {
		dest := params.Dest
		if strings.HasSuffix(params.Dest, "/") || isDir(params.Dest) {
			dest = filepath.Join(params.Dest, filepath.Base(params.Src))
		}
		return dest
	}
	return filepath.Join(params.Dest, hostName, params.Src)
}

func registerResult(hostRuntimeVars map[string]map[string]interface{}, hostName string, task config.Task, result map[string]interface{}) {
	if task.Register == "" {
		return
	}
	hostRuntimeVars[hostName][task.Register] = normalizeRegisteredResult(result)
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

func parseExtraVars(raw string) (map[string]interface{}, error) {
	varsMap := map[string]interface{}{}
	if strings.TrimSpace(raw) == "" {
		return varsMap, nil
	}

	if strings.HasPrefix(raw, "@") {
		path := strings.TrimPrefix(raw, "@")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read extra vars file: %w", err)
		}
		if err := yaml.Unmarshal(data, &varsMap); err != nil {
			return nil, fmt.Errorf("failed to parse extra vars file: %w", err)
		}
		return varsMap, nil
	}

	pairs := strings.Split(raw, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid extra var %q", pair)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid extra var %q", pair)
		}
		varsMap[key] = value
	}

	return varsMap, nil
}

func renderArgs(args map[string]interface{}, context map[string]interface{}) (map[string]interface{}, error) {
	rendered, err := vars.RenderValue(args, context)
	if err != nil {
		return nil, fmt.Errorf("template rendering failed: %w", err)
	}
	renderedMap, ok := rendered.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected render result type: %T", rendered)
	}
	return renderedMap, nil
}

func renderFetchParams(params *config.FetchParams, context map[string]interface{}) (*config.FetchParams, error) {
	rendered, err := vars.RenderValue(map[string]interface{}{
		"src":  params.Src,
		"dest": params.Dest,
		"flat": params.Flat,
	}, context)
	if err != nil {
		return nil, err
	}
	data, ok := rendered.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid fetch params")
	}
	out := *params
	if src, ok := data["src"].(string); ok {
		out.Src = src
	}
	if dest, ok := data["dest"].(string); ok {
		out.Dest = dest
	}
	return &out, nil
}

func renderScriptParams(params *config.ScriptParams, context map[string]interface{}) (*config.ScriptParams, error) {
	rendered, err := vars.RenderValue(map[string]interface{}{
		"cmd":        params.Cmd,
		"chdir":      params.Chdir,
		"creates":    params.Creates,
		"removes":    params.Removes,
		"executable": params.Executable,
	}, context)
	if err != nil {
		return nil, err
	}
	data, ok := rendered.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid script params")
	}
	out := *params
	if cmd, ok := data["cmd"].(string); ok {
		out.Cmd = cmd
	}
	if chdir, ok := data["chdir"].(string); ok {
		out.Chdir = chdir
	}
	if creates, ok := data["creates"].(string); ok {
		out.Creates = creates
	}
	if removes, ok := data["removes"].(string); ok {
		out.Removes = removes
	}
	if executable, ok := data["executable"].(string); ok {
		out.Executable = executable
	}
	return &out, nil
}

func renderRebootParams(params *config.RebootParams, context map[string]interface{}) (*config.RebootParams, error) {
	rendered, err := vars.RenderValue(map[string]interface{}{
		"test_command":      params.TestCommand,
		"msg":               params.Msg,
		"boot_time_command": params.BootTimeCommand,
		"reboot_command":    params.RebootCommand,
	}, context)
	if err != nil {
		return nil, err
	}
	data, ok := rendered.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid reboot params")
	}
	out := *params
	if testCommand, ok := data["test_command"].(string); ok {
		out.TestCommand = testCommand
	}
	if msg, ok := data["msg"].(string); ok {
		out.Msg = msg
	}
	if boot, ok := data["boot_time_command"].(string); ok {
		out.BootTimeCommand = boot
	}
	if rebootCmd, ok := data["reboot_command"].(string); ok {
		out.RebootCommand = rebootCmd
	}
	return &out, nil
}

func buildHostvarsForTask(hostInfos []vars.HostInfo, resolver vars.Resolver, inventory vars.InventoryVars, playVars map[string]interface{}, taskVars map[string]interface{}, extraVars map[string]interface{}, runtimeVars map[string]map[string]interface{}, groups map[string][]string) (map[string]map[string]interface{}, error) {
	hostvars := map[string]map[string]interface{}{}
	for _, hostInfo := range hostInfos {
		resolved, err := resolver.ResolveHostVars(vars.ResolveRequest{
			Host:          hostInfo,
			InventoryVars: inventory,
			PlayVars:      playVars,
			TaskVars:      taskVars,
			ExtraVars:     extraVars,
			RuntimeVars:   runtimeVars[hostInfo.Name],
		})
		if err != nil {
			return nil, err
		}
		baseContext := vars.BuildContext(hostInfo.Name, hostInfo.Groups, resolved, nil, groups)
		hostvars[hostInfo.Name] = baseContext
	}
	return hostvars, nil
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

func executeScript(client *ssh.Client, agentPath string, params *config.ScriptParams, verbose bool) (GenericResponse, string) {
	if params.Cmd == "" {
		return GenericResponse{Failed: true, Msg: "no script path specified (cmd parameter required)"}, ""
	}

	scriptPath, scriptArgs := parseScriptCmd(params.Cmd)

	localFile, err := os.Stat(scriptPath)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to stat local script: %v", err)}, ""
	}
	if localFile.IsDir() {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("script path is a directory: %s", scriptPath)}, ""
	}

	localChecksum, err := sha1File(scriptPath)
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to compute script checksum: %v", err)}, ""
	}

	remoteTmpPath := fmt.Sprintf("/tmp/.dibra-script-%s", localChecksum[:12])

	if verbose {
		fmt.Printf("    Uploading script %s -> %s\n", scriptPath, remoteTmpPath)
	}

	if err := client.UploadFile(scriptPath, remoteTmpPath); err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to upload script: %v", err)}, ""
	}

	_, _, err = client.RunWithSudo(fmt.Sprintf("chmod +x %s", remoteTmpPath))
	if err != nil {
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to chmod script: %v", err)}, ""
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
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to execute script: %v", err)}, ""
	}

	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	jsonOutput := lines[len(lines)-1]

	var resp GenericResponse
	if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
		cleanupScript(client, remoteTmpPath)
		return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to parse script response: %v", err)}, ""
	}

	cleanupScript(client, remoteTmpPath)

	return resp, jsonOutput
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
	_, _, _ = client.Run(fmt.Sprintf("rm -f %s", remotePath))
}

func executeReboot(client *ssh.Client, agentPath string, host config.Host, params *config.RebootParams, verbose bool) GenericResponse {
	startTime := time.Now()

	rebootTimeout := params.RebootTimeout
	if rebootTimeout == 0 {
		rebootTimeout = 600
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
		msg = "Reboot initiated by dibra"
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
