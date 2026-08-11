package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestControllerFlagsParseCheckAndDiff(t *testing.T) {
	var options controllerFlags
	flags := newControllerFlagSet(&options, flag.ContinueOnError, &bytes.Buffer{})
	if err := flags.Parse([]string{"--check", "--diff"}); err != nil {
		t.Fatal(err)
	}
	if !options.CheckMode || !options.DiffMode {
		t.Fatalf("parsed options = %#v", options)
	}
}

func TestControllerGeneratesEverySupportedCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var options controllerFlags
			flags := newControllerFlagSet(&options, flag.ContinueOnError, &bytes.Buffer{})
			var output bytes.Buffer
			if err := runCompletion([]string{shell}, flags, &output); err != nil {
				t.Fatal(err)
			}
			if output.Len() == 0 || !strings.Contains(output.String(), "dibra") {
				t.Errorf("%s completion output is invalid", shell)
			}
		})
	}
}

func TestControllerDynamicCompletionIncludesCheckAndDiffFlags(t *testing.T) {
	var options controllerFlags
	flags := newControllerFlagSet(&options, flag.ContinueOnError, &bytes.Buffer{})
	var output bytes.Buffer
	if err := runCompletionRequest([]string{"__complete", "--"}, flags, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"--check", "--diff"} {
		if !strings.Contains(output.String(), name) {
			t.Errorf("dynamic completion does not contain %s:\n%s", name, output.String())
		}
	}
}

func TestControllerCompletionRejectsUnsupportedShell(t *testing.T) {
	var options controllerFlags
	flags := newControllerFlagSet(&options, flag.ContinueOnError, &bytes.Buffer{})
	err := runCompletion([]string{"tcsh"}, flags, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("runCompletion() error = %v", err)
	}
}
