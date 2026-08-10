package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gjergjiramku/dibra/internal/agent"
	controller "github.com/gjergjiramku/dibra/internal/controller"
	"github.com/gjergjiramku/dibra/internal/version"
)

func main() {
	validateCommand := false
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		validateCommand = true
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}

	configPath := flag.String("config", "playbook.yaml", "Path to playbook config file (YAML)")
	forceUpload := flag.Bool("force-agent-upload", false, "Force upload of agent binary")
	verbose := flag.Bool("v", false, "Verbose output")
	showVersion := flag.Bool("version", false, "Print version and exit")
	agentPath := flag.String("agent-path", "", "Path to a pre-built agent binary")
	agentBuild := flag.Bool("agent-build", false, "Build agent from source (requires Go)")
	inventoryPath := flag.String("i", "", "Path to YAML inventory file")
	inventoryPathLong := flag.String("inventory", "", "Path to YAML inventory file")
	extraVars := flag.String("e", "", "Extra variables (key=value or @file.yaml)")
	extraVarsLong := flag.String("extra-vars", "", "Extra variables (key=value or @file.yaml)")
	validateFlag := flag.Bool("validate", false, "Validate config and exit (no execution)")
	flag.Parse()
	validate := validateCommand || *validateFlag

	if *showVersion {
		fmt.Printf("dibra %s (commit: %s, built: %s)\n", version.Version, version.Commit, version.Date)
		return
	}
	if *agentPath != "" && *agentBuild {
		fatal("--agent-path and --agent-build are mutually exclusive")
	}
	if validateCommand && (*agentPath != "" || *agentBuild || *forceUpload) {
		fatal("validate mode does not allow agent flags")
	}

	resolvedInventoryPath := *inventoryPath
	if resolvedInventoryPath == "" {
		resolvedInventoryPath = *inventoryPathLong
	}
	resolvedExtraVars := *extraVars
	if resolvedExtraVars == "" {
		resolvedExtraVars = *extraVarsLong
	}
	resolverMode := agent.ModeAuto
	if *agentPath != "" {
		resolverMode = agent.ModePath
	} else if *agentBuild {
		resolverMode = agent.ModeBuild
	}

	_, err := controller.Run(context.Background(), controller.RunOptions{
		ConfigPath:       *configPath,
		InventoryPath:    resolvedInventoryPath,
		ExtraVars:        resolvedExtraVars,
		Validate:         validate,
		ForceAgentUpload: *forceUpload,
		Verbose:          *verbose,
		AgentMode:        resolverMode,
		AgentPath:        *agentPath,
		Version:          version.Version,
		ProjectRoot:      findProjectRoot(),
	})
	if err != nil {
		fatal("%v", err)
	}
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
