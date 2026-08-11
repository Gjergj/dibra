package controller

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gjergjiramku/dibra/internal/agent"
	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/inventory"
	"github.com/gjergjiramku/dibra/internal/modules/registry"
	"github.com/gjergjiramku/dibra/internal/secrets"
	"github.com/gjergjiramku/dibra/internal/secrets/bitwarden"
	"github.com/gjergjiramku/dibra/internal/secrets/onepassword"
	"github.com/gjergjiramku/dibra/internal/ssh"
	"github.com/gjergjiramku/dibra/internal/template"
	"github.com/gjergjiramku/dibra/internal/vars"
	"gopkg.in/yaml.v3"
)

const (
	remoteAgentDir  = "/tmp"
	remoteAgentName = ".dibra-agent"
)

type ModuleRequest = execution.ModuleRequest[interface{}]

type GenericResponse struct {
	Changed  bool     `json:"changed"`
	Failed   bool     `json:"failed"`
	Skipped  bool     `json:"skipped"`
	Rebooted bool     `json:"rebooted,omitempty"`
	Msg      string   `json:"msg,omitempty"`
	RC       int      `json:"rc"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Packages []string `json:"packages,omitempty"`
	KeyID    string   `json:"key_id,omitempty"`
	Filename string   `json:"filename,omitempty"`
}

type ExecutionClient interface {
	Close() error
	Reconnect() (ExecutionClient, error)
	UploadFile(localPath, remotePath string) error
	FileExists(path string) (bool, error)
	ExecuteAgent(agentPath string, input []byte) ([]byte, error)
	Run(command string) (string, string, error)
	RunWithSudo(command string) (string, string, error)
	DownloadFile(sourcePath, destinationPath string) error
	IsLocal() bool
}

type RunOptions struct {
	ConfigPath       string
	InventoryPath    string
	ExtraVars        string
	Validate         bool
	ForceAgentUpload bool
	Verbose          bool
	CheckMode        bool
	DiffMode         bool
	AgentMode        agent.Mode
	AgentPath        string
	Version          string
	ProjectRoot      string
	Local            bool
	LocalAgentPath   string
	WorkingDir       string
	AllowReboot      bool
	Stdout           io.Writer
	Stderr           io.Writer
}

type RunResult struct {
	Failed             bool
	RebootInitiated    bool
	CompletedTaskCount int
}

var runOutput = struct {
	sync.Mutex
	stdout io.Writer
	stderr io.Writer
}{stdout: os.Stdout, stderr: os.Stderr}

func printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(runOutput.stdout, format, args...)
}

func println(args ...interface{}) {
	_, _ = fmt.Fprintln(runOutput.stdout, args...)
}

func Run(ctx context.Context, opts RunOptions) (RunResult, error) {
	runOutput.Lock()
	previousStdout, previousStderr := runOutput.stdout, runOutput.stderr
	if opts.Stdout != nil {
		runOutput.stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		runOutput.stderr = opts.Stderr
	}
	defer func() {
		runOutput.stdout, runOutput.stderr = previousStdout, previousStderr
		runOutput.Unlock()
	}()

	var runResult RunResult
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "playbook.yaml"
	}
	if opts.AgentMode == agent.ModePath && opts.AgentPath == "" && !opts.Local {
		return runResult, fmt.Errorf("--agent-path requires a path")
	}
	if opts.WorkingDir != "" {
		previousDir, err := os.Getwd()
		if err != nil {
			return runResult, fmt.Errorf("Failed to determine working directory: %v", err)
		}
		if err := os.Chdir(opts.WorkingDir); err != nil {
			return runResult, fmt.Errorf("Failed to change working directory: %v", err)
		}
		defer func() { _ = os.Chdir(previousDir) }()
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return runResult, fmt.Errorf("Failed to load config: %v", err)
	}

	extraVarsMap, err := parseExtraVars(opts.ExtraVars)
	if err != nil {
		return runResult, fmt.Errorf("Failed to parse extra vars: %v", err)
	}

	invPath := opts.InventoryPath
	if !opts.Local && invPath == "" && cfg.Inventory != "" {
		invPath = cfg.Inventory
		if !filepath.IsAbs(invPath) {
			baseDir := filepath.Dir(opts.ConfigPath)
			if info, statErr := os.Stat(opts.ConfigPath); statErr == nil && info.IsDir() {
				baseDir = opts.ConfigPath
			}
			invPath = filepath.Join(baseDir, invPath)
		}
	}
	if opts.Local {
		invPath = ""
		cfg.Inventory = ""
		cfg.Hosts = []config.Host{{Name: "localhost", Host: "localhost"}}
	}

	var inv *inventory.Inventory
	if invPath != "" {
		if len(cfg.Hosts) > 0 {
			return runResult, fmt.Errorf("Cannot use both inventory (-i) and playbook hosts; remove one")
		}
		inv, err = inventory.Load(invPath)
		if err != nil {
			return runResult, fmt.Errorf("Failed to load inventory: %v", err)
		}

		secretsResolver := secrets.NewResolver()
		secretsResolver.Register("bw", bitwarden.NewProvider())
		secretsResolver.Register("op", onepassword.NewProvider())
		if err := inv.ResolveSecrets(secretsResolver); err != nil {
			return runResult, fmt.Errorf("Failed to resolve inventory secrets: %v", err)
		}

		if err := inv.ResolveTemplates(); err != nil {
			return runResult, fmt.Errorf("Failed to resolve inventory templates: %v", err)
		}

		cfg.Hosts, err = inv.HostsAsConfig()
		if err != nil {
			return runResult, fmt.Errorf("Failed to convert inventory hosts: %v", err)
		}
	}

	if opts.Validate {
		printValidationSummary(opts.ConfigPath, cfg, inv)
		return runResult, nil
	}

	agentResolver := agent.NewResolver(agent.Options{
		Mode:        opts.AgentMode,
		AgentPath:   opts.AgentPath,
		Version:     opts.Version,
		ProjectRoot: opts.ProjectRoot,
	})

	hostInfos := make([]vars.HostInfo, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		hostInfos = append(hostInfos, vars.HostInfo{Name: host.Name, Groups: host.Groups})
	}

	baseDir := filepath.Dir(opts.ConfigPath)
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
		return runResult, fmt.Errorf("Failed to load inventory vars: %v", err)
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
			return runResult, fmt.Errorf("Failed to load vars_files: %v", err)
		}
		playVars = vars.MergeMaps(playVars, varsFromFiles, vars.MergeStrategy(cfg.VarsMerge))
	}

	{
		sr := secrets.NewResolver()
		sr.Register("bw", bitwarden.NewProvider())
		sr.Register("op", onepassword.NewProvider())
		resolved, err := sr.ResolveMap(playVars)
		if err != nil {
			return runResult, fmt.Errorf("Failed to resolve secrets in play vars: %v", err)
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
		return runResult, fmt.Errorf("Failed to expand import_tasks: %v", err)
	}
	globalExecutionState := execution.State{CheckMode: opts.CheckMode, DiffMode: opts.DiffMode}

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
		if err := ctx.Err(); err != nil {
			return runResult, err
		}
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

		printf("\n=== Host: %s (%s) ===\n", host.Name, host.Host)

		var client ExecutionClient
		agentExecutionPath := filepath.Join(remoteAgentDir, remoteAgentName)
		if opts.Local {
			if opts.LocalAgentPath == "" {
				return runResult, fmt.Errorf("local agent path is required")
			}
			client = NewLocalClient(ctx)
			agentExecutionPath = opts.LocalAgentPath
			printf("  Local agent: %s\n", agentExecutionPath)
		} else {
			sshConfig := ssh.Config{
				Host:           host.Host,
				Port:           host.Port,
				User:           host.User,
				Password:       host.Password,
				SSHKeyPath:     host.SSHKeyPath,
				Become:         host.Become,
				BecomePassword: host.BecomePassword,
				Verbose:        opts.Verbose,
			}
			sshClient, connectErr := ssh.Connect(sshConfig)
			if connectErr != nil {
				printf("  ✗ Failed to connect: %v\n", connectErr)
				runResult.Failed = true
				continue
			}
			client = newSSHExecutionClient(sshClient, sshConfig)

			println("  Resolving agent binary...")
			agentBinary, resolveErr := agentResolver.Resolve(sshClient)
			if resolveErr != nil {
				printf("  ✗ Failed to resolve agent: %v\n", resolveErr)
				_ = client.Close()
				runResult.Failed = true
				continue
			}
			if opts.Verbose {
				printf("  Agent binary: %s\n", agentBinary)
			}

			needsUpload := opts.ForceAgentUpload
			if !needsUpload {
				exists, _ := client.FileExists(agentExecutionPath)
				if !exists {
					needsUpload = true
				} else {
					remoteVersion, versionErr := agent.CheckRemoteAgentVersion(sshClient, agentExecutionPath)
					if versionErr != nil {
						needsUpload = true
						if opts.Verbose {
							printf("  Could not check remote agent version: %v\n", versionErr)
						}
					} else {
						localVersion := strings.TrimPrefix(opts.Version, "v")
						if remoteVersion != localVersion {
							needsUpload = true
							if opts.Verbose {
								printf("  Remote agent version %q != local %q, uploading\n", remoteVersion, localVersion)
							}
						}
					}
				}
			}

			if needsUpload {
				println("  Uploading agent...")
				if host.Become && host.User != "root" {
					_, _, removeErr := client.RunWithSudo(fmt.Sprintf("rm -f %s", agentExecutionPath))
					if removeErr != nil {
						printf("  ✗ Failed to remove agent: %v\n", removeErr)
						_ = client.Close()
						runResult.Failed = true
						continue
					}
				}
				if uploadErr := client.UploadFile(agentBinary, agentExecutionPath); uploadErr != nil {
					printf("  ✗ Failed to upload agent: %v\n", uploadErr)
					_ = client.Close()
					runResult.Failed = true
					continue
				}
				_, _, chmodErr := client.RunWithSudo(fmt.Sprintf("chmod +x %s", agentExecutionPath))
				if chmodErr != nil {
					printf("  ✗ Failed to chmod agent: %v\n", chmodErr)
					_ = client.Close()
					runResult.Failed = true
					continue
				}
			} else {
				println("  Agent up-to-date on remote")
			}
		}
		defer client.Close()

		taskQueue := make([]config.Task, len(cfg.Tasks))
		copy(taskQueue, cfg.Tasks)

		for taskIdx := 0; taskIdx < len(taskQueue); taskIdx++ {
			if err := ctx.Err(); err != nil {
				return runResult, err
			}
			if opts.Local && countRebootTasks(taskQueue) > 1 {
				printf("    ✗ deployment may contain at most one local reboot task\n")
				runResult.Failed = true
				break
			}
			task := taskQueue[taskIdx]
			printf("  Task: %s\n", task.Name)
			if task.Module != nil && task.Module.DeprecationWarning != "" {
				printf("    ⚠ DEPRECATION: %s\n", task.Module.DeprecationWarning)
			}

			if task.Register != "" {
				if err := validateVarName(task.Register); err != nil {
					printf("    ✗ %v\n", err)
					runResult.Failed = true
					if opts.Local {
						break
					}
					continue
				}
			}

			taskHostvars, err := buildHostvarsForTask(hostInfos, varsResolver, inventoryVars, playVars, task.Vars, extraVarsMap, hostRuntimeVars, groupsMap)
			if err != nil {
				printf("    ✗ Failed to build hostvars: %v\n", err)
				runResult.Failed = true
				if opts.Local {
					break
				}
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
				printf("    ✗ Failed to resolve vars: %v\n", err)
				runResult.Failed = true
				if opts.Local {
					break
				}
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

			loopSpec, err := resolveLoopSpec(task, flattened)
			if err != nil {
				printf("    ✗ Failed to resolve loop: %v\n", err)
				runResult.Failed = true
				if opts.Local {
					break
				}
				continue
			}

			if loopSpec != nil {
				if task.IncludeTasks != nil {
					if err := includeTasksForLoop(task, loopSpec, flattened, baseDir, &taskQueue, taskIdx, renderImportPath); err != nil {
						printf("    ✗ include_tasks: %v\n", err)
						runResult.Failed = true
						if opts.Local {
							break
						}
					}
					continue
				}

				if len(loopSpec.Items) == 0 {
					printf("    ↷ SKIPPED (empty loop)\n")
					if task.Register != "" {
						registerResult(hostRuntimeVars, host.Name, task, map[string]interface{}{
							"changed": false,
							"skipped": true,
							"results": []interface{}{},
							"msg":     "empty loop",
						})
					}
					continue
				}

				loopResults := []interface{}{}
				loopChanged := false
				loopFailed := false
				loopSkippedAll := true

				for idx, item := range loopSpec.Items {
					loopInfo := buildLoopInfo(loopSpec.Items, idx, loopSpec.Extended)
					loopVars := buildLoopVars(loopSpec, item, idx, loopInfo)
					iterationContext := mergeContext(flattened, loopVars)
					iterationTask := task
					iterationTask.Loop = nil
					iterationTask.WithItems = nil
					iterationTask.WithList = nil
					iterationTask.WithDict = nil
					iterationTask.WithSequence = nil
					iterationTask.LoopControl = nil
					iterationTask.Vars = mergeTaskVars(task.Vars, loopVars)

					result, hasResult := executeTaskOnce(iterationTask, iterationContext, host, client, agentExecutionPath, baseDir, &taskQueue, taskIdx, renderImportPath, opts.Verbose, false, globalExecutionState)
					if hasResult {
						if iterationTask.GatherFacts != nil {
							applyGatheredFacts(hostRuntimeVars, host.Name, result)
						}
						result = attachLoopResult(result, loopSpec, item, idx, loopInfo)
						result = normalizeRegisteredResult(result)
						loopResults = append(loopResults, result)
						if changed, ok := result["changed"].(bool); ok && changed {
							loopChanged = true
						}
						if failed, ok := result["failed"].(bool); ok && failed {
							loopFailed = true
						}
						if skipped, ok := result["skipped"].(bool); ok && !skipped {
							loopSkippedAll = false
						}
					} else {
						loopSkippedAll = false
						if opts.Local {
							loopFailed = true
						}
					}
					if loopSpec.Pause > 0 && idx < len(loopSpec.Items)-1 {
						time.Sleep(loopSpec.Pause)
					}
				}

				if task.Register != "" {
					registerResult(hostRuntimeVars, host.Name, task, map[string]interface{}{
						"changed": loopChanged,
						"failed":  loopFailed,
						"skipped": loopSkippedAll,
						"results": loopResults,
					})
				}
				if loopFailed {
					printf("  ✗ Host %s failed; stopping remaining tasks\n", host.Name)
					runResult.Failed = true
					break
				}
				continue
			}

			allowReboot := opts.Local && opts.AllowReboot && taskIdx == len(taskQueue)-1
			result, hasResult := executeTaskOnce(task, flattened, host, client, agentExecutionPath, baseDir, &taskQueue, taskIdx, renderImportPath, opts.Verbose, allowReboot, globalExecutionState)
			if hasResult && task.GatherFacts != nil {
				applyGatheredFacts(hostRuntimeVars, host.Name, result)
			}
			if hasResult && task.Register != "" {
				registerResult(hostRuntimeVars, host.Name, task, result)
			}
			if hasResult {
				if failed, ok := result["failed"].(bool); ok && failed {
					printf("  ✗ Host %s failed; stopping remaining tasks\n", host.Name)
					runResult.Failed = true
					break
				}
				runResult.CompletedTaskCount++
				if task.Reboot != nil {
					if rebooted, _ := result["rebooted"].(bool); rebooted {
						runResult.RebootInitiated = true
					}
				}
			} else if task.ImportTasks == nil && task.IncludeTasks == nil {
				printf("  ✗ Host %s could not execute task; stopping remaining tasks\n", host.Name)
				runResult.Failed = true
				break
			}
		}
	}

	println("\nDone!")
	return runResult, nil
}

func countRebootTasks(tasks []config.Task) int {
	count := 0
	for _, task := range tasks {
		if task.Reboot != nil {
			count++
		}
	}
	return count
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

type loopSpec struct {
	Items    []interface{}
	LoopVar  string
	IndexVar string
	Pause    time.Duration
	Extended bool
	Label    string
}

func mergeContext(base map[string]interface{}, overrides map[string]interface{}) map[string]interface{} {
	if len(overrides) == 0 {
		return base
	}
	merged := make(map[string]interface{}, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

func mergeTaskVars(base map[string]interface{}, overrides map[string]interface{}) map[string]interface{} {
	if len(overrides) == 0 {
		return base
	}
	merged := make(map[string]interface{}, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

func resolveLoopSpec(task config.Task, context map[string]interface{}) (*loopSpec, error) {
	var sources []struct {
		name  string
		value interface{}
	}
	if task.Loop != nil {
		sources = append(sources, struct {
			name  string
			value interface{}
		}{name: "loop", value: task.Loop})
	}
	if task.WithItems != nil {
		sources = append(sources, struct {
			name  string
			value interface{}
		}{name: "with_items", value: task.WithItems})
	}
	if task.WithList != nil {
		sources = append(sources, struct {
			name  string
			value interface{}
		}{name: "with_list", value: task.WithList})
	}
	if task.WithDict != nil {
		sources = append(sources, struct {
			name  string
			value interface{}
		}{name: "with_dict", value: task.WithDict})
	}
	if task.WithSequence != nil {
		sources = append(sources, struct {
			name  string
			value interface{}
		}{name: "with_sequence", value: task.WithSequence})
	}
	if len(sources) == 0 {
		return nil, nil
	}
	if len(sources) > 1 {
		names := make([]string, 0, len(sources))
		for _, source := range sources {
			names = append(names, source.name)
		}
		return nil, fmt.Errorf("multiple loop sources provided: %s", strings.Join(names, ", "))
	}

	loopVar := "item"
	indexVar := ""
	pause := time.Duration(0)
	extended := false
	label := ""
	if task.LoopControl != nil {
		if task.LoopControl.LoopVar != "" {
			rendered, err := vars.RenderString(task.LoopControl.LoopVar, context)
			if err != nil {
				return nil, fmt.Errorf("failed to render loop_var: %w", err)
			}
			loopVar = rendered
		}
		if task.LoopControl.IndexVar != "" {
			rendered, err := vars.RenderString(task.LoopControl.IndexVar, context)
			if err != nil {
				return nil, fmt.Errorf("failed to render index_var: %w", err)
			}
			indexVar = rendered
		}
		if task.LoopControl.Pause > 0 {
			pause = time.Duration(task.LoopControl.Pause * float64(time.Second))
		}
		extended = task.LoopControl.Extended
		if task.LoopControl.Label != "" {
			rendered, err := vars.RenderString(task.LoopControl.Label, context)
			if err != nil {
				return nil, fmt.Errorf("failed to render loop label: %w", err)
			}
			label = rendered
		}
	}
	if err := validateVarName(loopVar); err != nil {
		return nil, err
	}
	if indexVar != "" {
		if err := validateVarName(indexVar); err != nil {
			return nil, err
		}
	}

	var items []interface{}
	source := sources[0]
	switch source.name {
	case "loop", "with_list":
		resolved, err := resolveLoopItems(source.value, context)
		if err != nil {
			return nil, err
		}
		items = resolved
	case "with_items":
		resolved, err := resolveLoopItems(source.value, context)
		if err != nil {
			return nil, err
		}
		items = flattenLoopItems(resolved)
	case "with_dict":
		resolved, err := resolveLoopDict(source.value, context)
		if err != nil {
			return nil, err
		}
		items = resolved
	case "with_sequence":
		resolved, err := resolveSequenceItems(source.value, context)
		if err != nil {
			return nil, err
		}
		items = resolved
	default:
		return nil, fmt.Errorf("unsupported loop source %q", source.name)
	}

	return &loopSpec{
		Items:    items,
		LoopVar:  loopVar,
		IndexVar: indexVar,
		Pause:    pause,
		Extended: extended,
		Label:    label,
	}, nil
}

func resolveLoopItems(value interface{}, context map[string]interface{}) ([]interface{}, error) {
	rendered, err := renderLoopValue(value, context)
	if err != nil {
		return nil, err
	}
	items, ok := toInterfaceSlice(rendered)
	if !ok {
		return nil, fmt.Errorf("loop value must be a list")
	}
	return items, nil
}

func renderLoopValue(value interface{}, context map[string]interface{}) (interface{}, error) {
	if value == nil {
		return []interface{}{}, nil
	}
	if str, ok := value.(string); ok {
		return vars.RenderTemplateValue(str, context)
	}
	return vars.RenderValue(value, context)
}

func toInterfaceSlice(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return []interface{}{}, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]interface{}, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	}
	return nil, false
}

func flattenLoopItems(items []interface{}) []interface{} {
	flattened := []interface{}{}
	for _, item := range items {
		if nested, ok := toInterfaceSlice(item); ok {
			flattened = append(flattened, nested...)
			continue
		}
		flattened = append(flattened, item)
	}
	return flattened
}

func resolveLoopDict(value interface{}, context map[string]interface{}) ([]interface{}, error) {
	rendered, err := renderLoopValue(value, context)
	if err != nil {
		return nil, err
	}
	data, ok := normalizeStringMap(rendered)
	if !ok {
		return nil, fmt.Errorf("with_dict expects a map")
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		items = append(items, map[string]interface{}{
			"key":   key,
			"value": data[key],
		})
	}
	return items, nil
}

func normalizeStringMap(value interface{}) (map[string]interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[fmt.Sprint(key)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

type sequenceSpec struct {
	Start    int
	End      int
	Count    int
	Stride   int
	Format   string
	HasEnd   bool
	HasCount bool
}

func resolveSequenceItems(value interface{}, context map[string]interface{}) ([]interface{}, error) {
	rendered, err := renderLoopValue(value, context)
	if err != nil {
		return nil, err
	}
	spec, err := parseSequenceSpec(rendered)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, spec.Count)
	for i := 0; i < spec.Count; i++ {
		val := spec.Start + i*spec.Stride
		if spec.Format != "" {
			items = append(items, fmt.Sprintf(spec.Format, val))
			continue
		}
		items = append(items, val)
	}
	return items, nil
}

func parseSequenceSpec(value interface{}) (sequenceSpec, error) {
	spec := sequenceSpec{Start: 1, Stride: 1}
	switch typed := value.(type) {
	case string:
		fields := strings.Fields(typed)
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				return spec, fmt.Errorf("invalid with_sequence token %q", field)
			}
			applySequenceToken(&spec, parts[0], parts[1])
		}
	case map[string]interface{}:
		for key, val := range typed {
			applySequenceToken(&spec, key, val)
		}
	default:
		return spec, fmt.Errorf("with_sequence expects a string or map")
	}
	if spec.Stride == 0 {
		return spec, fmt.Errorf("with_sequence stride cannot be 0")
	}
	if !spec.HasCount && !spec.HasEnd {
		return spec, fmt.Errorf("with_sequence requires count or end")
	}
	if !spec.HasCount {
		spec.Count = ((spec.End - spec.Start) / spec.Stride) + 1
		spec.HasCount = true
	}
	if spec.Count < 0 {
		return spec, fmt.Errorf("with_sequence count must be positive")
	}
	return spec, nil
}

func applySequenceToken(spec *sequenceSpec, key string, value interface{}) {
	val := fmt.Sprint(value)
	switch key {
	case "start":
		if parsed, err := strconv.Atoi(val); err == nil {
			spec.Start = parsed
		}
	case "end":
		if parsed, err := strconv.Atoi(val); err == nil {
			spec.End = parsed
			spec.HasEnd = true
		}
	case "count":
		if parsed, err := strconv.Atoi(val); err == nil {
			spec.Count = parsed
			spec.HasCount = true
		}
	case "stride":
		if parsed, err := strconv.Atoi(val); err == nil {
			spec.Stride = parsed
		}
	case "format":
		spec.Format = val
	}
}

func buildLoopInfo(items []interface{}, idx int, extended bool) map[string]interface{} {
	if !extended {
		return nil
	}
	length := len(items)
	info := map[string]interface{}{
		"index":     idx + 1,
		"index0":    idx,
		"first":     idx == 0,
		"last":      idx == length-1,
		"length":    length,
		"revindex":  length - idx,
		"revindex0": length - idx - 1,
		"allitems":  items,
	}
	if idx > 0 {
		info["previtem"] = items[idx-1]
	}
	if idx < length-1 {
		info["nextitem"] = items[idx+1]
	}
	return info
}

func buildLoopVars(spec *loopSpec, item interface{}, idx int, loopInfo map[string]interface{}) map[string]interface{} {
	varsMap := map[string]interface{}{
		spec.LoopVar:       item,
		"ansible_loop_var": spec.LoopVar,
	}
	if spec.IndexVar != "" {
		varsMap[spec.IndexVar] = idx
	}
	if loopInfo != nil {
		varsMap["ansible_loop"] = loopInfo
	}
	return varsMap
}

func attachLoopResult(result map[string]interface{}, spec *loopSpec, item interface{}, idx int, loopInfo map[string]interface{}) map[string]interface{} {
	if result == nil {
		return nil
	}
	result["item"] = item
	if spec.LoopVar != "item" {
		result[spec.LoopVar] = item
	}
	result["ansible_loop_var"] = spec.LoopVar
	if spec.IndexVar != "" {
		result[spec.IndexVar] = idx
	}
	if loopInfo != nil {
		result["ansible_loop"] = loopInfo
	}
	return result
}

func includeTasksForLoop(task config.Task, spec *loopSpec, baseContext map[string]interface{}, baseDir string, taskQueue *[]config.Task, taskIdx int, renderImportPath func(string) (string, error)) error {
	if task.IncludeTasks == nil {
		return nil
	}
	var expanded []config.Task
	for idx, item := range spec.Items {
		loopInfo := buildLoopInfo(spec.Items, idx, spec.Extended)
		loopVars := buildLoopVars(spec, item, idx, loopInfo)
		iterationContext := mergeContext(baseContext, loopVars)
		if len(task.When) > 0 {
			shouldRun, err := template.EvaluateWhen([]interface{}(task.When), iterationContext)
			if err != nil {
				return fmt.Errorf("when condition error: %w", err)
			}
			if !shouldRun {
				printf("    ↷ SKIPPED (when condition false)\n")
				continue
			}
		}
		iterationTask := task
		iterationTask.Vars = mergeTaskVars(task.Vars, loopVars)
		includedTasks, baseName, err := loadIncludeTasks(iterationTask, iterationContext, baseDir, renderImportPath)
		if err != nil {
			return err
		}
		printf("    ✓ included %d task(s) from %s\n", len(includedTasks), baseName)
		expanded = append(expanded, includedTasks...)
	}
	if len(expanded) == 0 {
		return nil
	}
	tail := make([]config.Task, len((*taskQueue)[taskIdx+1:]))
	copy(tail, (*taskQueue)[taskIdx+1:])
	*taskQueue = append((*taskQueue)[:taskIdx+1], expanded...)
	*taskQueue = append(*taskQueue, tail...)
	return nil
}

func loadIncludeTasks(task config.Task, context map[string]interface{}, baseDir string, renderImportPath func(string) (string, error)) ([]config.Task, string, error) {
	if task.IncludeTasks == nil || task.IncludeTasks.File == "" {
		return nil, "", fmt.Errorf("file path is required")
	}
	renderedPath, err := vars.RenderString(task.IncludeTasks.File, context)
	if err != nil {
		return nil, "", fmt.Errorf("failed to render include_tasks path: %w", err)
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
		return nil, "", fmt.Errorf("failed to read %q: %w", renderedPath, err)
	}
	var includedTasks []config.Task
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&includedTasks); err != nil {
		return nil, "", fmt.Errorf("failed to parse %q: %w", renderedPath, err)
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
		return nil, "", fmt.Errorf("failed to expand nested imports in %q: %w", renderedPath, err)
	}
	return includedTasks, filepath.Base(renderedPath), nil
}

func genericResponseToMap(resp GenericResponse) map[string]interface{} {
	result := map[string]interface{}{
		"changed": resp.Changed,
		"failed":  resp.Failed,
		"skipped": resp.Skipped,
		"rc":      resp.RC,
		"msg":     resp.Msg,
		"stdout":  resp.Stdout,
		"stderr":  resp.Stderr,
	}
	if resp.Rebooted {
		result["rebooted"] = true
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

func applyGatheredFacts(hostRuntimeVars map[string]map[string]interface{}, hostName string, result map[string]interface{}) {
	if result == nil {
		return
	}
	factsRaw, ok := result["ansible_facts"]
	if !ok {
		return
	}
	facts, ok := factsRaw.(map[string]interface{})
	if !ok {
		return
	}
	if hostRuntimeVars[hostName] == nil {
		hostRuntimeVars[hostName] = map[string]interface{}{}
	}
	hostRuntimeVars[hostName]["ansible_facts"] = facts
	for key, value := range facts {
		if strings.HasPrefix(key, "ansible_") {
			hostRuntimeVars[hostName][key] = value
			continue
		}
		hostRuntimeVars[hostName]["ansible_"+key] = value
	}
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

func printValidationSummary(configPath string, cfg *config.Config, inv *inventory.Inventory) {
	printf("✓ Config is valid\n")
	printf("Config: %s\n", configPath)
	if inv != nil {
		hosts, err := inv.HostsAsConfig()
		if err != nil {
			printf("Inventory: failed to resolve hosts (%v)\n", err)
		} else {
			printf("Inventory: %d host(s), %d group(s)\n", len(hosts), len(inv.Groups))
		}
	} else {
		printf("Inventory: none\n")
	}
	printf("Hosts: %d\n", len(cfg.Hosts))
	printf("Tasks: %d\n", len(cfg.Tasks))
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
func executeTaskOnce(task config.Task, flattened map[string]interface{}, host config.Host, client ExecutionClient, remoteAgentPath string, baseDir string, taskQueue *[]config.Task, taskIdx int, renderImportPath func(string) (string, error), verbose bool, allowReboot bool, globalExecutionState execution.State) (map[string]interface{}, bool) {
	if len(task.When) > 0 {
		shouldRun, err := template.EvaluateWhen(task.When, flattened)
		if err != nil {
			printf("    ✗ FAILED: when condition error: %v\n", err)
			return map[string]interface{}{
				"failed": true,
				"msg":    fmt.Sprintf("when condition error: %v", err),
			}, true
		}
		if !shouldRun {
			printf("    ↷ SKIPPED (when condition false)\n")
			return map[string]interface{}{
				"changed": false,
				"skipped": true,
				"msg":     "when condition false",
			}, true
		}
	}

	taskExecutionState := execution.ResolveState(globalExecutionState, task.CheckMode, task.Diff)
	if taskExecutionState.CheckMode {
		if task.Module != nil {
			definition, registered := registry.Lookup(task.Module.CanonicalName)
			if !registered || !definition.ImplementsCheckMode() {
				return skipUnsupportedCheckMode(task.Module.CanonicalName, verbose)
			}
		} else if task.IncludeTasks == nil {
			return skipUnsupportedCheckMode(fmt.Sprintf("task %q (legacy module)", task.Name), verbose)
		}
	}
	var modReq ModuleRequest

	switch {
	case task.Module != nil:
		var err error
		modReq, err = renderRegisteredModule(task.Module, flattened)
		if err != nil {
			printf("    ✗ Failed to render args: %v\n", err)
			return nil, false
		}

	case task.Apt != nil:
		args := map[string]any{
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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "apt", Args: renderedArgs}

	case task.AptKey != nil:
		args := map[string]any{
			"url":     task.AptKey.URL,
			"data":    task.AptKey.Data,
			"file":    task.AptKey.File,
			"keyring": task.AptKey.Keyring,
			"id":      task.AptKey.ID,
			"state":   task.AptKey.State,
		}
		renderedArgs, err := renderArgs(args, flattened)

		if err != nil {

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "apt_key", Args: renderedArgs}

	case task.AptRepository != nil:
		args := map[string]any{
			"repo":         task.AptRepository.Repo,
			"state":        task.AptRepository.State,
			"filename":     task.AptRepository.Filename,
			"update_cache": task.AptRepository.UpdateCache,
		}
		renderedArgs, err := renderArgs(args, flattened)

		if err != nil {

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "file", Args: renderedArgs}

	case task.Fetch != nil:
		renderedFetch, err := renderFetchParams(task.Fetch, flattened)
		if err != nil {
			printf("    ✗ Failed to render fetch params: %v\n", err)
			return nil, false
		}
		resp := executeFetch(client, remoteAgentPath, host.Name, renderedFetch, verbose, taskExecutionState)
		printResponse(resp, verbose)
		result := genericResponseToMap(resp)
		localDest := computeFetchDest(renderedFetch, host.Name)
		result["dest"] = localDest
		result["src"] = renderedFetch.Src
		if !resp.Failed {
			if checksum, err := sha1File(localDest); err == nil {
				result["checksum"] = checksum
			}
		}
		return result, true

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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
				printf("    ✗ Failed to compute local checksum: %v\n", err)
				return nil, false
			}

			remoteTmpPath := fmt.Sprintf("/tmp/.dibra-copy-%s", localChecksum[:12])
			if err := client.UploadFile(task.Copy.Src, remoteTmpPath); err != nil {
				printf("    ✗ Failed to upload file: %v\n", err)
				return nil, false
			}

			copyArgs["src"] = remoteTmpPath
			copyArgs["checksum"] = localChecksum
		}

		renderedArgs, err := renderArgs(copyArgs, flattened)

		if err != nil {

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "copy", Args: renderedArgs}

	case task.Template != nil:
		templateDest := task.Template.Dest
		if templateDest == "" {
			println("    ✗ template: dest is required")
			return nil, false
		}

		templateDest, err := vars.RenderString(templateDest, flattened)
		if err != nil {
			printf("    ✗ Failed to render template dest: %v\n", err)
			return nil, false
		}

		force := true
		if task.Template.Force != nil {
			force = *task.Template.Force
		}

		srcPath := task.Template.Src
		if srcPath == "" {
			println("    ✗ template: src is required")
			return nil, false
		}

		srcPath, err = vars.RenderString(srcPath, flattened)
		if err != nil {
			printf("    ✗ Failed to render template src: %v\n", err)
			return nil, false
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
			printf("    ✗ template: failed to read src %q: %v\n", srcPath, err)
			return nil, false
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
			printf("    ✗ template: render failed: %v\n", err)
			return nil, false
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
			printf("    ✗ Failed to render template args: %v\n", err)
			return nil, false
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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "service", Args: renderedArgs}

	case task.ServiceFacts != nil:
		modReq = ModuleRequest{
			Module: "service_facts",
			Args:   map[string]interface{}{},
		}

	case task.GatherFacts != nil:
		args := map[string]interface{}{
			"gather_subset": task.GatherFacts.GatherSubset,
			"filter":        task.GatherFacts.Filter,
			"fact_path":     task.GatherFacts.FactPath,
		}
		renderedArgs, err := renderArgs(args, flattened)
		if err != nil {
			printf("    ✗ Failed to render args: %v\n", err)
			return nil, false
		}
		modReq = ModuleRequest{Module: "gather_facts", Args: renderedArgs}

	case task.Ping != nil:
		args := map[string]interface{}{"data": task.Ping.Data}
		renderedArgs, err := renderArgs(args, flattened)

		if err != nil {

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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
			printf("    ✗ Failed to render args: %v\n", err)
			return nil, false
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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

		}

		modReq = ModuleRequest{Module: "shell", Args: renderedArgs}

	case task.Script != nil:
		renderedScript, err := renderScriptParams(task.Script, flattened)
		if err != nil {
			printf("    ✗ Failed to render script params: %v\n", err)
			return nil, false
		}
		resp, rawJSON := executeScript(client, remoteAgentPath, renderedScript, verbose, taskExecutionState)
		printResponse(resp, verbose)
		var fullResult map[string]interface{}
		if rawJSON != "" {
			err = json.Unmarshal([]byte(rawJSON), &fullResult)
			if err != nil {
				printf("    ✗ Failed to parse script response: %v\n", err)
				return nil, false
			}
		}
		if fullResult == nil {
			fullResult = genericResponseToMap(resp)
		}
		return fullResult, true

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
				printf("    ✗ Failed to compute local checksum: %v\n", err)
				return nil, false
			}

			remoteTmpPath := fmt.Sprintf("/tmp/.dibra-unarchive-%s", localChecksum[:12])
			if err := client.UploadFile(task.Unarchive.Src, remoteTmpPath); err != nil {
				printf("    ✗ Failed to upload archive: %v\n", err)
				return nil, false
			}

			unarchiveArgs["src"] = remoteTmpPath
			unarchiveArgs["checksum"] = localChecksum
			unarchiveArgs["original_src"] = task.Unarchive.Src
		}

		renderedArgs, err := renderArgs(unarchiveArgs, flattened)

		if err != nil {

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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

			printf("    ✗ Failed to render args: %v\n", err)

			return nil, false

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
			printf("    ✗ Failed to render args: %v\n", err)
			return nil, false
		}
		modReq = ModuleRequest{Module: "find", Args: renderedArgs}

	case task.Reboot != nil:
		if client.IsLocal() && !allowReboot {
			resp := GenericResponse{Failed: true, Msg: "local reboot must be the final non-looped task of the final playbook"}
			printResponse(resp, verbose)
			return genericResponseToMap(resp), true
		}
		renderedReboot, err := renderRebootParams(task.Reboot, flattened)
		if err != nil {
			printf("    ✗ Failed to render reboot params: %v\n", err)
			return nil, false
		}
		resp := executeReboot(client, remoteAgentPath, host, renderedReboot, verbose, taskExecutionState)
		printResponse(resp, verbose)
		result := genericResponseToMap(resp)
		result["rebooted"] = !resp.Failed
		return result, true

	case task.IncludeTasks != nil:
		filePath := task.IncludeTasks.File
		if filePath == "" {
			println("    ✗ include_tasks: file path is required")
			if client.IsLocal() {
				return map[string]interface{}{"failed": true, "msg": "include_tasks: file path is required"}, true
			}
			return nil, false
		}

		renderedPath, err := vars.RenderString(filePath, flattened)
		if err != nil {
			printf("    ✗ Failed to render include_tasks path: %v\n", err)
			if client.IsLocal() {
				return map[string]interface{}{"failed": true, "msg": fmt.Sprintf("failed to render include_tasks path: %v", err)}, true
			}
			return nil, false
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
			printf("    ✗ include_tasks: failed to read %q: %v\n", renderedPath, err)
			if client.IsLocal() {
				return map[string]interface{}{"failed": true, "msg": fmt.Sprintf("include_tasks: failed to read %q: %v", renderedPath, err)}, true
			}
			return nil, false
		}

		var includedTasks []config.Task
		dec := yaml.NewDecoder(bytes.NewReader(data))
		if err := dec.Decode(&includedTasks); err != nil {
			printf("    ✗ include_tasks: failed to parse %q: %v\n", renderedPath, err)
			if client.IsLocal() {
				return map[string]interface{}{"failed": true, "msg": fmt.Sprintf("include_tasks: failed to parse %q: %v", renderedPath, err)}, true
			}
			return nil, false
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
			printf("    ✗ include_tasks: failed to expand nested imports in %q: %v\n", renderedPath, err)
			if client.IsLocal() {
				return map[string]interface{}{"failed": true, "msg": fmt.Sprintf("include_tasks: failed to expand nested imports in %q: %v", renderedPath, err)}, true
			}
			return nil, false
		}

		tail := make([]config.Task, len((*taskQueue)[taskIdx+1:]))
		copy(tail, (*taskQueue)[taskIdx+1:])
		*taskQueue = append((*taskQueue)[:taskIdx+1], includedTasks...)
		*taskQueue = append(*taskQueue, tail...)

		printf("    ✓ included %d task(s) from %s\n", len(includedTasks), filepath.Base(renderedPath))
		return nil, false

	default:
		println("    ⚠ No module specified, skipping")
		return nil, false
	}

	modReq.State = taskExecutionState
	reqData, _ := json.Marshal(modReq)
	_, registeredModule := registry.Lookup(modReq.Module)
	if verbose {
		requestForLog := modReq
		if registeredModule {
			redactedArgs, redactErr := registry.RedactArguments(modReq.Module, modReq.Args)
			if redactErr != nil {
				requestForLog.Args = execution.RedactedValue
			} else {
				requestForLog.Args = redactedArgs
			}
		}
		redactedRequest, _ := json.Marshal(requestForLog)
		printf("    Request: %s\n", string(redactedRequest))
	}

	output, err := client.ExecuteAgent(remoteAgentPath, reqData)
	if err != nil {
		message := fmt.Sprintf("Execution failed: %v", err)
		if registeredModule {
			message = registry.RedactText(modReq.Module, modReq.Args, message)
		}
		printf("    ✗ %s\n", message)
		return map[string]interface{}{
			"changed":     false,
			"unreachable": true,
			"failed":      true,
			"msg":         message,
		}, true
	}

	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	jsonOutput := lines[len(lines)-1]

	var fullResult map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &fullResult); err != nil {
		rawOutput := outputStr
		if registeredModule {
			// A malformed response cannot be traversed safely using the
			// definition's sensitive result paths, so fail closed.
			rawOutput = execution.RedactedValue
		}
		printf("    ✗ Failed to parse response: %v\n", err)
		printf("    Raw output: %s\n", rawOutput)
		return map[string]interface{}{
			"changed": false,
			"failed":  true,
			"msg":     fmt.Sprintf("Failed to parse response: %v", err),
		}, true
	}
	fullResult, err = execution.NormalizeResult(fullResult)
	if err != nil {
		printf("    ✗ Invalid module result: %v\n", err)
		return map[string]interface{}{
			"changed": false,
			"failed":  true,
			"msg":     fmt.Sprintf("Invalid module result: %v", err),
		}, true
	}

	displayResult := fullResult
	if registeredModule {
		displayResult, err = registry.RedactResult(modReq.Module, modReq.Args, fullResult)
		if err != nil {
			printf("    ✗ Failed to redact module result: %v\n", err)
			return map[string]interface{}{
				"changed": false,
				"failed":  true,
				"msg":     "Failed to redact module result",
			}, true
		}
	}
	displayJSON, _ := json.Marshal(displayResult)
	var resp GenericResponse
	if err := json.Unmarshal(displayJSON, &resp); err != nil {
		printf("    ✗ Invalid common module result fields: %v\n", err)
		return map[string]interface{}{
			"changed": false,
			"failed":  true,
			"msg":     fmt.Sprintf("Invalid common module result fields: %v", err),
		}, true
	}

	if resp.Skipped {
		printf("    ↷ SKIPPED")
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	} else if resp.Failed {
		printf("    ✗ FAILED: %s\n", resp.Msg)
		if verbose && resp.Stderr != "" {
			printf("    Stderr: %s\n", resp.Stderr)
		}
	} else if resp.Changed {
		printf("    ✓ CHANGED")
		if len(resp.Packages) > 0 {
			printf(" (packages: %s)", strings.Join(resp.Packages, ", "))
		}
		if resp.KeyID != "" {
			printf(" (key: %s)", resp.KeyID)
		}
		if resp.Filename != "" {
			printf(" (file: %s)", resp.Filename)
		}
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	} else {
		printf("    ✓ OK (no changes)")
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	}

	if verbose && resp.Stdout != "" {
		printf("    Stdout: %s\n", truncate(resp.Stdout, 500))
	}
	if taskExecutionState.DiffMode {
		if diff, ok := displayResult["diff"]; ok {
			if data, marshalErr := json.MarshalIndent(diff, "      ", "  "); marshalErr == nil {
				printf("    Diff: %s\n", data)
			}
		}
	}
	return fullResult, true
}

func renderRegisteredModule(invocation *registry.Invocation, context map[string]interface{}) (ModuleRequest, error) {
	args, err := registry.ArgumentsMap(invocation)
	if err != nil {
		return ModuleRequest{}, err
	}
	renderedArgs, err := renderArgs(args, context)
	if err != nil {
		return ModuleRequest{}, err
	}
	return ModuleRequest{Module: invocation.CanonicalName, Args: renderedArgs}, nil
}

func skipUnsupportedCheckMode(module string, verbose bool) (map[string]interface{}, bool) {
	skipped := execution.UnsupportedCheckMode(module)
	printResponse(GenericResponse{Changed: skipped.Changed, Failed: skipped.Failed, Skipped: skipped.Skipped, Msg: skipped.Msg}, verbose)
	return map[string]interface{}{
		"changed": skipped.Changed,
		"failed":  skipped.Failed,
		"skipped": skipped.Skipped,
		"msg":     skipped.Msg,
	}, true
}

func executeFetch(client ExecutionClient, agentPath, hostName string, params *config.FetchParams, verbose bool, state execution.State) GenericResponse {
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
		State: state,
	}
	reqData, _ := json.Marshal(statReq)
	if verbose {
		printf("    Stat request: %s\n", string(reqData))
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
		printf("    Downloading %s -> %s\n", params.Src, localDest)
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
	if resp.Skipped {
		printf("    ↷ SKIPPED")
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	} else if resp.Failed {
		printf("    ✗ FAILED: %s\n", resp.Msg)
		if verbose && resp.Stderr != "" {
			printf("    Stderr: %s\n", resp.Stderr)
		}
	} else if resp.Changed {
		printf("    ✓ CHANGED")
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	} else {
		printf("    ✓ OK (no changes)")
		if resp.Msg != "" {
			printf(" - %s", resp.Msg)
		}
		println()
	}
}

func executeScript(client ExecutionClient, agentPath string, params *config.ScriptParams, verbose bool, state execution.State) (GenericResponse, string) {
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
		printf("    Uploading script %s -> %s\n", scriptPath, remoteTmpPath)
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
		State: state,
	}
	reqData, _ := json.Marshal(scriptReq)
	if verbose {
		printf("    Script request: %s\n", string(reqData))
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

func cleanupScript(client ExecutionClient, remotePath string) {
	_, _, _ = client.Run(fmt.Sprintf("rm -f %s", remotePath))
}

func executeReboot(client ExecutionClient, agentPath string, host config.Host, params *config.RebootParams, verbose bool, state execution.State) GenericResponse {
	if client.IsLocal() {
		rebootReq := ModuleRequest{
			Module: "reboot",
			Args: map[string]interface{}{
				"pre_reboot_delay":  params.PreRebootDelay,
				"post_reboot_delay": params.PostRebootDelay,
				"reboot_timeout":    params.RebootTimeout,
				"connect_timeout":   params.ConnectTimeout,
				"test_command":      params.TestCommand,
				"msg":               params.Msg,
				"search_paths":      params.SearchPaths,
				"boot_time_command": params.BootTimeCommand,
				"reboot_command":    params.RebootCommand,
			},
			State: state,
		}
		reqData, _ := json.Marshal(rebootReq)
		if verbose {
			printf("    Local reboot request: %s\n", string(reqData))
		}
		output, err := client.ExecuteAgent(agentPath, reqData)
		if err != nil {
			return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to initiate local reboot: %v", err)}
		}
		outputStr := strings.TrimSpace(string(output))
		lines := strings.Split(outputStr, "\n")
		jsonOutput := lines[len(lines)-1]
		var resp GenericResponse
		if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
			return GenericResponse{Failed: true, Msg: fmt.Sprintf("failed to parse local reboot response: %v", err)}
		}
		return resp
	}

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
		State: state,
	}
	reqData, _ := json.Marshal(bootTimeReq)
	if verbose {
		printf("    Getting boot time: %s\n", bootTimeCommand)
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
		printf("    Previous boot time: %s\n", previousBootTime)
	}

	if params.PreRebootDelay > 0 {
		if verbose {
			printf("    Waiting %d seconds before reboot...\n", params.PreRebootDelay)
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
				State: state,
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
					State: state,
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
		printf("    Executing reboot command: %s\n", rebootCmd)
	}

	rebootReq := ModuleRequest{
		Module: "shell",
		Args: map[string]interface{}{
			"cmd": rebootCmd,
		},
		State: state,
	}
	reqData, _ = json.Marshal(rebootReq)

	_, _ = client.ExecuteAgent(agentPath, reqData)

	client.Close()

	if params.PostRebootDelay > 0 {
		if verbose {
			printf("    Waiting %d seconds after reboot command...\n", params.PostRebootDelay)
		}
		time.Sleep(time.Duration(params.PostRebootDelay) * time.Second)
	}

	if verbose {
		println("    Waiting for system to come back up...")
	}

	deadline := time.Now().Add(time.Duration(rebootTimeout) * time.Second)
	var newClient ExecutionClient

	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		var err error
		newClient, err = client.Reconnect()
		if err != nil {
			if verbose {
				printf("    Reconnect attempt failed: %v\n", err)
			}
			continue
		}

		bootTimeReq := ModuleRequest{
			Module: "shell",
			Args: map[string]interface{}{
				"cmd": bootTimeCommand,
			},
			State: state,
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
				printf("    Boot time unchanged (%s), still waiting...\n", currentBootTime)
			}
			newClient.Close()
			continue
		}

		if verbose {
			printf("    New boot time: %s\n", currentBootTime)
		}

		testReq := ModuleRequest{
			Module: "shell",
			Args: map[string]interface{}{
				"cmd": testCommand,
			},
			State: state,
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
