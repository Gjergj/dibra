package docker_network

import (
	"net/netip"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/network"
)

func TestCompareIPAMConfigNormalizesIPv6AndAuxAddresses(t *testing.T) {
	existing := network.IPAM{Config: []network.IPAMConfig{{
		Subnet:  netip.MustParsePrefix("2001:db8::/64"),
		Gateway: netip.MustParseAddr("2001:db8::1"),
		AuxAddress: map[string]netip.Addr{
			"router": netip.MustParseAddr("2001:db8::2"),
		},
	}}}
	requested := []IPAMConfig{{
		Subnet:     "2001:0db8:0000::/64",
		Gateway:    "2001:0db8::1",
		AuxAddress: map[string]string{"router": "2001:0db8:0:0::2"},
	}}
	if !compareIPAMConfig(requested, existing) {
		t.Fatal("compareIPAMConfig() = false for equivalent IPv6 spellings")
	}
}

func TestNeedsEndpointUpdateNormalizesCIDR(t *testing.T) {
	current := network.EndpointResource{IPv6Address: netip.MustParsePrefix("2001:db8::10/64")}
	desired := ConnectedContainer{IPv6Address: "2001:0db8:0000::10"}
	if needsEndpointUpdate(desired, current) {
		t.Fatal("needsEndpointUpdate() = true for equivalent IPv6 addresses")
	}
	if docker.NormalizeEndpointAddress(current.IPv6Address.String()) != "2001:db8::10" {
		t.Fatal("test fixture did not normalize as expected")
	}
}
