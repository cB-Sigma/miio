// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Command miiospec keeps a local mirror of MIoT specifications and serves
// it in the shape of miot-spec.org, for sites without internet:
//
//	miiospec -dir /var/lib/miot-spec -fill zhimi.airpurifier.ma4,roborock.vacuum.s5
//	miiospec -dir /var/lib/miot-spec -listen :8080
//
// The first fills the directory once, where there is internet; the
// second serves it (also from a copy of the directory carried to the
// site), and clients set their specification base to
// http://<host>:8080/miot-spec-v2. -upstream "" serves strictly offline;
// otherwise what is missing is fetched through and kept.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cB-Sigma/miio"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, nil); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "miiospec:", err)
		os.Exit(1)
	}
}

// run fills and, with -listen, serves until ctx ends. ready, when not
// nil, receives the listening address.
func run(ctx context.Context, args []string, out io.Writer, ready func(addr string)) error {
	fs := flag.NewFlagSet("miiospec", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("dir", "", "the mirror's directory (required)")
	upstream := fs.String("upstream", miio.DefaultSpecURL, "the specification service to fill from; empty: offline")
	status := fs.String("status", "released", "the index to keep: released or all")
	fill := fs.String("fill", "", "comma-separated models whose newest specifications to fetch now")
	texts := fs.Bool("texts", false, "with -fill, also fetch the translations")
	all := fs.Int("all", 0, "fetch every model's newest specification with this many requests at a time (a long run)")
	listen := fs.String("listen", "", "serve the mirror on this address, such as :8080")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || fs.NArg() > 0 {
		return errors.New("usage: miiospec -dir <directory> [-fill models] [-all n] [-listen addr]")
	}
	m := &miio.SpecMirror{Dir: *dir}
	if *upstream != "" {
		m.Upstream = &miio.HTTPSpecs{Base: *upstream, HTTP: &http.Client{Timeout: 60 * time.Second}, Status: *status}
		if *upstream == miio.DefaultSpecURL {
			m.Upstream.Fallbacks = []string{miio.SpecMirrorURL}
		}
	}
	var models []string
	for _, s := range strings.Split(*fill, ",") {
		if s = strings.TrimSpace(s); s != "" {
			models = append(models, s)
		}
	}
	if len(models) > 0 || *all > 0 {
		if m.Upstream == nil {
			return errors.New("-fill and -all need an -upstream")
		}
		if err := m.Fill(ctx, models...); err != nil {
			return err
		}
		if *texts {
			if err := m.FillTranslations(ctx, models...); err != nil {
				return err
			}
		}
		if *all > 0 {
			if err := m.FillAll(ctx, *all); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "miiospec: %d models in the index, %d filled, in %s\n", len(m.Models()), len(models), *dir)
	}
	if *listen == "" {
		return nil
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	fmt.Fprintf(out, "miiospec: serving %s on http://%s/miot-spec-v2\n", *dir, ln.Addr())
	if ready != nil {
		ready(ln.Addr().String())
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
