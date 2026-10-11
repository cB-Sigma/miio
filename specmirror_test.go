// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cB-Sigma/miio"
)

// fakeService is a made-up specification service in miot-spec.org's
// shape, counting requests per path.
type fakeService struct {
	srv   *httptest.Server
	mu    sync.Mutex
	hits  map[string]int
	index string
	specs map[string]string // URN → JSON
	texts map[string]string // URN → JSON
	down  bool
}

func newFakeService(t *testing.T) *fakeService {
	f := &fakeService{hits: map[string]int{}, specs: map[string]string{}, texts: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits[r.URL.Path]++
		if f.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/miot-spec-v2/instances":
			_, _ = w.Write([]byte(f.index))
		case "/miot-spec-v2/instance":
			if s, ok := f.specs[r.URL.Query().Get("type")]; ok {
				_, _ = w.Write([]byte(s))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case "/instance/v2/multiLanguage":
			if s, ok := f.texts[r.URL.Query().Get("urn")]; ok {
				_, _ = w.Write([]byte(s))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeService) base() string { return f.srv.URL + "/miot-spec-v2" }

func (f *fakeService) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeService) setDown(d bool) {
	f.mu.Lock()
	f.down = d
	f.mu.Unlock()
}

const (
	urnV1 = "urn:miot-spec-v2:device:outlet:0000A002:test-plug:1"
	urnV2 = "urn:miot-spec-v2:device:outlet:0000A002:test-plug:2"
	urnLm = "urn:miot-spec-v2:device:light:0000A001:test-lamp:1"
)

func specJSON(urn string) string {
	b, _ := json.Marshal(miio.Spec{Type: urn, Services: []miio.Service{{IID: 2, Type: "urn:miot-spec-v2:service:switch:0000780C:test-plug:1",
		Properties: []miio.Property{{IID: 1, Type: "urn:miot-spec-v2:property:on:00000006:test-plug:1", Format: "bool", Access: []string{"read", "write"}}}}}})
	return string(b)
}

func filled(t *testing.T) *fakeService {
	f := newFakeService(t)
	// An index with every status: version 2 of the plug is not released.
	f.index = `{"instances":[{"model":"test.plug.v1","version":1,"type":"` + urnV1 + `","status":"released","ts":1},` +
		`{"model":"test.plug.v1","version":2,"type":"` + urnV2 + `","status":"debug","ts":2},` +
		`{"model":"test.lamp.v1","version":1,"type":"` + urnLm + `","status":"released","ts":1}]}`
	f.specs[urnV1], f.specs[urnV2], f.specs[urnLm] = specJSON(urnV1), specJSON(urnV2), specJSON(urnLm)
	f.texts[urnV1] = `{"type":"` + urnV1 + `","data":{"en":{"service:002":"Switch","service:002:property:001":"Power"},"hi":{"service:002:property:001":"बिजली"}}}`
	return f
}

// Filled once, the mirror serves from its directory with the service
// down, survives a restart, and keeps the released version first.
func TestSpecMirrorOffline(t *testing.T) {
	ctx := context.Background()
	f := filled(t)
	dir := t.TempDir()
	up := &miio.HTTPSpecs{Base: f.base(), HTTP: f.srv.Client(), Status: "all"}
	m := &miio.SpecMirror{Dir: dir, Upstream: up}
	if err := m.Fill(ctx, "test.plug.v1"); err != nil {
		t.Fatal(err)
	}
	if err := m.FillTranslations(ctx, "test.plug.v1"); err != nil {
		t.Fatal(err)
	}
	if !m.Stored("test.plug.v1") || m.Stored("test.lamp.v1") {
		t.Errorf("stored: plug %v lamp %v", m.Stored("test.plug.v1"), m.Stored("test.lamp.v1"))
	}
	if got := m.Models(); strings.Join(got, ",") != "test.lamp.v1,test.plug.v1" {
		t.Errorf("models %v", got)
	}
	f.setDown(true)
	// A new mirror on the same directory, with the upstream down.
	m2 := &miio.SpecMirror{Dir: dir, Upstream: up}
	sp, err := m2.Spec(ctx, "test.plug.v1")
	if err != nil || sp.Type != urnV1 {
		t.Fatalf("offline spec %+v %v (want the released version 1)", sp, err)
	}
	if _, err := m2.Spec(ctx, "test.lamp.v1"); !errors.Is(err, miio.ErrNotMirrored) {
		t.Errorf("a model not filled, upstream down: %v", err)
	}
	tr, err := m2.Translations(ctx, urnV1)
	if err != nil || tr.Text("hi", miio.TextKey(2, 1, -1, -1)) != "बिजली" || tr.Text("en", miio.TextKey(2, -1, -1, -1)) != "Switch" {
		t.Errorf("translations %v %v", tr, err)
	}
	// Strictly offline: no upstream at all.
	m3 := &miio.SpecMirror{Dir: dir}
	if _, err := miio.ModelFor(ctx, m3, "test.plug.v1"); err != nil {
		t.Errorf("offline model: %v", err)
	}
	if _, err := m3.Spec(ctx, "no.such.model"); !errors.Is(err, miio.ErrNoSpec) {
		t.Errorf("unknown model: %v", err)
	}
	if err := m3.Fill(ctx); err == nil {
		t.Error("Fill without an upstream")
	}
	if _, err := (&miio.SpecMirror{Dir: t.TempDir()}).Spec(ctx, "test.plug.v1"); !errors.Is(err, miio.ErrNotMirrored) {
		t.Errorf("an empty mirror: %v", err)
	}
}

// On first use the mirror fetches what it lacks, once.
func TestSpecMirrorFetchThrough(t *testing.T) {
	ctx := context.Background()
	f := filled(t)
	m := &miio.SpecMirror{Dir: t.TempDir(), Upstream: &miio.HTTPSpecs{Base: f.base(), HTTP: f.srv.Client()}}
	for i := 0; i < 3; i++ {
		if _, err := m.Spec(ctx, "test.lamp.v1"); err != nil {
			t.Fatal(err)
		}
	}
	if f.count("/miot-spec-v2/instances") != 1 || f.count("/miot-spec-v2/instance") != 1 {
		t.Errorf("requests: %v", f.hits)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "instances.json")); err != nil {
		t.Errorf("index not stored: %v", err)
	}
}

// The handler answers in the service's shape: HTTPSpecs reads a model
// through it, ?status filters, translations come from the host root, and
// a mirror can fill from another mirror.
func TestSpecMirrorHandler(t *testing.T) {
	ctx := context.Background()
	f := filled(t)
	dir := t.TempDir()
	m := &miio.SpecMirror{Dir: dir, Upstream: &miio.HTTPSpecs{Base: f.base(), HTTP: f.srv.Client(), Status: "all"}}
	if err := m.Fill(ctx, "test.plug.v1", "test.lamp.v1"); err != nil {
		t.Fatal(err)
	}
	if err := m.FillTranslations(ctx, "test.plug.v1"); err != nil {
		t.Fatal(err)
	}
	f.setDown(true)
	srv := httptest.NewServer((&miio.SpecMirror{Dir: dir}).Handler())
	defer srv.Close()
	hs := &miio.HTTPSpecs{Base: srv.URL + "/miot-spec-v2", HTTP: srv.Client()}
	sp, err := hs.Spec(ctx, "test.plug.v1")
	if err != nil || sp.Type != urnV1 {
		t.Fatalf("HTTPSpecs through the mirror %+v %v", sp, err)
	}
	ins, err := hs.Index(ctx)
	if err != nil || len(ins) != 2 {
		t.Errorf("released index %+v %v", ins, err)
	}
	all, err := (&miio.HTTPSpecs{Base: srv.URL + "/miot-spec-v2", Status: "all"}).Index(ctx)
	if err != nil || len(all) != 3 {
		t.Errorf("full index %+v %v", all, err)
	}
	if tr, err := hs.Translations(ctx, urnV1); err != nil || tr.Text("en", "service:002:property:001") != "Power" {
		t.Errorf("translations through the mirror %v %v", tr, err)
	}
	for path, code := range map[string]int{
		"/miot-spec-v2/instance?type=urn:none": http.StatusNotFound, "/miot-spec-v2/instance": http.StatusBadRequest,
		"/miot-spec-v2/other": http.StatusNotFound, "/instance/v2/multiLanguage?urn=" + urnLm: http.StatusNotFound,
	} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != code {
			t.Errorf("%s: HTTP %d, want %d", path, resp.StatusCode, code)
		}
	}
	resp, err := srv.Client().Post(srv.URL+"/miot-spec-v2/instances", "application/json", nil)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST: HTTP %d", resp.StatusCode)
		}
	}
	// A second site fills from the first site's mirror.
	m2 := &miio.SpecMirror{Dir: t.TempDir(), Upstream: hs}
	if err := m2.Fill(ctx, "test.lamp.v1"); err != nil || !m2.Stored("test.lamp.v1") {
		t.Errorf("fill from a mirror: %v", err)
	}
}

