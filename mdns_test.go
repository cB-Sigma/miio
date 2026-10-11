// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/cB-Sigma/miio"
)

func TestParseMDNSInstance(t *testing.T) {
	for name, want := range map[string]struct {
		model string
		did   uint32
		ok    bool
	}{
		"zhimi-airpurifier-m1_miio57421231":                {"zhimi.airpurifier.m1", 57421231, true},
		"yeelink-light-color1_miio12345._miio._udp.local.": {"yeelink.light.color1", 12345, true},
		"roborock-vacuum-s5_miio":                          {"roborock.vacuum.s5", 0, true},
		"chuangmi-plug-m3_miio99999999999":                 {"chuangmi.plug.m3", 0, true},
		"Living room speaker":                              {"", 0, false},
		"_miio1":                                           {"", 0, false},
	} {
		m, d, ok := miio.ParseMDNSInstance(name)
		if m != want.model || d != want.did || ok != want.ok {
			t.Errorf("%q: %q %d %v", name, m, d, ok)
		}
	}
}

func TestDiscoverMDNS(t *testing.T) {
	var asked string
	b := miio.BrowseFunc(func(ctx context.Context, service string) ([]miio.MDNSEntry, error) {
		asked = service
		return []miio.MDNSEntry{
			{Instance: "zhimi-airpurifier-ma4_miio171717", IPs: []net.IP{net.ParseIP("fe80::1"), net.ParseIP("192.168.1.20")}},
			{Instance: "zhimi-airpurifier-ma4_miio171717", IPs: []net.IP{net.ParseIP("192.168.1.20")}},
			{Instance: "printer", IPs: []net.IP{net.ParseIP("192.168.1.30")}},
			{Instance: "no-address_miio1"},
			{Instance: "yeelink-light-mono1_miio42", IPs: []net.IP{net.ParseIP("192.168.1.21")}, Port: 6000},
		}, errors.New("one interface failed")
	})
	got, err := miio.DiscoverMDNS(context.Background(), b)
	if asked != "_miio._udp" || err == nil {
		t.Errorf("service %q, error %v", asked, err)
	}
	if len(got) != 2 || got[0].Model != "zhimi.airpurifier.ma4" || got[0].DID != 171717 || got[0].Addr != "192.168.1.20:54321" ||
		got[1].Addr != "192.168.1.21:6000" || got[1].Model != "yeelink.light.mono1" {
		t.Errorf("devices %+v", got)
	}
}
