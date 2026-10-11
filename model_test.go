// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cB-Sigma/miio"
)

// testSpec is a made-up specification: two services named "switch",
// enumerated, ranged and float properties, an action with an input.
func testSpec() *miio.Spec {
	u := func(kind, name string) string {
		return "urn:miot-spec-v2:" + kind + ":" + name + ":00000001:test-model:1"
	}
	return &miio.Spec{Type: u("device", "outlet"), Services: []miio.Service{
		{IID: 2, Type: u("service", "switch"), Properties: []miio.Property{
			{IID: 1, Type: u("property", "on"), Format: "bool", Access: []string{"read", "write"}},
			{IID: 2, Type: u("property", "mode"), Format: "uint8", Access: []string{"read", "write"},
				ValueList: []miio.ValueItem{{Value: 0, Description: "Auto"}, {Value: 1, Description: "Sleep Mode"}}},
		}, Actions: []miio.Action{{IID: 1, Type: u("action", "toggle")}}},
		{IID: 3, Type: u("service", "switch"), Properties: []miio.Property{
			{IID: 1, Type: u("property", "on"), Format: "bool", Access: []string{"read", "write"}},
		}},
		{IID: 4, Type: u("service", "light"), Properties: []miio.Property{
			{IID: 1, Type: u("property", "brightness"), Format: "uint8", Access: []string{"read", "write"}, ValueRange: []float64{1, 100, 1}},
			{IID: 2, Type: u("property", "temperature"), Format: "float", Access: []string{"read"}, ValueRange: []float64{-40, 125, 0.5}},
			{IID: 3, Type: u("property", "level"), Format: "int16", Access: []string{"write"}, ValueRange: []float64{0, 50, 5}},
		}, Actions: []miio.Action{{IID: 1, Type: u("action", "set-level"), In: []int{3}}}},
	}}
}

// Covers: SIG-IMP-185
func TestModelNames(t *testing.T) {
	m, err := miio.NewModel(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][2]int{
		"switch.on": {2, 1}, "switch-3.on": {3, 1}, "mode": {2, 2}, "light.brightness": {4, 1}, "4.2": {4, 2}, "brightness": {4, 1},
	} {
		p, err := m.Property(name)
		if err != nil || p.SIID != want[0] || p.IID != want[1] {
			t.Errorf("%s: %+v %v", name, p, err)
		}
	}
	if _, err := m.Property("on"); !errors.Is(err, miio.ErrUnknownName) {
		t.Errorf("a short name shared by two services: %v", err)
	}
	if _, err := m.Property("nothing"); !errors.Is(err, miio.ErrUnknownName) {
		t.Errorf("unknown: %v", err)
	}
	if a, err := m.Action("set-level"); err != nil || a.SIID != 4 || a.IID != 1 || a.Name != "light.set-level" {
		t.Errorf("action %+v %v", a, err)
	}
	if len(m.Properties()) != 6 || len(m.Actions()) != 2 || !m.Has("switch.toggle") || m.Has("switch.nothing") {
		t.Errorf("lists %d %d", len(m.Properties()), len(m.Actions()))
	}
	if _, err := miio.NewModel(&miio.Spec{}); !errors.Is(err, miio.ErrNoSpec) {
		t.Errorf("empty spec: %v", err)
	}
}

// Covers: SIG-IMP-185
func TestPropertyCheck(t *testing.T) {
	m, _ := miio.NewModel(testSpec())
	p := func(n string) miio.MappedProperty { x, _ := m.Property(n); return x }
	ok := []struct {
		prop string
		in   any
		out  any
	}{
		{"switch.on", true, true}, {"switch.on", "off", false}, {"switch.on", 1, true},
		{"mode", 1, int64(1)}, {"mode", "sleep-mode", int64(1)}, {"mode", json.Number("0"), int64(0)},
		{"brightness", 100.0, int64(100)}, {"brightness", uint8(1), int64(1)},
		{"temperature", 21.5, 21.5}, {"level", 45, int64(45)},
	}
	for _, c := range ok {
		got, err := p(c.prop).Check(c.in)
		if err != nil || got != c.out {
			t.Errorf("%s %v: %v %v", c.prop, c.in, got, err)
		}
	}
	bad := []struct {
		prop string
		in   any
	}{
		{"switch.on", "maybe"}, {"mode", 2}, {"mode", "turbo"}, {"brightness", 0}, {"brightness", 101},
		{"brightness", 2.5}, {"brightness", 300}, {"temperature", 21.2}, {"level", 7}, {"level", "x"},
	}
	for _, c := range bad {
		if _, err := p(c.prop).Check(c.in); !errors.Is(err, miio.ErrValue) {
			t.Errorf("%s %v accepted: %v", c.prop, c.in, err)
		}
	}
	if d := p("mode").Describe(1.0); d != "Sleep Mode" {
		t.Errorf("describe %q", d)
	}
	if v, ok := p("mode").ValueOf("AUTO"); !ok || v != 0 {
		t.Errorf("value of %v %v", v, ok)
	}
}
