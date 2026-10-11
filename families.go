// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Helpers for the commonest device families: vacuums, lights, plugs and
// air purifiers. Each works in one of two ways:
//
//   - with a Model (MIoT): by the standard names of the family's services
//     and properties ("vacuum.start-sweep", "light.brightness",
//     "air-purifier.mode"), so one helper serves every model whose
//     specification uses those names;
//   - without a Model: by the legacy per-model methods (Roborock app_*
//     for vacuums; Yeelight and Philips methods for lights; chuangmi plug
//     methods; zhimi purifier methods), chosen from LegacyModel.
//
// A function the model lacks answers ErrUnsupported.

// ErrUnsupported means the device model has no such function.
var ErrUnsupported = errors.New("the device model has no such function")

// Family is what the helpers share: the client, the device ID used in
// MIoT requests, and either a Model or, for legacy devices, the model
// name.
type Family struct {
	Client      *Client
	DID         string
	Model       *Model // nil: legacy methods
	LegacyModel string // such as "yeelink.light.color1"; used when Model is nil
}

// prop returns the first of the names the model has as a property.
func (f *Family) prop(names ...string) (string, bool) {
	for _, n := range names {
		if _, err := f.Model.Property(n); err == nil {
			return n, true
		}
	}
	return "", false
}

func (f *Family) action(names ...string) (string, bool) {
	for _, n := range names {
		if _, err := f.Model.Action(n); err == nil {
			return n, true
		}
	}
	return "", false
}

func (f *Family) set(ctx context.Context, v any, names ...string) error {
	n, ok := f.prop(names...)
	if !ok {
		return ErrUnsupported
	}
	return f.Model.Set(ctx, f.Client, f.DID, n, v)
}

func (f *Family) do(ctx context.Context, names ...string) error {
	n, ok := f.action(names...)
	if !ok {
		return ErrUnsupported
	}
	return f.Model.Do(ctx, f.Client, f.DID, n)
}

// read gets the first existing name of each group; the result is keyed by
// the group's first name.
func (f *Family) read(ctx context.Context, groups ...[]string) (map[string]any, error) {
	var names, keys []string
	for _, g := range groups {
		if n, ok := f.prop(g...); ok {
			names = append(names, n)
			keys = append(keys, g[0])
		}
	}
	if len(names) == 0 {
		return nil, ErrUnsupported
	}
	got, err := f.Model.Get(ctx, f.Client, f.DID, names...)
	if got == nil {
		return nil, err
	}
	out := map[string]any{}
	for i, n := range names {
		if v, ok := got[n]; ok {
			out[keys[i]] = v
		}
	}
	return out, err
}

func (f *Family) legacy(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	return f.Client.Call(ctx, method, params)
}

// getProp reads legacy properties into a map.
func (f *Family) getProp(ctx context.Context, keys ...string) (map[string]any, error) {
	ps := make([]any, len(keys))
	for i, k := range keys {
		ps[i] = k
	}
	raw, err := f.legacy(ctx, "get_prop", ps...)
	if err != nil {
		return nil, err
	}
	var vals []any
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil, fmt.Errorf("the device answered get_prop with %s", raw)
	}
	out := map[string]any{}
	for i, k := range keys {
		if i < len(vals) {
			out[k] = vals[i]
		}
	}
	return out, nil
}

func asFloat(v any) float64 { f, _ := number(v); return f }

func asOn(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "on"
	}
	return asFloat(v) == 1
}

// Vacuum is a robot vacuum.
type Vacuum struct{ Family }

// VacuumStatus is a vacuum's state.
type VacuumStatus struct {
	// Code is the state number: MIoT's vacuum status value, or the legacy
	// Roborock state code.
	Code int
	// State names it: the specification's description (MIoT) or our own
	// name for the common Roborock codes; "" when unknown.
	State   string
	Battery int // per cent, -1 when not reported
}

