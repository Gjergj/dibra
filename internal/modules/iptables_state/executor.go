package iptables_state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	validTables = map[string]bool{
		"filter": true, "nat": true, "mangle": true, "raw": true, "security": true,
	}
	validStates = map[string]bool{
		"saved": true, "restored": true,
	}
	validIPVersions = map[string]bool{
		"ipv4": true, "ipv6": true,
	}
)

// Execute runs the iptables_state module logic
func Execute(req Request) Response {
	if err := validateRequest(&req); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	normalizeRequest(&req)

	// Get the appropriate commands for the IP version
	saveCmd, restoreCmd := getCommands(req.IPVersion)

	// Check if commands exist
	if _, err := exec.LookPath(saveCmd); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("%s command not found", saveCmd)}
	}
	if _, err := exec.LookPath(restoreCmd); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("%s command not found", restoreCmd)}
	}

	// Get initial state
	initialState, err := getState(saveCmd, req.Table, req.Counters, req.Wait, req.Modprobe)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to get initial state: %v", err)}
	}

	// Parse tables from initial state
	tables := parseTablesFromState(initialState, req.Table)

	if req.State == "saved" {
		return executeSave(req, initialState, tables)
	}

	return executeRestore(req, initialState, tables, restoreCmd)
}

func validateRequest(req *Request) error {
	if req.Path == "" {
		return fmt.Errorf("missing required arguments: path")
	}
	if req.State == "" {
		return fmt.Errorf("missing required arguments: state")
	}
	if !validStates[req.State] {
		return fmt.Errorf("value of state must be one of: saved, restored, got: %s", req.State)
	}
	if req.Table != "" && !validTables[req.Table] {
		return fmt.Errorf("value of table must be one of: filter, nat, mangle, raw, security, got: %s", req.Table)
	}
	if req.IPVersion != "" && !validIPVersions[req.IPVersion] {
		return fmt.Errorf("value of ip_version must be one of: ipv4, ipv6, got: %s", req.IPVersion)
	}
	return nil
}

func normalizeRequest(req *Request) {
	if req.IPVersion == "" {
		req.IPVersion = "ipv4"
	}
}

func getCommands(ipVersion string) (saveCmd, restoreCmd string) {
	if ipVersion == "ipv6" {
		return "ip6tables-save", "ip6tables-restore"
	}
	return "iptables-save", "iptables-restore"
}

func getState(saveCmd, table string, counters bool, wait int, modprobe string) ([]string, error) {
	args := []string{}

	if table != "" {
		args = append(args, "-t", table)
	}
	if counters {
		args = append(args, "-c")
	}
	if modprobe != "" {
		args = append(args, "-M", modprobe)
	}

	cmd := exec.Command(saveCmd, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %v - %s", saveCmd, err, string(output))
	}

	return filterAndFormatState(string(output), counters), nil
}

func filterAndFormatState(output string, counters bool) []string {
	// Remove timestamps for idempotence
	timestampRe := regexp.MustCompile(`(^|\n)(# (Generated|Completed)[^\n]*) on [^\n]*`)
	output = timestampRe.ReplaceAllString(output, "$1$2")

	// Reset counters to [0:0] if not preserving them
	if !counters {
		counterRe := regexp.MustCompile(`\[[0-9]+:[0-9]+\]`)
		output = counterRe.ReplaceAllString(output, "[0:0]")
	}

	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseTablesFromState(state []string, filterTable string) map[string][]string {
	tables := make(map[string][]string)
	var currentTable string
	var currentRules []string

	tableRe := regexp.MustCompile(`^\*(filter|nat|mangle|raw|security)$`)

	for _, line := range state {
		if matches := tableRe.FindStringSubmatch(line); len(matches) > 1 {
			// Save previous table if any
			if currentTable != "" {
				tables[currentTable] = currentRules
			}
			currentTable = matches[1]
			currentRules = []string{}
			continue
		}

		if line == "COMMIT" {
			if currentTable != "" {
				tables[currentTable] = currentRules
			}
			currentTable = ""
			currentRules = []string{}
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		if currentTable != "" {
			currentRules = append(currentRules, line)
		}
	}

	// If filtering to a specific table, only return that one
	if filterTable != "" {
		if tableRules, ok := tables[filterTable]; ok {
			return map[string][]string{filterTable: tableRules}
		}
		return map[string][]string{}
	}

	return tables
}

func executeSave(req Request, initialState []string, tables map[string][]string) Response {
	// Get the state to save (may be filtered by table)
	stateToSave := initialState
	if req.Table != "" {
		// Extract only the requested table from initial state
		stateToSave = extractTableState(initialState, req.Table)
	}

	// Check if file exists and matches
	existingContent, err := os.ReadFile(req.Path)
	if err == nil {
		existingLines := filterAndFormatState(string(existingContent), req.Counters)
		if statesEqual(existingLines, stateToSave) {
			return Response{
				Changed:      false,
				InitialState: initialState,
				Saved:        stateToSave,
				Tables:       tables,
				Msg:          "state already saved",
			}
		}
	}

	// Write state to file
	if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create directory: %v", err)}
	}

	content := strings.Join(stateToSave, "\n") + "\n"
	if err := os.WriteFile(req.Path, []byte(content), 0600); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write file: %v", err)}
	}

	return Response{
		Changed:      true,
		InitialState: initialState,
		Saved:        stateToSave,
		Tables:       tables,
		Msg:          "state saved",
	}
}

