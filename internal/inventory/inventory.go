package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/secrets"
	"github.com/gjergjiramku/dibra/internal/vars"
	"gopkg.in/yaml.v3"
)

type Inventory struct {
	Groups  map[string]*Group
	Hosts   map[string]*HostEntry
	BaseDir string
}

type Group struct {
	Name     string
	Vars     map[string]interface{}
	HostVars map[string]map[string]interface{}
	Children []string
	Parents  []string
}

type HostEntry struct {
	Name   string
	Groups []string
}

type rawGroup struct {
	Vars     map[string]interface{}            `json:"vars" yaml:"vars"`
	Hosts    map[string]map[string]interface{} `json:"hosts" yaml:"hosts"`
	Children map[string]*rawGroup              `json:"children" yaml:"children"`
}

func Load(path string) (*Inventory, error) {
	raw, err := loadYAMLInventory(path)
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("inventory file is empty")
	}

	inv := &Inventory{
		Groups:  map[string]*Group{},
		Hosts:   map[string]*HostEntry{},
		BaseDir: filepath.Dir(path),
	}

	if allRaw, ok := raw["all"]; ok {
		if allRaw == nil {
			allRaw = &rawGroup{}
		}
		for name, g := range raw {
			if name != "all" {
				if allRaw.Children == nil {
					allRaw.Children = map[string]*rawGroup{}
				}
				if _, exists := allRaw.Children[name]; !exists {
					allRaw.Children[name] = g
				}
			}
		}
		raw = map[string]*rawGroup{"all": allRaw}
	} else {
		raw = map[string]*rawGroup{"all": {Children: raw}}
	}

	collected := map[string]*rawGroup{}
	collectGroups(raw, collected)

	for name, rg := range collected {
		g := &Group{
			Name:     name,
			Vars:     rg.Vars,
			HostVars: map[string]map[string]interface{}{},
			Children: []string{},
		}
		if g.Vars == nil {
			g.Vars = map[string]interface{}{}
		}

		for hostname, hostVars := range rg.Hosts {
			if hostVars == nil {
				hostVars = map[string]interface{}{}
			}
			g.HostVars[hostname] = hostVars
			if _, exists := inv.Hosts[hostname]; !exists {
				inv.Hosts[hostname] = &HostEntry{Name: hostname}
			}
		}

		for childName := range rg.Children {
			g.Children = append(g.Children, childName)
		}
		sort.Strings(g.Children)

		if existing, ok := inv.Groups[name]; ok {
			for k, v := range g.Vars {
				existing.Vars[k] = v
			}
			for h, hv := range g.HostVars {
				if _, ok := existing.HostVars[h]; !ok {
					existing.HostVars[h] = hv
				} else {
					for k, v := range hv {
						existing.HostVars[h][k] = v
					}
				}
			}
			for _, c := range g.Children {
				existing.Children = appendUnique(existing.Children, c)
			}
			sort.Strings(existing.Children)
		} else {
			inv.Groups[name] = g
		}
	}

	for name := range inv.Groups {
		for _, child := range inv.Groups[name].Children {
			if _, ok := inv.Groups[child]; !ok {
				inv.Groups[child] = &Group{
					Name:     child,
					Vars:     map[string]interface{}{},
					HostVars: map[string]map[string]interface{}{},
				}
			}
		}
	}

	if err := inv.detectCycles(); err != nil {
		return nil, err
	}

	inv.computeParents()
	inv.computeHostGroups()
	inv.computeUngrouped()

	return inv, nil
}

func loadYAMLInventory(path string) (map[string]*rawGroup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read inventory file: %w", err)
	}

	var raw map[string]*rawGroup
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse inventory YAML: %w", err)
	}

	return raw, nil
}

func collectGroups(groups map[string]*rawGroup, collected map[string]*rawGroup) {
	for name, rg := range groups {
		if rg == nil {
			rg = &rawGroup{}
		}
		if existing, ok := collected[name]; ok {
			if existing.Vars == nil {
				existing.Vars = map[string]interface{}{}
			}
			for k, v := range rg.Vars {
				existing.Vars[k] = v
			}
			if existing.Hosts == nil {
				existing.Hosts = map[string]map[string]interface{}{}
			}
			for h, hv := range rg.Hosts {
				existing.Hosts[h] = hv
			}
			if existing.Children == nil {
				existing.Children = map[string]*rawGroup{}
			}
			for c, cg := range rg.Children {
				if _, ok := existing.Children[c]; !ok {
					existing.Children[c] = cg
				}
			}
		} else {
			collected[name] = rg
		}
		if rg.Children != nil {
			collectGroups(rg.Children, collected)
		}
	}
}

