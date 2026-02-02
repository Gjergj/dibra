//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_IptablesState tests the iptables_state module suite
func TestPlaybook_IptablesState(t *testing.T) {
	t.Run("BasicSave", testIptablesStateBasicSave)
	t.Run("BasicRestore", testIptablesStateBasicRestore)
	t.Run("SaveIdempotency", testIptablesStateSaveIdempotency)
	t.Run("RestoreIdempotency", testIptablesStateRestoreIdempotency)
	t.Run("SaveSpecificTable", testIptablesStateSaveSpecificTable)
	t.Run("RestoreSpecificTable", testIptablesStateRestoreSpecificTable)
	t.Run("RestoreTableNotInFile", testIptablesStateRestoreTableNotInFile)
	t.Run("RestorePartialState", testIptablesStateRestorePartialState)
	t.Run("RestoreWithNoflush", testIptablesStateRestoreWithNoflush)
	t.Run("SaveWithCounters", testIptablesStateSaveWithCounters)
	t.Run("InvalidParams", testIptablesStateInvalidParams)
	t.Run("MultipleTablesRoundTrip", testIptablesStateMultipleTablesRoundTrip)
	t.Run("RealWorldFirewallConfig", testIptablesStateRealWorldFirewallConfig)
}

// testIptablesStateBasicSave tests saving current iptables state to a file
func testIptablesStateBasicSave(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Clean up test file
	client.Run("rm -f /tmp/test_iptables_state.saved")

	playbook := playbookHeader + `
  - name: Save current iptables state
    iptables_state:
      path: /tmp/test_iptables_state.saved
      state: saved
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when saving to new file")
	}

	// Verify file was created
	if !remoteFileExists(t, client, "/tmp/test_iptables_state.saved") {
		t.Error("State file should exist")
	}

	// Verify file contains iptables state
	content := remoteFileContent(t, client, "/tmp/test_iptables_state.saved")
	if !strings.Contains(content, "*filter") {
		t.Errorf("State file should contain filter table, got: %s", content)
	}
	if !strings.Contains(content, "COMMIT") {
		t.Errorf("State file should contain COMMIT, got: %s", content)
	}
}

// testIptablesStateBasicRestore tests restoring iptables state from a file
func testIptablesStateBasicRestore(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create a state file with specific rules
	stateContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -p tcp -m tcp --dport 8888 -j ACCEPT
COMMIT
`
	client.Run("cat > /tmp/test_restore.saved << 'EOF'\n" + stateContent + "EOF")

	playbook := playbookHeader + `
  - name: Restore iptables state
    iptables_state:
      path: /tmp/test_restore.saved
      state: restored
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when restoring new rules")
	}

	// Verify rule was applied
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "8888") {
		t.Errorf("Expected port 8888 rule, got: %s", rules)
	}
}

// testIptablesStateSaveIdempotency tests that saving is idempotent
func testIptablesStateSaveIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -f /tmp/test_save_idem.saved")

	// First save
	playbook := playbookHeader + `
  - name: Save iptables state
    iptables_state:
      path: /tmp/test_save_idem.saved
      state: saved
`
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("First save should report CHANGED")
	}

	// Second save (should be idempotent)
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Second save should NOT report CHANGED (idempotent)")
	}
}

// testIptablesStateRestoreIdempotency tests that restoring is idempotent
func testIptablesStateRestoreIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Save current state
	client.Run("iptables-save > /tmp/test_restore_idem.saved")

	playbook := playbookHeader + `
  - name: Restore iptables state
    iptables_state:
      path: /tmp/test_restore_idem.saved
      state: restored
`
	// First restore (state should already match, so no change)
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("First restore should NOT report CHANGED when state matches")
	}

	// Add a rule to change state
	client.Run("iptables -A INPUT -p tcp --dport 9999 -j ACCEPT")

	// Second restore (should change)
	output = runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Restore should report CHANGED when state differs")
	}

	// Third restore (should be idempotent again)
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Third restore should NOT report CHANGED (idempotent)")
	}

	// Verify rule 9999 was removed
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if strings.Contains(rules, "9999") {
		t.Errorf("Port 9999 rule should be removed after restore, got: %s", rules)
	}
}

// testIptablesStateSaveSpecificTable tests saving only a specific table
func testIptablesStateSaveSpecificTable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -f /tmp/test_save_table.saved")

	playbook := playbookHeader + `
  - name: Save only filter table
    iptables_state:
      path: /tmp/test_save_table.saved
      state: saved
      table: filter
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	content := remoteFileContent(t, client, "/tmp/test_save_table.saved")
	if !strings.Contains(content, "*filter") {
		t.Errorf("Should contain filter table, got: %s", content)
	}
	if strings.Contains(content, "*nat") {
		t.Errorf("Should NOT contain nat table, got: %s", content)
	}
}

