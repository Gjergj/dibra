package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Include   []string          `yaml:"include"`
	SSH       *SSH              `yaml:"ssh"`
	Tasks     []Task            `yaml:"tasks"`
	Artifacts []Artifact        `yaml:"artifacts"`
	Secrets   *Secrets          `yaml:"secrets"`
	Variables map[string]string `yaml:"variables"`
}

func (c *Config) Validate() error {
	if c.SSH == nil {
		return fmt.Errorf("ssh is required")
	}

	if c.Artifacts != nil {
		for _, artifact := range c.Artifacts {
			if err := artifact.Validate(); err != nil {
				return err
			}
		}
	}

	return nil
}

type Secrets struct {
	Adapter string            `yaml:"adapter"`
	List    map[string]string `yaml:"list"`
}

type Task struct {
	// Type        string     `yaml:"type"`
	Hosts       []string `yaml:"hosts"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Systemd     *Systemd `yaml:"systemd"`
}

type Systemd struct {
	Name        string `yaml:"name"`
	Operation   string `yaml:"operation"`
	Description string `yaml:"description"`
	ExecStart   string `yaml:"exec_start"`
	Group       string `yaml:"group"`
	// BinPath     string            `yaml:"bin_path"`
	User       string            `yaml:"user"`
	WorkingDir string            `yaml:"working_dir"`
	Env        map[string]string `yaml:"env"`
	Artifacts  []Artifact        `yaml:"artifacts"`
}

type Artifact struct {
	Type        string            `yaml:"type"`
	Path        string            `yaml:"path"`
	RemotePath  string            `yaml:"remote_path"`
	Content     string            `yaml:"content"`
	Constraints map[string]string `yaml:"constraints"`
}

func (a *Artifact) Validate() error {
	if a.Type == "" {
		return fmt.Errorf("artifact type is required")
	} else if a.Type == "local" {
		if a.RemotePath == "" {
			return fmt.Errorf("remote artifact path is required")
		} else if a.Content == "" && a.Path == "" {
			return fmt.Errorf("artifact content or local path is required")
		}
	} else {
		return fmt.Errorf("unsupported artifact type: %s", a.Type)
	}

	for constraintType, constraint := range a.Constraints {
		if _, ok := artifactConstraintTypeMap[ArtifactConstraintType(constraintType)]; !ok {
			return fmt.Errorf("unsupported artifact constraint type: %s", constraintType)
		}
		if constraint == string(ArtifactConstraintTypeExecutable) && a.Content != "" {
			return fmt.Errorf("can not specify executable constraint with content")
		}
	}
	return nil
}

type SSH struct {
	Host          string `yaml:"host"`
	User          string `yaml:"user"`
	Port          string `yaml:"port"`
	KeyPath       string `yaml:"key_path"`
	Password      string `yaml:"password"`
	Timeout       int    `yaml:"timeout"`
	AllowInsecure bool   `yaml:"allow_insecure"`
}

func LoadConfig(filepath string) (*Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	yamlFile, err := mergeConfigs(filepath, dir)
	if err != nil {
		return nil, err
	}

	yamlFile, err = handleSecrets(yamlFile)
	if err != nil {
		return nil, err
	}

	yamlFile, err = expandVariables(yamlFile)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func mergeConfigs(configFilepath string, wd string) ([]byte, error) {
	configFilepath = filepath.Join(wd, configFilepath)
	yamlFile, err := os.ReadFile(configFilepath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}
	var tmpConfigs []Config

	if config.Include != nil {
		for _, path := range config.Include {
			configFilepath = filepath.Join(filepath.Dir(configFilepath), path)
			yamlFile, err = os.ReadFile(configFilepath)
			if err != nil {
				return nil, err
			}

			var tmpConfig Config
			err = yaml.Unmarshal(yamlFile, &tmpConfig)
			if err != nil {
				return nil, err
			}
			tmpConfigs = append(tmpConfigs, tmpConfig)
		}

		for _, tmpConfig := range tmpConfigs {
			config.Tasks = append(config.Tasks, tmpConfig.Tasks...)
			config.Artifacts = append(config.Artifacts, tmpConfig.Artifacts...)
			maps.Copy(config.Variables, tmpConfig.Variables)
		}
	}
	yamlFile, err = yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	return yamlFile, nil
}

func handleSecrets(configFileContent []byte) ([]byte, error) {
	var config Config
	err := yaml.Unmarshal(configFileContent, &config)
	if err != nil {
		return nil, err
	}

	if config.Secrets != nil {
		if config.Secrets.Adapter == "bitwarden" {
			sess, err := unlockBitwarden("")
			if err != nil {
				return nil, err
			}

			secrets, err := getSecrets(sess, config.Secrets.List)
			if err != nil {
				return nil, err
			}

			yamlFile := os.Expand(string(configFileContent), func(key string) string {
				if val, ok := secrets[key]; ok {
					return val
				}
				return fmt.Sprintf("${%s}", key)

			})
			return []byte(yamlFile), nil
		}

		return nil, fmt.Errorf("unsupported secret adapter: %s", config.Secrets.Adapter)
	}

	return configFileContent, nil
}

func expandVariables(configFileContent []byte) ([]byte, error) {
	var config Config
	err := yaml.Unmarshal(configFileContent, &config)
	if err != nil {
		return nil, err
	}

	yamlFile := os.Expand(string(configFileContent), func(key string) string {
		if val, ok := config.Variables[key]; ok {
			return val
		}
		return fmt.Sprintf("${%s}", key)
	})
	return []byte(yamlFile), nil
}
