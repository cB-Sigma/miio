// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// A Model is a MIoT specification turned into names: each property and
// action is reached as "<service>.<name>" from the names in their type
// URNs ("air-purifier.on", "environment.pm2.5-density",
// "vacuum.start-sweep"), so callers need not carry siid and piid numbers.
// When two services share a name, the later ones are "<service>-<siid>"
// ("switch-3.on"). A property or action name that occurs once in the
// whole specification also works alone ("pm2.5-density"), and
// "<siid>.<piid>" always works. Values are checked against the
// property's format, value range and value list before they are sent.
// Using a Model is optional: GetProperties, SetProperties and DoAction
// take numbers directly.

// Errors of the specification mapping.
var (
	// ErrUnknownName: the specification has no such property or action, or
	// the short name is shared by several.
	ErrUnknownName = errors.New("no such property or action in the MIoT specification")
	// ErrValue: a value does not fit the property's format, range or list.
	ErrValue = errors.New("the value does not fit the property")
	// ErrAccess: the property cannot be read or written.
	ErrAccess = errors.New("the property does not allow this access")
)

// MappedProperty is one property of a Model with its full name.
type MappedProperty struct {
	Name string // "<service>.<property>"
	SIID int
	Property
}

// MappedAction is one action of a Model with its full name.
type MappedAction struct {
	Name string // "<service>.<action>"
	SIID int
	Action
}

// Model maps a specification's names to IDs. Build it with NewModel; it is
// safe for concurrent use once built.
type Model struct {
	Spec *Spec

	props   map[string]MappedProperty
	actions map[string]MappedAction
	short   map[string][]string // short name → full names (properties)
	shortA  map[string][]string // short name → full names (actions)
	order   []string
	orderA  []string
}

// NewModel builds the mapping of a specification.
func NewModel(sp *Spec) (*Model, error) {
	if sp == nil || len(sp.Services) == 0 {
		return nil, fmt.Errorf("%w: the specification has no services", ErrNoSpec)
	}
	m := &Model{Spec: sp, props: map[string]MappedProperty{}, actions: map[string]MappedAction{},
		short: map[string][]string{}, shortA: map[string][]string{}}
	seen := map[string]bool{}
	for _, s := range sp.Services {
		sn, _ := URNName(s.Type)
		if sn == "" {
			sn = "service"
		}
		if seen[sn] {
			sn = sn + "-" + strconv.Itoa(s.IID)
		}
		seen[sn] = true
		for _, p := range s.Properties {
			pn, _ := URNName(p.Type)
			if pn == "" {
				pn = "property-" + strconv.Itoa(p.IID)
			}
			full := sn + "." + pn
			if _, dup := m.props[full]; dup {
				full = sn + "." + pn + "-" + strconv.Itoa(p.IID)
			}
			m.props[full] = MappedProperty{Name: full, SIID: s.IID, Property: p}
			m.short[pn] = append(m.short[pn], full)
			m.order = append(m.order, full)
		}
		for _, a := range s.Actions {
			an, _ := URNName(a.Type)
			if an == "" {
				an = "action-" + strconv.Itoa(a.IID)
			}
			full := sn + "." + an
			if _, dup := m.actions[full]; dup {
				full = sn + "." + an + "-" + strconv.Itoa(a.IID)
			}
			m.actions[full] = MappedAction{Name: full, SIID: s.IID, Action: a}
			m.shortA[an] = append(m.shortA[an], full)
			m.orderA = append(m.orderA, full)
		}
	}
	return m, nil
}

// ModelFor fetches a model's specification and maps it.
func ModelFor(ctx context.Context, specs SpecSource, model string) (*Model, error) {
	sp, err := specs.Spec(ctx, model)
	if err != nil {
		return nil, err
	}
	return NewModel(sp)
}

// Properties lists the properties in specification order.
func (m *Model) Properties() []MappedProperty {
	out := make([]MappedProperty, 0, len(m.order))
	for _, n := range m.order {
		out = append(out, m.props[n])
	}
	return out
}

// Actions lists the actions in specification order.
func (m *Model) Actions() []MappedAction {
	out := make([]MappedAction, 0, len(m.orderA))
	for _, n := range m.orderA {
		out = append(out, m.actions[n])
	}
	return out
}

