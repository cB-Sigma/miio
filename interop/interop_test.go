// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package interop checks the miio client against python-miio's own device
// simulators (miiocli devtools miio-simulator and miot-simulator), in a
// container. The tests need Docker and are skipped unless
// SIGMA_MIIO_INTEROP=1.
package interop

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

var ctx = context.Background()

// The simulators answer to the all-zero token.
var zeroToken = make([]byte, 16)

// didFor is the device ID python-miio's simulators derive from the model.
func didFor(model string) uint32 {
	s := md5.Sum([]byte(model))
	n, _ := strconv.ParseUint(hex.EncodeToString(s[:4]), 16, 32)
	return uint32(n)
}

// compact removes the spaces Python's JSON encoder puts in.
func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// waitInfo calls miIO.info until the simulator answers.
func waitInfo(t *testing.T, c *miio.Client) map[string]any {
	t.Helper()
	var lastErr error
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		raw, err := c.Call(ctx, "miIO.info", nil)
		if err == nil {
			var info map[string]any
			if err := json.Unmarshal(raw, &info); err != nil {
				t.Fatalf("miIO.info %s: %v", raw, err)
			}
			return info
		}
		lastErr = err
	}
	t.Fatalf("the simulator did not answer: %v", lastErr)
	return nil
}

// A legacy device (the zhimi.fan simulator file python-miio ships):
// hello, miIO.info, get_prop, a setter, a wrong token, discovery by hello.
func TestClientAgainstMiioSimulator(t *testing.T) {
	needDocker(t)
	const model = "zhimi.fan.sa1"
	port := freeUDPPort(t)
	startPeer(t, "-p", fmt.Sprintf("127.0.0.1:%d:54321/udp", port), image,
		"miiocli", "-d", "devtools", "miio-simulator", "--file", "/python-miio/miio/integrations/zhimi/fan/zhimi_fan.yaml", "--model", model)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	c := &miio.Client{Addr: addr, Token: zeroToken, Timeout: 3 * time.Second}
	info := waitInfo(t, c)
	if info["model"] != model || c.DeviceID() != didFor(model) {
		t.Errorf("miIO.info %v, device ID %d want %d", info, c.DeviceID(), didFor(model))
	}
	raw, err := c.Call(ctx, "get_prop", []any{"power", "angle", "speed_level"})
	if err != nil || compact(raw) != `["on",120,1]` {
		t.Errorf("get_prop %s %v", raw, err)
	}
	if raw, err := c.Call(ctx, "set_power", []any{"off"}); err != nil || compact(raw) != `["ok"]` {
		t.Errorf("set_power %s %v", raw, err)
	}
	if raw, err := c.Call(ctx, "get_prop", []any{"power"}); err != nil || compact(raw) != `["off"]` {
		t.Errorf("get_prop after set_power %s %v", raw, err)
	}
	if _, err := c.Call(ctx, "no_such_method", nil); err == nil {
		t.Error("an unknown method was accepted")
	}
	wrong := &miio.Client{Addr: addr, Token: []byte("0123456789abcdef"), Timeout: 2 * time.Second}
	if _, err := wrong.Call(ctx, "miIO.info", nil); err == nil {
		t.Error("a wrong token was answered")
	}
	found, err := miio.Discover(ctx, addr, time.Second)
	if err != nil || found[didFor(model)] == "" {
		t.Errorf("discovery %v %v", found, err)
	}
}

// A MIoT device (python-miio's MIoT simulator with our made-up
// specification peer/spec.json): get_properties, set_properties, an
// unknown property, a read-only property, an action.
func TestClientAgainstMiotSimulator(t *testing.T) {
	needDocker(t)
	const model = "sigma.airpurifier.t1"
	port := freeUDPPort(t)
	startPeer(t, "-p", fmt.Sprintf("127.0.0.1:%d:54321/udp", port), image,
		"miiocli", "-d", "devtools", "miot-simulator", "--file", "/peer/spec.json", "--model", model)
	c := &miio.Client{Addr: fmt.Sprintf("127.0.0.1:%d", port), Token: zeroToken, Timeout: 3 * time.Second}
	if info := waitInfo(t, c); info["model"] != model {
		t.Errorf("miIO.info %v", info)
	}
	did := strconv.FormatUint(uint64(didFor(model)), 10)
	res, err := miio.GetProperties(ctx, c, []miio.PropRef{{DID: did, SIID: 2, PIID: 2}, {DID: did, SIID: 2, PIID: 4}, {DID: did, SIID: 3, PIID: 6}, {DID: did, SIID: 9, PIID: 9}})
	if err != nil || len(res) != 4 {
		t.Fatalf("get_properties %+v %v", res, err)
	}
	if res[0].Code != 0 || res[1].Code != 0 || res[2].Code != 0 || res[3].Code == 0 || res[0].SIID != 2 || res[2].PIID != 6 || res[0].DID != did {
		t.Errorf("get_properties %+v", res)
	}
	// The simulator's initial values are its placeholder (-10000), so
	// only the values written below are checked.
	if _, err := miio.SetProperties(ctx, c, []miio.PropRef{{DID: did, SIID: 2, PIID: 2, Value: false}, {DID: did, SIID: 2, PIID: 4, Value: 3}}); err != nil {
		t.Errorf("set_properties %v", err)
	}
	res, err = miio.GetProperties(ctx, c, []miio.PropRef{{DID: did, SIID: 2, PIID: 2}, {DID: did, SIID: 2, PIID: 4}})
	if err != nil || len(res) != 2 || res[0].Value != false || res[1].Value != 3.0 {
		t.Errorf("after set_properties %+v %v", res, err)
	}
	if _, err := miio.SetProperties(ctx, c, []miio.PropRef{{DID: did, SIID: 3, PIID: 6, Value: 5.0}}); err == nil {
		t.Error("a read-only property was written")
	}
	if err := miio.DoAction(ctx, c, did, 2, 1, nil); err != nil {
		t.Errorf("action %v", err)
	}
}
