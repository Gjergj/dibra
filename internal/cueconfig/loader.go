package cueconfig

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/gjergjiramku/dibra/internal/config"
)

// Load reads .cue files from the given path (file or directory) and returns
// a config.Config. The path can be:
//   - A directory containing .cue files (all .cue files in the directory are loaded)
//   - A single .cue file (only that file is loaded)
func Load(path string) (*config.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", path, err)
	}

	var v cue.Value
	if info.IsDir() {
		v, err = loadCUEDir(path)
	} else {
		v, err = loadCUEFile(path)
	}
	if err != nil {
		return nil, err
	}

	return Extract(v)
}

// LoadValue reads .cue files from the given path (file or directory) and returns
// the evaluated CUE value without extracting into config.Config.
func LoadValue(path string) (cue.Value, error) {
	info, err := os.Stat(path)
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to stat %s: %w", path, err)
	}

	if info.IsDir() {
		return loadCUEDir(path)
	}
	return loadCUEFile(path)
}

// loadCUEDir loads and evaluates all CUE files from a directory.
func loadCUEDir(dir string) (cue.Value, error) {
	ctx := cuecontext.New()
	cfg := &load.Config{Dir: dir, Overlay: schemaOverlay(dir)}
	instances := load.Instances([]string{"."}, cfg)
	if len(instances) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instances found in %s", dir)
	}

	inst := instances[0]
	if inst.Err != nil {
		return cue.Value{}, fmt.Errorf("failed to load CUE files: %w", inst.Err)
	}

	v := ctx.BuildInstance(inst)
	if v.Err() != nil {
		return cue.Value{}, fmt.Errorf("failed to build CUE instance: %w", v.Err())
	}

	if err := v.Validate(cue.Concrete(true)); err != nil {
		return cue.Value{}, fmt.Errorf("CUE validation failed: %w", err)
	}

	return v, nil
}

// loadCUEFile loads and evaluates a single CUE file.
// It uses the CUE loader (not CompileBytes) so that import statements are resolved.
func loadCUEFile(file string) (cue.Value, error) {
	absPath, err := filepath.Abs(file)
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to resolve path %s: %w", file, err)
	}
	dir := filepath.Dir(absPath)
	name := filepath.Base(absPath)

	ctx := cuecontext.New()
	cfg := &load.Config{Dir: dir, Overlay: schemaOverlay(dir)}
	instances := load.Instances([]string{name}, cfg)
	if len(instances) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instances found for %s", file)
	}

	inst := instances[0]
	if inst.Err != nil {
		return cue.Value{}, fmt.Errorf("failed to load CUE file: %w", inst.Err)
	}

	v := ctx.BuildInstance(inst)
	if v.Err() != nil {
		return cue.Value{}, fmt.Errorf("failed to build CUE instance: %w", v.Err())
	}

	if err := v.Validate(cue.Concrete(true)); err != nil {
		return cue.Value{}, fmt.Errorf("CUE validation failed: %w", err)
	}

	return v, nil
}

// ConfigFormat represents the detected config file format.
type ConfigFormat int

const (
	FormatYAML ConfigFormat = iota
	FormatCUE
)

// DetectFormat determines whether the given path should be loaded as YAML or CUE.
// Returns an error if a directory contains both .cue and .yaml/.yml files (ambiguous).
func DetectFormat(path string) (ConfigFormat, error) {
	ext := filepath.Ext(path)
	if ext == ".cue" {
		return FormatCUE, nil
	}
	if ext == ".yaml" || ext == ".yml" {
		return FormatYAML, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return FormatYAML, nil // let downstream handle the error
	}
	if !info.IsDir() {
		return FormatYAML, nil // non-directory, non-recognized extension → YAML default
	}

	cueFiles, _ := filepath.Glob(filepath.Join(path, "*.cue"))
	yamlFiles, _ := filepath.Glob(filepath.Join(path, "*.yaml"))
	ymlFiles, _ := filepath.Glob(filepath.Join(path, "*.yml"))
	hasYAML := len(yamlFiles)+len(ymlFiles) > 0
	hasCUE := len(cueFiles) > 0

	if hasCUE && hasYAML {
		return 0, fmt.Errorf("directory %s contains both .cue and .yaml files; use -config to point at a specific file", path)
	}
	if hasCUE {
		return FormatCUE, nil
	}
	return FormatYAML, nil
}

// IsCUEConfig returns true if the given path points to a .cue file or a
// directory containing only .cue files (no .yaml/.yml).
func IsCUEConfig(path string) bool {
	f, err := DetectFormat(path)
	return err == nil && f == FormatCUE
}

func schemaOverlay(baseDir string) map[string]load.Source {
	overlay := map[string]load.Source{}

	entries, err := fs.ReadDir(schemaFS, "schema")
	if err != nil {
		return overlay
	}

	moduleRoot := findModuleRoot(baseDir)
	if moduleRoot == "" {
		moduleRoot = baseDir
	}
	modulePath := "dibra.dev"

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cue") {
			continue
		}
		data, err := schemaFS.ReadFile(filepath.Join("schema", entry.Name()))
		if err != nil {
			continue
		}
		virtualPath := filepath.Join(moduleRoot, "cue.mod", "pkg", modulePath, "schema", entry.Name())
		overlay[virtualPath] = load.FromBytes(data)
	}

	return overlay
}

func findModuleRoot(start string) string {
	cur := start
	for {
		if cur == "" || cur == "/" {
			return ""
		}
		if info, err := os.Stat(filepath.Join(cur, "cue.mod")); err == nil && info.IsDir() {
			return cur
		}
		next := filepath.Dir(cur)
		if next == cur {
			return ""
		}
		cur = next
	}
}
