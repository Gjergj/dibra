package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/template"
	"github.com/gjergjiramku/dibra/internal/vars"
	"gopkg.in/yaml.v3"
)

func executeIncludeVars(
	params *config.IncludeVarsParams,
	context map[string]interface{},
	runtimeVars map[string]interface{},
	sourceDir string,
) map[string]interface{} {
	rendered, err := renderIncludeVarsParams(params, context)
	if err != nil {
		return includeVarsFailure(err)
	}
	if rendered.File != "" && rendered.Dir != "" {
		return includeVarsFailure(fmt.Errorf("include_vars: file and dir arguments are incompatible"))
	}
	if rendered.File == "" && rendered.Dir == "" {
		return includeVarsFailure(fmt.Errorf("include_vars: a file or dir argument is required"))
	}
	if rendered.Depth < 0 {
		return includeVarsFailure(fmt.Errorf("include_vars: depth must be zero or greater"))
	}
	if rendered.HashBehaviour != "" && rendered.HashBehaviour != "replace" && rendered.HashBehaviour != "merge" {
		return includeVarsFailure(fmt.Errorf("include_vars: hash_behaviour must be replace or merge"))
	}
	if rendered.Name != "" {
		if err := validateVarName(rendered.Name); err != nil {
			return includeVarsFailure(fmt.Errorf("include_vars: %v", err))
		}
	}

	var included map[string]interface{}
	var files []string
	if rendered.File != "" {
		path := resolveControllerPath(sourceDir, rendered.File)
		included, err = loadIncludedVarsFile(path)
		if err != nil {
			return includeVarsFailure(fmt.Errorf("include_vars: %w", err))
		}
		files = []string{path}
	} else {
		included, files, err = loadIncludedVarsDirectory(sourceDir, rendered)
		if err != nil {
			return includeVarsFailure(fmt.Errorf("include_vars: %w", err))
		}
	}

	if rendered.Name != "" {
		included = map[string]interface{}{rendered.Name: included}
	}
	if rendered.HashBehaviour == "merge" {
		existing := make(map[string]interface{})
		for key := range included {
			if value, ok := context[key]; ok {
				existing[key] = value
			}
		}
		included = vars.MergeMaps(existing, included, vars.MergeDeep)
	}
	for key, value := range included {
		runtimeVars[key] = value
	}

	return map[string]interface{}{
		"changed":                    false,
		"failed":                     false,
		"msg":                        "",
		"ansible_facts":              included,
		"ansible_included_var_files": stringSliceToInterfaces(files),
	}
}

func renderIncludeVarsParams(params *config.IncludeVarsParams, context map[string]interface{}) (*config.IncludeVarsParams, error) {
	rendered := *params
	var err error
	if rendered.File, err = template.RenderString(params.File, context); err != nil {
		return nil, fmt.Errorf("include_vars: failed to render file: %w", err)
	}
	if rendered.Dir, err = template.RenderString(params.Dir, context); err != nil {
		return nil, fmt.Errorf("include_vars: failed to render dir: %w", err)
	}
	if rendered.Name, err = template.RenderString(params.Name, context); err != nil {
		return nil, fmt.Errorf("include_vars: failed to render name: %w", err)
	}
	if rendered.FilesMatching, err = template.RenderString(params.FilesMatching, context); err != nil {
		return nil, fmt.Errorf("include_vars: failed to render files_matching: %w", err)
	}
	if rendered.HashBehaviour, err = template.RenderString(params.HashBehaviour, context); err != nil {
		return nil, fmt.Errorf("include_vars: failed to render hash_behaviour: %w", err)
	}
	rendered.IgnoreFiles, err = renderStringSlice(params.IgnoreFiles, context)
	if err != nil {
		return nil, fmt.Errorf("include_vars: failed to render ignore_files: %w", err)
	}
	rendered.Extensions, err = renderStringSlice(params.Extensions, context)
	if err != nil {
		return nil, fmt.Errorf("include_vars: failed to render extensions: %w", err)
	}
	return &rendered, nil
}

func loadIncludedVarsDirectory(sourceDir string, params *config.IncludeVarsParams) (map[string]interface{}, []string, error) {
	root := resolveControllerPath(sourceDir, params.Dir)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("%s directory does not exist", root)
		}
		return nil, nil, fmt.Errorf("failed to inspect directory %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", root)
	}

	var matcher *regexp.Regexp
	if params.FilesMatching != "" {
		matcher, err = regexp.Compile(params.FilesMatching)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid files_matching regular expression %q: %w", params.FilesMatching, err)
		}
	}
	ignoreMatchers := make([]*regexp.Regexp, 0, len(params.IgnoreFiles))
	for _, pattern := range params.IgnoreFiles {
		compiled, compileErr := regexp.Compile(pattern + "$")
		if compileErr != nil {
			return nil, nil, fmt.Errorf("invalid ignore_files regular expression %q: %w", pattern, compileErr)
		}
		ignoreMatchers = append(ignoreMatchers, compiled)
	}
	extensions := params.Extensions
	if len(extensions) == 0 {
		extensions = []string{"json", "yaml", "yml"}
	}
	allowedExtensions := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		allowedExtensions[strings.TrimPrefix(extension, ".")] = struct{}{}
	}

	var candidates []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		depth := len(strings.Split(filepath.ToSlash(relative), "/"))
		if entry.IsDir() {
			if params.Depth > 0 && depth >= params.Depth {
				return filepath.SkipDir
			}
			return nil
		}
		if params.Depth > 0 && depth > params.Depth {
			return nil
		}
		name := entry.Name()
		for _, ignored := range ignoreMatchers {
			if ignored.MatchString(name) {
				return nil
			}
		}
		if matcher != nil && !matcher.MatchString(name) {
			return nil
		}
		extension := strings.TrimPrefix(filepath.Ext(name), ".")
		if _, ok := allowedExtensions[extension]; !ok {
			if params.IgnoreUnknownExtensions {
				return nil
			}
			return fmt.Errorf("%q does not have a valid extension: %s", path, strings.Join(extensions, ", "))
		}
		candidates = append(candidates, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(candidates)

	merged := make(map[string]interface{})
	for _, path := range candidates {
		loaded, loadErr := loadIncludedVarsFile(path)
		if loadErr != nil {
			return nil, nil, loadErr
		}
		for key, value := range loaded {
			merged[key] = value
		}
	}
	return merged, candidates, nil
}

func loadIncludedVarsFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read vars file %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]interface{}{}, nil
	}
	loaded := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return nil, fmt.Errorf("failed to parse vars file %s: %w", path, err)
	}
	if loaded == nil {
		return map[string]interface{}{}, nil
	}
	return loaded, nil
}

func resolveControllerPath(sourceDir, path string) string {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(sourceDir, path)
	}
	return filepath.Clean(path)
}

func renderStringSlice(values []string, context map[string]interface{}) ([]string, error) {
	rendered := make([]string, len(values))
	for index, value := range values {
		item, err := template.RenderString(value, context)
		if err != nil {
			return nil, err
		}
		rendered[index] = item
	}
	return rendered, nil
}

func includeVarsFailure(err error) map[string]interface{} {
	return map[string]interface{}{
		"changed": false,
		"failed":  true,
		"msg":     err.Error(),
		"message": err.Error(),
	}
}

func stringSliceToInterfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
