// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MIoT is the request set of newer Xiaomi devices, carried by the same
// miIO packets: get_properties and set_properties name each value by its
// service ID (siid) and property ID (piid), and action runs an action by
// siid and action ID (aiid). What each ID means for a model is in the
// model's MIoT specification, which Xiaomi publishes at miot-spec.org as
// JSON: services with a type URN (urn:miot-spec-v2:service:<name>:...),
// each with properties (type URN, format, access, unit, value range or
// value list) and actions. HTTPSpecs fetches a model's specification.

// Spec is a model's MIoT specification, as miot-spec.org serves it.
type Spec struct {
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	Services    []Service `json:"services"`
}

// Service is one service of a specification.
type Service struct {
	IID         int        `json:"iid"`
	Type        string     `json:"type"`
	Description string     `json:"description,omitempty"`
	Properties  []Property `json:"properties,omitempty"`
	Actions     []Action   `json:"actions,omitempty"`
}

// Property is one property of a service.
type Property struct {
	IID         int         `json:"iid"`
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Format      string      `json:"format,omitempty"`
	Access      []string    `json:"access,omitempty"`
	Unit        string      `json:"unit,omitempty"`
	ValueRange  []float64   `json:"value-range,omitempty"`
	ValueList   []ValueItem `json:"value-list,omitempty"`
}

// ValueItem is one allowed value of an enumerated property.
type ValueItem struct {
	Value       int    `json:"value"`
	Description string `json:"description"`
}

// Action is one action of a service.
type Action struct {
	IID  int    `json:"iid"`
	Type string `json:"type"`
	In   []int  `json:"in,omitempty"`
}

// URNName is the name part of a MIoT type URN
// ("urn:miot-spec-v2:property:on:00000006:..." gives "on") and whether it
// is a standard (miot-spec-v2) one.
func URNName(urn string) (string, bool) {
	p := strings.Split(urn, ":")
	if len(p) < 4 {
		return "", false
	}
	return p[3], p[1] == "miot-spec-v2"
}

// Can reports whether the property allows an access: "read", "write" or
// "notify".
func (p Property) Can(a string) bool {
	for _, x := range p.Access {
		if x == a {
			return true
		}
	}
	return false
}

// PropRef names one property in a MIoT request.
type PropRef struct {
	DID   string `json:"did"`
	SIID  int    `json:"siid"`
	PIID  int    `json:"piid"`
	Value any    `json:"value,omitempty"`
	Code  int    `json:"code,omitempty"`
}

// PropResult is one property of a get_properties or set_properties
// answer, or the answer to an action. Code 0 is success; devices answer
// -4001 to -4005 and others for a property they lack, cannot write or
// cannot take the value of.
type PropResult struct {
	DID   string `json:"did"`
	SIID  int    `json:"siid"`
	PIID  int    `json:"piid"`
	Code  int    `json:"code"`
	Value any    `json:"value"`
}

// maxPropsPerRequest is how many properties one get_properties asks for:
// devices take a limited number per request.
const maxPropsPerRequest = 15

// GetProperties reads properties, in requests of at most 15, and returns
// one result per property in order. A property the device lacks has a
// non-zero Code.
func GetProperties(ctx context.Context, c *Client, refs []PropRef) ([]PropResult, error) {
	var all []PropResult
	for len(refs) > 0 {
		n := min(len(refs), maxPropsPerRequest)
		batch := refs[:n]
		refs = refs[n:]
		raw, err := c.Call(ctx, "get_properties", batch)
		if err != nil {
			return nil, err
		}
		var res []PropResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("the device answered get_properties with %s", raw)
		}
		all = append(all, res...)
	}
	return all, nil
}

// PropError is a set_properties or action answer with a non-zero code.
type PropError struct {
	SIID, IID int // IID is the property or action ID
	Code      int
	Action    bool
}

// Error tells which property or action was refused and the code.
func (e *PropError) Error() string {
	if e.Action {
		return fmt.Sprintf("the device refused the action: code %d", e.Code)
	}
	return fmt.Sprintf("the device refused siid %d piid %d: code %d", e.SIID, e.IID, e.Code)
}

// SetProperties writes properties (each PropRef with its Value) in one
// request. It returns the results, and a *PropError for the first
// property answered with a non-zero code.
func SetProperties(ctx context.Context, c *Client, refs []PropRef) ([]PropResult, error) {
	raw, err := c.Call(ctx, "set_properties", refs)
	if err != nil {
		return nil, err
	}
	return CheckSet(raw)
}

// CheckSet reads a set_properties answer and returns a *PropError for the
// first property answered with a non-zero code.
func CheckSet(raw json.RawMessage) ([]PropResult, error) {
	var list []PropResult
	if json.Unmarshal(raw, &list) != nil {
		return nil, nil
	}
	for _, r := range list {
		if r.Code != 0 {
			return list, &PropError{SIID: r.SIID, IID: r.PIID, Code: r.Code}
		}
	}
	return list, nil
}

// DoAction runs an action of a service with its input values.
func DoAction(ctx context.Context, c *Client, did string, siid, aiid int, in []any) error {
	if in == nil {
		in = []any{}
	}
	raw, err := c.Call(ctx, "action", map[string]any{"did": did, "siid": siid, "aiid": aiid, "in": in})
	if err != nil {
		return err
	}
	return CheckAction(raw)
}

// CheckAction reads an action answer and returns a *PropError when its
// code is not 0.
func CheckAction(raw json.RawMessage) error {
	var one struct {
		SIID int `json:"siid"`
		AIID int `json:"aiid"`
		Code int `json:"code"`
	}
	if json.Unmarshal(raw, &one) == nil && one.Code != 0 {
		return &PropError{SIID: one.SIID, IID: one.AIID, Code: one.Code, Action: true}
	}
	return nil
}
