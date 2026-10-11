// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cB-Sigma/miio"
)

// List the account's devices with their tokens, then switch a plug on its
// LAN. It needs a Mi account and a device, so it is not run;
// github.com/cB-Sigma/miio-sim runs the same steps against a simulated
// account and plug.
func Example() {
	ctx := context.Background()
	cloud := &miio.Cloud{Username: "user@example.invalid", Password: "secret"}
	devs, err := cloud.Devices(ctx, []string{"i2"}) // India; miio.Regions lists all
	if err != nil || len(devs) == 0 {
		fmt.Println(devs, err)
		return
	}
	tok, _ := hex.DecodeString(devs[0].Token)
	c := &miio.Client{Addr: devs[0].LocalIP + ":54321", Token: tok, Timeout: 3 * time.Second}
	_, err = c.Call(ctx, "set_power", []any{"on"})
	fmt.Println(devs[0].Model, err)
}

// Read a MIoT device's properties by service and property ID. It needs a
// device, so it is not run.
func ExampleGetProperties() {
	tok, _ := hex.DecodeString("0f1e2d3c4b5a69788796a5b4c3d2e1f0")
	c := &miio.Client{Addr: "192.168.1.70:54321", Token: tok, Timeout: 3 * time.Second}
	res, err := miio.GetProperties(context.Background(), c, []miio.PropRef{
		{DID: "purifier", SIID: 2, PIID: 2}, // power
		{DID: "purifier", SIID: 3, PIID: 6}, // PM2.5
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, r := range res {
		fmt.Println(r.SIID, r.PIID, r.Code, r.Value)
	}
}

// Map a specification to names and check values before sending them.
func ExampleNewModel() {
	m, _ := miio.NewModel(&miio.Spec{Type: "urn:miot-spec-v2:device:light:0000A001:demo-lamp:1", Services: []miio.Service{
		{IID: 2, Type: "urn:miot-spec-v2:service:light:00007802:demo-lamp:1", Properties: []miio.Property{
			{IID: 2, Type: "urn:miot-spec-v2:property:brightness:0000000D:demo-lamp:1", Format: "uint8",
				Access: []string{"read", "write"}, ValueRange: []float64{1, 100, 1}}}}}})
	p, _ := m.Property("light.brightness")
	_, err := p.Check(150)
	fmt.Println(p.SIID, p.IID, err != nil)
	// Output: 2 2 true
}

func ExampleKeys() {
	tok, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	key, iv := miio.Keys(tok)
	fmt.Println(len(key), len(iv))
	// Output: 16 16
}
