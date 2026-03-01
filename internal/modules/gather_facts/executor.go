package gather_facts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const defaultFactPath = "/etc/ansible/facts.d"

var allSubsets = map[string]bool{
	"min":         true,
	"network":     true,
	"hardware":    true,
	"virtual":     true,
	"service_mgr": true,
	"pkg_mgr":     true,
	"env":         true,
	"date_time":   true,
	"local":       true,
}

func Execute(req Request) Response {
	subsets, err := resolveSubsets(req.GatherSubset)
	if err != nil {
		return Response{
			Changed: false,
			Failed:  true,
			Msg:     err.Error(),
		}
	}
	facts := map[string]interface{}{}

	if subsets["min"] {
		mergeFacts(facts, gatherMinFacts())
	}
	if subsets["env"] {
		mergeFacts(facts, gatherEnvFacts())
	}
	if subsets["date_time"] {
		mergeFacts(facts, gatherDateTimeFacts())
	}
	if subsets["network"] {
		mergeFacts(facts, gatherNetworkFacts())
	}
	if subsets["hardware"] {
		mergeFacts(facts, gatherHardwareFacts())
	}
	if subsets["virtual"] {
		mergeFacts(facts, gatherVirtualFacts())
	}
	if subsets["service_mgr"] {
		mergeFacts(facts, gatherServiceMgrFacts())
	}
	if subsets["pkg_mgr"] {
		mergeFacts(facts, gatherPkgMgrFacts())
	}
	if subsets["local"] {
		mergeFacts(facts, gatherLocalFacts(req.FactPath))
	}

	filters := normalizeFilters(req.Filter)
	if len(filters) > 0 {
		facts = filterFacts(facts, filters)
	}

	return Response{
		Changed:      false,
		AnsibleFacts: facts,
	}
}

func resolveSubsets(raw interface{}) (map[string]bool, error) {
	requested := normalizeSubset(raw)
	if len(requested) == 0 {
		return cloneSubset(allSubsets), nil
	}
	if err := validateSubsets(requested); err != nil {
		return nil, err
	}

	hasAll := false
	hasNotAll := false
	hasPositive := false
	hasNegative := false
	hasNegMin := false

	for _, item := range requested {
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "!") {
			hasNegative = true
			if strings.TrimPrefix(item, "!") == "min" {
				hasNegMin = true
			}
		} else {
			hasPositive = true
		}
		if item == "all" {
			hasAll = true
		}
		if item == "!all" {
			hasNotAll = true
		}
	}

	selected := map[string]bool{}
	baseAll := hasAll || (hasNegative && !hasPositive)
	if baseAll {
		selected = cloneSubset(allSubsets)
	} else if !hasNotAll {
		selected["min"] = true
	}

	for _, item := range requested {
		if item == "" {
			continue
		}
		if item == "all" {
			selected = cloneSubset(allSubsets)
			continue
		}
		if item == "!all" {
			selected = map[string]bool{}
			continue
		}
		if strings.HasPrefix(item, "!") {
			name := strings.TrimPrefix(item, "!")
			delete(selected, name)
			continue
		}
		selected[item] = true
	}

	if !selected["min"] && !hasNegMin && !hasNotAll && !baseAll {
		selected["min"] = true
	}

	return selected, nil
}

func validateSubsets(requested []string) error {
	for _, item := range requested {
		if item == "" {
			continue
		}
		name := strings.TrimPrefix(item, "!")
		if name == "all" {
			continue
		}
		if _, ok := allSubsets[name]; !ok {
			return fmt.Errorf("bad subset %q given to gather_facts", name)
		}
	}
	return nil
}

func normalizeSubset(raw interface{}) []string {
	switch typed := raw.(type) {
	case nil:
		return nil
	case string:
		return splitSubsetString(typed)
	case []string:
		return normalizeSubsetList(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%v", item))
		}
		return normalizeSubsetList(out)
	default:
		return []string{fmt.Sprintf("%v", typed)}
	}
}

func splitSubsetString(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, ",") {
		parts := strings.Split(trimmed, ",")
		return normalizeSubsetList(parts)
	}
	return []string{trimmed}
}

func normalizeSubsetList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, strings.ToLower(trimmed))
	}
	return out
}

func normalizeFilters(raw interface{}) []string {
	switch typed := raw.(type) {
	case nil:
		return nil
	case string:
		return normalizeFilterList(splitFilterString(typed))
	case []string:
		return normalizeFilterList(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%v", item))
		}
		return normalizeFilterList(out)
	default:
		return normalizeFilterList([]string{fmt.Sprintf("%v", typed)})
	}
}

func splitFilterString(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, ",") {
		return strings.Split(trimmed, ",")
	}
	return []string{trimmed}
}

func normalizeFilterList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func filterFacts(facts map[string]interface{}, filters []string) map[string]interface{} {
	if len(filters) == 0 {
		return facts
	}
	filtered := map[string]interface{}{}
	for key, value := range facts {
		if matchesFilters(key, filters) {
			filtered[key] = value
		}
	}
	return filtered
}

