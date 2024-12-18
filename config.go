package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	MachineName string            `yaml:"machine_name"`
	SSH         *SSH              `yaml:"ssh"`
	Service     *Service          `yaml:"service"`
	Artifacts   []Artifact        `yaml:"artifacts"`
	Secrets     *Secrets          `yaml:"secrets"`
	Variables   map[string]string `yaml:"variables"`
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

type Service struct {
	Type        string     `yaml:"type"`
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Operation   string     `yaml:"operation"`
	Systemd     *Systemd   `yaml:"systemd"`
	Artifacts   []Artifact `yaml:"artifacts"`
}

type Systemd struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	BinPath     string            `yaml:"bin_path"`
	User        string            `yaml:"user"`
	WorkingDir  string            `yaml:"working_dir"`
	Env         map[string]string `yaml:"env"`
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
	yamlFile, err := os.ReadFile(filepath)
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