// Add puts a site's own specification in; the newest version wins.
func TestSpecMirrorAdd(t *testing.T) {
	ctx := context.Background()
	m := &miio.SpecMirror{Dir: t.TempDir()}
	if err := m.Add("test.plug.v1", 1, []byte(specJSON(urnV1))); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("test.plug.v1", 2, []byte(specJSON(urnV2))); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("test.plug.v1", 2, []byte(specJSON(urnV2))); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("x", 1, []byte(`{"services":[]}`)); err == nil {
		t.Error("a specification without a type was added")
	}
	if err := m.AddTranslations(urnV2, []byte(`{"data":{"en":{"service:002":"Plug"}}}`)); err != nil {
		t.Fatal(err)
	}
	sp, err := m.Spec(ctx, "test.plug.v1")
	if err != nil || sp.Type != urnV2 {
		t.Errorf("newest added %+v %v", sp, err)
	}
	if u, _ := m.URN(ctx, "test.plug.v1"); u != urnV2 {
		t.Errorf("URN %s", u)
	}
	ins := (&miio.SpecMirror{Dir: m.Dir}).Models()
	if len(ins) != 1 {
		t.Errorf("models %v", ins)
	}
	if tr, err := m.Translations(ctx, urnV2); err != nil || tr.Text("en", "service:002") != "Plug" {
		t.Errorf("added translations %v %v", tr, err)
	}
}

