package iptables

import (
	"fmt"
	"os/exec"
	"strings"
)

// Execute runs the iptables module logic
func Execute(req Request) Response {
	normalizeRequest(&req)

	if err := validateRequest(&req); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	commands := []string{}
	changed := false

	ipVersions := []string{"ipv4"}
	if req.IPVersion == "ipv6" {
		ipVersions = []string{"ipv6"}
	} else if req.IPVersion == "both" {
		ipVersions = []string{"ipv4", "ipv6"}
	}

	for _, ipv := range ipVersions {
		iptPath := getIptablesPath(ipv)
		if iptPath == "" {
			if ipv == "ipv4" {
				return Response{Failed: true, Msg: "iptables command not found"}
			}
			return Response{Failed: true, Msg: "ip6tables command not found"}
		}

		resp := executeForIPVersion(iptPath, ipv, req, &commands)
		if resp.Failed {
			return resp
		}
		if resp.Changed {
			changed = true
		}
	}

	return Response{
		Changed:  changed,
		Commands: commands,
		State:    req.State,
		Table:    req.Table,
		Chain:    req.Chain,
	}
}

func getIptablesPath(ipVersion string) string {
	if ipVersion == "ipv6" {
		path, err := exec.LookPath("ip6tables")
		if err != nil {
			return ""
		}
		return path
	}
	path, err := exec.LookPath("iptables")
	if err != nil {
		return ""
	}
	return path
}

func normalizeRequest(req *Request) {
	if req.Table == "" {
		req.Table = "filter"
	}
	if req.State == "" {
		req.State = "present"
	}
	if req.Action == "" {
		req.Action = "append"
	}
	if req.IPVersion == "" {
		req.IPVersion = "ipv4"
	}
	if req.Syn == "" {
		req.Syn = "ignore"
	}

	// If log_prefix or log_level is set but jump is not, default jump to LOG
	if (req.LogPrefix != "" || req.LogLevel != "") && req.Jump == "" {
		req.Jump = "LOG"
	}

	// If set_dscp_mark or set_dscp_mark_class is set but jump is not, default to DSCP
	if (req.SetDscpMark != "" || req.SetDscpMarkClass != "") && req.Jump == "" {
		req.Jump = "DSCP"
	}

	// If reject_with is set, imply jump=REJECT
	if req.RejectWith != "" && req.Jump == "" {
		req.Jump = "REJECT"
	}
}

func validateRequest(req *Request) error {
	validTables := map[string]bool{
		"filter": true, "nat": true, "mangle": true, "raw": true, "security": true,
	}
	if !validTables[req.Table] {
		return fmt.Errorf("table must be one of: filter, nat, mangle, raw, security")
	}

	validStates := map[string]bool{"present": true, "absent": true}
	if !validStates[req.State] {
		return fmt.Errorf("state must be one of: present, absent")
	}

	validActions := map[string]bool{"append": true, "insert": true}
	if !validActions[req.Action] {
		return fmt.Errorf("action must be one of: append, insert")
	}

	validIPVersions := map[string]bool{"ipv4": true, "ipv6": true, "both": true}
	if !validIPVersions[req.IPVersion] {
		return fmt.Errorf("ip_version must be one of: ipv4, ipv6, both")
	}

	validSyn := map[string]bool{"ignore": true, "match": true, "negate": true}
	if !validSyn[req.Syn] {
		return fmt.Errorf("syn must be one of: ignore, match, negate")
	}

	if req.Policy != "" {
		validPolicies := map[string]bool{
			"ACCEPT": true, "DROP": true, "QUEUE": true, "RETURN": true,
		}
		if !validPolicies[req.Policy] {
			return fmt.Errorf("policy must be one of: ACCEPT, DROP, QUEUE, RETURN")
		}
	}

	// Mutual exclusivity checks
	if req.SetDscpMark != "" && req.SetDscpMarkClass != "" {
		return fmt.Errorf("set_dscp_mark and set_dscp_mark_class are mutually exclusive")
	}

	if req.Flush && req.Policy != "" {
		return fmt.Errorf("flush and policy are mutually exclusive")
	}

	// Required dependencies
	if req.Jump == "TEE" && req.Gateway == "" {
		return fmt.Errorf("gateway is required when jump=TEE")
	}

	if req.MatchSet != "" && req.MatchSetFlags == "" {
		return fmt.Errorf("match_set_flags is required when match_set is specified")
	}

	if req.MatchSetFlags != "" && req.MatchSet == "" {
		return fmt.Errorf("match_set is required when match_set_flags is specified")
	}

	if req.RuleNum > 0 && req.Action != "insert" {
		return fmt.Errorf("rule_num can only be used with action=insert")
	}

	// Chain is required unless flushing entire table
	if !req.Flush && req.Policy == "" && req.Chain == "" {
		return fmt.Errorf("chain is required")
	}

	return nil
}

