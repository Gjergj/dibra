package dibra_deploy_cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionIncludesAgentFlags(t *testing.T) {
	t.Parallel()
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"completion", "bash"})
	if err := command.Execute(); err != nil {
		t.Fatalf("completion command failed: %v", err)
	}
	for _, flag := range []string{"--agent-path", "--agent-build", "--force-agent-upload"} {
		if !strings.Contains(output.String(), flag) {
			t.Fatalf("completion output does not include %s", flag)
		}
	}
}
