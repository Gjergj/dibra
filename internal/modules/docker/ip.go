package docker

import (
	"net/netip"
	"strings"
)

// NormalizeIPAddress returns the canonical compressed spelling of a valid IP
// address. Invalid values are returned unchanged so callers can normalize
// comparison data without turning validation into a side effect.
func NormalizeIPAddress(value string) string {
	address, err := netip.ParseAddr(value)
	if err != nil {
		return value
	}
	return address.String()
}

// NormalizeIPNetwork returns the canonical compressed spelling of a strict
// CIDR network. Values with host bits set, or otherwise invalid values, are
// returned unchanged to match community.docker comparison behavior.
func NormalizeIPNetwork(value string) string {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix != prefix.Masked() {
		return value
	}
	return prefix.String()
}

// NormalizeEndpointAddress canonicalizes an endpoint address and ignores an
// optional CIDR suffix reported by Docker inspection APIs.
func NormalizeEndpointAddress(value string) string {
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String()
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return strings.TrimSpace(value)
}
