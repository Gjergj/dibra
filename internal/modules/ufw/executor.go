package ufw

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func Execute(req Request) Response {
	normalizeRequest(&req)

	if err := validateRequest(req); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	ufwPath, err := exec.LookPath("ufw")
	if err != nil {
		return Response{Failed: true, Msg: "ufw command not found - is ufw package installed?"}
	}

	preState := getUFWStatus(ufwPath)
	preRules := getCurrentRules()

	commands := []string{}
	changed := false

	if req.State != "" {
		resp := handleState(ufwPath, req, preState, &commands)
		if resp.Failed {
			return resp
		}
		changed = resp.Changed
	}

	if req.Logging != "" {
		resp := handleLogging(ufwPath, req, preState, &commands)
		if resp.Failed {
			return resp
		}
		if resp.Changed {
			changed = true
		}
	}

	if req.Default != "" {
		resp := handleDefault(ufwPath, req, preState, &commands)
		if resp.Failed {
			return resp
		}
		if resp.Changed {
			changed = true
		}
	}

	if req.Rule != "" {
		resp := handleRule(ufwPath, req, preRules, &commands)
		if resp.Failed {
			return resp
		}
		if resp.Changed {
			changed = true
		}
	}

	postState := getUFWStatus(ufwPath)
	if !changed {
		postRules := getCurrentRules()
		if preState != postState || preRules != postRules {
			changed = true
		}
	}

	return Response{
		Changed:  changed,
		Commands: commands,
		Msg:      strings.TrimSpace(postState),
	}
}

func normalizeRequest(req *Request) {
	if req.Policy != "" && req.Default == "" {
		req.Default = req.Policy
	}
	if req.From != "" && req.FromIP == "" {
		req.FromIP = req.From
	}
	if req.Src != "" && req.FromIP == "" {
		req.FromIP = req.Src
	}
	if req.Dest != "" && req.ToIP == "" {
		req.ToIP = req.Dest
	}
	if req.To != "" && req.ToIP == "" {
		req.ToIP = req.To
	}
	if req.Port != "" && req.ToPort == "" {
		req.ToPort = req.Port
	}
	if req.If != "" && req.Interface == "" {
		req.Interface = req.If
	}
	if req.IfIn != "" && req.InterfaceIn == "" {
		req.InterfaceIn = req.IfIn
	}
	if req.IfOut != "" && req.InterfaceOut == "" {
		req.InterfaceOut = req.IfOut
	}
	if req.App != "" && req.Name == "" {
		req.Name = req.App
	}
	if req.Protocol != "" && req.Proto == "" {
		req.Proto = req.Protocol
	}

	if req.FromIP == "" {
		req.FromIP = "any"
	}
	if req.ToIP == "" {
		req.ToIP = "any"
	}
	if req.InsertRelativeTo == "" {
		req.InsertRelativeTo = "zero"
	}
}

func validateRequest(req Request) error {
	if req.State != "" {
		validStates := map[string]bool{
			"enabled": true, "disabled": true, "reloaded": true, "reset": true,
		}
		if !validStates[req.State] {
			return fmt.Errorf("state must be one of: enabled, disabled, reloaded, reset")
		}
	}

	if req.Logging != "" {
		validLogging := map[string]bool{
			"on": true, "off": true, "low": true, "medium": true, "high": true, "full": true,
		}
		if !validLogging[req.Logging] {
			return fmt.Errorf("logging must be one of: on, off, low, medium, high, full")
		}
	}

	if req.Default != "" {
		validDefaults := map[string]bool{"allow": true, "deny": true, "reject": true}
		if !validDefaults[req.Default] {
			return fmt.Errorf("default/policy must be one of: allow, deny, reject")
		}
		if req.Direction != "" {
			validDirs := map[string]bool{"incoming": true, "outgoing": true, "routed": true}
			if !validDirs[req.Direction] {
				return fmt.Errorf("for default, direction must be one of: incoming, outgoing, routed")
			}
		}
	}

	if req.Rule != "" {
		validRules := map[string]bool{"allow": true, "deny": true, "limit": true, "reject": true}
		if !validRules[req.Rule] {
			return fmt.Errorf("rule must be one of: allow, deny, limit, reject")
		}
		if req.Direction != "" {
			validDirs := map[string]bool{"in": true, "out": true}
			if !validDirs[req.Direction] {
				return fmt.Errorf("for rules, direction must be one of: in, out")
			}
		}
	}

	if req.Proto != "" {
		validProtos := map[string]bool{
			"any": true, "tcp": true, "udp": true, "ipv6": true,
			"esp": true, "ah": true, "gre": true, "igmp": true, "vrrp": true,
		}
		if !validProtos[req.Proto] {
			return fmt.Errorf("proto must be one of: any, tcp, udp, ipv6, esp, ah, gre, igmp, vrrp")
		}
	}

	if req.Direction != "" && (req.InterfaceIn != "" || req.InterfaceOut != "") {
		return fmt.Errorf("direction is mutually exclusive with interface_in and interface_out")
	}

	if req.Interface != "" && (req.InterfaceIn != "" || req.InterfaceOut != "") {
		return fmt.Errorf("interface is mutually exclusive with interface_in and interface_out")
	}

	if req.Interface != "" && req.Direction == "" && req.Rule != "" {
		return fmt.Errorf("interface requires direction to be specified")
	}

	if req.InsertRelativeTo != "" {
		validRelative := map[string]bool{
			"zero": true, "first-ipv4": true, "last-ipv4": true, "first-ipv6": true, "last-ipv6": true,
		}
		if !validRelative[req.InsertRelativeTo] {
			return fmt.Errorf("insert_relative_to must be one of: zero, first-ipv4, last-ipv4, first-ipv6, last-ipv6")
		}
	}

	hasCommand := req.State != "" || req.Logging != "" || req.Default != "" || req.Rule != ""
	if !hasCommand {
		return fmt.Errorf("one of state, logging, default/policy, or rule is required")
	}

	return nil
}

