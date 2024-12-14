package main

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	MachineName string   `yaml:"machine_name"`
	SSH         *SSH     `yaml:"ssh"`
	Service     *Service `yaml:"service"`
}

type Service struct {
	Type    string   `yaml:"type"`
	Systemd *Systemd `yaml:"systemd"`
}

type Systemd struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	BinPath     string            `yaml:"bin_path"`
	User        string            `yaml:"user"`
	WorkingDir  string            `yaml:"working_dir"`
	Env         map[string]string `yaml:"env"`
	Artifact    *Artifact         `yaml:"artifact"`
}

type Artifact struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

type SSH struct {
	Host          string `yaml:"host"`
	User          string `yaml:"user"`
	Port          int    `yaml:"port"`
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

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func validateConfig(c *Config) error {
	return nil
}
