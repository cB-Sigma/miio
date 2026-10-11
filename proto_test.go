// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

// Demo tokens (made up).
const (
	plugToken  = "00112233445566778899aabbccddeeff"
	lightToken = "ffeeddccbbaa99887766554433221100"
)

// Covers: SIG-IPE-4470
func TestMiIOPackets(t *testing.T) {
	h := miio.Hello()
	if hex.EncodeToString(h[:4]) != "21310020" || len(h) != 32 || h[31] != 0xff {
		t.Errorf("hello %x", h)
	}
	tok, _ := hex.DecodeString(plugToken)
	key, iv := miio.Keys(tok)
	if len(key) != 16 || len(iv) != 16 || bytes.Equal(key, iv) {
		t.Error("keys")
	}
	plain := []byte(`{"id":1,"method":"miIO.info","params":[]}`)
	p := miio.Packet(tok, 0x0a1b2c01, 1234, plain)
	hd, got, err := miio.Open(tok, p)
	if err != nil || !bytes.Equal(got, plain) || hd.DeviceID != 0x0a1b2c01 || hd.Stamp != 1234 || int(hd.Length) != len(p) {
		t.Fatalf("open %+v %q %v", hd, got, err)
	}
	other, _ := hex.DecodeString(lightToken)
	if _, _, err := miio.Open(other, p); err == nil {
		t.Error("a packet opened with another token")
	}
	p[40] ^= 1
	if _, _, err := miio.Open(tok, p); err == nil {
		t.Error("a changed packet passed the checksum")
	}
}

// An answer with neither "result" nor "error" is returned whole, as
// python-miio takes it (its simulators answer miIO.info so; found by the
// interop test).
func TestAnswerWithoutResult(t *testing.T) {
	tok, _ := hex.DecodeString(plugToken)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n == 32 { // hello
				h := miio.Hello()
				copy(h[4:16], []byte{0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 100})
				_, _ = conn.WriteToUDP(h, from)
				continue
			}
			_, plain, err := miio.Open(tok, buf[:n])
			if err != nil {
				continue
			}
			var req struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(plain, &req)
			_, _ = conn.WriteToUDP(miio.Packet(tok, 7, 101, []byte(fmt.Sprintf(`{"model":"test.plug.x","id":%d}`, req.ID))), from)
		}
	}()
	c := &miio.Client{Addr: conn.LocalAddr().String(), Token: tok, Timeout: 2 * time.Second}
	raw, err := c.Call(context.Background(), "miIO.info", nil)
	var info map[string]any
	if err != nil || json.Unmarshal(raw, &info) != nil || info["model"] != "test.plug.x" || c.DeviceID() != 7 {
		t.Errorf("answer %s %v id %d", raw, err, c.DeviceID())
	}
}

// The Mi account's request crypto: RC4 under the signed nonce both ways,
// and a base64 SHA-1 signature.
//
// Covers: SIG-IMP-186
func TestMiAccountCrypto(t *testing.T) {
	sn := miio.SignedNonce("AAAAAAAAAAAAAAAAAAAAAA==", "AQIDBAUGBwgAAAAA")
	enc := miio.RC4Encrypt(sn, `{"a":1}`)
	if p, err := miio.RC4Decrypt(sn, enc); err != nil || string(p) != `{"a":1}` {
		t.Errorf("rc4 %q %v", p, err)
	}
	if s := miio.EncSignature("POST", "/v2/homeroom/gethome", [][2]string{{"data", "x"}}, sn); len(s) != 28 {
		t.Errorf("signature %q", s)
	}
}