// testIptablesStateRestoreSpecificTable tests restoring only a specific table
func testIptablesStateRestoreSpecificTable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create state file with filter and nat tables
	stateContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -p tcp -m tcp --dport 7777 -j ACCEPT
COMMIT
*nat
:PREROUTING ACCEPT [0:0]
:INPUT ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
:POSTROUTING ACCEPT [0:0]
-A POSTROUTING -o eth0 -j MASQUERADE
COMMIT
`
	client.Run("cat > /tmp/test_restore_table.saved << 'EOF'\n" + stateContent + "EOF")

	// Add a nat rule that we want to preserve
	client.Run("iptables -t nat -A PREROUTING -p tcp --dport 80 -j DNAT --to-destination 192.168.1.100:80")

	playbook := playbookHeader + `
  - name: Restore only filter table
    iptables_state:
      path: /tmp/test_restore_table.saved
      state: restored
      table: filter
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify filter rule was applied
	filterRules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(filterRules, "7777") {
		t.Errorf("Expected port 7777 rule in filter, got: %s", filterRules)
	}

	// Verify nat table was NOT touched (our DNAT rule should still be there)
	natRules := remoteExec(t, client, "iptables -t nat -L PREROUTING -n")
	if !strings.Contains(natRules, "192.168.1.100") {
		t.Errorf("NAT DNAT rule should still exist, got: %s", natRules)
	}
}

// testIptablesStateRestoreTableNotInFile tests error when restoring missing table
func testIptablesStateRestoreTableNotInFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create state file with only filter table
	stateContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
COMMIT
`
	client.Run("cat > /tmp/test_missing_table.saved << 'EOF'\n" + stateContent + "EOF")

	playbook := playbookHeader + `
  - name: Try to restore mangle table (not in file)
    iptables_state:
      path: /tmp/test_missing_table.saved
      state: restored
      table: mangle
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED when table not in file, got: %s", output)
	}
	if !strings.Contains(output, "mangle") || !strings.Contains(output, "not defined") {
		t.Errorf("Error message should mention missing table, got: %s", output)
	}
}

// testIptablesStateRestorePartialState tests restoring when file has subset of current tables
func testIptablesStateRestorePartialState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create initial state with rules in both filter and nat
	stateContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -m state --state NEW,ESTABLISHED -j ACCEPT
COMMIT
*nat
:PREROUTING ACCEPT [0:0]
:INPUT ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
:POSTROUTING ACCEPT [0:0]
-A POSTROUTING -o eth0 -j MASQUERADE
COMMIT
`
	client.Run("cat > /tmp/test_initial.saved << 'EOF'\n" + stateContent + "EOF")
	client.Run("iptables-restore < /tmp/test_initial.saved")

	// Create partial state with only filter
	partialContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -m state --state NEW,ESTABLISHED -j ACCEPT
COMMIT
`
	client.Run("cat > /tmp/test_partial.saved << 'EOF'\n" + partialContent + "EOF")

	playbook := playbookHeader + `
  - name: Restore partial state (filter only)
    iptables_state:
      path: /tmp/test_partial.saved
      state: restored
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Should NOT report changed because filter table matches
	// and we're only restoring what's in the file
	if strings.Contains(output, "CHANGED") {
		t.Error("Should NOT report CHANGED when filter table matches")
	}
}

// testIptablesStateRestoreWithNoflush tests noflush option
func testIptablesStateRestoreWithNoflush(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add an existing rule
	client.Run("iptables -A INPUT -p tcp --dport 1111 -j ACCEPT")

	// Create state file with different rule
	stateContent := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -p tcp -m tcp --dport 2222 -j ACCEPT
COMMIT
`
	client.Run("cat > /tmp/test_noflush.saved << 'EOF'\n" + stateContent + "EOF")

	playbook := playbookHeader + `
  - name: Restore with noflush
    iptables_state:
      path: /tmp/test_noflush.saved
      state: restored
      noflush: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Both rules should exist (noflush doesn't clear existing)
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "1111") {
		t.Errorf("Original rule should still exist with noflush, got: %s", rules)
	}
	if !strings.Contains(rules, "2222") {
		t.Errorf("New rule should be added with noflush, got: %s", rules)
	}
}

// testIptablesStateSaveWithCounters tests saving with counters
func testIptablesStateSaveWithCounters(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add a rule that will get some packets
	client.Run("iptables -A INPUT -p icmp -j ACCEPT")

	client.Run("rm -f /tmp/test_counters.saved")

	playbook := playbookHeader + `
  - name: Save with counters
    iptables_state:
      path: /tmp/test_counters.saved
      state: saved
      counters: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	content := remoteFileContent(t, client, "/tmp/test_counters.saved")
	if !strings.Contains(content, "*filter") {
		t.Errorf("Should contain filter table, got: %s", content)
	}
	// With counters, we might see [x:y] format
	// Without counters, it's always [0:0]
}

