// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

// A push from a registered device is answered with result 0 under the
// listener's device ID and handed over with its source and action; a
// hello gets the listener's header; a packet with another token is
// dropped.
func TestPushServerReceives(t *testing.T) {
	tok, _ := hex.DecodeString(plugToken)
	s := &miio.PushServer{Addr: "127.0.0.1:0", DeviceID: 4242}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := make(chan miio.Push, 2)
	c := &miio.Client{Addr: "127.0.0.1:54321", Token: tok}
	if err := s.Register(c, func(p miio.Push) { got <- p }); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUDP("udp4", nil, s.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buf := make([]byte, 2048)
	_, _ = conn.Write(miio.Hello())
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if h, e := miio.ParseHeader(buf[:n]); err != nil || e != nil || h.DeviceID != 4242 || n != 32 {
		t.Fatalf("hello answer %x %v", buf[:n], err)
	}
	req, _ := json.Marshal(map[string]any{"id": 7, "method": "chuangmi.plug.v3.open:lumi_158d0001", "params": []any{"x"}})
	_, _ = conn.Write(miio.Packet(tok, 1, 1, req))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	h, plain, err := miio.Open(tok, buf[:n])
	if err != nil || h.DeviceID != 4242 || string(plain) != `{"id":7,"result":0}` {
		t.Errorf("answer %s %v", plain, err)
	}
	select {
	case p := <-got:
		if p.Action != "open" || p.Source != "lumi.158d0001" || string(p.Params) != `["x"]` || p.Device != "127.0.0.1" {
			t.Errorf("push %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no push")
	}
	other, _ := hex.DecodeString(lightToken)
	_, _ = conn.Write(miio.Packet(other, 1, 1, req))
	select {
	case p := <-got:
		t.Errorf("a push with another token was taken: %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := s.Subscribe(context.Background(), &miio.Client{Addr: "127.0.0.2:54321", Token: tok}, miio.EventInfo{Action: "a"}); err == nil {
		t.Error("an unregistered device was subscribed")
	}
}

// TokenEnc is the first block of the token encrypted with itself.
func TestTokenEnc(t *testing.T) {
	tok, _ := hex.DecodeString(plugToken)
	enc := miio.TokenEnc(tok)
	if len(enc) != 32 || enc != hex.EncodeToString(miio.Encrypt(tok, tok)[:16]) {
		t.Errorf("token enc %s", enc)
	}
}

// A verification error is ErrCloudVerify and says what was asked.
func TestVerifyError(t *testing.T) {
	var err error = &miio.VerifyError{CaptchaURL: "https://example.invalid/c"}
	if !errors.Is(err, miio.ErrCloudVerify) || err.Error() == miio.ErrCloudVerify.Error() {
		t.Errorf("%v", err)
	}
}