func matchesFilters(key string, filters []string) bool {
	candidates := []string{key, "ansible_" + key}
	for _, pattern := range filters {
		if pattern == "" || pattern == "*" {
			return true
		}
		for _, candidate := range candidates {
			matched, err := filepath.Match(pattern, candidate)
			if err == nil && matched {
				return true
			}
			if !strings.Contains(pattern, "*") && candidate == pattern {
				return true
			}
		}
	}
	return false
}

func gatherMinFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	mergeFacts(facts, gatherPlatformFacts())
	mergeFacts(facts, gatherDistributionFacts())
	mergeFacts(facts, gatherHostnameFacts())
	mergeFacts(facts, gatherUserFacts())
	mergeFacts(facts, gatherEnvFacts())
	mergeFacts(facts, gatherDateTimeFacts())
	return facts
}

func gatherPlatformFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	if system := runCommand("uname", "-s"); system != "" {
		facts["system"] = system
	}
	if kernel := runCommand("uname", "-r"); kernel != "" {
		facts["kernel"] = kernel
	}
	if machine := runCommand("uname", "-m"); machine != "" {
		facts["machine"] = machine
		facts["architecture"] = machine
	}
	return facts
}

func gatherDistributionFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	osRelease := readOSRelease()
	if name := osRelease["NAME"]; name != "" {
		facts["distribution"] = name
	} else if id := osRelease["ID"]; id != "" {
		facts["distribution"] = capitalizeFirst(id)
	}
	if version := osRelease["VERSION_ID"]; version != "" {
		facts["distribution_version"] = version
	}
	if id := osRelease["ID"]; id != "" {
		facts["distribution_id"] = id
		if family := osFamilyFromID(id); family != "" {
			facts["os_family"] = family
		}
	}
	return facts
}

func gatherHostnameFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		facts["nodename"] = hostname
		short := hostname
		if idx := strings.Index(hostname, "."); idx > 0 {
			short = hostname[:idx]
		}
		facts["hostname"] = short
		fqdn := runCommand("hostname", "-f")
		if fqdn == "" {
			fqdn = hostname
		}
		facts["fqdn"] = fqdn
	}
	return facts
}

func gatherUserFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	if current, err := user.Current(); err == nil && current.Username != "" {
		facts["user_id"] = current.Username
	} else if userID := runCommand("id", "-un"); userID != "" {
		facts["user_id"] = userID
	}
	return facts
}

func gatherEnvFacts() map[string]interface{} {
	env := map[string]interface{}{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return map[string]interface{}{"env": env}
}

func gatherDateTimeFacts() map[string]interface{} {
	now := time.Now()
	facts := map[string]interface{}{
		"year":     now.Year(),
		"month":    int(now.Month()),
		"day":      now.Day(),
		"hour":     now.Hour(),
		"minute":   now.Minute(),
		"second":   now.Second(),
		"epoch":    now.Unix(),
		"iso8601":  now.Format(time.RFC3339),
		"timezone": now.Format("-0700"),
	}
	return map[string]interface{}{"date_time": facts}
}

func gatherNetworkFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	interfaces, err := net.Interfaces()
	if err != nil {
		return facts
	}

	names := make([]string, 0, len(interfaces))
	ipv4Addresses := []string{}
	ipv6Addresses := []string{}

	for _, iface := range interfaces {
		names = append(names, iface.Name)
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := extractIP(addr)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				ipv4Addresses = append(ipv4Addresses, ip.String())
			} else {
				ipv6Addresses = append(ipv6Addresses, ip.String())
			}
		}
	}

	facts["interfaces"] = names
	facts["all_ipv4_addresses"] = ipv4Addresses
	if len(ipv6Addresses) > 0 {
		facts["all_ipv6_addresses"] = ipv6Addresses
	}

	if def := detectDefaultIPv4(); len(def) > 0 {
		facts["default_ipv4"] = def
	}

	return facts
}

func gatherHardwareFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	memTotal, memFree := readMemInfo()
	if memTotal > 0 {
		facts["memory_mb"] = map[string]interface{}{
			"real": map[string]interface{}{
				"total": memTotal,
				"free":  memFree,
			},
		}
		facts["memtotal_mb"] = memTotal
	}
	if memFree > 0 {
		facts["memfree_mb"] = memFree
	}

	mounts := readMounts()
	if len(mounts) > 0 {
		facts["mounts"] = mounts
	}

	devices := readBlockDevices()
	if len(devices) > 0 {
		facts["devices"] = devices
	}

	return facts
}

func gatherVirtualFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	if virtType, role := detectVirtualization(); virtType != "" || role != "" {
		if virtType != "" {
			facts["virtualization_type"] = virtType
		}
		if role != "" {
			facts["virtualization_role"] = role
		}
	}
	return facts
}

func gatherServiceMgrFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	mgr := detectServiceMgr()
	if mgr != "" {
		facts["service_mgr"] = mgr
	}
	return facts
}

func gatherPkgMgrFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	pkg := detectPkgMgr()
	if pkg != "" {
		facts["pkg_mgr"] = pkg
	}
	return facts
}