// testIptablesStateInvalidParams tests error handling for invalid parameters
func testIptablesStateInvalidParams(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	tests := []struct {
		name     string
		playbook string
		errMsg   string
	}{
		{
			name: "missing path",
			playbook: playbookHeader + `
  - name: Missing path
    iptables_state:
      state: saved
`,
			errMsg: "missing required arguments",
		},
		{
			name: "missing state",
			playbook: playbookHeader + `
  - name: Missing state
    iptables_state:
      path: /tmp/foo
`,
			errMsg: "missing required arguments",
		},
		{
			name: "invalid state value",
			playbook: playbookHeader + `
  - name: Invalid state
    iptables_state:
      path: /tmp/foo
      state: invalid
`,
			errMsg: "value of state must be one of",
		},
		{
			name: "invalid table value",
			playbook: playbookHeader + `
  - name: Invalid table
    iptables_state:
      path: /tmp/foo
      state: saved
      table: invalid
`,
			errMsg: "value of table must be one of",
		},
		{
			name: "invalid ip_version value",
			playbook: playbookHeader + `
  - name: Invalid ip_version
    iptables_state:
      path: /tmp/foo
      state: saved
      ip_version: invalid
`,
			errMsg: "value of ip_version must be one of",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := runPlaybook(t, tc.playbook)
			if !strings.Contains(output, "FAILED") {
				t.Errorf("Expected FAILED, got: %s", output)
			}
			if !strings.Contains(output, tc.errMsg) {
				t.Errorf("Expected error containing '%s', got: %s", tc.errMsg, output)
			}
		})
	}
}

// testIptablesStateMultipleTablesRoundTrip tests save and restore of multiple tables
func testIptablesStateMultipleTablesRoundTrip(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add rules to multiple tables
	client.Run("iptables -A INPUT -p tcp --dport 5555 -j ACCEPT")
	client.Run("iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE")
	client.Run("iptables -t mangle -A PREROUTING -p tcp --dport 80 -j MARK --set-mark 1")

	// Save all tables
	playbook := playbookHeader + `
  - name: Save all tables
    iptables_state:
      path: /tmp/test_multi.saved
      state: saved
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Save failed: %s", output)
	}

	content := remoteFileContent(t, client, "/tmp/test_multi.saved")
	if !strings.Contains(content, "*filter") {
		t.Errorf("Should contain filter table, got: %s", content)
	}
	if !strings.Contains(content, "*nat") {
		t.Errorf("Should contain nat table, got: %s", content)
	}
	if !strings.Contains(content, "*mangle") {
		t.Errorf("Should contain mangle table, got: %s", content)
	}

	// Clear all rules
	resetIptables(t, client)

	// Restore
	playbook = playbookHeader + `
  - name: Restore all tables
    iptables_state:
      path: /tmp/test_multi.saved
      state: restored
`
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Restore failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when restoring rules")
	}

	// Verify rules are back
	filterRules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(filterRules, "5555") {
		t.Errorf("Filter rule should be restored, got: %s", filterRules)
	}

	natRules := remoteExec(t, client, "iptables -t nat -L POSTROUTING -n")
	if !strings.Contains(natRules, "MASQUERADE") {
		t.Errorf("NAT rule should be restored, got: %s", natRules)
	}
}

// testIptablesStateRealWorldFirewallConfig tests a realistic firewall configuration
func testIptablesStateRealWorldFirewallConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create a realistic firewall config file (keep ACCEPT policy to not break SSH)
	firewallConfig := `*filter
:INPUT ACCEPT [0:0]
:FORWARD ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
-A INPUT -i lo -j ACCEPT
-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
-A INPUT -p icmp -j ACCEPT
-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT
-A INPUT -p tcp -m tcp --dport 80 -j ACCEPT
-A INPUT -p tcp -m tcp --dport 443 -j ACCEPT
COMMIT
`
	client.Run("cat > /tmp/firewall.rules << 'EOF'\n" + firewallConfig + "EOF")

	playbook := playbookHeader + `
  - name: Apply firewall configuration
    iptables_state:
      path: /tmp/firewall.rules
      state: restored
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify rules
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "dpt:22") {
		t.Errorf("SSH rule should exist, got: %s", rules)
	}
	if !strings.Contains(rules, "dpt:80") {
		t.Errorf("HTTP rule should exist, got: %s", rules)
	}
	if !strings.Contains(rules, "dpt:443") {
		t.Errorf("HTTPS rule should exist, got: %s", rules)
	}

	// Test idempotency
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Second apply should NOT report CHANGED (idempotent)")
	}
}

