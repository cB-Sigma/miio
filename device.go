// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Requests every miIO device takes, whatever its family.

// ConfigureWiFi tells a device to join a Wi-Fi network (miIO.config_router
// with ssid, passwd and uid; extra adds further parameters some models
// take). It is sent to a device in setup mode, on its own access point
// (usually 192.168.13.1:54321); the device leaves that network at once,
// so a missing answer is not proof of failure.
func ConfigureWiFi(ctx context.Context, c *Client, ssid, password string, uid int64, extra map[string]any) error {
	p := map[string]any{"ssid": ssid, "passwd": password, "uid": uid}
	for k, v := range extra {
		p[k] = v
	}
	_, err := c.Call(ctx, "miIO.config_router", p)
	return err
}

// SupportsMIoT tells whether the device answers MIoT get_properties (it
// asks for one property, service 1 property 1). A device that refuses the
// method speaks only the legacy methods; err is set only when the device
// could not be asked.
func SupportsMIoT(ctx context.Context, c *Client) (bool, error) {
	_, err := c.Call(ctx, "get_properties", []PropRef{{DID: "probe", SIID: 1, PIID: 1}})
	var de *DeviceError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &de):
		return false, nil
	}
	return false, err
}

// GetProp reads legacy properties with get_prop, at most max at a time
// (devices that cannot take a long list need a few at a time; max < 1
// asks for all at once). The values come in the order of keys.
func GetProp(ctx context.Context, c *Client, keys []string, max int) ([]any, error) {
	if max < 1 {
		max = len(keys)
	}
	out := make([]any, 0, len(keys))
	for len(keys) > 0 {
		n := min(max, len(keys))
		ps := make([]any, n)
		for i, k := range keys[:n] {
			ps[i] = k
		}
		raw, err := c.Call(ctx, "get_prop", ps)
		if err != nil {
			return out, err
		}
		var vals []any
		if err := json.Unmarshal(raw, &vals); err != nil || len(vals) != n {
			return out, fmt.Errorf("the device answered get_prop for %d keys with %s", n, raw)
		}
		out = append(out, vals...)
		keys = keys[n:]
	}
	return out, nil
}