// Property finds a property by full name, unique short name or
// "<siid>.<piid>".
func (m *Model) Property(name string) (MappedProperty, error) {
	if p, ok := m.props[name]; ok {
		return p, nil
	}
	if l := m.short[name]; len(l) == 1 {
		return m.props[l[0]], nil
	} else if len(l) > 1 {
		return MappedProperty{}, fmt.Errorf("%w: %q is one of %s", ErrUnknownName, name, strings.Join(l, ", "))
	}
	if s, p, ok := ids(name); ok {
		for _, n := range m.order {
			if x := m.props[n]; x.SIID == s && x.IID == p {
				return x, nil
			}
		}
	}
	return MappedProperty{}, fmt.Errorf("%w: property %q", ErrUnknownName, name)
}

// Action finds an action by full name, unique short name or
// "<siid>.<aiid>".
func (m *Model) Action(name string) (MappedAction, error) {
	if a, ok := m.actions[name]; ok {
		return a, nil
	}
	if l := m.shortA[name]; len(l) == 1 {
		return m.actions[l[0]], nil
	} else if len(l) > 1 {
		return MappedAction{}, fmt.Errorf("%w: %q is one of %s", ErrUnknownName, name, strings.Join(l, ", "))
	}
	if s, a, ok := ids(name); ok {
		for _, n := range m.orderA {
			if x := m.actions[n]; x.SIID == s && x.IID == a {
				return x, nil
			}
		}
	}
	return MappedAction{}, fmt.Errorf("%w: action %q", ErrUnknownName, name)
}

// Has reports whether a property or action of that name exists.
func (m *Model) Has(name string) bool {
	if _, err := m.Property(name); err == nil {
		return true
	}
	_, err := m.Action(name)
	return err == nil
}

func ids(s string) (int, int, bool) {
	a, b, ok := strings.Cut(s, ".")
	if !ok {
		return 0, 0, false
	}
	x, e1 := strconv.Atoi(a)
	y, e2 := strconv.Atoi(b)
	return x, y, e1 == nil && e2 == nil
}

// Get reads properties by name and returns their values by the names
// given. Values of enumerated properties stay numbers; Describe names
// them. A property the device refused is left out and reported as a
// *PropError (the first one) next to the values it did give.
func (m *Model) Get(ctx context.Context, c *Client, did string, names ...string) (map[string]any, error) {
	refs := make([]PropRef, 0, len(names))
	for _, n := range names {
		p, err := m.Property(n)
		if err != nil {
			return nil, err
		}
		if len(p.Access) > 0 && !p.Can("read") {
			return nil, fmt.Errorf("%w: %s cannot be read", ErrAccess, p.Name)
		}
		refs = append(refs, PropRef{DID: did, SIID: p.SIID, PIID: p.IID})
	}
	res, err := GetProperties(ctx, c, refs)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	var first error
	for i, r := range res {
		if i >= len(names) {
			break
		}
		if r.Code != 0 {
			if first == nil {
				first = &PropError{SIID: r.SIID, IID: r.PIID, Code: r.Code}
			}
			continue
		}
		out[names[i]] = r.Value
	}
	return out, first
}

// Set checks and writes one property.
func (m *Model) Set(ctx context.Context, c *Client, did, name string, v any) error {
	return m.SetMany(ctx, c, did, map[string]any{name: v})
}

// SetMany checks every value first and writes them in one request. A value
// for an enumerated property may be the number or its description (for
// example "Sleep" for a mode).
func (m *Model) SetMany(ctx context.Context, c *Client, did string, vals map[string]any) error {
	names := make([]string, 0, len(vals))
	for n := range vals {
		names = append(names, n)
	}
	sort.Strings(names)
	refs := make([]PropRef, 0, len(vals))
	for _, n := range names {
		p, err := m.Property(n)
		if err != nil {
			return err
		}
		if len(p.Access) > 0 && !p.Can("write") {
			return fmt.Errorf("%w: %s cannot be written", ErrAccess, p.Name)
		}
		v, err := p.Check(vals[n])
		if err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
		refs = append(refs, PropRef{DID: did, SIID: p.SIID, PIID: p.IID, Value: v})
	}
	_, err := SetProperties(ctx, c, refs)
	return err
}