// TestPlaybook_IptablesStateFilePermissions tests file permissions on saved state
func TestPlaybook_IptablesStateFilePermissions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -f /tmp/test_perms.saved")

	playbook := playbookHeader + `
  - name: Save iptables state
    iptables_state:
      path: /tmp/test_perms.saved
      state: saved
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Check file permissions (should be 600 for security)
	mode := remoteFileMode(t, client, "/tmp/test_perms.saved")
	if mode != "600" {
		t.Errorf("State file should have mode 600, got: %s", mode)
	}
}

// TestPlaybook_IptablesStateNonexistentRestoreFile tests error on missing restore file
func TestPlaybook_IptablesStateNonexistentRestoreFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -f /tmp/nonexistent.saved")

	playbook := playbookHeader + `
  - name: Restore from nonexistent file
    iptables_state:
      path: /tmp/nonexistent.saved
      state: restored
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED for nonexistent file, got: %s", output)
	}
	if !strings.Contains(output, "failed to read file") && !strings.Contains(output, "no such file") {
		t.Errorf("Error should mention file not found, got: %s", output)
	}
}

// TestPlaybook_IptablesStateDirectoryCreation tests directory creation for save path
func TestPlaybook_IptablesStateDirectoryCreation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -rf /tmp/nested")

	playbook := playbookHeader + `
  - name: Save to nested directory
    iptables_state:
      path: /tmp/nested/path/to/rules.saved
      state: saved
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if !remoteFileExists(t, client, "/tmp/nested/path/to/rules.saved") {
		t.Error("State file should exist in nested directory")
	}
}

// TestPlaybook_IptablesStateEmptyTables tests behavior with empty iptables
func TestPlaybook_IptablesStateEmptyTables(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	client.Run("rm -f /tmp/empty.saved")

	playbook := playbookHeader + `
  - name: Save empty iptables state
    iptables_state:
      path: /tmp/empty.saved
      state: saved
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify file contains basic structure
	content := remoteFileContent(t, client, "/tmp/empty.saved")
	if !strings.Contains(content, "*filter") {
		t.Errorf("Should contain filter table header, got: %s", content)
	}
	if !strings.Contains(content, "COMMIT") {
		t.Errorf("Should contain COMMIT, got: %s", content)
	}
}

// TestPlaybook_IptablesStateSaveRestoreCycle tests complete save-modify-restore cycle
func TestPlaybook_IptablesStateSaveRestoreCycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add initial rules
	client.Run("iptables -A INPUT -p tcp --dport 3333 -j ACCEPT")
	client.Run("iptables -A INPUT -p tcp --dport 4444 -j ACCEPT")

	// Save state
	playbook := playbookHeader + `
  - name: Save state
    iptables_state:
      path: /tmp/cycle.saved
      state: saved
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Save failed: %s", output)
	}

	// Modify rules
	client.Run("iptables -A INPUT -p tcp --dport 5555 -j ACCEPT")
	client.Run("iptables -D INPUT -p tcp --dport 3333 -j ACCEPT")

	// Verify modification
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if strings.Contains(rules, "3333") {
		t.Error("Rule 3333 should be deleted")
	}
	if !strings.Contains(rules, "5555") {
		t.Error("Rule 5555 should exist")
	}

	// Restore original state
	playbook = playbookHeader + `
  - name: Restore state
    iptables_state:
      path: /tmp/cycle.saved
      state: restored
`
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Restore failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Restore should report CHANGED")
	}

	// Verify original state is back
	rules = remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "3333") {
		t.Error("Rule 3333 should be restored")
	}
	if !strings.Contains(rules, "4444") {
		t.Error("Rule 4444 should still exist")
	}
	if strings.Contains(rules, "5555") {
		t.Error("Rule 5555 should be removed after restore")
	}
}
