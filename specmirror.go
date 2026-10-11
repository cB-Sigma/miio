// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SpecMirror keeps MIoT specifications in a local directory, for sites
// without internet or with unreliable internet. Filled from the
// specification service once (Fill, or on first use when Upstream is
// set), it serves from the directory from then on, also when the
// service cannot be reached. It is a SpecSource, and Handler serves the
// directory in the service's own URL and JSON shape, so HTTPSpecs (and
// other clients of miot-spec.org) can be pointed at it.
//
// The directory holds instances.json (the service's index as it answered
// it), instance/<URN>.json and multiLanguage/<URN>.json (one
// specification and its translations each, as the service answered
// them; the URN query-escaped). Files are written whole through a
// temporary file and a rename, so a reader never sees half a file.
type SpecMirror struct {
	Dir string
	// Upstream fills the mirror: the index when there is none (or it is
	// older than MaxAge), and each specification or translation on first
	// request. nil: offline, only what the directory holds.
	Upstream *HTTPSpecs
	// MaxAge is how old the index may grow before Upstream is asked for
	// a new one; 0: the index is kept until Fill is called. An unknown
	// model also asks Upstream for a new index, at most once an hour.
	MaxAge time.Duration

	mu       sync.Mutex
	raw      []byte            // the index as stored
	newest   map[string]string // model → URN of its newest version
	loadedAt time.Time         // when the index was fetched (file time)
	askedAt  time.Time         // last index request to Upstream
}

const (
	mirrorIndex = "instances.json"
	mirrorSpecs = "instance"
	mirrorTexts = "multiLanguage"
)

// ErrNotMirrored means the mirror has no copy and no upstream to fetch it
// from (or the upstream failed).
var ErrNotMirrored = errors.New("not in the MIoT specification mirror")

func urnFile(urn string) string { return url.QueryEscape(urn) + ".json" }

// writeFile writes b to Dir/rel whole: a temporary file renamed into
// place.
func (m *SpecMirror) writeFile(rel string, b []byte) error {
	p := filepath.Join(m.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(f.Name(), p)
	}
	if werr != nil {
		_ = os.Remove(f.Name())
	}
	return werr
}

func (m *SpecMirror) readFile(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(m.Dir, rel))
}

// setIndex parses and keeps an index; the caller holds m.mu.
func (m *SpecMirror) setIndex(b []byte, at time.Time) error {
	ins, err := parseIndex(b)
	if err != nil {
		return err
	}
	m.raw, m.newest, m.loadedAt = b, newestByModel(ins), at
	return nil
}

// loadIndex reads the stored index once; the caller holds m.mu.
func (m *SpecMirror) loadIndex() {
	if m.newest != nil {
		return
	}
	p := filepath.Join(m.Dir, mirrorIndex)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	at := time.Now()
	if st, err := os.Stat(p); err == nil {
		at = st.ModTime()
	}
	_ = m.setIndex(b, at)
}

// fetchIndex asks Upstream for the index and stores it; the caller holds
// m.mu.
func (m *SpecMirror) fetchIndex(ctx context.Context) error {
	m.askedAt = time.Now()
	b, err := m.Upstream.RawIndex(ctx)
	if err != nil {
		return err
	}
	if _, err := parseIndex(b); err != nil {
		return err
	}
	if err := m.writeFile(mirrorIndex, b); err != nil {
		return err
	}
	return m.setIndex(b, time.Now())
}

// urn returns the model's newest URN, refreshing the index as MaxAge and
// unknown models ask.
func (m *SpecMirror) urn(ctx context.Context, model string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadIndex()
	var upErr error
	if m.Upstream != nil {
		stale := m.newest == nil || (m.MaxAge > 0 && time.Since(m.loadedAt) > m.MaxAge)
		if stale && time.Since(m.askedAt) > time.Minute {
			upErr = m.fetchIndex(ctx)
		}
		if _, ok := m.newest[model]; !ok && m.newest != nil && upErr == nil && time.Since(m.askedAt) > time.Hour {
			upErr = m.fetchIndex(ctx)
		}
	}
	if m.newest == nil {
		if upErr != nil {
			return "", fmt.Errorf("%w: no index, and the upstream failed: %v", ErrNotMirrored, upErr)
		}
		return "", fmt.Errorf("%w: no index in %s", ErrNotMirrored, m.Dir)
	}
	if u := m.newest[model]; u != "" {
		return u, nil
	}
	return "", fmt.Errorf("%w %s in the mirror at %s", ErrNoSpec, model, m.Dir)
}

