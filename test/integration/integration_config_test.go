package integration

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const (
	integrationProfileFull   integrationProfile = "full"
	integrationProfileDocker integrationProfile = "docker"
	integrationProfileCore   integrationProfile = "core"
)

type integrationProfile string

type integrationConfig struct {
	Profile                integrationProfile
	Host                   string
	Port                   int
	User                   string
	Password               string
	PlaybookHost           string
	PlaybookHeaderOverride string
}

func defaultIntegrationConfig() integrationConfig {
	return integrationConfig{
		Profile:      integrationProfileFull,
		Host:         "127.0.0.1",
		Port:         2222,
		User:         "root",
		Password:     "rootpass",
		PlaybookHost: "localhost",
	}
}

func loadIntegrationConfig(getenv func(string) string) (integrationConfig, error) {
	config := defaultIntegrationConfig()

	switch profile := strings.ToLower(strings.TrimSpace(getenv("DIBRA_INTEGRATION_PROFILE"))); profile {
	case "", "full":
		config.Profile = integrationProfileFull
	case "docker":
		config.Profile = integrationProfileDocker
	case "core":
		config.Profile = integrationProfileCore
	default:
		return integrationConfig{}, fmt.Errorf(
			"invalid DIBRA_INTEGRATION_PROFILE %q (want full, docker, or core)",
			profile,
		)
	}

	if value := getenv("DIBRA_INTEGRATION_HOST"); value != "" {
		config.Host = value
	}
	if value := getenv("DIBRA_INTEGRATION_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return integrationConfig{}, fmt.Errorf(
				"invalid DIBRA_INTEGRATION_PORT %q (want 1-65535)",
				value,
			)
		}
		config.Port = port
	}
	if value := getenv("DIBRA_INTEGRATION_USER"); value != "" {
		config.User = value
	}
	if value := getenv("DIBRA_INTEGRATION_PASSWORD"); value != "" {
		config.Password = value
	}
	if value := getenv("DIBRA_INTEGRATION_PLAYBOOK_HOST"); value != "" {
		config.PlaybookHost = value
	}
	if value := getenv("DIBRA_INTEGRATION_PLAYBOOK_HEADER"); value != "" {
		config.PlaybookHeaderOverride = ensureTrailingNewline(value)
	}

	return config, nil
}

func (profile integrationProfile) requiresDockerBaselines() bool {
	return profile == integrationProfileFull || profile == integrationProfileDocker
}

func (config integrationConfig) playbookHeader() string {
	if config.PlaybookHeaderOverride != "" {
		return config.PlaybookHeaderOverride
	}
	return config.playbookHeaderFor(config.User, config.Password, true, "")
}

func (config integrationConfig) playbookHeaderFor(
	user string,
	password string,
	become bool,
	becomePassword string,
) string {
	var header strings.Builder
	fmt.Fprintf(&header, `
hosts:
  - name: testhost
    host: %s
    port: %d
    user: %s
    password: %s
`, strconv.Quote(config.PlaybookHost), config.Port, strconv.Quote(user), strconv.Quote(password))
	if become {
		header.WriteString("    become: true\n")
	}
	if becomePassword != "" {
		fmt.Fprintf(&header, "    become_password: %s\n", strconv.Quote(becomePassword))
	}
	header.WriteString("\ntasks:\n")
	return header.String()
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func TestLoadIntegrationConfigDefaults(t *testing.T) {
	config, err := loadIntegrationConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	want := defaultIntegrationConfig()
	if config != want {
		t.Fatalf("got %#v, want %#v", config, want)
	}
	if !config.Profile.requiresDockerBaselines() {
		t.Fatal("the default full profile must enforce Docker baselines")
	}
}

func TestLoadIntegrationConfigCoreOverrides(t *testing.T) {
	values := map[string]string{
		"DIBRA_INTEGRATION_PROFILE":       "core",
		"DIBRA_INTEGRATION_HOST":          "192.0.2.10",
		"DIBRA_INTEGRATION_PORT":          "2205",
		"DIBRA_INTEGRATION_USER":          "tester",
		"DIBRA_INTEGRATION_PASSWORD":      "password with spaces",
		"DIBRA_INTEGRATION_PLAYBOOK_HOST": "core-host",
	}

	config, err := loadIntegrationConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if config.Profile != integrationProfileCore {
		t.Fatalf("profile = %q, want core", config.Profile)
	}
	if config.Profile.requiresDockerBaselines() {
		t.Fatal("the core profile must not enforce Docker baselines")
	}
	if config.Host != "192.0.2.10" || config.Port != 2205 {
		t.Fatalf("SSH endpoint = %s:%d", config.Host, config.Port)
	}

	header := config.playbookHeader()
	for _, expected := range []string{
		`host: "core-host"`,
		"port: 2205",
		`user: "tester"`,
		`password: "password with spaces"`,
		"become: true",
	} {
		if !strings.Contains(header, expected) {
			t.Errorf("header missing %q:\n%s", expected, header)
		}
	}
}

func TestLoadIntegrationConfigDockerProfileIsStrict(t *testing.T) {
	config, err := loadIntegrationConfig(func(key string) string {
		if key == "DIBRA_INTEGRATION_PROFILE" {
			return "docker"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("load docker profile: %v", err)
	}
	if !config.Profile.requiresDockerBaselines() {
		t.Fatal("the Docker profile must enforce Docker baselines")
	}
}

func TestLoadIntegrationConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		needle string
	}{
		{name: "profile", key: "DIBRA_INTEGRATION_PROFILE", value: "minimal", needle: "full, docker, or core"},
		{name: "port text", key: "DIBRA_INTEGRATION_PORT", value: "ssh", needle: "1-65535"},
		{name: "port zero", key: "DIBRA_INTEGRATION_PORT", value: "0", needle: "1-65535"},
		{name: "port high", key: "DIBRA_INTEGRATION_PORT", value: "65536", needle: "1-65535"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadIntegrationConfig(func(key string) string {
				if key == test.key {
					return test.value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("error = %v, want containing %q", err, test.needle)
			}
		})
	}
}

func TestIntegrationConfigPlaybookHeaderOverride(t *testing.T) {
	const override = "hosts: []\ntasks: []"
	config, err := loadIntegrationConfig(func(key string) string {
		if key == "DIBRA_INTEGRATION_PLAYBOOK_HEADER" {
			return override
		}
		return ""
	})
	if err != nil {
		t.Fatalf("load header override: %v", err)
	}
	if got, want := config.playbookHeader(), override+"\n"; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}