// Start starts cleaning.
func (v *Vacuum) Start(ctx context.Context) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "app_start")
		return err
	}
	return v.do(ctx, "vacuum.start-sweep", "vacuum.start-clean", "start-sweep", "start-clean")
}

// Stop stops cleaning.
func (v *Vacuum) Stop(ctx context.Context) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "app_stop")
		return err
	}
	return v.do(ctx, "vacuum.stop-sweeping", "vacuum.stop-clean", "stop-sweeping", "stop-clean")
}

// Pause pauses cleaning.
func (v *Vacuum) Pause(ctx context.Context) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "app_pause")
		return err
	}
	return v.do(ctx, "vacuum.pause-sweeping", "vacuum.pause", "pause-sweeping", "pause")
}

// Home sends the vacuum back to its dock.
func (v *Vacuum) Home(ctx context.Context) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "app_charge")
		return err
	}
	return v.do(ctx, "battery.start-charge", "vacuum.start-charge", "start-charge")
}

// roborockStates names the common legacy Roborock state codes (our own
// words).
var roborockStates = map[int]string{2: "charger disconnected", 3: "idle", 5: "cleaning", 6: "returning home",
	8: "charging", 10: "paused", 11: "spot cleaning", 12: "error", 15: "docking", 17: "zoned cleaning",
	18: "segment cleaning", 100: "charged"}

// Status reads the state and battery.
func (v *Vacuum) Status(ctx context.Context) (VacuumStatus, error) {
	st := VacuumStatus{Battery: -1}
	if v.Model == nil {
		raw, err := v.legacy(ctx, "get_status")
		if err != nil {
			return st, err
		}
		var list []map[string]any
		if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
			return st, fmt.Errorf("the vacuum answered get_status with %s", raw)
		}
		st.Code = int(asFloat(list[0]["state"]))
		st.State = roborockStates[st.Code]
		if b, ok := list[0]["battery"]; ok {
			st.Battery = int(asFloat(b))
		}
		return st, nil
	}
	got, err := v.read(ctx, []string{"vacuum.status", "status"}, []string{"battery.battery-level", "battery-level"})
	if got == nil {
		return st, err
	}
	if s, ok := got["vacuum.status"]; ok {
		st.Code = int(asFloat(s))
		if p, e := v.Model.Property("vacuum.status"); e == nil {
			st.State = p.Describe(s)
		} else if p, e := v.Model.Property("status"); e == nil {
			st.State = p.Describe(s)
		}
	}
	if b, ok := got["battery.battery-level"]; ok {
		st.Battery = int(asFloat(b))
	}
	return st, err
}

// Light is a lamp or bulb.
type Light struct{ Family }

// LightStatus is a light's state; fields the model lacks stay 0.
type LightStatus struct {
	On               bool
	Brightness       int // per cent
	ColorTemperature int // kelvin (MIoT and Yeelight) or the Philips 1-100 scale
	Color            int // 0xRRGGBB
}

func (l *Light) dialect() string {
	switch {
	case strings.HasPrefix(l.LegacyModel, "yeelink."):
		return "yeelight"
	case strings.HasPrefix(l.LegacyModel, "philips."):
		return "philips"
	}
	return ""
}

func (l *Light) power(ctx context.Context, on bool) error {
	if l.Model == nil {
		if l.dialect() == "" {
			return ErrUnsupported
		}
		s := "off"
		if on {
			s = "on"
		}
		_, err := l.legacy(ctx, "set_power", s)
		return err
	}
	return l.set(ctx, on, "light.on", "on")
}

// On switches the light on.
func (l *Light) On(ctx context.Context) error { return l.power(ctx, true) }

// Off switches the light off.
func (l *Light) Off(ctx context.Context) error { return l.power(ctx, false) }

// SetBrightness sets the brightness in per cent.
func (l *Light) SetBrightness(ctx context.Context, pct int) error {
	if l.Model == nil {
		if l.dialect() == "" {
			return ErrUnsupported
		}
		_, err := l.legacy(ctx, "set_bright", pct)
		return err
	}
	return l.set(ctx, pct, "light.brightness", "brightness")
}