// rawOf returns a stored file, fetching and storing it first when it is
// missing and Upstream is set.
func (m *SpecMirror) rawOf(ctx context.Context, dir, urn string, fetch func(context.Context, string) ([]byte, error)) ([]byte, error) {
	rel := filepath.Join(dir, urnFile(urn))
	b, err := m.readFile(rel)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if m.Upstream == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotMirrored, urn)
	}
	b, err = fetch(ctx, urn)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrNotMirrored, urn, err)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("%w: %s: the upstream answer is not JSON", ErrNotMirrored, urn)
	}
	return b, m.writeFile(rel, b)
}

// RawInstance returns the stored specification with this URN, fetching
// it once when Upstream is set.
func (m *SpecMirror) RawInstance(ctx context.Context, urn string) ([]byte, error) {
	return m.rawOf(ctx, mirrorSpecs, urn, func(ctx context.Context, u string) ([]byte, error) { return m.Upstream.RawInstance(ctx, u) })
}

// RawTranslations returns the stored translations of the specification
// with this URN, fetching them once when Upstream is set.
func (m *SpecMirror) RawTranslations(ctx context.Context, urn string) ([]byte, error) {
	return m.rawOf(ctx, mirrorTexts, urn, func(ctx context.Context, u string) ([]byte, error) { return m.Upstream.RawTranslations(ctx, u) })
}

// Translations returns the translations of the specification with this
// URN.
func (m *SpecMirror) Translations(ctx context.Context, urn string) (Translations, error) {
	b, err := m.RawTranslations(ctx, urn)
	if err != nil {
		return nil, err
	}
	return ParseTranslations(b)
}

// Spec implements SpecSource.
func (m *SpecMirror) Spec(ctx context.Context, model string) (*Spec, error) {
	urn, err := m.urn(ctx, model)
	if err != nil {
		return nil, err
	}
	b, err := m.RawInstance(ctx, urn)
	if err != nil {
		return nil, err
	}
	return parseSpec(b, model)
}

// URN returns the URN of the model's newest specification in the mirror.
func (m *SpecMirror) URN(ctx context.Context, model string) (string, error) {
	return m.urn(ctx, model)
}

// Fill fetches a new index from Upstream and the newest specification of
// each model named (already stored ones are kept); with no models, only
// the index.
func (m *SpecMirror) Fill(ctx context.Context, models ...string) error {
	if m.Upstream == nil {
		return errors.New("the specification mirror has no upstream to fill from")
	}
	m.mu.Lock()
	err := m.fetchIndex(ctx)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	for _, model := range models {
		if _, err := m.Spec(ctx, model); err != nil {
			return fmt.Errorf("%s: %w", model, err)
		}
	}
	return nil
}

// FillTranslations fetches the translations of each model's newest
// specification (already stored ones are kept).
func (m *SpecMirror) FillTranslations(ctx context.Context, models ...string) error {
	for _, model := range models {
		urn, err := m.urn(ctx, model)
		if err == nil {
			_, err = m.RawTranslations(ctx, urn)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", model, err)
		}
	}
	return nil
}

// FillAll fetches the newest specification of every model in the index
// that is not stored yet, with up to workers requests at a time (1 when
// below 1). It goes on past a failed model and returns the first error
// with the number of failures. The full index has many thousand models:
// expect a long run and a large directory.
func (m *SpecMirror) FillAll(ctx context.Context, workers int) error {
	if err := m.Fill(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	urns := map[string]bool{}
	for _, u := range m.newest {
		urns[u] = true
	}
	m.mu.Unlock()
	if workers < 1 {
		workers = 1
	}
	work := make(chan string)
	var wg sync.WaitGroup
	var emu sync.Mutex
	var first error
	failed := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range work {
				if _, err := m.RawInstance(ctx, u); err != nil {
					emu.Lock()
					if first == nil {
						first = err
					}
					failed++
					emu.Unlock()
				}
			}
		}()
	}
	for u := range urns {
		if ctx.Err() != nil {
			break
		}
		work <- u
	}
	close(work)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if first != nil {
		return fmt.Errorf("%d specifications not mirrored, the first: %w", failed, first)
	}
	return nil
}

// Models lists the models in the mirror's index, sorted.
func (m *SpecMirror) Models() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadIndex()
	out := make([]string, 0, len(m.newest))
	for k := range m.newest {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Stored tells whether the model's newest specification is in the
// directory.
func (m *SpecMirror) Stored(model string) bool {
	m.mu.Lock()
	m.loadIndex()
	urn := m.newest[model]
	m.mu.Unlock()
	if urn == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(m.Dir, mirrorSpecs, urnFile(urn)))
	return err == nil
}

