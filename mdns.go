// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"net"
	"strconv"
	"strings"
)

// Many miIO devices also announce themselves by mDNS as service
// "_miio._udp", with an instance name of the model (dots written as
// dashes), "_miio" and the device ID in decimal:
// "zhimi-airpurifier-m1_miio12345678". This module has no mDNS code of
// its own: the application passes the browser it already uses (any
// mDNS library), and DiscoverMDNS turns the answers into devices.

// MDNSService is the miIO mDNS service type.
const MDNSService = "_miio._udp"

// MDNSEntry is one service instance a browser found.
type MDNSEntry struct {
	Instance string // the instance name, with or without the service and domain after it
	IPs      []net.IP
	Port     int // 0: the miIO port, 54321
}

// Browser browses mDNS for a service type ("_miio._udp"), returning the
// instances found before ctx ends or its own wait is over.
type Browser interface {
	Browse(ctx context.Context, service string) ([]MDNSEntry, error)
}

// BrowseFunc makes a function a Browser.
type BrowseFunc func(ctx context.Context, service string) ([]MDNSEntry, error)

// Browse implements Browser.
func (f BrowseFunc) Browse(ctx context.Context, service string) ([]MDNSEntry, error) {
	return f(ctx, service)
}

// MDNSDevice is a device found by mDNS.
type MDNSDevice struct {
	Model    string // such as "zhimi.airpurifier.m1"
	DID      uint32 // 0 when the name does not carry it
	Addr     string // host:port for Client.Addr
	Instance string
}

// ParseMDNSInstance reads the model and device ID from a miIO instance
// name ("yeelink-light-color1_miio12345678", optionally followed by
// "._miio._udp.local."). ok is false for a name without "_miio"; did is 0
// when no number follows it.
func ParseMDNSInstance(name string) (model string, did uint32, ok bool) {
	if i := strings.Index(name, "."+MDNSService); i >= 0 {
		name = name[:i]
	}
	i := strings.LastIndex(name, "_miio")
	if i <= 0 {
		return "", 0, false
	}
	if n, err := strconv.ParseUint(name[i+len("_miio"):], 10, 32); err == nil {
		did = uint32(n)
	}
	return strings.ReplaceAll(name[:i], "-", "."), did, true
}

// DiscoverMDNS browses for "_miio._udp" with the application's browser
// and returns the miIO devices among the answers, one per instance, with
// an IPv4 address preferred. A device found this way still needs its
// token; Client's first call does the hello.
func DiscoverMDNS(ctx context.Context, b Browser) ([]MDNSDevice, error) {
	entries, err := b.Browse(ctx, MDNSService)
	var out []MDNSDevice
	seen := map[string]bool{}
	for _, e := range entries {
		model, did, ok := ParseMDNSInstance(e.Instance)
		if !ok || seen[e.Instance] || len(e.IPs) == 0 {
			continue
		}
		ip := e.IPs[0]
		for _, x := range e.IPs {
			if x.To4() != nil {
				ip = x
				break
			}
		}
		port := e.Port
		if port == 0 {
			port = 54321
		}
		seen[e.Instance] = true
		out = append(out, MDNSDevice{Model: model, DID: did, Addr: net.JoinHostPort(ip.String(), strconv.Itoa(port)), Instance: e.Instance})
	}
	return out, err
}
