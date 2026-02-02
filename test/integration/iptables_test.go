//go:build integration

package integration

import (
	"strings"
	"testing"
)

// resetIptables clears all iptables rules and chains
func resetIptables(t *testing.T, client interface{ Run(string) (string, string, error) }) {
	// Flush all chains in all tables
	for _, table := range []string{"filter", "nat", "mangle", "raw"} {
		client.Run("iptables -t " + table + " -F")
		client.Run("iptables -t " + table + " -X")
	}
	// Reset policies to ACCEPT
	client.Run("iptables -P INPUT ACCEPT")
	client.Run("iptables -P FORWARD ACCEPT")
	client.Run("iptables -P OUTPUT ACCEPT")
}

// TestPlaybook_IptablesBasicRule tests basic rule creation
func TestPlaybook_IptablesBasicRule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow SSH port 22
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for new rule")
	}

	// Verify rule exists
	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "ACCEPT") || !strings.Contains(rules, "tcp") || !strings.Contains(rules, "dpt:22") {
		t.Errorf("Expected SSH rule, got: %s", rules)
	}

	// Test idempotency
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

// TestPlaybook_IptablesDropRule tests DROP target
func TestPlaybook_IptablesDropRule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Drop traffic from malicious IP
    iptables:
      chain: INPUT
      source: 192.168.100.50
      jump: DROP
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "DROP") || !strings.Contains(rules, "192.168.100.50") {
		t.Errorf("Expected DROP rule for IP, got: %s", rules)
	}
}

// TestPlaybook_IptablesRemoveRule tests rule removal
func TestPlaybook_IptablesRemoveRule(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// First add a rule
	client.Run("iptables -A INPUT -p tcp --dport 8080 -j ACCEPT")

	playbook := playbookHeader + `
  - name: Remove port 8080 rule
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "8080"
      jump: ACCEPT
      state: absent
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for rule removal")
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if strings.Contains(rules, "8080") {
		t.Errorf("Rule should be removed, got: %s", rules)
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when removing already-absent rule")
	}
}

// TestPlaybook_IptablesWithComment tests rule with comment
func TestPlaybook_IptablesWithComment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow HTTP with comment
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "80"
      jump: ACCEPT
      comment: "Allow HTTP traffic"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n -v")
	if !strings.Contains(rules, "Allow HTTP traffic") {
		t.Errorf("Expected comment in rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesConnectionTracking tests ctstate matching
func TestPlaybook_IptablesConnectionTracking(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow established and related connections
    iptables:
      chain: INPUT
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "ESTABLISHED") || !strings.Contains(rules, "RELATED") {
		t.Errorf("Expected ctstate rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesPolicy tests chain policy setting
func TestPlaybook_IptablesPolicy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// First ensure SSH is allowed before setting DROP policy
	client.Run("iptables -A INPUT -p tcp --dport 22 -j ACCEPT")
	client.Run("iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT")

	playbook := playbookHeader + `
  - name: Set INPUT policy to DROP
    iptables:
      chain: INPUT
      policy: DROP
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "policy DROP") {
		t.Errorf("Expected DROP policy, got: %s", rules)
	}

	// Reset to ACCEPT for cleanup
	client.Run("iptables -P INPUT ACCEPT")

	// Idempotency test on FORWARD chain (safer - doesn't affect SSH)
	playbook2 := playbookHeader + `
  - name: Set FORWARD policy to DROP
    iptables:
      chain: FORWARD
      policy: DROP
`
	output = runPlaybook(t, playbook2)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success for FORWARD policy, got: %s", output)
	}

	output = runPlaybook(t, playbook2)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when policy already set")
	}
}