func (inv *Inventory) detectCycles() error {
	visited := map[string]bool{}
	inStack := map[string]bool{}

	var dfs func(name string, path []string) error
	dfs = func(name string, path []string) error {
		if inStack[name] {
			return fmt.Errorf("circular group reference detected: %s", strings.Join(append(path, name), " -> "))
		}
		if visited[name] {
			return nil
		}
		visited[name] = true
		inStack[name] = true
		path = append(path, name)

		g, ok := inv.Groups[name]
		if !ok {
			return fmt.Errorf("group %q referenced but not defined", name)
		}
		for _, child := range g.Children {
			if err := dfs(child, path); err != nil {
				return err
			}
		}
		inStack[name] = false
		return nil
	}

	for name := range inv.Groups {
		if !visited[name] {
			if err := dfs(name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (inv *Inventory) computeParents() {
	for name, g := range inv.Groups {
		for _, child := range g.Children {
			if cg, ok := inv.Groups[child]; ok {
				cg.Parents = append(cg.Parents, name)
			}
		}
		_ = name
	}
	for _, g := range inv.Groups {
		sort.Strings(g.Parents)
	}
}

func (inv *Inventory) computeHostGroups() {
	groupHosts := map[string]map[string]bool{}
	for groupName, g := range inv.Groups {
		for hostname := range g.HostVars {
			if groupHosts[hostname] == nil {
				groupHosts[hostname] = map[string]bool{}
			}
			groupHosts[hostname][groupName] = true
		}
	}

	for hostname, he := range inv.Hosts {
		directGroups := groupHosts[hostname]
		if directGroups == nil {
			directGroups = map[string]bool{}
		}

		allGroups := map[string]bool{"all": true}
		for gName := range directGroups {
			allGroups[gName] = true
		}

		for gName := range directGroups {
			inv.addParentGroups(gName, allGroups)
		}

		groups := make([]string, 0, len(allGroups))
		for g := range allGroups {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		he.Groups = groups
	}
}

func (inv *Inventory) addParentGroups(groupName string, collected map[string]bool) {
	g, ok := inv.Groups[groupName]
	if !ok {
		return
	}
	for _, parent := range g.Parents {
		if !collected[parent] {
			collected[parent] = true
			inv.addParentGroups(parent, collected)
		}
	}
}

func (inv *Inventory) computeUngrouped() {
	var ungroupedHosts []string
	for hostname, he := range inv.Hosts {
		hasNonImplicitGroup := false
		for _, g := range he.Groups {
			if g != "all" && g != "ungrouped" {
				hasNonImplicitGroup = true
				break
			}
		}
		if !hasNonImplicitGroup {
			ungroupedHosts = append(ungroupedHosts, hostname)
		}
	}

	if len(ungroupedHosts) > 0 {
		ug, ok := inv.Groups["ungrouped"]
		if !ok {
			ug = &Group{
				Name:     "ungrouped",
				Vars:     map[string]interface{}{},
				HostVars: map[string]map[string]interface{}{},
			}
			inv.Groups["ungrouped"] = ug
		}
		for _, hostname := range ungroupedHosts {
			if _, exists := ug.HostVars[hostname]; !exists {
				ug.HostVars[hostname] = map[string]interface{}{}
			}
			he := inv.Hosts[hostname]
			he.Groups = appendUnique(he.Groups, "ungrouped")
			sort.Strings(he.Groups)
		}

		if allG, ok := inv.Groups["all"]; ok {
			allG.Children = appendUnique(allG.Children, "ungrouped")
			sort.Strings(allG.Children)
		}
		ug.Parents = appendUnique(ug.Parents, "all")
		sort.Strings(ug.Parents)
	}
}

func (inv *Inventory) HostNames() []string {
	names := make([]string, 0, len(inv.Hosts))
	for name := range inv.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (inv *Inventory) GroupsForHost(hostname string) []string {
	he, ok := inv.Hosts[hostname]
	if !ok {
		return nil
	}
	result := make([]string, len(he.Groups))
	copy(result, he.Groups)
	return result
}

func (inv *Inventory) GroupMembers() map[string][]string {
	result := map[string][]string{}

	var collectHosts func(groupName string) []string
	collectHosts = func(groupName string) []string {
		g, ok := inv.Groups[groupName]
		if !ok {
			return nil
		}
		hosts := make([]string, 0)
		for hostname := range g.HostVars {
			hosts = append(hosts, hostname)
		}
		for _, child := range g.Children {
			hosts = append(hosts, collectHosts(child)...)
		}
		return hosts
	}

	for name := range inv.Groups {
		members := collectHosts(name)
		unique := map[string]struct{}{}
		var deduped []string
		for _, h := range members {
			if _, ok := unique[h]; !ok {
				unique[h] = struct{}{}
				deduped = append(deduped, h)
			}
		}
		sort.Strings(deduped)
		result[name] = deduped
	}

	return result
}

func (inv *Inventory) EffectiveVarsForHost(hostname string) map[string]interface{} {
	he, ok := inv.Hosts[hostname]
	if !ok {
		return map[string]interface{}{}
	}

	depths := inv.computeGroupDepths()

	type groupWithDepth struct {
		name  string
		depth int
	}
	var hostGroups []groupWithDepth
	for _, gName := range he.Groups {
		d := depths[gName]
		hostGroups = append(hostGroups, groupWithDepth{name: gName, depth: d})
	}
	sort.Slice(hostGroups, func(i, j int) bool {
		if hostGroups[i].depth != hostGroups[j].depth {
			return hostGroups[i].depth < hostGroups[j].depth
		}
		return hostGroups[i].name < hostGroups[j].name
	})

	merged := map[string]interface{}{}
	for _, gwd := range hostGroups {
		g, ok := inv.Groups[gwd.name]
		if !ok {
			continue
		}
		for k, v := range g.Vars {
			merged[k] = v
		}
	}

	for _, gwd := range hostGroups {
		g, ok := inv.Groups[gwd.name]
		if !ok {
			continue
		}
		if hv, ok := g.HostVars[hostname]; ok {
			for k, v := range hv {
				merged[k] = v
			}
		}
	}

	return merged
}

func (inv *Inventory) computeGroupDepths() map[string]int {
	depths := map[string]int{}
	if _, ok := inv.Groups["all"]; !ok {
		return depths
	}

	type item struct {
		name  string
		depth int
	}
	queue := []item{{name: "all", depth: 0}}
	depths["all"] = 0

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		g, ok := inv.Groups[cur.name]
		if !ok {
			continue
		}
		for _, child := range g.Children {
			newDepth := cur.depth + 1
			if existing, ok := depths[child]; !ok || newDepth > existing {
				depths[child] = newDepth
				queue = append(queue, item{name: child, depth: newDepth})
			}
		}
	}

	return depths
}

func (inv *Inventory) HostsAsConfig() ([]config.Host, error) {
	hostNames := inv.HostNames()
	hosts := make([]config.Host, 0, len(hostNames))

	for _, hostname := range hostNames {
		vars := inv.EffectiveVarsForHost(hostname)
		h := config.Host{
			Name:           hostname,
			Host:           getString(vars, "host", hostname),
			Port:           getInt(vars, "port", 22),
			User:           getString(vars, "user", ""),
			Password:       getString(vars, "ssh_pass", ""),
			SSHKeyPath:     getString(vars, "ssh_private_key_file", ""),
			Become:         getBool(vars, "become", false),
			BecomePassword: getString(vars, "become_password", ""),
		}

		he := inv.Hosts[hostname]
		var groups []string
		for _, g := range he.Groups {
			if g != "all" && g != "ungrouped" {
				groups = append(groups, g)
			}
		}
		h.Groups = groups

		hosts = append(hosts, h)
	}

	return hosts, nil
}

func getString(vars map[string]interface{}, key string, defaultVal string) string {
	v, ok := vars[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func getInt(vars map[string]interface{}, key string, defaultVal int) int {
	v, ok := vars[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
		return defaultVal
	default:
		return defaultVal
	}
}

func getBool(vars map[string]interface{}, key string, defaultVal bool) bool {
	v, ok := vars[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		lower := strings.ToLower(val)
		return lower == "true" || lower == "yes" || lower == "1"
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return defaultVal
	}
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// ResolveSecrets walks all group vars and host vars in the inventory,
// replacing secret references (!bw:, !op://) with their resolved values.
// Must be called after Load() but before HostsAsConfig() or EffectiveVarsForHost().
func (inv *Inventory) ResolveSecrets(resolver *secrets.Resolver) error {
	if resolver == nil {
		return nil
	}
	for name, g := range inv.Groups {
		if len(g.Vars) > 0 {
			resolved, err := resolver.ResolveMap(g.Vars)
			if err != nil {
				return fmt.Errorf("resolving secrets in group %q vars: %w", name, err)
			}
			g.Vars = resolved
		}
		for hostname, hv := range g.HostVars {
			if len(hv) > 0 {
				resolved, err := resolver.ResolveMap(hv)
				if err != nil {
					return fmt.Errorf("resolving secrets in host %q vars (group %q): %w", hostname, name, err)
				}
				g.HostVars[hostname] = resolved
			}
		}
	}
	return nil
}

// ResolveTemplates resolves {{ }} template expressions in inventory vars.
// For each host, the effective vars (merged group + host vars) are used as
// context to resolve templates in that host's vars. This allows patterns like:
//
//	all:
//	  vars:
//	    ssh_port: "2222"
//	  children:
//	    mygroup:
//	      hosts:
//	        myhost:
//	          port: "{{ ssh_port }}"
//
// Must be called after ResolveSecrets() but before HostsAsConfig().
func (inv *Inventory) ResolveTemplates() error {
	for _, hostname := range inv.HostNames() {
		ctx := inv.EffectiveVarsForHost(hostname)

		for gName, g := range inv.Groups {
			hv, ok := g.HostVars[hostname]
			if !ok || len(hv) == 0 {
				continue
			}
			resolved, err := vars.RenderValue(hv, ctx)
			if err != nil {
				return fmt.Errorf("resolving templates for host %q in group %q: %w", hostname, gName, err)
			}
			if rm, ok := resolved.(map[string]interface{}); ok {
				g.HostVars[hostname] = rm
			}
		}
	}
	return nil
}