// Add stores a specification under a model and version (its URN is the
// specification's "type") and lists it in the index: for specifications
// carried in by hand, or a site's own. A model's newest version is the
// one used.
func (m *SpecMirror) Add(model string, version int, raw []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return fmt.Errorf("the specification is not JSON: %w", err)
	}
	if head.Type == "" || model == "" {
		return errors.New("a specification needs a model and a \"type\" URN")
	}
	if err := m.writeFile(filepath.Join(mirrorSpecs, urnFile(head.Type)), raw); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadIndex()
	var idx map[string]json.RawMessage
	if m.raw == nil || json.Unmarshal(m.raw, &idx) != nil {
		idx = map[string]json.RawMessage{}
	}
	var ins []json.RawMessage
	_ = json.Unmarshal(idx["instances"], &ins)
	kept := ins[:0]
	for _, e := range ins {
		var in SpecInstance
		if json.Unmarshal(e, &in) == nil && in.Model == model && in.Version == version {
			continue
		}
		kept = append(kept, e)
	}
	e, _ := json.Marshal(SpecInstance{Model: model, Version: version, Type: head.Type, Status: "released", TS: time.Now().Unix()})
	idx["instances"], _ = json.Marshal(append(kept, e))
	b, _ := json.Marshal(idx)
	if err := m.writeFile(mirrorIndex, b); err != nil {
		return err
	}
	at := m.loadedAt
	if at.IsZero() {
		at = time.Now()
	}
	return m.setIndex(b, at)
}

// AddTranslations stores the translations of the specification with this
// URN, in the service's answer shape.
func (m *SpecMirror) AddTranslations(urn string, raw []byte) error {
	if !json.Valid(raw) {
		return errors.New("the translations are not JSON")
	}
	return m.writeFile(filepath.Join(mirrorTexts, urnFile(urn)), raw)
}

// Handler serves the mirror in the specification service's shape: a path
// ending in /instances answers the index (filtered by ?status= when its
// entries carry a status), /instance?type=<URN> a specification, and
// /multiLanguage?urn=<URN> its translations. Mounted at the root of a
// server, HTTPSpecs{Base: "http://<server>/miot-spec-v2"} reads it. With
// Upstream set, what is missing is fetched through and kept.
func (m *SpecMirror) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var b []byte
		var err error
		p := strings.TrimRight(r.URL.Path, "/")
		switch {
		case strings.HasSuffix(p, "/instances"):
			b, err = m.indexFor(r.Context(), r.URL.Query().Get("status"))
		case strings.HasSuffix(p, "/instance"):
			urn := r.URL.Query().Get("type")
			if urn == "" {
				http.Error(w, "type is missing", http.StatusBadRequest)
				return
			}
			b, err = m.RawInstance(r.Context(), urn)
		case strings.HasSuffix(p, "/multiLanguage"):
			urn := r.URL.Query().Get("urn")
			if urn == "" {
				http.Error(w, "urn is missing", http.StatusBadRequest)
				return
			}
			b, err = m.RawTranslations(r.Context(), urn)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, ErrNotMirrored) {
				code = http.StatusNotFound
			}
			http.Error(w, err.Error(), code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
}

// indexFor returns the stored index, keeping only entries of the status
// asked for when entries say theirs ("all" or "" keeps every entry).
func (m *SpecMirror) indexFor(ctx context.Context, status string) ([]byte, error) {
	m.mu.Lock()
	m.loadIndex()
	if m.raw == nil && m.Upstream != nil && time.Since(m.askedAt) > time.Minute {
		_ = m.fetchIndex(ctx)
	}
	raw := m.raw
	m.mu.Unlock()
	if raw == nil {
		return nil, fmt.Errorf("%w: no index in %s", ErrNotMirrored, m.Dir)
	}
	if status == "" || status == "all" {
		return raw, nil
	}
	var idx map[string]json.RawMessage
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	var ins []json.RawMessage
	_ = json.Unmarshal(idx["instances"], &ins)
	kept := make([]json.RawMessage, 0, len(ins))
	for _, e := range ins {
		var in SpecInstance
		if json.Unmarshal(e, &in) == nil && in.Status != "" && in.Status != status {
			continue
		}
		kept = append(kept, e)
	}
	idx["instances"], _ = json.Marshal(kept)
	return json.Marshal(idx)
}
