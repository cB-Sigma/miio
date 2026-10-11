// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Device control through the Mi account, for devices the caller cannot
// reach on the LAN, and for Bluetooth devices whose readings only reach
// the account (through a gateway or the phone app). The same encrypted
// API as the device list:
//
//   - miotspec/prop/get {"params": [{did, siid, piid}...]} answers a list
//     like the local get_properties;
//   - miotspec/prop/set {"params": [{did, siid, piid, value}...]};
//   - miotspec/action {"params": {did, siid, aiid, in}};
//   - home/rpc/<did> {"method": m, "params": p} relays a legacy miIO
//     method to the device and answers its result.
//
// From public descriptions of open-source Mi account clients
// (PROVENANCE.md); tested against miio-sim's account only.

func (c *Cloud) api(ctx context.Context, region, path string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.serviceToken == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	out, err := c.call(ctx, region, path, string(data))
	if errors.Is(err, ErrCloudAuth) {
		if err = c.login(ctx); err == nil {
			out, err = c.call(ctx, region, path, string(data))
		}
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out["result"])
}

// CloudGetProperties reads MIoT properties through the account.
func (c *Cloud) CloudGetProperties(ctx context.Context, region string, refs []PropRef) ([]PropResult, error) {
	params := make([]map[string]any, len(refs))
	for i, r := range refs {
		params[i] = map[string]any{"did": r.DID, "siid": r.SIID, "piid": r.PIID}
	}
	raw, err := c.api(ctx, region, "/miotspec/prop/get", map[string]any{"params": params})
	if err != nil {
		return nil, err
	}
	var res []PropResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("the Mi account answered prop/get with %s", raw)
	}
	return res, nil
}

// CloudSetProperties writes MIoT properties through the account and
// returns a *PropError for the first one refused.
func (c *Cloud) CloudSetProperties(ctx context.Context, region string, refs []PropRef) ([]PropResult, error) {
	params := make([]map[string]any, len(refs))
	for i, r := range refs {
		params[i] = map[string]any{"did": r.DID, "siid": r.SIID, "piid": r.PIID, "value": r.Value}
	}
	raw, err := c.api(ctx, region, "/miotspec/prop/set", map[string]any{"params": params})
	if err != nil {
		return nil, err
	}
	return CheckSet(raw)
}

// CloudDoAction runs a MIoT action through the account.
func (c *Cloud) CloudDoAction(ctx context.Context, region, did string, siid, aiid int, in []any) error {
	if in == nil {
		in = []any{}
	}
	raw, err := c.api(ctx, region, "/miotspec/action", map[string]any{"params": map[string]any{"did": did, "siid": siid, "aiid": aiid, "in": in}})
	if err != nil {
		return err
	}
	return CheckAction(raw)
}

// CloudCall relays a legacy miIO method to a device through the account
// and returns the device's result.
func (c *Cloud) CloudCall(ctx context.Context, region, did, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	return c.api(ctx, region, "/home/rpc/"+did, map[string]any{"method": method, "params": params})
}
