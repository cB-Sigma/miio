// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

// runPeer runs the peer image to completion and returns its "PEER <step>
// <json>" lines by step.
func runPeer(t *testing.T, args ...string) map[string]string {
	t.Helper()
	name := fmt.Sprintf("sigma-miio-run-%d", time.Now().UnixNano())
	cmd := exec.Command("docker", append([]string{"run", "--rm", "--name", name}, args...)...)
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("peer: %v\n%s", err, out.String())
		}
	case <-time.After(90 * time.Second):
		_ = exec.Command("docker", "stop", "-t", "2", name).Run()
		t.Fatalf("the peer did not finish\n%s", out.String())
	}
	if os.Getenv(envVar+"_LOG") == "1" {
		t.Logf("peer output:\n%s", out.String())
	}
	lines := map[string]string{}
	for _, l := range strings.Split(out.String(), "\n") {
		if rest, ok := strings.CutPrefix(l, "PEER "); ok {
			k, v, _ := strings.Cut(rest, " ")
			lines[k] = v
		}
	}
	if _, ok := lines["done"]; !ok {
		t.Fatalf("the peer did not get to the end\n%s", out.String())
	}
	return lines
}

// python-miio's specification client (MiotCloud and micloud's MiotSpec)
// reads the index and a specification from SpecMirror's handler, serving
// offline what was added to it: the release it picks and the device
// model it builds (pydantic-checked) match.
func TestSpecMirrorWithPythonMiio(t *testing.T) {
	needDocker(t)
	raw, err := os.ReadFile("peer/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var sp miio.Spec
	if err := json.Unmarshal(raw, &sp); err != nil {
		t.Fatal(err)
	}
	const model = "sigma.airpurifier.t1"
	m := &miio.SpecMirror{Dir: t.TempDir()}
	if err := m.Add(model, 1, raw); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("sigma.other.t2", 1, []byte(`{"type":"urn:miot-spec-v2:device:outlet:0000A002:sigma-other:1","description":"Other","services":[]}`)); err != nil {
		t.Fatal(err)
	}
	// Listen on every interface: the container reaches the host through
	// Docker's gateway.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: m.Handler()}}
	srv.Start()
	defer srv.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	lines := runPeer(t, "--add-host=host.docker.internal:host-gateway", image,
		"python", "-u", "spec.py", fmt.Sprintf("http://host.docker.internal:%d/miot-spec-v2", port), model)
	var rel struct {
		Model, Type string
		Version     int
	}
	if err := json.Unmarshal([]byte(lines["release"]), &rel); err != nil || rel.Model != model || rel.Type != sp.Type || rel.Version != 1 {
		t.Errorf("release as python-miio read it: %s %v", lines["release"], err)
	}
	var dm struct {
		Description string
		Services    []int
		Properties  int
	}
	props := 0
	for _, s := range sp.Services {
		props += len(s.Properties)
	}
	if err := json.Unmarshal([]byte(lines["model"]), &dm); err != nil || dm.Description != sp.Description || len(dm.Services) != len(sp.Services) || dm.Properties != props {
		t.Errorf("device model as python-miio built it: %s %v (want %d services, %d properties)", lines["model"], err, len(sp.Services), props)
	}
}

// The vacuum's extras against python-miio's miio simulator with the
// Roborock file python-miio ships (simulated_roborock.yaml): status,
// consumables, the history in its list form, a record in its object
// form, do-not-disturb, timers and the time zone, all as python-miio's
// own simulator answers them.
func TestVacuumAgainstMiioSimulator(t *testing.T) {
	needDocker(t)
	const model = "roborock.vacuum.a15"
	port := freeUDPPort(t)
	startPeer(t, "-p", fmt.Sprintf("127.0.0.1:%d:54321/udp", port), image,
		"miiocli", "-d", "devtools", "miio-simulator", "--file", "/python-miio/miio/integrations/roborock/vacuum/simulated_roborock.yaml", "--model", model)
	c := &miio.Client{Addr: fmt.Sprintf("127.0.0.1:%d", port), Token: zeroToken, Timeout: 3 * time.Second}
	waitInfo(t, c)
	v := &miio.Vacuum{Family: miio.Family{Client: c, LegacyModel: model}}
	if st, err := v.Status(ctx); err != nil || st.Code != 8 || st.State != "charging" || st.Battery != 87 {
		t.Errorf("status %+v %v", st, err)
	}
	cons, err := v.Consumables(ctx)
	if err != nil || cons.MainBrush != 32454*time.Second || cons.Sensors != 3798*time.Second || cons.Filter != 32454*time.Second {
		t.Errorf("consumables %+v %v", cons, err)
	}
	s, err := v.CleanSummary(ctx)
	if err != nil || s.Duration != 174145*time.Second || s.Area != 2410.15 || s.Count != 82 || len(s.Records) != 7 || s.Records[0] != 1488240000 {
		t.Errorf("clean summary %+v %v", s, err)
	}
	r, err := v.CleanRecord(ctx, 1488347071)
	if err != nil || r.Start.Unix() != 1488347071 || r.End.Unix() != 1488347123 || r.Duration != 16*time.Second || r.Complete {
		t.Errorf("clean record %+v %v", r, err)
	}
	if d, err := v.DND(ctx); err != nil || d != (miio.DND{Enabled: true, StartHour: 22, EndHour: 8}) {
		t.Errorf("dnd %+v %v", d, err)
	}
	// The file's get_timer answer nests the second timer inside the first
	// record (a bracket placement), so it holds one record of four
	// entries; python-miio's own Timer reads the first three of it, as
	// this module does: one timer.
	ts, err := v.Timers(ctx)
	if err != nil || len(ts) != 1 || ts[0].ID != "1488667794112" || ts[0].Enabled || ts[0].Cron != "49 22 * * 6" || ts[0].Action != "start_clean" {
		t.Errorf("timers %+v %v", ts, err)
	}
	if tz, err := v.Timezone(ctx); err != nil || tz != "UTC" {
		t.Errorf("time zone %q %v", tz, err)
	}
	// A method the file does not define is refused as a device error.
	if _, err := v.Volume(ctx); err == nil {
		t.Error("get_sound_volume answered by a simulator that lacks it")
	}
}
