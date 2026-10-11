// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MIoT specifications come from Xiaomi's public specification service
// (miot-spec.org): /instances?status=released lists every model with the
// type URN of each version, /instance?type=<URN> gives one. Xiaomi states
// no licence for them, so none is bundled here: HTTPSpecs fetches them,
// SpecMirror keeps them in a directory, and keeping them is the caller's
// choice.

// SpecSource gives a model's MIoT specification.
type SpecSource interface {
	Spec(ctx context.Context, model string) (*Spec, error)
}

// DefaultSpecURL is miot-spec.org's specification service.
const DefaultSpecURL = "https://miot-spec.org/miot-spec-v2"

// SpecMirrorURL is a second public host of the same service, for
// HTTPSpecs.Fallbacks.
const SpecMirrorURL = "https://spec.miot-spec.com/miot-spec-v2"

// ErrNoSpec means no specification of the model could be found.
var ErrNoSpec = errors.New("no MIoT specification of the model")

// SpecInstance is one entry of the service's index: a version of a
// model's specification.
type SpecInstance struct {
	Model   string `json:"model"`
	Version int    `json:"version"`
	Type    string `json:"type"`             // the specification's URN
	Status  string `json:"status,omitempty"` // "released" and others; only in the full index
	TS      int64  `json:"ts,omitempty"`
}

// HTTPSpecs fetches specifications from a specification service.
type HTTPSpecs struct {
	Base string
	HTTP *http.Client
	// Fallbacks are further bases tried in order when Base cannot be
	// reached or answers a server error (such as SpecMirrorURL).
	Fallbacks []string
	// Status is the index asked for: "released" when empty, or "all" for
	// every version (a released version is then still preferred).
	Status string

	mu      sync.Mutex
	index   map[string]string // model → type URN of its newest version
	indexAt time.Time
}

func (s *HTTPSpecs) client() *http.Client {
	if s.HTTP == nil {
		return http.DefaultClient
	}
	return s.HTTP
}

