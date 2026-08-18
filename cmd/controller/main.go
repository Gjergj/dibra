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
	options := controllerFlags{}
	flags := newControllerFlagSet(&options, flag.ExitOnError, os.Stderr)
	args := os.Args[1:]
	if isCompletionRequest(args) {
		if err := runCompletionRequest(args, flags, os.Stdout, os.Stderr); err != nil {
			fatal("completion failed: %v", err)
		}
		return
	}
	if len(args) > 0 && args[0] == "completion" {
		if err := runCompletion(args[1:], flags, os.Stdout); err != nil {
			fatal("completion failed: %v", err)
		}
		return
	}

	validateCommand := false
	if len(args) > 0 && args[0] == "validate" {
		validateCommand = true
		args = args[1:]
	}

	if err := flags.Parse(args); err != nil {
		fatal("%v", err)
	}
	validate := validateCommand || options.Validate

	if options.ShowVersion {
		fmt.Printf("dibra %s (commit: %s, built: %s)\n", version.Version, version.Commit, version.Date)
		return
	}
	if options.AgentPath != "" && options.AgentBuild {
		fatal("--agent-path and --agent-build are mutually exclusive")
	}
	if validateCommand && (options.AgentPath != "" || options.AgentBuild || options.ForceUpload) {
		fatal("validate mode does not allow agent flags")
	}

	resolvedInventoryPath := options.InventoryPath
	if resolvedInventoryPath == "" {
		resolvedInventoryPath = options.InventoryPathLong
	}
	resolvedExtraVars := options.ExtraVars
	if resolvedExtraVars == "" {
		resolvedExtraVars = options.ExtraVarsLong
	}
	resolverMode := agent.ModeAuto
	if options.AgentPath != "" {
		resolverMode = agent.ModePath
	} else if options.AgentBuild {
		resolverMode = agent.ModeBuild
	}

	_, err := controller.Run(context.Background(), controller.RunOptions{
		ConfigPath:       options.ConfigPath,
		InventoryPath:    resolvedInventoryPath,
		ExtraVars:        resolvedExtraVars,
		Validate:         validate,
		ForceAgentUpload: options.ForceUpload,
		Verbose:          options.Verbose,
		CheckMode:        options.CheckMode,
		DiffMode:         options.DiffMode,
		ForceHandlers:    options.ForceHandlers,
		AgentMode:        resolverMode,
		AgentPath:        options.AgentPath,
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