// Do runs an action by name with its inputs, checking their number and,
// where the inputs are properties of the same service, their values.
func (m *Model) Do(ctx context.Context, c *Client, did, name string, in ...any) error {
	a, err := m.Action(name)
	if err != nil {
		return err
	}
	if len(in) != len(a.In) {
		return fmt.Errorf("%w: %s takes %d inputs, not %d", ErrValue, a.Name, len(a.In), len(in))
	}
	args := make([]any, len(in))
	for i, v := range in {
		args[i] = v
		for _, n := range m.order {
			if p := m.props[n]; p.SIID == a.SIID && p.IID == a.In[i] {
				if args[i], err = p.Check(v); err != nil {
					return fmt.Errorf("%s input %d: %w", a.Name, i+1, err)
				}
			}
		}
	}
	return DoAction(ctx, c, did, a.SIID, a.IID, args)
}

// Describe names a value of an enumerated property by its description
// ("" when the property has no value list or the value is not in it).
func (p Property) Describe(v any) string {
	f, ok := number(v)
	if !ok {
		return ""
	}
	for _, it := range p.ValueList {
		if float64(it.Value) == f {
			return it.Description
		}
	}
	return ""
}

// ValueOf finds a value of an enumerated property by its description,
// ignoring case, spaces, hyphens and underscores.
func (p Property) ValueOf(desc string) (int, bool) {
	k := squash(desc)
	for _, it := range p.ValueList {
		if squash(it.Description) == k {
			return it.Value, true
		}
	}
	return 0, false
}

func squash(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_':
			return -1
		}
		return r
	}, strings.ToLower(s))
}

// intRange is the range of each integer format.
var intRange = map[string][2]float64{
	"uint8": {0, math.MaxUint8}, "uint16": {0, math.MaxUint16}, "uint32": {0, math.MaxUint32},
	"int8": {math.MinInt8, math.MaxInt8}, "int16": {math.MinInt16, math.MaxInt16},
	"int32": {math.MinInt32, math.MaxInt32}, "int64": {math.MinInt64, math.MaxInt64},
}

// Check tests a value against the property's format, value range (min,
// max, step) and value list, and returns it in the form to send: bool,
// int64 for integer formats, float64 for float, string for string. It
// takes numbers of any Go kind, json.Number, "on"/"off" and "true"/"false"
// for bool, and a value-list description for an enumerated property.
func (p Property) Check(v any) (any, error) {
	switch p.Format {
	case "bool":
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(x) {
			case "true", "on", "1":
				return true, nil
			case "false", "off", "0":
				return false, nil
			}
		default:
			if f, ok := number(v); ok && (f == 0 || f == 1) {
				return f == 1, nil
			}
		}
		return nil, fmt.Errorf("%w: %v is not a bool", ErrValue, v)
	case "string":
		if s, ok := v.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("%w: %v is not a string", ErrValue, v)
	case "float":
		f, ok := number(v)
		if !ok {
			return nil, fmt.Errorf("%w: %v is not a number", ErrValue, v)
		}
		if err := p.inRange(f); err != nil {
			return nil, err
		}
		return f, nil
	}
	if lim, isInt := intRange[p.Format]; isInt {
		f, ok := number(v)
		if s, isStr := v.(string); !ok && isStr && len(p.ValueList) > 0 {
			if n, found := p.ValueOf(s); found {
				f, ok = float64(n), true
			}
		}
		if !ok {
			return nil, fmt.Errorf("%w: %v is not a number", ErrValue, v)
		}
		if f != math.Trunc(f) || f < lim[0] || f > lim[1] {
			return nil, fmt.Errorf("%w: %v is not a %s", ErrValue, v, p.Format)
		}
		if len(p.ValueList) > 0 {
			in := false
			for _, it := range p.ValueList {
				if float64(it.Value) == f {
					in = true
				}
			}
			if !in {
				return nil, fmt.Errorf("%w: %v is not in the value list", ErrValue, v)
			}
		}
		if err := p.inRange(f); err != nil {
			return nil, err
		}
		return int64(f), nil
	}
	// An unknown format (such as hex): passed on as given.
	return v, nil
}

func (p Property) inRange(f float64) error {
	if len(p.ValueRange) < 2 {
		return nil
	}
	lo, hi := p.ValueRange[0], p.ValueRange[1]
	if f < lo || f > hi {
		return fmt.Errorf("%w: %v is outside %v to %v", ErrValue, f, lo, hi)
	}
	if len(p.ValueRange) > 2 && p.ValueRange[2] > 0 {
		steps := (f - lo) / p.ValueRange[2]
		if math.Abs(steps-math.Round(steps)) > 1e-6 {
			return fmt.Errorf("%w: %v is not a step of %v from %v", ErrValue, f, p.ValueRange[2], lo)
		}
	}
	return nil
}

// number reads any Go number or json.Number.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}
