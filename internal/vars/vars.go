package vars

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type MergeStrategy string

const (
	MergeReplace MergeStrategy = "replace"
	MergeDeep    MergeStrategy = "merge"
)

type InventoryVars struct {
	Group map[string]map[string]interface{}
	Host  map[string]map[string]interface{}
}

type Resolver struct {
	MergeStrategy MergeStrategy
	InventoryDir  string
}

func (r Resolver) LoadInventoryVars(hosts []HostInfo, extraGroupNames []string) (InventoryVars, error) {
	groupNames := map[string]struct{}{}
	for _, host := range hosts {
		for _, group := range host.Groups {
			if group == "" {
				continue
			}
			groupNames[group] = struct{}{}
		}
	}
	for _, group := range extraGroupNames {
		if group == "" {
			continue
		}
		groupNames[group] = struct{}{}
	}

	sortedGroups := make([]string, 0, len(groupNames))
	for group := range groupNames {
		sortedGroups = append(sortedGroups, group)
	}
	sort.Strings(sortedGroups)

	inv := InventoryVars{
		Group: map[string]map[string]interface{}{},
		Host:  map[string]map[string]interface{}{},
	}

	for _, group := range sortedGroups {
		varsMap, err := loadVarsFile(r.InventoryDir, "group_vars", group)
		if err != nil {
			return InventoryVars{}, err
		}
		if len(varsMap) > 0 {
			inv.Group[group] = varsMap
		}
	}

	for _, host := range hosts {
		varsMap, err := loadVarsFile(r.InventoryDir, "host_vars", host.Name)
		if err != nil {
			return InventoryVars{}, err
		}
		if len(varsMap) > 0 {
			inv.Host[host.Name] = varsMap
		}
	}

	return inv, nil
}

func LoadVarsFiles(root string, files []string, strategy MergeStrategy) (map[string]interface{}, error) {
	if strategy == "" {
		strategy = MergeReplace
	}
	merged := map[string]interface{}{}
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		path := file
		if !filepath.IsAbs(file) {
			path = filepath.Join(root, file)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read vars file %s: %w", path, err)
		}
		vars := map[string]interface{}{}
		if err := yaml.Unmarshal(data, &vars); err != nil {
			return nil, fmt.Errorf("failed to parse vars file %s: %w", path, err)
		}
		merged = MergeMaps(merged, vars, strategy)
	}
	return merged, nil
}

func (r Resolver) ResolveHostVars(req ResolveRequest) (ResolvedVars, error) {
	if r.MergeStrategy == "" {
		r.MergeStrategy = MergeReplace
	}

	layers := []layer{
		{Name: "group", Values: mergeGroupVars(req.Host.Groups, req.InventoryVars.Group, r.MergeStrategy)},
		{Name: "host", Values: req.InventoryVars.Host[req.Host.Name]},
		{Name: "play", Values: req.PlayVars},
		{Name: "task", Values: req.TaskVars},
		{Name: "extra", Values: req.ExtraVars},
	}

	merged := map[string]interface{}{}
	for _, layer := range layers {
		merged = mergeMaps(merged, layer.Values, r.MergeStrategy)
	}

	namespaces := map[string]interface{}{
		"group": layers[0].Values,
		"host":  layers[1].Values,
		"play":  layers[2].Values,
		"task":  layers[3].Values,
		"extra": layers[4].Values,
	}

	return ResolvedVars{
		Values:     merged,
		Namespaces: namespaces,
	}, nil
}

type HostInfo struct {
	Name   string
	Groups []string
}

type ResolveRequest struct {
	Host          HostInfo
	InventoryVars InventoryVars
	PlayVars      map[string]interface{}
	TaskVars      map[string]interface{}
	ExtraVars     map[string]interface{}
}

type ResolvedVars struct {
	Values     map[string]interface{}
	Namespaces map[string]interface{}
}

type layer struct {
	Name   string
	Values map[string]interface{}
}

func loadVarsFile(root, dir, name string) (map[string]interface{}, error) {
	if root == "" {
		root = "."
	}
	base := filepath.Join(root, dir)
	candidates := []string{
		filepath.Join(base, name+".yml"),
		filepath.Join(base, name+".yaml"),
		filepath.Join(base, name+".json"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read vars file %s: %w", path, err)
		}

		if len(strings.TrimSpace(string(data))) == 0 {
			return map[string]interface{}{}, nil
		}

		vars := map[string]interface{}{}
		if err := yaml.Unmarshal(data, &vars); err != nil {
			return nil, fmt.Errorf("failed to parse vars file %s: %w", path, err)
		}
		return vars, nil
	}

	return map[string]interface{}{}, nil
}

func mergeGroupVars(groups []string, groupVars map[string]map[string]interface{}, strategy MergeStrategy) map[string]interface{} {
	if len(groups) == 0 {
		return map[string]interface{}{}
	}
	unique := map[string]struct{}{}
	for _, group := range groups {
		if group == "" {
			continue
		}
		unique[group] = struct{}{}
	}
	sorted := make([]string, 0, len(unique))
	for group := range unique {
		sorted = append(sorted, group)
	}
	sort.Strings(sorted)

	merged := map[string]interface{}{}
	for _, group := range sorted {
		merged = mergeMaps(merged, groupVars[group], strategy)
	}
	return merged
}

func mergeMaps(base, override map[string]interface{}, strategy MergeStrategy) map[string]interface{} {
	return MergeMaps(base, override, strategy)
}

func MergeMaps(base, override map[string]interface{}, strategy MergeStrategy) map[string]interface{} {
	result := map[string]interface{}{}
	for key, value := range base {
		result[key] = value
	}

	for key, value := range override {
		if existing, ok := result[key]; ok && strategy == MergeDeep {
			existingMap, existingOK := existing.(map[string]interface{})
			overrideMap, overrideOK := value.(map[string]interface{})
			if existingOK && overrideOK {
				result[key] = mergeMaps(existingMap, overrideMap, strategy)
				continue
			}
		}
		result[key] = value
	}

	return result
}