func getUFWStatus(ufwPath string) string {
	cmd := exec.Command(ufwPath, "status", "verbose")
	cmd.Env = append(cmd.Environ(), "LANG=C")
	output, _ := cmd.Output()
	return string(output)
}

func getCurrentRules() string {
	data := []byte{}
	for _, path := range []string{"/etc/ufw/user.rules", "/lib/ufw/user.rules"} {
		if content, err := os.ReadFile(path); err == nil {
			data = append(data, content...)
		}
	}
	for _, path := range []string{"/etc/ufw/user6.rules", "/lib/ufw/user6.rules"} {
		if content, err := os.ReadFile(path); err == nil {
			data = append(data, content...)
		}
	}
	return string(data)
}

func runUFW(ufwPath string, args []string, commands *[]string) (string, error) {
	cmdStr := ufwPath + " " + strings.Join(args, " ")
	*commands = append(*commands, cmdStr)

	cmd := exec.Command(ufwPath, args...)
	cmd.Env = append(cmd.Environ(), "LANG=C")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func handleState(ufwPath string, req Request, preState string, commands *[]string) Response {
	stateMap := map[string]string{
		"enabled":  "enable",
		"disabled": "disable",
		"reloaded": "reload",
		"reset":    "reset",
	}

	if req.State == "reloaded" || req.State == "reset" {
		if req.State == "reset" {
			// Clean up backup files that might cause reset to fail if called multiple times
			// within the same second (UFW uses timestamps with second precision for backups)
			exec.Command("sh", "-c", "rm -f /etc/ufw/*.rules.*").Run()
		}
		args := []string{"--force", stateMap[req.State]}
		if output, err := runUFW(ufwPath, args, commands); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to %s ufw: %v: %s", req.State, err, strings.TrimSpace(output))}
		}
		return Response{Changed: true}
	}

	isActive := strings.Contains(preState, "Status: active")

	if (req.State == "enabled" && isActive) || (req.State == "disabled" && !isActive) {
		return Response{Changed: false}
	}

	args := []string{"--force", stateMap[req.State]}
	if _, err := runUFW(ufwPath, args, commands); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to %s ufw: %v", req.State, err)}
	}

	return Response{Changed: true}
}

func handleLogging(ufwPath string, req Request, preState string, commands *[]string) Response {
	loggingRe := regexp.MustCompile(`Logging: (on|off)(?: \(([a-z]+)\))?`)
	matches := loggingRe.FindStringSubmatch(preState)

	var currentOnOff, currentLevel string
	if matches != nil {
		currentOnOff = matches[1]
		if len(matches) > 2 {
			currentLevel = matches[2]
		}
	}

	needsChange := false
	if req.Logging == "off" {
		if currentOnOff != "off" {
			needsChange = true
		}
	} else if req.Logging == "on" {
		if currentOnOff == "off" {
			needsChange = true
		}
	} else {
		if currentOnOff == "off" || currentLevel != req.Logging {
			needsChange = true
		}
	}

	if !needsChange {
		return Response{Changed: false}
	}

	args := []string{"logging", req.Logging}
	if _, err := runUFW(ufwPath, args, commands); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set logging: %v", err)}
	}

	return Response{Changed: true}
}