// fetch gets path from base, which may end in /miot-spec-v2 or not; root
// paths (translations) start from the host part.
func (s *HTTPSpecs) fetchFrom(ctx context.Context, base, path string, root bool) ([]byte, int, error) {
	b := strings.TrimRight(base, "/")
	if root {
		b = strings.TrimSuffix(b, "/miot-spec-v2")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b+path, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("the specification service answered HTTP %d", resp.StatusCode)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return out, resp.StatusCode, err
}

// getRaw tries Base, then each fallback while the answer is no answer or
// a server error.
func (s *HTTPSpecs) getRaw(ctx context.Context, path string, root bool) ([]byte, error) {
	var lastErr error
	for i, base := range append([]string{s.Base}, s.Fallbacks...) {
		if i > 0 && base == "" {
			continue
		}
		b, code, err := s.fetchFrom(ctx, base, path, root)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if ctx.Err() != nil || (code != 0 && code < 500) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (s *HTTPSpecs) get(ctx context.Context, path string, out any) error {
	b, err := s.getRaw(ctx, path, false)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (s *HTTPSpecs) status() string {
	if s.Status == "" {
		return "released"
	}
	return s.Status
}

// RawIndex returns the service's index as it answered it.
func (s *HTTPSpecs) RawIndex(ctx context.Context) ([]byte, error) {
	return s.getRaw(ctx, "/instances?status="+s.status(), false)
}

// Index returns the service's index.
func (s *HTTPSpecs) Index(ctx context.Context) ([]SpecInstance, error) {
	b, err := s.RawIndex(ctx)
	if err != nil {
		return nil, err
	}
	return parseIndex(b)
}

func parseIndex(b []byte) ([]SpecInstance, error) {
	var idx struct {
		Instances []SpecInstance `json:"instances"`
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("the specification index is not JSON: %w", err)
	}
	return idx.Instances, nil
}

// newestByModel picks each model's newest version, a released one before
// any other when the index says which are released.
func newestByModel(ins []SpecInstance) map[string]string {
	type best struct {
		ver      int
		released bool
		urn      string
	}
	m := map[string]best{}
	for _, in := range ins {
		rel := in.Status == "" || in.Status == "released"
		b, seen := m[in.Model]
		if !seen || (rel && !b.released) || (rel == b.released && in.Version >= b.ver) {
			m[in.Model] = best{in.Version, rel, in.Type}
		}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.urn
	}
	return out
}

// RawInstance returns the specification with this URN as the service
// answered it.
func (s *HTTPSpecs) RawInstance(ctx context.Context, urn string) ([]byte, error) {
	return s.getRaw(ctx, "/instance?type="+urn, false)
}

// RawTranslations returns the texts of the specification with this URN
// in other languages, as the service answered them
// (/instance/v2/multiLanguage, beside /miot-spec-v2 on the same host).
func (s *HTTPSpecs) RawTranslations(ctx context.Context, urn string) ([]byte, error) {
	return s.getRaw(ctx, "/instance/v2/multiLanguage?urn="+urn, true)
}

// Translations returns the texts of the specification with this URN in
// other languages.
func (s *HTTPSpecs) Translations(ctx context.Context, urn string) (Translations, error) {
	b, err := s.RawTranslations(ctx, urn)
	if err != nil {
		return nil, err
	}
	return ParseTranslations(b)
}

// Spec implements SpecSource.
func (s *HTTPSpecs) Spec(ctx context.Context, model string) (*Spec, error) {
	s.mu.Lock()
	stale := s.index == nil || time.Since(s.indexAt) > 24*time.Hour
	s.mu.Unlock()
	if stale {
		ins, err := s.Index(ctx)
		if err != nil {
			return nil, err
		}
		m := newestByModel(ins)
		s.mu.Lock()
		s.index, s.indexAt = m, time.Now()
		s.mu.Unlock()
	}
	s.mu.Lock()
	urn := s.index[model]
	s.mu.Unlock()
	if urn == "" {
		return nil, fmt.Errorf("%w %s at %s", ErrNoSpec, model, s.Base)
	}
	b, err := s.RawInstance(ctx, urn)
	if err != nil {
		return nil, err
	}
	return parseSpec(b, model)
}

func parseSpec(b []byte, model string) (*Spec, error) {
	var sp Spec
	if err := json.Unmarshal(b, &sp); err != nil {
		return nil, err
	}
	if len(sp.Services) == 0 {
		return nil, fmt.Errorf("%w %s: the specification has no services", ErrNoSpec, model)
	}
	return &sp, nil
}

// Translations are a specification's texts in other languages: language
// (such as "zh_cn" or "en") → text key → text. The keys name a service, a
// property, a property's value-list entry or an action by their IIDs:
// TextKey builds them.
type Translations map[string]map[string]string

// ParseTranslations reads the service's answer to a translations request
// ({"type": ..., "data": {language: {key: text}}}).
func ParseTranslations(b []byte) (Translations, error) {
	var t struct {
		Data Translations `json:"data"`
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("the translations are not JSON: %w", err)
	}
	if t.Data == nil {
		t.Data = Translations{}
	}
	return t.Data, nil
}

// TextKey is the translation key of a service (piid, aiid and value all
// below 0), of one of its properties (piid ≥ 0), of a value of that
// property's value list (value ≥ 0: its position in the list, from 0), or
// of an action (aiid ≥ 0).
func TextKey(siid, piid, aiid, value int) string {
	k := fmt.Sprintf("service:%03d", siid)
	switch {
	case aiid >= 0:
		return k + fmt.Sprintf(":action:%03d", aiid)
	case piid >= 0:
		k += fmt.Sprintf(":property:%03d", piid)
		if value >= 0 {
			k += fmt.Sprintf(":valuelist:%03d", value)
		}
	}
	return k
}

// Text returns the text of key in lang, or "" when there is none.
func (t Translations) Text(lang, key string) string { return t[lang][key] }