func executeRestore(req Request, initialState []string, tables map[string][]string, restoreCmd string) Response {
	// Read state from file
	content, err := os.ReadFile(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read file: %v", err)}
	}

	fileLines := filterAndFormatState(string(content), req.Counters)

	// If a specific table is requested, verify it exists in the file
	if req.Table != "" {
		if !tableExistsInState(fileLines, req.Table) {
			return Response{
				Failed: true,
				Msg:    fmt.Sprintf("Table %s to restore not defined in %s", req.Table, req.Path),
			}
		}
	}

	// Determine what we're actually restoring
	stateToRestore := fileLines
	if req.Table != "" {
		stateToRestore = extractTableState(fileLines, req.Table)
	}

	// Compare current state with what we want to restore
	// When restoring, we only compare the tables that are in the file
	currentStateToCompare := initialState
	if req.Table != "" {
		currentStateToCompare = extractTableState(initialState, req.Table)
	} else {
		// Extract only tables that are in the file for comparison
		tablesInFile := getTablesInState(fileLines)
		currentStateToCompare = extractMultiTableState(initialState, tablesInFile)
	}

	// Check if states match (idempotency)
	if statesEqual(currentStateToCompare, stateToRestore) {
		return Response{
			Changed:      false,
			Applied:      true,
			InitialState: initialState,
			Restored:     stateToRestore,
			Tables:       tables,
			Msg:          "state already matches",
		}
	}

	// Build restore command
	args := []string{}
	if req.Noflush {
		args = append(args, "-n")
	}
	if req.Counters {
		args = append(args, "-c")
	}
	if req.Table != "" {
		args = append(args, "-T", req.Table)
	}
	if req.Wait > 0 {
		args = append(args, "-w", fmt.Sprintf("%d", req.Wait))
	}
	if req.Modprobe != "" {
		args = append(args, "-M", req.Modprobe)
	}

	cmd := exec.Command(restoreCmd, args...)
	cmd.Stdin = strings.NewReader(strings.Join(stateToRestore, "\n") + "\n")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{
			Failed:       true,
			Applied:      false,
			InitialState: initialState,
			Restored:     stateToRestore,
			Tables:       tables,
			Msg:          fmt.Sprintf("restore failed: %v - %s", err, string(output)),
		}
	}

	return Response{
		Changed:      true,
		Applied:      true,
		InitialState: initialState,
		Restored:     stateToRestore,
		Tables:       tables,
		Msg:          "state restored",
	}
}

func extractTableState(state []string, table string) []string {
	var result []string
	inTable := false
	tableHeader := "*" + table

	for _, line := range state {
		if line == tableHeader {
			inTable = true
			result = append(result, line)
			continue
		}

		if inTable {
			result = append(result, line)
			if line == "COMMIT" {
				break
			}
		}

		// Check for other table headers
		if strings.HasPrefix(line, "*") && line != tableHeader {
			inTable = false
		}
	}

	return result
}

func tableExistsInState(state []string, table string) bool {
	tableHeader := "*" + table
	for _, line := range state {
		if line == tableHeader {
			return true
		}
	}
	return false
}

func getTablesInState(state []string) []string {
	var tables []string
	tableRe := regexp.MustCompile(`^\*(filter|nat|mangle|raw|security)$`)
	for _, line := range state {
		if matches := tableRe.FindStringSubmatch(line); len(matches) > 1 {
			tables = append(tables, matches[1])
		}
	}
	return tables
}

func extractMultiTableState(state []string, tables []string) []string {
	var result []string
	tableSet := make(map[string]bool)
	for _, t := range tables {
		tableSet[t] = true
	}

	inWantedTable := false
	for _, line := range state {
		// Check for table header
		if strings.HasPrefix(line, "*") {
			tableName := strings.TrimPrefix(line, "*")
			if tableSet[tableName] {
				inWantedTable = true
				result = append(result, line)
			} else {
				inWantedTable = false
			}
			continue
		}

		if inWantedTable {
			result = append(result, line)
			if line == "COMMIT" {
				inWantedTable = false
			}
		}
	}
	return result
}

func statesEqual(a, b []string) bool {
	// Filter out comment lines for comparison
	filterRules := func(lines []string) []string {
		var result []string
		for _, line := range lines {
			if !strings.HasPrefix(line, "#") {
				result = append(result, line)
			}
		}
		return result
	}

	aRules := filterRules(a)
	bRules := filterRules(b)

	if len(aRules) != len(bRules) {
		return false
	}
	for i := range aRules {
		if aRules[i] != bRules[i] {
			return false
		}
	}
	return true
}
