package main

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestLoadConfig(t *testing.T) {
	yamlFile, err := os.ReadFile("testdata/test_config.yml")
	if err != nil {
		t.Fatalf("Failed to read test_config.yml: %v", err)
	}

	yamlFile, err = expandVariables(yamlFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}
	fmt.Println(config)
}

func TestArtifactConstraints(t *testing.T) {
	yamlFile, err := os.ReadFile("testdata/test_config.yml")
	if err != nil {
		t.Fatalf("Failed to read test_config.yml: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}
	err = config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

}

func TestInclude(t *testing.T) {
	config, err := LoadConfig("testdata/test_config.yml")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	slices.Compare(config.Include, []string{"test_config_include.yml"})
	fmt.Println(config)
}