// SetColorTemperature sets the colour temperature in kelvin (MIoT and
// Yeelight). Philips legacy lights take a 1-100 scale instead: use
// SetColorTemperatureScale.
func (l *Light) SetColorTemperature(ctx context.Context, kelvin int) error {
	if l.Model == nil {
		if l.dialect() != "yeelight" {
			return ErrUnsupported
		}
		_, err := l.legacy(ctx, "set_ct_abx", kelvin, "smooth", 500)
		return err
	}
	return l.set(ctx, kelvin, "light.color-temperature", "color-temperature")
}

// SetColorTemperatureScale sets a Philips legacy light's colour
// temperature on its 1-100 scale.
func (l *Light) SetColorTemperatureScale(ctx context.Context, v int) error {
	if l.Model != nil || l.dialect() != "philips" {
		return ErrUnsupported
	}
	_, err := l.legacy(ctx, "set_cct", v)
	return err
}

// SetColor sets the colour as 0xRRGGBB.
func (l *Light) SetColor(ctx context.Context, rgb int) error {
	if l.Model == nil {
		if l.dialect() != "yeelight" {
			return ErrUnsupported
		}
		_, err := l.legacy(ctx, "set_rgb", rgb)
		return err
	}
	return l.set(ctx, rgb, "light.color", "color")
}

// Status reads the light's state.
func (l *Light) Status(ctx context.Context) (LightStatus, error) {
	var st LightStatus
	if l.Model == nil {
		var got map[string]any
		var err error
		switch l.dialect() {
		case "yeelight":
			got, err = l.getProp(ctx, "power", "bright", "ct", "rgb")
		case "philips":
			got, err = l.getProp(ctx, "power", "bright", "cct")
		default:
			return st, ErrUnsupported
		}
		if err != nil {
			return st, err
		}
		st.On = asOn(got["power"])
		st.Brightness = int(asFloat(got["bright"]))
		st.ColorTemperature = int(asFloat(got["ct"]) + asFloat(got["cct"]))
		st.Color = int(asFloat(got["rgb"]))
		return st, nil
	}
	got, err := l.read(ctx, []string{"light.on", "on"}, []string{"light.brightness", "brightness"},
		[]string{"light.color-temperature", "color-temperature"}, []string{"light.color", "color"})
	if got == nil {
		return st, err
	}
	st.On = asOn(got["light.on"])
	st.Brightness = int(asFloat(got["light.brightness"]))
	st.ColorTemperature = int(asFloat(got["light.color-temperature"]))
	st.Color = int(asFloat(got["light.color"]))
	return st, err
}

// Plug is a smart plug or outlet.
type Plug struct{ Family }

// PlugStatus is a plug's state.
type PlugStatus struct {
	On          bool
	Temperature float64 // °C, 0 when not reported
}

func (p *Plug) onKey() bool {
	return p.LegacyModel == "chuangmi.plug.v1" || p.LegacyModel == "chuangmi.plug.v3"
}

func (p *Plug) power(ctx context.Context, on bool) error {
	if p.Model == nil {
		var err error
		switch {
		case p.LegacyModel == "chuangmi.plug.v1" && on:
			_, err = p.legacy(ctx, "set_on")
		case p.LegacyModel == "chuangmi.plug.v1":
			_, err = p.legacy(ctx, "set_off")
		case on:
			_, err = p.legacy(ctx, "set_power", "on")
		default:
			_, err = p.legacy(ctx, "set_power", "off")
		}
		return err
	}
	return p.set(ctx, on, "switch.on", "on")
}

// On switches the plug on.
func (p *Plug) On(ctx context.Context) error { return p.power(ctx, true) }

// Off switches the plug off.
func (p *Plug) Off(ctx context.Context) error { return p.power(ctx, false) }

