// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/miio"
)

const urn = "urn:miot-spec-v2:device:outlet:0000A002:test-plug:1"

// Fill from a made-up service, then serve the directory offline and read
// a model through it.
func TestFillThenServe(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/miot-spec-v2/instances":
			_, _ = w.Write([]byte(`{"instances":[{"model":"test.plug.v1","version":1,"type":"` + urn + `"}]}`))
		case "/miot-spec-v2/instance":
			_, _ = w.Write([]byte(`{"type":"` + urn + `","services":[{"iid":2,"type":"urn:miot-spec-v2:service:switch:0000780C:test-plug:1"}]}`))
		case "/instance/v2/multiLanguage":
			_, _ = w.Write([]byte(`{"data":{"en":{"service:002":"Switch"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-dir", dir, "-upstream", up.URL + "/miot-spec-v2", "-fill", "test.plug.v1", "-texts"}, &out, nil); err != nil {
		t.Fatal(err)
	}
	up.Close()
	if !strings.Contains(out.String(), "1 models in the index") {
		t.Errorf("output %q", out.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-dir", dir, "-upstream", "", "-listen", "127.0.0.1:0"}, &bytes.Buffer{}, func(a string) { addr <- a })
	}()
	var a string
	select {
	case a = <-addr:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("not listening")
	}
	hs := &miio.HTTPSpecs{Base: "http://" + a + "/miot-spec-v2"}
	if sp, err := hs.Spec(context.Background(), "test.plug.v1"); err != nil || sp.Type != urn {
		t.Errorf("served spec %+v %v", sp, err)
	}
	if tr, err := hs.Translations(context.Background(), urn); err != nil || tr.Text("en", "service:002") != "Switch" {
		t.Errorf("served translations %v %v", tr, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
	if err := run(context.Background(), []string{"-upstream", ""}, &bytes.Buffer{}, nil); err == nil {
		t.Error("no -dir accepted")
	}
	if err := run(context.Background(), []string{"-dir", dir, "-upstream", "", "-fill", "x"}, &bytes.Buffer{}, nil); err == nil {
		t.Error("-fill offline accepted")
	}
}