func executeForIPVersion(iptPath, ipVersion string, req Request, commands *[]string) Response {
	// Handle flush
	if req.Flush {
		return handleFlush(iptPath, req, commands)
	}

	// Handle policy
	if req.Policy != "" {
		return handlePolicy(iptPath, req, commands)
	}

	// Handle chain management (create/delete chain with no rule spec)
	if req.ChainManagement && !hasRuleSpec(req) {
		return handleChainManagement(iptPath, req, commands)
	}

	// Handle rule operations
	return handleRule(iptPath, ipVersion, req, commands)
}

func hasRuleSpec(req Request) bool {
	return req.Protocol != "" ||
		req.Source != "" ||
		req.Destination != "" ||
		len(req.Match) > 0 ||
		req.Jump != "" ||
		req.Goto != "" ||
		req.InInterface != "" ||
		req.OutInterface != "" ||
		req.SourcePort != "" ||
		req.DestinationPort != "" ||
		len(req.DestinationPorts) > 0 ||
		len(req.Ctstate) > 0 ||
		req.Comment != "" ||
		req.IcmpType != "" ||
		req.Fragment != "" ||
		req.TcpFlags != nil ||
		req.Limit != "" ||
		req.LogPrefix != "" ||
		req.LogLevel != "" ||
		req.RejectWith != "" ||
		req.ToDestination != "" ||
		req.ToSource != "" ||
		req.ToPorts != "" ||
		req.Gateway != "" ||
		req.SrcRange != "" ||
		req.DstRange != "" ||
		req.SetDscpMark != "" ||
		req.SetDscpMarkClass != "" ||
		req.UidOwner != "" ||
		req.GidOwner != "" ||
		req.MatchSet != ""
}

func handleFlush(iptPath string, req Request, commands *[]string) Response {
	args := []string{"-t", req.Table, "-F"}
	if req.Chain != "" {
		args = append(args, req.Chain)
	}

	output, err := runIptables(iptPath, args, req.Wait, commands)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("flush failed: %s - %s", err, output)}
	}

	return Response{Changed: true, Flushed: true, Table: req.Table, Chain: req.Chain}
}

func handlePolicy(iptPath string, req Request, commands *[]string) Response {
	if req.Chain == "" {
		return Response{Failed: true, Msg: "chain is required for setting policy"}
	}

	// Check current policy
	currentPolicy := getChainPolicy(iptPath, req.Table, req.Chain, req.Numeric, commands)
	if currentPolicy == req.Policy {
		return Response{Changed: false, Policy: req.Policy}
	}

	args := []string{"-t", req.Table, "-P", req.Chain, req.Policy}
	output, err := runIptables(iptPath, args, req.Wait, commands)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("set policy failed: %s - %s", err, output)}
	}

	return Response{Changed: true, Policy: req.Policy}
}

func getChainPolicy(iptPath, table, chain string, numeric bool, commands *[]string) string {
	args := []string{"-t", table, "-L", chain}
	if numeric {
		args = append(args, "-n")
	}

	output, err := runIptablesNoLog(iptPath, args, 0)
	if err != nil {
		return ""
	}

	lines := strings.Split(output, "\n")
	if len(lines) > 0 {
		// Parse "Chain INPUT (policy ACCEPT)"
		line := lines[0]
		if strings.Contains(line, "policy") {
			start := strings.Index(line, "policy ")
			if start != -1 {
				rest := line[start+7:]
				end := strings.Index(rest, ")")
				if end != -1 {
					return rest[:end]
				}
			}
		}
	}
	return ""
}

func handleChainManagement(iptPath string, req Request, commands *[]string) Response {
	chainExists := checkChainPresent(iptPath, req.Table, req.Chain, req.Numeric)

	if req.State == "present" {
		if chainExists {
			return Response{Changed: false, Chain: req.Chain}
		}
		// Create chain
		args := []string{"-t", req.Table, "-N", req.Chain}
		output, err := runIptables(iptPath, args, req.Wait, commands)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("create chain failed: %s - %s", err, output)}
		}
		return Response{Changed: true, Chain: req.Chain}
	}

	// state == absent
	if !chainExists {
		return Response{Changed: false, Chain: req.Chain}
	}
	// Delete chain
	args := []string{"-t", req.Table, "-X", req.Chain}
	output, err := runIptables(iptPath, args, req.Wait, commands)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("delete chain failed: %s - %s", err, output)}
	}
	return Response{Changed: true, Chain: req.Chain}
}