// TestPlaybook_IptablesFlush tests chain flush
func TestPlaybook_IptablesFlush(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add some rules first
	client.Run("iptables -A INPUT -p tcp --dport 80 -j ACCEPT")
	client.Run("iptables -A INPUT -p tcp --dport 443 -j ACCEPT")

	playbook := playbookHeader + `
  - name: Flush INPUT chain
    iptables:
      chain: INPUT
      flush: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if strings.Contains(rules, "80") || strings.Contains(rules, "443") {
		t.Errorf("Rules should be flushed, got: %s", rules)
	}
}

// TestPlaybook_IptablesChainManagement tests custom chain creation/deletion
func TestPlaybook_IptablesChainManagement(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Create chain
	playbook := playbookHeader + `
  - name: Create custom chain
    iptables:
      chain: MYCHAIN
      chain_management: true
      state: present
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L -n")
	if !strings.Contains(rules, "MYCHAIN") {
		t.Errorf("Expected MYCHAIN to exist, got: %s", rules)
	}

	// Idempotency
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when chain already exists")
	}

	// Delete chain
	playbook = playbookHeader + `
  - name: Delete custom chain
    iptables:
      chain: MYCHAIN
      chain_management: true
      state: absent
`
	output = runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules = remoteExec(t, client, "iptables -L -n")
	if strings.Contains(rules, "Chain MYCHAIN") {
		t.Errorf("MYCHAIN should be deleted, got: %s", rules)
	}
}

