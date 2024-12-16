package main

import (
	"fmt"
	"os"
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
