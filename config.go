package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	MachineName string   `yaml:"machine_name"`
	SSH         *SSH     `yaml:"ssh"`
	Service     *Service `yaml:"service"`
	Secrets     *Secrets `yaml:"secrets"`
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
	Constraints map[string]string `yaml:"constraints"`
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

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
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
			return secrets[key]

		})
		return []byte(yamlFile), nil
	}

	return nil, fmt.Errorf("unsupported secret adapter: %s", config.Secrets.Adapter)
}

func validateConfig(c *Config) error {
	return nil
}
