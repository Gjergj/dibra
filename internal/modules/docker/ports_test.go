package docker

import (
	"testing"

	"github.com/docker/go-connections/nat"
)

func TestParsePortBinding(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    PortBinding
		expectError bool
		errorContains string
	}{
		{
			name:  "container port only",
			input: "80",
			expected: PortBinding{
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "container port with protocol",
			input: "80/udp",
			expected: PortBinding{
				ContainerPort: "80",
				Protocol:      "udp",
			},
		},
		{
			name:  "host and container port",
			input: "8080:80",
			expected: PortBinding{
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "host and container port with protocol",
			input: "8080:80/tcp",
			expected: PortBinding{
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "ip:host:container",
			input: "127.0.0.1:8080:80",
			expected: PortBinding{
				HostIP:        "127.0.0.1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "ip::container (random host port)",
			input: "127.0.0.1::80",
			expected: PortBinding{
				HostIP:        "127.0.0.1",
				HostPort:      "",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "ipv6 with brackets",
			input: "[::1]:8080:80",
			expected: PortBinding{
				HostIP:        "::1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "port range",
			input: "8000-8010:9000-9010",
			expected: PortBinding{
				HostPort:      "8000-8010",
				ContainerPort: "9000-9010",
				Protocol:      "tcp",
			},
		},
		{
			name:  "port range with ip",
			input: "0.0.0.0:8000-8010:9000-9010/udp",
			expected: PortBinding{
				HostIP:        "0.0.0.0",
				HostPort:      "8000-8010",
				ContainerPort: "9000-9010",
				Protocol:      "udp",
			},
		},
		// Error cases
		{
			name:          "invalid protocol",
			input:         "80/xyz",
			expectError:   true,
			errorContains: "invalid protocol",
		},
		{
			name:          "unclosed bracket",
			input:         "[::1:8080:80",
			expectError:   true,
			errorContains: "closing \"]\"",
		},
		{
			name:          "hostname instead of ip",
			input:         "localhost:8080:80",
			expectError:   true,
			errorContains: "hostnames",
		},
		{
			name:          "too many colons",
			input:         "::1:8080:80:extra",
			expectError:   true,
			errorContains: "colon-separated parts",
		},
		{
			name:          "invalid port number",
			input:         "99999:80",
			expectError:   true,
			errorContains: "port must be",
		},
		{
			name:          "invalid port range",
			input:         "8080-8000:80", // Start > end
			expectError:   true,
			errorContains: "less than or equal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParsePortBinding(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorContains)
				} else if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.HostIP != tt.expected.HostIP {
				t.Errorf("HostIP: got %q, want %q", result.HostIP, tt.expected.HostIP)
			}
			if result.HostPort != tt.expected.HostPort {
				t.Errorf("HostPort: got %q, want %q", result.HostPort, tt.expected.HostPort)
			}
			if result.ContainerPort != tt.expected.ContainerPort {
				t.Errorf("ContainerPort: got %q, want %q", result.ContainerPort, tt.expected.ContainerPort)
			}
			if result.Protocol != tt.expected.Protocol {
				t.Errorf("Protocol: got %q, want %q", result.Protocol, tt.expected.Protocol)
			}
		})
	}
}

func TestPortBindingString(t *testing.T) {
	tests := []struct {
		name     string
		binding  PortBinding
		expected string
	}{
		{
			name: "simple",
			binding: PortBinding{
				ContainerPort: "80",
				Protocol:      "tcp",
			},
			expected: "80/tcp",
		},
		{
			name: "with host port",
			binding: PortBinding{
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
			expected: "8080:80/tcp",
		},
		{
			name: "with ip",
			binding: PortBinding{
				HostIP:        "127.0.0.1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
			expected: "127.0.0.1:8080:80/tcp",
		},
		{
			name: "ipv6",
			binding: PortBinding{
				HostIP:        "::1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "udp",
			},
			expected: "[::1]:8080:80/udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.binding.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestToNatPortMap(t *testing.T) {
	bindings := []PortBinding{
		{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		{HostPort: "9090", ContainerPort: "90", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}, // Duplicate container port
	}

	portMap, exposedPorts, err := ToNatPortMap(bindings)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should have 2 container ports
	if len(exposedPorts) != 2 {
		t.Errorf("Expected 2 exposed ports, got %d", len(exposedPorts))
	}

	// Port 80 should have 2 bindings
	port80Bindings := portMap[nat.Port("80/tcp")]
	if len(port80Bindings) != 2 {
		t.Errorf("Expected 2 bindings for port 80, got %d", len(port80Bindings))
	}
}

func TestComparePortBindings(t *testing.T) {
	tests := []struct {
		name     string
		desired  nat.PortMap
		current  nat.PortMap
		expected bool
	}{
		{
			name:     "both empty",
			desired:  nat.PortMap{},
			current:  nat.PortMap{},
			expected: true,
		},
		{
			name: "same bindings",
			desired: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "8080"}},
			},
			current: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "8080"}},
			},
			expected: true,
		},
		{
			name: "different host ports",
			desired: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "8080"}},
			},
			current: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "9090"}},
			},
			expected: false,
		},
		{
			name: "empty vs 0.0.0.0 IP (equivalent)",
			desired: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostIP: "", HostPort: "8080"}},
			},
			current: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "8080"}},
			},
			expected: true,
		},
		{
			name: "random host port desired",
			desired: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: ""}},
			},
			current: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "32768"}},
			},
			expected: true, // Empty desired means any port is OK
		},
		{
			name: "missing port in current",
			desired: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "8080"}},
				"443/tcp": []nat.PortBinding{{HostPort: "8443"}},
			},
			current: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostPort: "8080"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComparePortBindings(tt.desired, tt.current)
			if result != tt.expected {
				t.Errorf("ComparePortBindings() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExpandPortRange(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string
		expectError bool
	}{
		{
			name:     "single port",
			input:    "8080",
			expected: []string{"8080"},
		},
		{
			name:     "port range",
			input:    "8080-8082",
			expected: []string{"8080", "8081", "8082"},
		},
		{
			name:        "invalid range (start > end)",
			input:       "8082-8080",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExpandPortRange(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !CompareStringSlicesOrdered(result, tt.expected) {
				t.Errorf("ExpandPortRange(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
