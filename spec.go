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
// and keeping them is the caller's choice.

// SpecSource gives a model's MIoT specification.
type SpecSource interface {
	Spec(ctx context.Context, model string) (*Spec, error)
}

// DefaultSpecURL is miot-spec.org's specification service.
const DefaultSpecURL = "https://miot-spec.org/miot-spec-v2"

// ErrNoSpec means no specification of the model could be found.
var ErrNoSpec = errors.New("no MIoT specification of the model")

// HTTPSpecs fetches specifications from a specification service.
type HTTPSpecs struct {
	Base string
	HTTP *http.Client

	mu      sync.Mutex
	index   map[string]string // model → type URN of its newest version
	indexAt time.Time
}

func (s *HTTPSpecs) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.Base, "/")+path, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the specification service answered HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// Spec implements SpecSource.
func (s *HTTPSpecs) Spec(ctx context.Context, model string) (*Spec, error) {
	s.mu.Lock()
	stale := s.index == nil || time.Since(s.indexAt) > 24*time.Hour
	s.mu.Unlock()
	if stale {
		var idx struct {
			Instances []struct {
				Model   string `json:"model"`
				Version int    `json:"version"`
				Type    string `json:"type"`
			} `json:"instances"`
		}
		if err := s.get(ctx, "/instances?status=released", &idx); err != nil {
			return nil, err
		}
		m := map[string]string{}
		ver := map[string]int{}
		for _, in := range idx.Instances {
			if in.Version >= ver[in.Model] {
				m[in.Model], ver[in.Model] = in.Type, in.Version
			}
		}
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
	var sp Spec
	if err := s.get(ctx, "/instance?type="+urn, &sp); err != nil {
		return nil, err
	}
	if len(sp.Services) == 0 {
		return nil, fmt.Errorf("%w %s: the specification has no services", ErrNoSpec, model)
	}
	return &sp, nil
}
