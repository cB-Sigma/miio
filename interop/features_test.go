// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

// The specification mapping against python-miio's MIoT simulator running
// the same specification (peer/spec.json): properties read and written
// by name, a mode set by its description, a value outside the list
// refused before sending, an action by name.
func TestModelAgainstMiotSimulator(t *testing.T) {
	needDocker(t)
	b, err := os.ReadFile("peer/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var sp miio.Spec
	if err := json.Unmarshal(b, &sp); err != nil {
		t.Fatal(err)
	}
	m, err := miio.NewModel(&sp)
	if err != nil {
		t.Fatal(err)
	}
	const model = "sigma.airpurifier.t1"
	port := freeUDPPort(t)
	startPeer(t, "-p", fmt.Sprintf("127.0.0.1:%d:54321/udp", port), image,
		"miiocli", "-d", "devtools", "miot-simulator", "--file", "/peer/spec.json", "--model", model)
	c := &miio.Client{Addr: fmt.Sprintf("127.0.0.1:%d", port), Token: zeroToken, Timeout: 3 * time.Second}
	waitInfo(t, c)
	did := strconv.FormatUint(uint64(didFor(model)), 10)
	if err := m.SetMany(ctx, c, did, map[string]any{"air-purifier.on": "on", "mode": "Favorite", "fan-level": 2}); err != nil {
		t.Fatalf("set by name: %v", err)
	}
	got, err := m.Get(ctx, c, did, "air-purifier.on", "mode", "fan-level", "pm2.5-density")
	if err != nil || got["air-purifier.on"] != true || got["mode"] != 2.0 || got["fan-level"] != 2.0 {
		t.Errorf("get by name %v %v", got, err)
	}
	if err := m.Set(ctx, c, did, "fan-level", 9); !errors.Is(err, miio.ErrValue) {
		t.Errorf("a fan level outside the list: %v", err)
	}
	if err := m.Do(ctx, c, did, "toggle"); err != nil {
		t.Errorf("action by name: %v", err)
	}
	p := &miio.Purifier{Family: miio.Family{Client: c, DID: did, Model: m}}
	if err := p.SetMode(ctx, "Sleep"); err != nil {
		t.Error(err)
	}
	if st, err := p.Status(ctx); err != nil || st.Mode != "Sleep" || st.FanLevel != 2 {
		t.Errorf("purifier helper %+v %v", st, err)
	}
}

// A push built with python-miio's own packet code reaches the push
// server: its hello is answered with the server's device ID, the push
// with result 0 under that ID, and the handler gets the source and action.
func TestPushServerFromPythonMiio(t *testing.T) {
	needDocker(t)
	tok, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	name := fmt.Sprintf("sigma-miio-push-%d", time.Now().UnixNano())
	cmd := exec.Command("docker", "run", "--rm", "--name", name, "--add-host=host.docker.internal:host-gateway", image,
		"python", "-u", "push.py", "host.docker.internal", strconv.Itoa(port), hex.EncodeToString(tok), "177001")
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = exec.Command("docker", "stop", "-t", "2", name).Run() })

	// The probe tells where the container's packets come from.
	_ = probe.SetReadDeadline(time.Now().Add(60 * time.Second))
	buf := make([]byte, 64)
	_, from, err := probe.ReadFromUDP(buf)
	probe.Close()
	if err != nil {
		t.Fatalf("no probe: %v\n%s", err, out.String())
	}
	s := &miio.PushServer{Addr: fmt.Sprintf(":%d", port), DeviceID: 4242}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := make(chan miio.Push, 1)
	if err := s.Register(&miio.Client{Addr: net.JoinHostPort(from.IP.String(), "54321"), Token: tok}, func(p miio.Push) { got <- p }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("peer: %v\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the peer did not finish\n%s", out.String())
	}
	lines := map[string]string{}
	for _, l := range strings.Split(out.String(), "\n") {
		if rest, ok := strings.CutPrefix(l, "PEER "); ok {
			k, v, _ := strings.Cut(rest, " ")
			lines[k] = v
		}
	}
	if !strings.Contains(lines["hello"], `"device_id": 4242`) || !strings.Contains(lines["hello"], `"length": 32`) {
		t.Errorf("hello as python-miio read it: %s", lines["hello"])
	}
	if !strings.Contains(lines["answer"], `"result": 0`) || !strings.Contains(lines["answer"], `"device_id": 4242`) {
		t.Errorf("answer as python-miio read it: %s\n%s", lines["answer"], out.String())
	}
	select {
	case p := <-got:
		if p.Action != "open" || p.Source != "lumi.158d0001aaaa02" || string(p.Params) != `["open"]` {
			t.Errorf("push %+v", p)
		}
	default:
		t.Error("the handler got no push")
	}
}
