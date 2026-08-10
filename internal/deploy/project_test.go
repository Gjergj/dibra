package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRebootPlacement(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		playbooks []string
		contents  []string
		wantError string
	}{
		{
			name:      "final reboot",
			playbooks: []string{"first.yaml", "last.yaml"},
			contents: []string{
				"tasks:\n  - name: ping\n    ping:\n",
				"tasks:\n  - name: reboot\n    reboot:\n      reboot_command: /bin/true\n",
			},
		},
		{
			name:      "reboot before final playbook",
			playbooks: []string{"first.yaml", "last.yaml"},
			contents: []string{
				"tasks:\n  - name: reboot\n    reboot:\n      reboot_command: /bin/true\n",
				"tasks:\n  - name: ping\n    ping:\n",
			},
			wantError: "final task of the final playbook",
		},
		{
			name:      "looped reboot",
			playbooks: []string{"last.yaml"},
			contents: []string{
				"tasks:\n  - name: reboot\n    reboot:\n      reboot_command: /bin/true\n    loop: [one]\n",
			},
			wantError: "cannot use a loop",
		},
		{
			name:      "invalid static import",
			playbooks: []string{"last.yaml"},
			contents: []string{
				"tasks:\n  - name: missing import\n    import_tasks: missing.yaml\n",
			},
			wantError: "expand import_tasks",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for index, playbook := range testCase.playbooks {
				if err := os.WriteFile(filepath.Join(root, playbook), []byte(testCase.contents[index]), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := validateRebootPlacement(Project{Root: root, Manifest: Manifest{Version: 1, Playbooks: testCase.playbooks}})
			if testCase.wantError == "" && err != nil {
				t.Fatalf("validateRebootPlacement() error = %v", err)
			}
			if testCase.wantError != "" && (err == nil || !strings.Contains(err.Error(), testCase.wantError)) {
				t.Fatalf("validateRebootPlacement() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}