func handleDefault(ufwPath string, req Request, preState string, commands *[]string) Response {
	dir := req.Direction
	if dir == "" {
		dir = "incoming"
	}

	defaultRe := regexp.MustCompile(`Default: (deny|allow|reject) \(incoming\), (deny|allow|reject) \(outgoing\), (deny|allow|reject|disabled) \(routed\)`)
	matches := defaultRe.FindStringSubmatch(preState)

	if matches != nil {
		currentDefaults := map[string]string{
			"incoming": matches[1],
			"outgoing": matches[2],
			"routed":   matches[3],
		}
		if currentDefaults[dir] == req.Default || (dir == "routed" && currentDefaults[dir] == "disabled" && req.Default == "deny") {
			return Response{Changed: false}
		}
	}

	args := []string{"default", req.Default}
	if dir != "incoming" {
		args = append(args, dir)
	}

	if _, err := runUFW(ufwPath, args, commands); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set default policy: %v", err)}
	}

	return Response{Changed: true}
}

func handleRule(ufwPath string, req Request, preRules string, commands *[]string) Response {
	args := buildRuleCommand(req)

	existingRules := parseExistingRules(preRules)
	ruleSignature := buildRuleSignature(req)

	ruleExists := false
	for _, existing := range existingRules {
		if existing == ruleSignature {
			ruleExists = true
			break
		}
	}

	if req.Delete {
		if !ruleExists {
			return Response{Changed: false}
		}
		deleteArgs := append([]string{"--force", "delete"}, args...)
		if _, err := runUFW(ufwPath, deleteArgs, commands); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to delete rule: %v", err)}
		}
		return Response{Changed: true}
	}

	if ruleExists {
		return Response{Changed: false}
	}

	var finalArgs []string
	if req.Insert > 0 {
		insertPos := calculateInsertPosition(req, preRules)
		finalArgs = append(finalArgs, "insert", strconv.Itoa(insertPos))
		finalArgs = append(finalArgs, args[1:]...)
	} else {
		finalArgs = args
	}

	if output, err := runUFW(ufwPath, finalArgs, commands); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to add rule: %v - %s", err, output)}
	}

	return Response{Changed: true}
}

func buildRuleCommand(req Request) []string {
	args := []string{}

	if req.Route {
		args = append(args, "route")
	}

	args = append(args, req.Rule)

	if req.Direction != "" && req.Interface == "" {
		args = append(args, req.Direction)
	}

	if req.Interface != "" {
		args = append(args, req.Direction, "on", req.Interface)
	}

	if req.InterfaceIn != "" {
		args = append(args, "in", "on", req.InterfaceIn)
	}

	if req.InterfaceOut != "" {
		args = append(args, "out", "on", req.InterfaceOut)
	}

	if req.Log {
		args = append(args, "log")
	}

	hasFromIP := req.FromIP != "any"
	hasFromPort := req.FromPort != ""
	hasToIP := req.ToIP != "any"
	hasToPort := req.ToPort != ""
	hasProto := req.Proto != "" && req.Proto != "any"
	hasApp := req.Name != ""

	isSimplePortRule := !hasFromIP && !hasFromPort && !hasToIP && !hasApp && hasToPort

	if isSimplePortRule {
		if hasProto {
			args = append(args, req.ToPort+"/"+req.Proto)
		} else {
			args = append(args, req.ToPort)
		}
	} else {
		if hasFromIP || hasFromPort {
			args = append(args, "from", req.FromIP)
			if hasFromPort {
				args = append(args, "port", req.FromPort)
			}
		}

		if hasToIP || hasToPort {
			args = append(args, "to", req.ToIP)
			if hasToPort {
				args = append(args, "port", req.ToPort)
			}
		}

		if hasProto {
			args = append(args, "proto", req.Proto)
		}
	}

	if hasApp {
		args = append(args, "app", req.Name)
	}

	if req.Comment != "" {
		args = append(args, "comment", req.Comment)
	}

	return args
}

