package docker

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
)

// PortBinding represents a parsed port binding specification.
type PortBinding struct {
	HostIP        string // Host IP to bind to (empty for all interfaces)
	HostPort      string // Host port or range (e.g., "8080" or "8080-8090")
	ContainerPort string // Container port or range (e.g., "80" or "80-90")
	Protocol      string // Protocol: "tcp" or "udp" (default: "tcp")
}

// String returns the string representation of the port binding.
func (p PortBinding) String() string {
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}

	var parts []string
	if p.HostIP != "" {
		// IPv6 addresses need brackets
		if strings.Contains(p.HostIP, ":") {
			parts = append(parts, "["+p.HostIP+"]")
		} else {
			parts = append(parts, p.HostIP)
		}
	}
	if p.HostPort != "" {
		parts = append(parts, p.HostPort)
	}
	parts = append(parts, p.ContainerPort+"/"+proto)

	return strings.Join(parts, ":")
}

// ContainerPortKey returns the container port key for nat.PortMap.
func (p PortBinding) ContainerPortKey() nat.Port {
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}
	return nat.Port(p.ContainerPort + "/" + proto)
}

// NatPortBinding returns the nat.PortBinding for this port binding.
func (p PortBinding) NatPortBinding() nat.PortBinding {
	return nat.PortBinding{
		HostIP:   p.HostIP,
		HostPort: p.HostPort,
	}
}

// ParsePortBinding parses a Docker port specification string.
// Supported formats:
//   - "containerPort"                      -> bind random host port
//   - "hostPort:containerPort"             -> bind specific host port
//   - "ip:hostPort:containerPort"          -> bind specific host port on IP
//   - "[ipv6]:hostPort:containerPort"      -> bind specific host port on IPv6
//   - "containerPort/protocol"             -> specify protocol (tcp/udp)
//   - "hostPort:containerPort/protocol"    -> with protocol
//   - "ip::containerPort"                  -> random host port on specific IP
//   - "hostPortRange:containerPortRange"   -> port ranges (e.g., "8000-8010:9000-9010")
func ParsePortBinding(spec string) (PortBinding, error) {
	result := PortBinding{Protocol: "tcp"}

	// Extract protocol if present
	protoIdx := strings.LastIndex(spec, "/")
	if protoIdx > 0 {
		proto := strings.ToLower(spec[protoIdx+1:])
		if proto != "tcp" && proto != "udp" && proto != "sctp" {
			return result, fmt.Errorf("invalid protocol %q: must be tcp, udp, or sctp", proto)
		}
		result.Protocol = proto
		spec = spec[:protoIdx]
	}

	// Handle IPv6 addresses in brackets
	if strings.HasPrefix(spec, "[") {
		// Find closing bracket
		closeBracket := strings.Index(spec, "]")
		if closeBracket < 0 {
			return result, fmt.Errorf("cannot find closing \"]\" in input %q for opening \"[\" at index 1", spec)
		}
		result.HostIP = spec[1:closeBracket]
		spec = spec[closeBracket+1:]
		spec = strings.TrimPrefix(spec, ":")
	}

	// Split remaining by colon
	parts := strings.Split(spec, ":")

	switch len(parts) {
	case 1:
		// Just container port
		result.ContainerPort = parts[0]
	case 2:
		if result.HostIP != "" {
			// Already have IP from brackets: [ip]:host:container parsed as [ip] + host:container
			result.HostPort = parts[0]
			result.ContainerPort = parts[1]
		} else {
			// hostPort:containerPort
			if parts[0] == "" {
				return result, fmt.Errorf("invalid port %q: host port cannot be empty without a host IP", spec)
			}
			result.HostPort = parts[0]
			result.ContainerPort = parts[1]
		}
	case 3:
		if result.HostIP != "" {
			return result, fmt.Errorf("invalid port description %q: too many colons after IPv6 address", spec)
		}
		// ip:hostPort:containerPort
		ip := parts[0]
		if net.ParseIP(ip) == nil {
			return result, fmt.Errorf("bind addresses for published ports must be IPv4 or IPv6 addresses, not hostnames. Use the dig lookup to resolve hostnames. (Found hostname: %s)", ip)
		}
		result.HostIP = ip
		result.HostPort = parts[1]
		result.ContainerPort = parts[2]
	default:
		// community.docker also accepts an unbracketed IPv6 host followed by
		// host and container ports. Parse those two fields from the right.
		ip := strings.Join(parts[:len(parts)-2], ":")
		if net.ParseIP(ip) == nil {
			return result, fmt.Errorf("invalid port description %q - expected a valid IPv6 address followed by host and container ports", spec)
		}
		result.HostIP = ip
		result.HostPort = parts[len(parts)-2]
		result.ContainerPort = parts[len(parts)-1]
	}

	// Validate container port
	if result.ContainerPort == "" {
		return result, fmt.Errorf("container port is required")
	}

	if err := validatePortOrRange(result.ContainerPort); err != nil {
		return result, fmt.Errorf("invalid container port %q: %v", result.ContainerPort, err)
	}

	if result.HostPort != "" {
		if err := validatePortOrRange(result.HostPort); err != nil {
			return result, fmt.Errorf("invalid host port %q: %v", result.HostPort, err)
		}
	}

	return result, nil
}