// FillAll fetches every model in the index.
func TestSpecMirrorFillAll(t *testing.T) {
	f := filled(t)
	m := &miio.SpecMirror{Dir: t.TempDir(), Upstream: &miio.HTTPSpecs{Base: f.base(), HTTP: f.srv.Client()}}
	if err := m.FillAll(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if !m.Stored("test.plug.v1") || !m.Stored("test.lamp.v1") {
		t.Error("FillAll left models out")
	}
	delete(f.specs, urnLm)
	m2 := &miio.SpecMirror{Dir: t.TempDir(), Upstream: m.Upstream}
	if err := m2.FillAll(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "1 specifications") {
		t.Errorf("a failed model: %v", err)
	}
}

// HTTPSpecs falls back to the next base when the first fails with a
// server error, not when it says a URN does not exist.
func TestHTTPSpecsFallbacks(t *testing.T) {
	ctx := context.Background()
	down := newFakeService(t)
	down.setDown(true)
	up := filled(t)
	hs := &miio.HTTPSpecs{Base: down.base(), Fallbacks: []string{up.base()}}
	if _, err := hs.Spec(ctx, "test.lamp.v1"); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	before := down.count("/miot-spec-v2/instance")
	if _, err := (&miio.HTTPSpecs{Base: up.base(), Fallbacks: []string{down.base()}}).RawInstance(ctx, "urn:none"); err == nil || down.count("/miot-spec-v2/instance") != before {
		t.Errorf("a 404 went to the fallback: %v", err)
	}
}

func TestTextKey(t *testing.T) {
	for want, got := range map[string]string{
		"service:002":                            miio.TextKey(2, -1, -1, -1),
		"service:002:property:011":               miio.TextKey(2, 11, -1, -1),
		"service:002:property:011:valuelist:000": miio.TextKey(2, 11, -1, 0),
		"service:010:action:001":                 miio.TextKey(10, -1, 1, -1),
	} {
		if got != want {
			t.Errorf("%s, want %s", got, want)
		}
	}
	if _, err := miio.ParseTranslations([]byte("nope")); err == nil {
		t.Error("bad translations accepted")
	}
}