// Status reads the plug's state.
func (p *Plug) Status(ctx context.Context) (PlugStatus, error) {
	var st PlugStatus
	if p.Model == nil {
		key := "power"
		if p.onKey() {
			key = "on"
		}
		got, err := p.getProp(ctx, key, "temperature")
		if err != nil {
			return st, err
		}
		st.On, st.Temperature = asOn(got[key]), asFloat(got["temperature"])
		return st, nil
	}
	got, err := p.read(ctx, []string{"switch.on", "on"}, []string{"temperature", "switch.temperature"})
	if got == nil {
		return st, err
	}
	st.On, st.Temperature = asOn(got["switch.on"]), asFloat(got["temperature"])
	return st, err
}

// Purifier is an air purifier.
type Purifier struct{ Family }

// PurifierStatus is a purifier's state; fields the model lacks stay 0.
type PurifierStatus struct {
	On          bool
	Mode        string // the mode's description (MIoT) or name (legacy)
	FanLevel    int
	PM25        float64 // µg/m³ (the legacy "aqi" reading)
	Humidity    float64 // per cent
	Temperature float64 // °C
}

func (p *Purifier) power(ctx context.Context, on bool) error {
	if p.Model == nil {
		s := "off"
		if on {
			s = "on"
		}
		_, err := p.legacy(ctx, "set_power", s)
		return err
	}
	return p.set(ctx, on, "air-purifier.on", "on")
}

// On switches the purifier on.
func (p *Purifier) On(ctx context.Context) error { return p.power(ctx, true) }

// Off switches the purifier off.
func (p *Purifier) Off(ctx context.Context) error { return p.power(ctx, false) }

// SetMode sets the mode by name: a description from the specification's
// value list for MIoT ("Auto", "Sleep", "Favorite"), the legacy method's
// word otherwise ("auto", "silent", "favorite").
func (p *Purifier) SetMode(ctx context.Context, mode string) error {
	if p.Model == nil {
		_, err := p.legacy(ctx, "set_mode", strings.ToLower(mode))
		return err
	}
	return p.set(ctx, mode, "air-purifier.mode", "mode")
}

// SetFanLevel sets the fan level (MIoT only).
func (p *Purifier) SetFanLevel(ctx context.Context, level int) error {
	if p.Model == nil {
		return ErrUnsupported
	}
	return p.set(ctx, level, "air-purifier.fan-level", "fan-level")
}

// Status reads the purifier's state.
func (p *Purifier) Status(ctx context.Context) (PurifierStatus, error) {
	var st PurifierStatus
	if p.Model == nil {
		got, err := p.getProp(ctx, "power", "mode", "aqi", "humidity", "temp_dec")
		if err != nil {
			return st, err
		}
		st.On = asOn(got["power"])
		st.Mode, _ = got["mode"].(string)
		st.PM25, st.Humidity, st.Temperature = asFloat(got["aqi"]), asFloat(got["humidity"]), asFloat(got["temp_dec"])/10
		return st, nil
	}
	got, err := p.read(ctx, []string{"air-purifier.on", "on"}, []string{"air-purifier.mode", "mode"},
		[]string{"air-purifier.fan-level", "fan-level"}, []string{"environment.pm2.5-density", "pm2.5-density"},
		[]string{"environment.relative-humidity", "relative-humidity"}, []string{"environment.temperature", "temperature"})
	if got == nil {
		return st, err
	}
	st.On = asOn(got["air-purifier.on"])
	if m, ok := got["air-purifier.mode"]; ok {
		n, _ := p.prop("air-purifier.mode", "mode")
		mp, _ := p.Model.Property(n)
		st.Mode = mp.Describe(m)
	}
	st.FanLevel = int(asFloat(got["air-purifier.fan-level"]))
	st.PM25 = asFloat(got["environment.pm2.5-density"])
	st.Humidity = asFloat(got["environment.relative-humidity"])
	st.Temperature = asFloat(got["environment.temperature"])
	return st, err
}