// validatePortOrRange validates a port number or range (e.g., "8080" or "8080-8090").
func validatePortOrRange(port string) error {
	if strings.Contains(port, "-") {
		parts := strings.Split(port, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid port range format")
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil || start < 1 || start > 65535 {
			return fmt.Errorf("invalid start port in range")
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil || end < 1 || end > 65535 {
			return fmt.Errorf("invalid end port in range")
		}
		if start > end {
			return fmt.Errorf("start port must be less than or equal to end port")
		}
		return nil
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}

// ParsePortBindings parses multiple port binding specifications.
func ParsePortBindings(specs []string) ([]PortBinding, error) {
	result := make([]PortBinding, 0, len(specs))
	for _, spec := range specs {
		pb, err := ParsePortBinding(spec)
		if err != nil {
			return nil, err
		}
		result = append(result, pb)
	}
	return result, nil
}

// ToNatPortMap converts port bindings to Docker's nat.PortMap format.
func ToNatPortMap(bindings []PortBinding) (nat.PortMap, nat.PortSet, error) {
	portMap := make(nat.PortMap)
	exposedPorts := make(nat.PortSet)

	for _, b := range bindings {
		expanded, err := ExpandPortBinding(b)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range expanded {
			key := item.ContainerPortKey()
			exposedPorts[key] = struct{}{}

			if portMap[key] == nil {
				portMap[key] = []nat.PortBinding{}
			}
			portMap[key] = append(portMap[key], item.NatPortBinding())
		}
	}

	return portMap, exposedPorts, nil
}

// BuildPortBindings parses and expands a collection of Docker port
// specifications into the Engine API representation.
func BuildPortBindings(specs []string) (nat.PortMap, nat.PortSet, error) {
	bindings, err := ParsePortBindings(specs)
	if err != nil {
		return nil, nil, err
	}
	return ToNatPortMap(bindings)
}

// ExpandPortBinding expands container and host ranges. Paired ranges must have
// equal lengths. A host range paired with one container port is preserved
// because Docker treats it as a range from which it may select an available
// host port.
func ExpandPortBinding(binding PortBinding) ([]PortBinding, error) {
	containerPorts, err := ExpandPortRange(binding.ContainerPort)
	if err != nil {
		return nil, fmt.Errorf("invalid container port range %q: %w", binding.ContainerPort, err)
	}

	var hostPorts []string
	if binding.HostPort != "" {
		hostPorts, err = ExpandPortRange(binding.HostPort)
		if err != nil {
			return nil, fmt.Errorf("invalid host port range %q: %w", binding.HostPort, err)
		}
		if len(containerPorts) == 1 && len(hostPorts) > 1 {
			hostPorts = []string{binding.HostPort}
		} else if len(hostPorts) != len(containerPorts) {
			return nil, fmt.Errorf("Port ranges don't match in length")
		}
	}

	result := make([]PortBinding, 0, len(containerPorts))
	for index, containerPort := range containerPorts {
		item := binding
		item.ContainerPort = containerPort
		if len(hostPorts) > 0 {
			item.HostPort = hostPorts[index]
		}
		result = append(result, item)
	}
	return result, nil
}

// PortMapKey is a sortable key for port map entries.
type PortMapKey struct {
	ContainerPort int
	Protocol      string
}

// NormalizePortBindings normalizes a nat.PortMap for comparison.
// Returns a sorted, normalized representation.
func NormalizePortBindings(pm nat.PortMap) nat.PortMap {
	if pm == nil {
		return nil
	}

	result := make(nat.PortMap)
	for port, bindings := range pm {
		// Normalize port key
		normalizedPort := normalizePort(port)

		// Sort bindings by IP:Port
		sortedBindings := make([]nat.PortBinding, len(bindings))
		copy(sortedBindings, bindings)
		for index := range sortedBindings {
			sortedBindings[index].HostIP = normalizeBindingIP(sortedBindings[index].HostIP)
		}
		sort.Slice(sortedBindings, func(i, j int) bool {
			if sortedBindings[i].HostIP != sortedBindings[j].HostIP {
				return sortedBindings[i].HostIP < sortedBindings[j].HostIP
			}
			return sortedBindings[i].HostPort < sortedBindings[j].HostPort
		})
		result[normalizedPort] = sortedBindings
	}

	return result
}

// normalizePort normalizes a nat.Port (e.g., "80/tcp" -> "80/tcp").
func normalizePort(port nat.Port) nat.Port {
	p := port.Port()
	proto := port.Proto()
	if proto == "" {
		proto = "tcp"
	}
	return nat.Port(p + "/" + proto)
}

// ComparePortBindings compares two port maps for equality.
func ComparePortBindings(desired, current nat.PortMap) bool {
	return comparePortBindings(desired, current, true)
}

// PortBindingsContain reports whether every desired container-port binding is
// present in current while allowing current to expose additional container
// ports. Bindings for a requested port still compare exactly, matching
// community.docker's allow_more_present dictionary semantics.
func PortBindingsContain(desired, current nat.PortMap) bool {
	return comparePortBindings(desired, current, false)
}

func comparePortBindings(desired, current nat.PortMap, strict bool) bool {
	if strict && len(desired) != len(current) {
		return false
	}

	normDesired := NormalizePortBindings(desired)
	normCurrent := NormalizePortBindings(current)

	for port, desiredBindings := range normDesired {
		currentBindings, ok := normCurrent[port]
		if !ok {
			return false
		}

		if len(desiredBindings) != len(currentBindings) {
			return false
		}

		for i, db := range desiredBindings {
			cb := currentBindings[i]
			// Compare IPs (empty and 0.0.0.0 are equivalent for IPv4)
			if !compareHostIP(db.HostIP, cb.HostIP) {
				return false
			}
			// Compare ports (ignore if desired is empty - means random)
			if db.HostPort != "" && db.HostPort != cb.HostPort {
				return false
			}
		}
	}

	return true
}

// compareHostIP compares two host IPs for equivalence.
// Empty string and "0.0.0.0" are considered equivalent.
func compareHostIP(a, b string) bool {
	return normalizeBindingIP(a) == normalizeBindingIP(b)
}

func normalizeBindingIP(ip string) string {
	if ip == "" || ip == "0.0.0.0" {
		return ""
	}
	return NormalizeEndpointAddress(ip)
}

// ExpandPortRange expands a port range into individual ports.
// "8080-8082" -> ["8080", "8081", "8082"]
func ExpandPortRange(portRange string) ([]string, error) {
	if !strings.Contains(portRange, "-") {
		return []string{portRange}, nil
	}

	parts := strings.Split(portRange, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid port range: %s", portRange)
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 || start > 65535 {
		return nil, fmt.Errorf("invalid start port: %s", parts[0])
	}

	end, err := strconv.Atoi(parts[1])
	if err != nil || end < 1 || end > 65535 {
		return nil, fmt.Errorf("invalid end port: %s", parts[1])
	}

	if start > end {
		return nil, fmt.Errorf("start port %d is greater than end port %d", start, end)
	}

	result := make([]string, 0, end-start+1)
	for p := start; p <= end; p++ {
		result = append(result, strconv.Itoa(p))
	}
	return result, nil
}