func checkChainPresent(iptPath, table, chain string, numeric bool) bool {
	args := []string{"-t", table, "-L", chain}
	if numeric {
		args = append(args, "-n")
	}
	_, err := runIptablesNoLog(iptPath, args, 0)
	return err == nil
}

func handleRule(iptPath, ipVersion string, req Request, commands *[]string) Response {
	rule := constructRule(req, ipVersion)

	// Check if rule exists
	ruleExists := checkRulePresent(iptPath, req.Table, req.Chain, rule, req.Wait)

	if req.State == "present" {
		if ruleExists {
			return Response{Changed: false, Rule: strings.Join(rule, " ")}
		}
		// Add rule
		var args []string
		if req.Action == "insert" {
			if req.RuleNum > 0 {
				args = append([]string{"-t", req.Table, "-I", req.Chain, fmt.Sprintf("%d", req.RuleNum)}, rule...)
			} else {
				args = append([]string{"-t", req.Table, "-I", req.Chain}, rule...)
			}
		} else {
			args = append([]string{"-t", req.Table, "-A", req.Chain}, rule...)
		}

		output, err := runIptables(iptPath, args, req.Wait, commands)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("add rule failed: %s - %s", err, output)}
		}
		return Response{Changed: true, Rule: strings.Join(rule, " ")}
	}

	// state == absent
	if !ruleExists {
		return Response{Changed: false, Rule: strings.Join(rule, " ")}
	}
	// Delete rule
	args := append([]string{"-t", req.Table, "-D", req.Chain}, rule...)
	output, err := runIptables(iptPath, args, req.Wait, commands)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("delete rule failed: %s - %s", err, output)}
	}
	return Response{Changed: true, Rule: strings.Join(rule, " ")}
}

func checkRulePresent(iptPath, table, chain string, rule []string, wait int) bool {
	args := append([]string{"-t", table, "-C", chain}, rule...)
	_, err := runIptablesNoLog(iptPath, args, wait)
	return err == nil
}