func buildRuleSignature(req Request) string {
	parts := []string{req.Rule}

	if req.Route {
		parts = append(parts, "route")
	}

	if req.Direction != "" {
		parts = append(parts, req.Direction)
	}

	if req.Interface != "" {
		parts = append(parts, "if:"+req.Interface)
	}

	if req.InterfaceIn != "" {
		parts = append(parts, "in:"+req.InterfaceIn)
	}

	if req.InterfaceOut != "" {
		parts = append(parts, "out:"+req.InterfaceOut)
	}

	fromIP := req.FromIP
	if fromIP == "" {
		fromIP = "any"
	}
	parts = append(parts, "from:"+fromIP)

	if req.FromPort != "" {
		parts = append(parts, "fport:"+req.FromPort)
	}

	toIP := req.ToIP
	if toIP == "" {
		toIP = "any"
	}
	parts = append(parts, "to:"+toIP)

	if req.ToPort != "" {
		parts = append(parts, "tport:"+req.ToPort)
	}

	if req.Proto != "" && req.Proto != "any" {
		parts = append(parts, "proto:"+req.Proto)
	}

	if req.Name != "" {
		parts = append(parts, "app:"+req.Name)
	}

	return strings.Join(parts, "|")
}

func parseExistingRules(rulesContent string) []string {
	var signatures []string

	tupleRe := regexp.MustCompile(`### tuple ### (\w+) (\w+) ([\w:]+) ([\w:\.\/]+) ([\w:]+) ([\w:\.\/]+) (\w+)`)

	for _, line := range strings.Split(rulesContent, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "### tuple ###") {
			continue
		}

		matches := tupleRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		action := matches[1]
		proto := matches[2]
		toPort := matches[3]
		toAddr := matches[4]
		fromPort := matches[5]
		fromAddr := matches[6]

		sig := buildSignatureFromTuple(action, proto, toPort, fromPort, toAddr, fromAddr)
		signatures = append(signatures, sig)
	}

	return signatures
}

func buildSignatureFromTuple(action, proto, toPort, fromPort, toAddr, fromAddr string) string {
	parts := []string{strings.ToLower(action)}

	fromIP := fromAddr
	if fromIP == "0.0.0.0/0" || fromIP == "::/0" {
		fromIP = "any"
	}
	parts = append(parts, "from:"+fromIP)

	if fromPort != "any" {
		parts = append(parts, "fport:"+fromPort)
	}

	toIP := toAddr
	if toIP == "0.0.0.0/0" || toIP == "::/0" {
		toIP = "any"
	}
	parts = append(parts, "to:"+toIP)

	if toPort != "any" {
		parts = append(parts, "tport:"+toPort)
	}

	if proto != "any" {
		parts = append(parts, "proto:"+proto)
	}

	return strings.Join(parts, "|")
}

func calculateInsertPosition(req Request, preRules string) int {
	if req.InsertRelativeTo == "zero" {
		return req.Insert
	}

	numberedOutput, _ := exec.Command("ufw", "status", "numbered").Output()
	lines := strings.Split(string(numberedOutput), "\n")

	lineRe := regexp.MustCompile(`^\[ *(\d+)\]`)
	ipv4Re := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?\b`)
	ipv6Re := regexp.MustCompile(`(?i)\b(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:/\d{1,3})?\b`)

	var firstIPv4, lastIPv4, firstIPv6, lastIPv6 int
	maxRule := 0

	for _, line := range lines {
		matches := lineRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		ruleNum, _ := strconv.Atoi(matches[1])
		if ruleNum > maxRule {
			maxRule = ruleNum
		}

		hasIPv4 := ipv4Re.MatchString(line) || strings.Contains(line, "(v4)")
		hasIPv6 := ipv6Re.MatchString(line) || strings.Contains(line, "(v6)")

		if hasIPv4 {
			if firstIPv4 == 0 {
				firstIPv4 = ruleNum
			}
			lastIPv4 = ruleNum
		}

		if hasIPv6 {
			if firstIPv6 == 0 {
				firstIPv6 = ruleNum
			}
			lastIPv6 = ruleNum
		}
	}

	basePos := 1
	switch req.InsertRelativeTo {
	case "first-ipv4":
		if firstIPv4 > 0 {
			basePos = firstIPv4
		}
	case "last-ipv4":
		if lastIPv4 > 0 {
			basePos = lastIPv4 + 1
		}
	case "first-ipv6":
		if firstIPv6 > 0 {
			basePos = firstIPv6
		}
	case "last-ipv6":
		if lastIPv6 > 0 {
			basePos = lastIPv6 + 1
		}
	}

	return basePos + req.Insert - 1
}