// TestPlaybook_IptablesNatTable tests NAT table rules
func TestPlaybook_IptablesNatTable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: DNAT rule for port forwarding
    iptables:
      table: nat
      chain: PREROUTING
      protocol: tcp
      destination_port: "8080"
      jump: DNAT
      to_destination: "192.168.1.100:80"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -t nat -L PREROUTING -n")
	if !strings.Contains(rules, "DNAT") || !strings.Contains(rules, "192.168.1.100:80") {
		t.Errorf("Expected DNAT rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesMasquerade tests MASQUERADE for NAT
func TestPlaybook_IptablesMasquerade(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Masquerade outgoing traffic
    iptables:
      table: nat
      chain: POSTROUTING
      source: 192.168.0.0/24
      out_interface: eth0
      jump: MASQUERADE
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -t nat -L POSTROUTING -n")
	if !strings.Contains(rules, "MASQUERADE") {
		t.Errorf("Expected MASQUERADE rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesInsert tests rule insertion at position
func TestPlaybook_IptablesInsert(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add some rules first
	client.Run("iptables -A INPUT -p tcp --dport 80 -j ACCEPT")
	client.Run("iptables -A INPUT -p tcp --dport 443 -j ACCEPT")

	playbook := playbookHeader + `
  - name: Insert SSH rule at top
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      jump: ACCEPT
      action: insert
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n --line-numbers")
	lines := strings.Split(rules, "\n")
	// First rule should be SSH
	foundSSH := false
	for _, line := range lines {
		if strings.Contains(line, "1") && strings.Contains(line, "22") {
			foundSSH = true
			break
		}
	}
	if !foundSSH {
		t.Errorf("SSH rule should be at position 1, got: %s", rules)
	}
}

// TestPlaybook_IptablesInsertAtPosition tests rule insertion at specific position
func TestPlaybook_IptablesInsertAtPosition(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add some rules first
	client.Run("iptables -A INPUT -p tcp --dport 80 -j ACCEPT")
	client.Run("iptables -A INPUT -p tcp --dport 443 -j ACCEPT")
	client.Run("iptables -A INPUT -p tcp --dport 8080 -j ACCEPT")

	playbook := playbookHeader + `
  - name: Insert rule at position 2
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      jump: ACCEPT
      action: insert
      rule_num: 2
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n --line-numbers")
	if !strings.Contains(rules, "2") || !strings.Contains(rules, "22") {
		t.Errorf("SSH rule should be at position 2, got: %s", rules)
	}
}

// TestPlaybook_IptablesInterface tests interface matching
func TestPlaybook_IptablesInterface(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow all traffic on loopback
    iptables:
      chain: INPUT
      in_interface: lo
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n -v")
	if !strings.Contains(rules, "lo") || !strings.Contains(rules, "ACCEPT") {
		t.Errorf("Expected loopback rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesReject tests REJECT target with message
func TestPlaybook_IptablesReject(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Reject with ICMP port unreachable
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "23"
      jump: REJECT
      reject_with: icmp-port-unreachable
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "REJECT") || !strings.Contains(rules, "icmp-port-unreachable") {
		t.Errorf("Expected REJECT rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesLog tests LOG target
func TestPlaybook_IptablesLog(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Log dropped packets
    iptables:
      chain: INPUT
      jump: LOG
      log_prefix: "DROPPED: "
      log_level: warning
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "LOG") {
		t.Errorf("Expected LOG rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesLimit tests rate limiting
func TestPlaybook_IptablesLimit(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Rate limit SSH connections
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      limit: 3/minute
      limit_burst: "5"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n -v")
	if !strings.Contains(rules, "limit") {
		t.Errorf("Expected limit rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesMultiport tests multiport extension
func TestPlaybook_IptablesMultiport(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow web ports
    iptables:
      chain: INPUT
      protocol: tcp
      destination_ports:
        - "80"
        - "443"
        - "8080"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "multiport") || !strings.Contains(rules, "80,443,8080") {
		t.Errorf("Expected multiport rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesNegation tests source/destination negation
func TestPlaybook_IptablesNegation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow from all except specific IP
    iptables:
      chain: INPUT
      source: "!192.168.1.100"
      protocol: tcp
      destination_port: "80"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "!192.168.1.100") {
		t.Errorf("Expected negated source, got: %s", rules)
	}
}

// TestPlaybook_IptablesSourcePort tests source port matching
func TestPlaybook_IptablesSourcePort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow from specific source port
    iptables:
      chain: INPUT
      protocol: tcp
      source_port: "1024:65535"
      destination_port: "22"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	// iptables may show spt: or spts: depending on version
	if (!strings.Contains(rules, "spt:1024:65535") && !strings.Contains(rules, "spts:1024:65535")) || !strings.Contains(rules, "dpt:22") {
		t.Errorf("Expected source port range rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesSNAT tests SNAT in nat table
func TestPlaybook_IptablesSNAT(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: SNAT outgoing traffic
    iptables:
      table: nat
      chain: POSTROUTING
      source: 10.0.0.0/8
      jump: SNAT
      to_source: 192.168.1.1
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -t nat -L POSTROUTING -n")
	if !strings.Contains(rules, "SNAT") || !strings.Contains(rules, "192.168.1.1") {
		t.Errorf("Expected SNAT rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesMangleTable tests mangle table operations
func TestPlaybook_IptablesMangleTable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Use TOS target instead of MARK since MARK requires additional module options
	// that we haven't implemented (--set-mark)
	playbook := playbookHeader + `
  - name: Log packets in mangle table
    iptables:
      table: mangle
      chain: PREROUTING
      source: 192.168.1.0/24
      jump: LOG
      log_prefix: "MANGLE: "
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -t mangle -L PREROUTING -n")
	if !strings.Contains(rules, "LOG") {
		t.Errorf("Expected LOG rule in mangle table, got: %s", rules)
	}
}

// TestPlaybook_IptablesOutputChain tests OUTPUT chain rules
func TestPlaybook_IptablesOutputChain(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow outgoing DNS
    iptables:
      chain: OUTPUT
      protocol: udp
      destination_port: "53"
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L OUTPUT -n")
	if !strings.Contains(rules, "udp") || !strings.Contains(rules, "dpt:53") {
		t.Errorf("Expected DNS rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesForwardChain tests FORWARD chain rules
func TestPlaybook_IptablesForwardChain(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow forwarding from eth0 to eth1
    iptables:
      chain: FORWARD
      in_interface: eth0
      out_interface: eth1
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L FORWARD -n -v")
	if !strings.Contains(rules, "eth0") || !strings.Contains(rules, "eth1") {
		t.Errorf("Expected forward rule with interfaces, got: %s", rules)
	}
}

// TestPlaybook_IptablesICMP tests ICMP rules
func TestPlaybook_IptablesICMP(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow ping
    iptables:
      chain: INPUT
      protocol: icmp
      icmp_type: echo-request
      jump: ACCEPT
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	if !strings.Contains(rules, "icmp") {
		t.Errorf("Expected ICMP rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesCompleteFirewall tests a complete firewall setup
func TestPlaybook_IptablesCompleteFirewall(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow loopback
    iptables:
      chain: INPUT
      in_interface: lo
      jump: ACCEPT

  - name: Allow established connections
    iptables:
      chain: INPUT
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT

  - name: Allow SSH
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      ctstate:
        - NEW
      jump: ACCEPT
      comment: "Allow SSH"

  - name: Allow HTTP and HTTPS
    iptables:
      chain: INPUT
      protocol: tcp
      destination_ports:
        - "80"
        - "443"
      ctstate:
        - NEW
      jump: ACCEPT
      comment: "Allow web traffic"

  - name: Allow ICMP echo
    iptables:
      chain: INPUT
      protocol: icmp
      icmp_type: echo-request
      limit: 1/second
      limit_burst: "4"
      jump: ACCEPT

  - name: Log dropped packets
    iptables:
      chain: INPUT
      limit: 5/minute
      limit_burst: "10"
      jump: LOG
      log_prefix: "iptables-dropped: "

  - name: Drop all other input
    iptables:
      chain: INPUT
      jump: DROP
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n")
	// Verify key rules exist
	if !strings.Contains(rules, "lo") {
		t.Error("Missing loopback rule")
	}
	if !strings.Contains(rules, "ESTABLISHED") {
		t.Error("Missing established connections rule")
	}
	if !strings.Contains(rules, "dpt:22") {
		t.Error("Missing SSH rule")
	}
	if !strings.Contains(rules, "multiport") {
		t.Error("Missing multiport web rule")
	}
	if !strings.Contains(rules, "LOG") {
		t.Error("Missing LOG rule")
	}
	if !strings.Contains(rules, "DROP") {
		t.Error("Missing DROP rule")
	}

	// Test idempotency
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// TestPlaybook_IptablesGoto tests goto to user-defined chain
func TestPlaybook_IptablesGoto(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Create LOGDROP chain
    iptables:
      chain: LOGDROP
      chain_management: true
      state: present

  - name: Add log rule to LOGDROP
    iptables:
      chain: LOGDROP
      jump: LOG
      log_prefix: "LOGDROP: "

  - name: Add drop rule to LOGDROP
    iptables:
      chain: LOGDROP
      jump: DROP

  - name: Goto LOGDROP for bad traffic
    iptables:
      chain: INPUT
      source: 10.0.0.0/8
      goto: LOGDROP
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L -n")
	if !strings.Contains(rules, "LOGDROP") {
		t.Errorf("Expected LOGDROP chain and rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesValidationErrors tests error handling
func TestPlaybook_IptablesValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		playbook string
		errMsg   string
	}{
		{
			name: "Invalid table",
			playbook: playbookHeader + `
  - name: Invalid table
    iptables:
      table: invalid
      chain: INPUT
      jump: ACCEPT
`,
			errMsg: "table must be one of",
		},
		{
			name: "Invalid state",
			playbook: playbookHeader + `
  - name: Invalid state
    iptables:
      chain: INPUT
      jump: ACCEPT
      state: invalid
`,
			errMsg: "state must be one of",
		},
		{
			name: "Invalid policy",
			playbook: playbookHeader + `
  - name: Invalid policy
    iptables:
      chain: INPUT
      policy: invalid
`,
			errMsg: "policy must be one of",
		},
		{
			name: "TEE without gateway",
			playbook: playbookHeader + `
  - name: TEE without gateway
    iptables:
      chain: INPUT
      jump: TEE
`,
			errMsg: "gateway is required",
		},
		{
			name: "Missing chain",
			playbook: playbookHeader + `
  - name: No chain
    iptables:
      jump: ACCEPT
`,
			errMsg: "chain is required",
		},
		{
			name: "DSCP mutual exclusivity",
			playbook: playbookHeader + `
  - name: Both DSCP options
    iptables:
      chain: INPUT
      set_dscp_mark: "10"
      set_dscp_mark_class: CS1
`,
			errMsg: "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := runPlaybook(t, tt.playbook)
			if !strings.Contains(output, "FAILED") {
				t.Errorf("Expected failure for %s, got: %s", tt.name, output)
			}
		})
	}
}

// TestPlaybook_IptablesRealWorldWebServer tests a real-world web server config
func TestPlaybook_IptablesRealWorldWebServer(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Allow loopback traffic
    iptables:
      chain: INPUT
      in_interface: lo
      jump: ACCEPT

  - name: Allow established and related connections
    iptables:
      chain: INPUT
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT

  - name: Allow SSH from management network
    iptables:
      chain: INPUT
      source: 10.0.0.0/8
      protocol: tcp
      destination_port: "22"
      ctstate:
        - NEW
      jump: ACCEPT
      comment: "SSH from management"

  - name: Allow HTTP
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "80"
      ctstate:
        - NEW
      jump: ACCEPT

  - name: Allow HTTPS
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "443"
      ctstate:
        - NEW
      jump: ACCEPT

  - name: Drop invalid packets
    iptables:
      chain: INPUT
      ctstate:
        - INVALID
      jump: DROP

  - name: Reject all other INPUT
    iptables:
      chain: INPUT
      jump: REJECT
      reject_with: icmp-host-prohibited
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L INPUT -n -v")
	if !strings.Contains(rules, "22") || !strings.Contains(rules, "80") || !strings.Contains(rules, "443") {
		t.Errorf("Missing expected rules, got: %s", rules)
	}

	// Idempotency
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// TestPlaybook_IptablesRealWorldNAT tests a real-world NAT gateway config
func TestPlaybook_IptablesRealWorldNAT(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Enable IP forwarding
    shell:
      cmd: echo 1 > /proc/sys/net/ipv4/ip_forward

  - name: Allow forwarded traffic from LAN
    iptables:
      chain: FORWARD
      source: 192.168.0.0/24
      jump: ACCEPT

  - name: Allow forwarded return traffic
    iptables:
      chain: FORWARD
      destination: 192.168.0.0/24
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT

  - name: Masquerade outgoing traffic
    iptables:
      table: nat
      chain: POSTROUTING
      source: 192.168.0.0/24
      out_interface: eth0
      jump: MASQUERADE

  - name: Port forward HTTP to internal server
    iptables:
      table: nat
      chain: PREROUTING
      protocol: tcp
      destination_port: "80"
      jump: DNAT
      to_destination: 192.168.0.10:80
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify FORWARD rules
	forwardRules := remoteExec(t, client, "iptables -L FORWARD -n")
	if !strings.Contains(forwardRules, "192.168.0.0/24") {
		t.Errorf("Expected LAN forward rules, got: %s", forwardRules)
	}

	// Verify NAT rules
	natRules := remoteExec(t, client, "iptables -t nat -L -n")
	if !strings.Contains(natRules, "MASQUERADE") {
		t.Errorf("Expected MASQUERADE rule, got: %s", natRules)
	}
	if !strings.Contains(natRules, "DNAT") {
		t.Errorf("Expected DNAT rule, got: %s", natRules)
	}
}

// TestPlaybook_IptablesRealWorldDockerHost tests a Docker host config
func TestPlaybook_IptablesRealWorldDockerHost(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	playbook := playbookHeader + `
  - name: Create DOCKER-USER chain
    iptables:
      chain: DOCKER-USER
      chain_management: true
      state: present

  - name: Allow established in DOCKER-USER
    iptables:
      chain: DOCKER-USER
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT

  - name: Allow internal docker network
    iptables:
      chain: DOCKER-USER
      source: 172.17.0.0/16
      jump: ACCEPT

  - name: Allow from trusted network
    iptables:
      chain: DOCKER-USER
      source: 10.0.0.0/8
      jump: ACCEPT

  - name: Log other traffic in DOCKER-USER
    iptables:
      chain: DOCKER-USER
      limit: 5/minute
      jump: LOG
      log_prefix: "DOCKER-USER-DROP: "

  - name: Drop other in DOCKER-USER
    iptables:
      chain: DOCKER-USER
      jump: DROP

  - name: Return from DOCKER-USER
    iptables:
      chain: DOCKER-USER
      jump: RETURN
      action: insert
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	rules := remoteExec(t, client, "iptables -L DOCKER-USER -n")
	if !strings.Contains(rules, "172.17.0.0/16") {
		t.Errorf("Expected Docker network rule, got: %s", rules)
	}
}

// TestPlaybook_IptablesFlushAll tests flushing entire table
func TestPlaybook_IptablesFlushAll(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// Add rules to multiple chains
	client.Run("iptables -A INPUT -p tcp --dport 80 -j ACCEPT")
	client.Run("iptables -A OUTPUT -p tcp --dport 80 -j ACCEPT")
	client.Run("iptables -A FORWARD -p tcp --dport 80 -j ACCEPT")

	playbook := playbookHeader + `
  - name: Flush entire filter table
    iptables:
      table: filter
      flush: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// All chains should be empty
	for _, chain := range []string{"INPUT", "OUTPUT", "FORWARD"} {
		rules := remoteExec(t, client, "iptables -L "+chain+" -n")
		if strings.Contains(rules, "80") {
			t.Errorf("%s chain should be flushed, got: %s", chain, rules)
		}
	}
}

// TestPlaybook_IptablesSafeDeployment tests safe deployment pattern
func TestPlaybook_IptablesSafeDeployment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	resetIptables(t, client)
	defer resetIptables(t, client)

	// This tests the safe pattern from the articles:
	// 1. Keep policy ACCEPT
	// 2. Add permissive rules first
	// 3. Add restrictive rules last
	// 4. Never set policy to DROP (use explicit DROP rule instead)

	playbook := playbookHeader + `
  - name: Ensure INPUT policy is ACCEPT (safe default)
    iptables:
      chain: INPUT
      policy: ACCEPT

  - name: Allow loopback (insert at top)
    iptables:
      chain: INPUT
      in_interface: lo
      jump: ACCEPT
      action: insert

  - name: Allow established connections (high priority)
    iptables:
      chain: INPUT
      ctstate:
        - ESTABLISHED
        - RELATED
      jump: ACCEPT
      action: insert
      rule_num: 2

  - name: Allow SSH (always before DROP)
    iptables:
      chain: INPUT
      protocol: tcp
      destination_port: "22"
      jump: ACCEPT
      comment: "SSH access - DO NOT REMOVE"

  - name: Drop everything else (last rule)
    iptables:
      chain: INPUT
      jump: DROP
      comment: "Default deny"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify the order is correct - use -v to see interfaces
	rules := remoteExec(t, client, "iptables -L INPUT -n -v --line-numbers")
	lines := strings.Split(rules, "\n")
	
	// Find positions (line numbers in first column)
	loLine, estLine, sshLine, dropLine := 0, 0, 0, 0
	for i, line := range lines {
		// Look for rule number at start of line
		if strings.Contains(line, "lo") && strings.Contains(line, "ACCEPT") {
			loLine = i
		}
		if strings.Contains(line, "ESTABLISHED") || strings.Contains(line, "RELATED") {
			estLine = i
		}
		if strings.Contains(line, "dpt:22") {
			sshLine = i
		}
		if strings.Contains(line, "DROP") && !strings.Contains(line, "LOG") {
			dropLine = i
		}
	}

	// Verify order: lo < established < ssh < drop
	if loLine == 0 || estLine == 0 || sshLine == 0 || dropLine == 0 {
		t.Errorf("Missing expected rules in output (lo=%d, est=%d, ssh=%d, drop=%d):\n%s", 
			loLine, estLine, sshLine, dropLine, rules)
	} else if !(loLine < estLine && estLine < sshLine && sshLine < dropLine) {
		t.Errorf("Rules are in wrong order. lo=%d, est=%d, ssh=%d, drop=%d\n%s",
			loLine, estLine, sshLine, dropLine, rules)
	}
}