func constructRule(req Request, ipVersion string) []string {
	var rule []string
	loadedExtensions := make(map[string]bool)

	// Protocol
	rule = appendParam(rule, req.Protocol, "-p")

	// Source/Destination
	rule = appendParam(rule, req.Source, "-s")
	rule = appendParam(rule, req.Destination, "-d")

	// Explicit match extensions
	for _, m := range req.Match {
		if !loadedExtensions[m] {
			rule = append(rule, "-m", m)
			loadedExtensions[m] = true
		}
	}

	// TCP flags
	if req.TcpFlags != nil && len(req.TcpFlags.Flags) > 0 && len(req.TcpFlags.FlagsSet) > 0 {
		if !loadedExtensions["tcp"] && req.Protocol == "tcp" {
			// tcp match is implicit with -p tcp
		}
		rule = append(rule, "--tcp-flags",
			strings.Join(req.TcpFlags.Flags, ","),
			strings.Join(req.TcpFlags.FlagsSet, ","))
	}

	// SYN matching
	if req.Syn == "match" {
		rule = append(rule, "--syn")
	} else if req.Syn == "negate" {
		rule = append(rule, "!", "--syn")
	}

	// Jump target
	if req.Jump != "" {
		rule = append(rule, "-j", req.Jump)
	}

	// Goto
	if req.Goto != "" {
		rule = append(rule, "-g", req.Goto)
	}

	// Gateway (for TEE target)
	if req.Gateway != "" {
		rule = append(rule, "--gateway", req.Gateway)
	}

	// Log options
	if req.LogPrefix != "" {
		rule = append(rule, "--log-prefix", req.LogPrefix)
	}
	if req.LogLevel != "" {
		rule = append(rule, "--log-level", req.LogLevel)
	}

	// REJECT options
	if req.RejectWith != "" {
		rule = append(rule, "--reject-with", req.RejectWith)
	}

	// NAT options
	if req.ToDestination != "" {
		rule = append(rule, "--to-destination", req.ToDestination)
	}
	if req.ToSource != "" {
		rule = append(rule, "--to-source", req.ToSource)
	}
	if req.ToPorts != "" {
		rule = append(rule, "--to-ports", req.ToPorts)
	}

	// Multiport
	if len(req.DestinationPorts) > 0 {
		if !loadedExtensions["multiport"] {
			rule = append(rule, "-m", "multiport")
			loadedExtensions["multiport"] = true
		}
		rule = append(rule, "--dports", strings.Join(req.DestinationPorts, ","))
	}

	// Interface matching
	rule = appendParam(rule, req.InInterface, "-i")
	rule = appendParam(rule, req.OutInterface, "-o")

	// Fragment
	if req.Fragment != "" {
		if strings.HasPrefix(req.Fragment, "!") {
			rule = append(rule, "!", "-f")
		} else {
			rule = append(rule, "-f")
		}
	}

	// Counters
	if req.SetCounters != "" {
		rule = append(rule, "-c", req.SetCounters)
	}

	// Port specifications
	if req.SourcePort != "" {
		rule = append(rule, "--source-port", req.SourcePort)
	}
	if req.DestinationPort != "" {
		rule = append(rule, "--destination-port", req.DestinationPort)
	}

	// DSCP marking
	if req.SetDscpMark != "" {
		rule = append(rule, "--set-dscp", req.SetDscpMark)
	}
	if req.SetDscpMarkClass != "" {
		rule = append(rule, "--set-dscp-class", req.SetDscpMarkClass)
	}

	// Connection tracking states
	if len(req.Ctstate) > 0 {
		// Check if conntrack or state is already loaded
		if !loadedExtensions["conntrack"] && !loadedExtensions["state"] {
			rule = append(rule, "-m", "conntrack")
			loadedExtensions["conntrack"] = true
		}
		if loadedExtensions["state"] {
			rule = append(rule, "--state", strings.Join(req.Ctstate, ","))
		} else {
			rule = append(rule, "--ctstate", strings.Join(req.Ctstate, ","))
		}
	}

	// IP ranges
	if req.SrcRange != "" {
		if !loadedExtensions["iprange"] {
			rule = append(rule, "-m", "iprange")
			loadedExtensions["iprange"] = true
		}
		rule = appendParam(rule, req.SrcRange, "--src-range")
	}
	if req.DstRange != "" {
		if !loadedExtensions["iprange"] {
			rule = append(rule, "-m", "iprange")
			loadedExtensions["iprange"] = true
		}
		rule = appendParam(rule, req.DstRange, "--dst-range")
	}

	// Ipset matching
	if req.MatchSet != "" {
		if !loadedExtensions["set"] {
			rule = append(rule, "-m", "set")
			loadedExtensions["set"] = true
		}
		rule = appendParam(rule, req.MatchSet, "--match-set")
		if req.MatchSetFlags != "" {
			rule = append(rule, req.MatchSetFlags)
		}
	}

	// Rate limiting
	if req.Limit != "" {
		if !loadedExtensions["limit"] {
			rule = append(rule, "-m", "limit")
			loadedExtensions["limit"] = true
		}
		rule = append(rule, "--limit", req.Limit)
	}
	if req.LimitBurst != "" {
		if !loadedExtensions["limit"] {
			rule = append(rule, "-m", "limit")
			loadedExtensions["limit"] = true
		}
		rule = append(rule, "--limit-burst", req.LimitBurst)
	}

	// Owner matching
	if req.UidOwner != "" {
		if !loadedExtensions["owner"] {
			rule = append(rule, "-m", "owner")
			loadedExtensions["owner"] = true
		}
		rule = append(rule, "--uid-owner", req.UidOwner)
	}
	if req.GidOwner != "" {
		if !loadedExtensions["owner"] {
			rule = append(rule, "-m", "owner")
			loadedExtensions["owner"] = true
		}
		rule = append(rule, "--gid-owner", req.GidOwner)
	}

	// ICMP type
	if req.IcmpType != "" {
		if ipVersion == "ipv6" {
			rule = append(rule, "--icmpv6-type", req.IcmpType)
		} else {
			rule = append(rule, "--icmp-type", req.IcmpType)
		}
	}

	// Comment (add last)
	if req.Comment != "" {
		if !loadedExtensions["comment"] {
			rule = append(rule, "-m", "comment")
			loadedExtensions["comment"] = true
		}
		rule = append(rule, "--comment", req.Comment)
	}

	return rule
}

func appendParam(rule []string, value, flag string) []string {
	if value == "" {
		return rule
	}
	// Handle negation
	if strings.HasPrefix(value, "!") {
		return append(rule, "!", flag, strings.TrimPrefix(value, "!"))
	}
	return append(rule, flag, value)
}

func runIptables(iptPath string, args []string, wait int, commands *[]string) (string, error) {
	if wait > 0 {
		args = append([]string{"-w", fmt.Sprintf("%d", wait)}, args...)
	}

	cmdStr := iptPath + " " + strings.Join(args, " ")
	*commands = append(*commands, cmdStr)

	cmd := exec.Command(iptPath, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runIptablesNoLog(iptPath string, args []string, wait int) (string, error) {
	if wait > 0 {
		args = append([]string{"-w", fmt.Sprintf("%d", wait)}, args...)
	}
	cmd := exec.Command(iptPath, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