func gatherLocalFacts(path string) map[string]interface{} {
	factPath := path
	if strings.TrimSpace(factPath) == "" {
		factPath = defaultFactPath
	}
	entries, err := os.ReadDir(factPath)
	if err != nil {
		return map[string]interface{}{"local": map[string]interface{}{}}
	}

	locals := map[string]interface{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".fact") {
			continue
		}
		fullPath := filepath.Join(factPath, name)
		content, err := readFactContent(fullPath, entry)
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(name, filepath.Ext(name))
		locals[key] = content
	}
	return map[string]interface{}{"local": locals}
}

func readFactContent(path string, entry os.DirEntry) (interface{}, error) {
	info, err := entry.Info()
	if err != nil {
		return nil, err
	}

	if info.Mode()&0111 != 0 {
		output, err := exec.Command(path).Output()
		if err != nil {
			return nil, err
		}
		return parseFactPayload(output)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseFactPayload(data)
}

func parseFactPayload(data []byte) (interface{}, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty fact")
	}

	var decoded interface{}
	if err := json.Unmarshal(trimmed, &decoded); err == nil {
		return decoded, nil
	}
	if err := yaml.Unmarshal(trimmed, &decoded); err == nil {
		return decoded, nil
	}
	return string(trimmed), nil
}

func readOSRelease() map[string]string {
	paths := []string{"/etc/os-release", "/usr/lib/os-release"}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseKeyValue(data)
	}
	return map[string]string{}
}

func parseKeyValue(data []byte) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.Trim(parts[1], `"`)
		result[key] = value
	}
	return result
}

func osFamilyFromID(id string) string {
	switch strings.ToLower(id) {
	case "ubuntu", "debian", "linuxmint":
		return "Debian"
	case "rhel", "centos", "fedora", "rocky", "almalinux", "amazon":
		return "RedHat"
	case "arch":
		return "Archlinux"
	case "alpine":
		return "Alpine"
	default:
		return strings.ToUpper(id[:1]) + id[1:]
	}
}

func runCommand(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func extractIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func detectDefaultIPv4() map[string]interface{} {
	output := runCommand("ip", "-4", "route", "show", "default")
	if output == "" {
		return map[string]interface{}{}
	}
	fields := strings.Fields(output)
	def := map[string]interface{}{}
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				def["gateway"] = fields[i+1]
			}
		case "dev":
			if i+1 < len(fields) {
				def["interface"] = fields[i+1]
			}
		}
	}
	if iface, ok := def["interface"].(string); ok {
		if addr := findInterfaceIPv4(iface); addr != "" {
			def["address"] = addr
		}
	}
	return def
}

func findInterfaceIPv4(ifaceName string) string {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ip := extractIP(addr)
		if ip == nil || ip.To4() == nil {
			continue
		}
		return ip.String()
	}
	return ""
}

func readMemInfo() (int, int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB int
	var freeKB int
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB, _ = strconv.Atoi(fields[1])
		case "MemAvailable":
			freeKB, _ = strconv.Atoi(fields[1])
		case "MemFree":
			if freeKB == 0 {
				freeKB, _ = strconv.Atoi(fields[1])
			}
		}
	}
	return totalKB / 1024, freeKB / 1024
}

func readMounts() []map[string]interface{} {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	mounts := []map[string]interface{}{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mounts = append(mounts, map[string]interface{}{
			"device":  fields[0],
			"mount":   fields[1],
			"fstype":  fields[2],
			"options": fields[3],
		})
	}
	return mounts
}

func readBlockDevices() map[string]interface{} {
	base := "/sys/block"
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	devices := map[string]interface{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			sizePath := filepath.Join(base, entry.Name(), "size")
			sizeData, err := os.ReadFile(sizePath)
			if err != nil {
				continue
			}
			sizeSectors, err := strconv.ParseInt(strings.TrimSpace(string(sizeData)), 10, 64)
			if err != nil {
				continue
			}
			devices[entry.Name()] = map[string]interface{}{
				"size": sizeSectors * 512,
			}
		}
	}
	return devices
}

func detectVirtualization() (string, string) {
	if _, err := exec.LookPath("systemd-detect-virt"); err != nil {
		return "", ""
	}
	output := runCommand("systemd-detect-virt")
	if output == "" {
		return "", ""
	}
	if output == "none" {
		return "none", "host"
	}
	return output, "guest"
}

func detectServiceMgr() string {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return "systemd"
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		return "systemd"
	}
	if _, err := exec.LookPath("service"); err == nil {
		return "sysvinit"
	}
	return "unknown"
}

func detectPkgMgr() string {
	switch {
	case pathExists("/usr/bin/apt-get"):
		return "apt"
	case pathExists("/usr/bin/dnf"):
		return "dnf"
	case pathExists("/usr/bin/yum"):
		return "yum"
	case pathExists("/sbin/apk"):
		return "apk"
	case pathExists("/usr/bin/pacman"):
		return "pacman"
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mergeFacts(dest map[string]interface{}, src map[string]interface{}) {
	for key, value := range src {
		dest[key] = value
	}
}

func cloneSubset(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
