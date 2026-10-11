// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"fmt"
)

// Zigbee sub-devices of Xiaomi (Aqara, "lumi") gateways are reached through
// the gateway's own miIO methods, each naming the sub-device by its sid
// ("lumi.158d0001..."; the gateway itself is "lumi.0"):
//
//   - get_device_prop ["lumi.0", "device_list"] lists the sub-devices of
//     the older gateways (lumi.gateway.v3, the AC partners) as a flat
//     list, five values per device: sid, type number, two unknown values,
//     firmware version;
//   - get_device_list lists those of the multi-mode gateway
//     (lumi.gateway.mgl03) as objects with did and model;
//   - get_device_prop [sid, name] reads one property, get_device_prop_exp
//     [[sid, name...]] several, set_device_prop {"sid": sid, name: value}
//     writes;
//   - get_battery [sid] gives the battery level, remove_device [sid]
//     unpairs.
//
// The lumi.gateway.mieu01 gateway answers none of the lists; its
// sub-devices come from the Mi account (CloudDevice entries whose parent
// is the gateway: GatewaySubDevices).

// SubDevice is a sub-device of a gateway.
type SubDevice struct {
	SID      string // "lumi.158d..."
	TypeID   int    // the gateway's type number; -1 when listed by model
	Model    string // zigbee model ("lumi.sensor_ht.v1"), from the list or from TypeID
	Firmware int    // -1 when not listed
}

// subTypes are type numbers of common sub-devices, as the older gateways
// list them (facts gathered from public client projects; PROVENANCE.md).
var subTypes = map[int]string{
	1:  "lumi.sensor_switch.v2",
	2:  "lumi.sensor_motion.v2",
	3:  "lumi.sensor_magnet.v2",
	10: "lumi.sensor_ht.v1",
	19: "lumi.weather.v1",
	52: "lumi.sensor_motion.aq2",
	53: "lumi.sensor_magnet.aq2",
}

// SubDeviceModel names the zigbee model of a type number ("" when not
// known).
func SubDeviceModel(typeID int) string { return subTypes[typeID] }

// Gateway reaches a gateway's sub-devices.
type Gateway struct {
	Client *Client
	// Model is the gateway's model; "lumi.gateway.mgl03" lists with
	// get_device_list, the others with the device_list property.
	Model string
}

// SubDevices lists the gateway's sub-devices, without the gateway itself.
func (g *Gateway) SubDevices(ctx context.Context) ([]SubDevice, error) {
	if g.Model == "lumi.gateway.mgl03" {
		raw, err := g.Client.Call(ctx, "get_device_list", nil)
		if err != nil {
			return nil, err
		}
		var list []struct {
			DID   string `json:"did"`
			Model string `json:"model"`
		}
		if json.Unmarshal(raw, &list) != nil {
			return nil, nil // not a list: no sub-devices
		}
		out := make([]SubDevice, 0, len(list))
		for _, d := range list {
			if d.DID == "lumi.0" {
				continue
			}
			out = append(out, SubDevice{SID: d.DID, TypeID: -1, Model: d.Model, Firmware: -1})
		}
		return out, nil
	}
	raw, err := g.Client.Call(ctx, "get_device_prop", []any{"lumi.0", "device_list"})
	if err != nil {
		return nil, err
	}
	var flat []any
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("the gateway answered device_list with %s", raw)
	}
	var out []SubDevice
	for i := 0; i+5 <= len(flat); i += 5 {
		sid, _ := flat[i].(string)
		t := int(asFloat(flat[i+1]))
		if sid == "" || sid == "lumi.0" || t == 0 {
			continue
		}
		out = append(out, SubDevice{SID: sid, TypeID: t, Model: subTypes[t], Firmware: int(asFloat(flat[i+4]))})
	}
	return out, nil
}

// SubProps reads properties of a sub-device ("temperature", "humidity",
// "voltage"...) in one get_device_prop_exp request, in order.
func (g *Gateway) SubProps(ctx context.Context, sid string, names ...string) ([]any, error) {
	arg := []any{sid}
	for _, n := range names {
		arg = append(arg, n)
	}
	raw, err := g.Client.Call(ctx, "get_device_prop_exp", []any{arg})
	if err != nil {
		return nil, err
	}
	var outer [][]any
	if err := json.Unmarshal(raw, &outer); err != nil || len(outer) == 0 {
		return nil, fmt.Errorf("the gateway answered get_device_prop_exp with %s", raw)
	}
	vals := outer[len(outer)-1]
	if len(vals) != len(names) {
		return nil, fmt.Errorf("the gateway gave %d values for %d properties", len(vals), len(names))
	}
	return vals, nil
}

// SubProp reads one property of a sub-device with get_device_prop.
func (g *Gateway) SubProp(ctx context.Context, sid, name string) (any, error) {
	raw, err := g.Client.Call(ctx, "get_device_prop", []any{sid, name})
	if err != nil {
		return nil, err
	}
	var vals []any
	if err := json.Unmarshal(raw, &vals); err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("the gateway answered get_device_prop with %s", raw)
	}
	return vals[0], nil
}

// SetSubProp writes a property of a sub-device.
func (g *Gateway) SetSubProp(ctx context.Context, sid, name string, v any) error {
	return okAnswer(g.Client.Call(ctx, "set_device_prop", map[string]any{"sid": sid, name: v}))
}

// SubBattery reads a battery-powered sub-device's battery level, per cent
// (older gateways; the multi-mode gateway reports "voltage" instead).
func (g *Gateway) SubBattery(ctx context.Context, sid string) (int, error) {
	raw, err := g.Client.Call(ctx, "get_battery", []any{sid})
	if err != nil {
		return 0, err
	}
	var vals []any
	if err := json.Unmarshal(raw, &vals); err != nil || len(vals) == 0 {
		return 0, fmt.Errorf("the gateway answered get_battery with %s", raw)
	}
	return int(asFloat(vals[len(vals)-1])), nil
}

// RemoveSubDevice unpairs a sub-device from the gateway.
func (g *Gateway) RemoveSubDevice(ctx context.Context, sid string) error {
	return okAnswer(g.Client.Call(ctx, "remove_device", []any{sid}))
}

// GatewaySubDevices picks from an account's device list the sub-devices
// whose parent is the gateway (for gateways that do not list them
// locally).
func GatewaySubDevices(devs []CloudDevice, gatewayDID string) []CloudDevice {
	var out []CloudDevice
	for _, d := range devs {
		if d.ParentID == gatewayDID && d.DID != gatewayDID {
			out = append(out, d)
		}
	}
	return out
}

// okAnswer accepts ["ok"], "ok" and 0 (devices answer each).
func okAnswer(raw json.RawMessage, err error) error {
	if err != nil {
		return err
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return fmt.Errorf("the device answered %s", raw)
	}
	if l, ok := v.([]any); ok && len(l) > 0 {
		v = l[0]
	}
	if v == "ok" || v == 0.0 {
		return nil
	}
	return fmt.Errorf("the device answered %s", raw)
}
