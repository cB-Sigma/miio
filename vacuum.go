// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// More of a vacuum's functions. Most are the legacy Roborock methods
// (roborock.vacuum.* and rockrobo.vacuum.* models without a Model);
// a few also work through a Model by the standard MIoT names. Without
// either, a function answers ErrUnsupported.

// object reads an answer that is one object, or a list whose first entry
// is one (Roborock wraps most answers in a list).
func object(raw json.RawMessage, method string) (map[string]any, error) {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		return obj, nil
	}
	var list []map[string]any
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 && list[0] != nil {
		return list[0], nil
	}
	return nil, fmt.Errorf("the vacuum answered %s with %s", method, raw)
}

// scalar reads an answer that is a number, or a list starting with one.
func scalar(raw json.RawMessage, method string) (float64, error) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, nil
	}
	var list []any
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		if f, ok := number(list[0]); ok {
			return f, nil
		}
	}
	return 0, fmt.Errorf("the vacuum answered %s with %s", method, raw)
}

func seconds(v any) time.Duration { return time.Duration(asFloat(v) * float64(time.Second)) }

// Consumable names a part whose use the vacuum counts.
type Consumable string

// The parts reset_consumable takes; each is also the key of its counter in
// get_consumable's answer.
const (
	MainBrush     Consumable = "main_brush_work_time"
	SideBrush     Consumable = "side_brush_work_time"
	Filter        Consumable = "filter_work_time"
	Sensors       Consumable = "sensor_dirty_time"
	CleaningBrush Consumable = "cleaning_brush_work_times" // a counter of uses, on docks that wash
	Strainer      Consumable = "strainer_work_times"       // a counter of uses, on docks that wash
)

// Consumables are how long each part has been used since its last
// reset.
type Consumables struct {
	MainBrush, SideBrush, Filter, Sensors time.Duration
	// Raw is the whole answer, with counters of models that have more
	// parts.
	Raw map[string]any
}

func (v *Vacuum) legacyOnly() error {
	if v.Model != nil {
		return ErrUnsupported
	}
	return nil
}

// Consumables reads the use of the brushes, filter and sensors (legacy).
func (v *Vacuum) Consumables(ctx context.Context) (Consumables, error) {
	var c Consumables
	if err := v.legacyOnly(); err != nil {
		return c, err
	}
	raw, err := v.legacy(ctx, "get_consumable")
	if err != nil {
		return c, err
	}
	o, err := object(raw, "get_consumable")
	if err != nil {
		return c, err
	}
	c.MainBrush, c.SideBrush = seconds(o[string(MainBrush)]), seconds(o[string(SideBrush)])
	c.Filter, c.Sensors = seconds(o[string(Filter)]), seconds(o[string(Sensors)])
	c.Raw = o
	return c, nil
}

// ResetConsumable sets a part's counter back to zero after it was
// replaced or cleaned (legacy).
func (v *Vacuum) ResetConsumable(ctx context.Context, part Consumable) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	_, err := v.legacy(ctx, "reset_consumable", string(part))
	return err
}

// CleanSummary is the vacuum's cleaning history in total.
type CleanSummary struct {
	Duration time.Duration
	Area     float64 // m²
	Count    int
	// Records are the IDs of the stored runs (start times in Unix
	// seconds), newest first, for CleanRecord.
	Records []int64
}

// The vacuum counts area in mm².
const mm2PerM2 = 1e6

func int64s(v any) []int64 {
	list, _ := v.([]any)
	out := make([]int64, 0, len(list))
	for _, x := range list {
		if f, ok := number(x); ok {
			out = append(out, int64(f))
		}
	}
	return out
}

// CleanSummary reads the cleaning history (legacy). Older firmware
// answers a list (time, area, count, record IDs), newer an object with
// the same named fields; both are read.
func (v *Vacuum) CleanSummary(ctx context.Context) (CleanSummary, error) {
	var s CleanSummary
	if err := v.legacyOnly(); err != nil {
		return s, err
	}
	raw, err := v.legacy(ctx, "get_clean_summary")
	if err != nil {
		return s, err
	}
	var list []any
	var obj map[string]any
	switch {
	case json.Unmarshal(raw, &list) == nil && len(list) >= 3:
		s.Duration, s.Area, s.Count = seconds(list[0]), asFloat(list[1])/mm2PerM2, int(asFloat(list[2]))
		if len(list) > 3 {
			s.Records = int64s(list[3])
		}
	case json.Unmarshal(raw, &obj) == nil && obj != nil:
		s.Duration, s.Area, s.Count = seconds(obj["clean_time"]), asFloat(obj["clean_area"])/mm2PerM2, int(asFloat(obj["clean_count"]))
		s.Records = int64s(obj["records"])
	default:
		return s, fmt.Errorf("the vacuum answered get_clean_summary with %s", raw)
	}
	return s, nil
}

// CleanRecord is one cleaning run.
type CleanRecord struct {
	Start, End time.Time
	Duration   time.Duration
	Area       float64 // m²
	Error      int     // 0: none
	Complete   bool
}

// CleanRecord reads one run by its ID from CleanSummary.Records (legacy).
// Older firmware answers a list (start, end, duration, area, error,
// complete), newer an object; both are read.
func (v *Vacuum) CleanRecord(ctx context.Context, id int64) (CleanRecord, error) {
	var r CleanRecord
	if err := v.legacyOnly(); err != nil {
		return r, err
	}
	raw, err := v.legacy(ctx, "get_clean_record", id)
	if err != nil {
		return r, err
	}
	var lists [][]any
	var objs []map[string]any
	switch {
	case json.Unmarshal(raw, &lists) == nil && len(lists) > 0 && len(lists[len(lists)-1]) >= 6:
		l := lists[len(lists)-1]
		r = CleanRecord{Start: time.Unix(int64(asFloat(l[0])), 0), End: time.Unix(int64(asFloat(l[1])), 0),
			Duration: seconds(l[2]), Area: asFloat(l[3]) / mm2PerM2, Error: int(asFloat(l[4])), Complete: asFloat(l[5]) == 1}
	case json.Unmarshal(raw, &objs) == nil && len(objs) > 0 && objs[len(objs)-1] != nil:
		o := objs[len(objs)-1]
		r = CleanRecord{Start: time.Unix(int64(asFloat(o["begin"])), 0), End: time.Unix(int64(asFloat(o["end"])), 0),
			Duration: seconds(o["duration"]), Area: asFloat(o["area"]) / mm2PerM2, Error: int(asFloat(o["error"])), Complete: asFloat(o["complete"]) == 1}
	default:
		return r, fmt.Errorf("the vacuum has no cleaning record %d (it answered %s)", id, raw)
	}
	return r, nil
}

// DND is the vacuum's do-not-disturb period, in its own time zone.
type DND struct {
	Enabled                                    bool
	StartHour, StartMinute, EndHour, EndMinute int
}

// DND reads the do-not-disturb period (legacy).
func (v *Vacuum) DND(ctx context.Context) (DND, error) {
	var d DND
	if err := v.legacyOnly(); err != nil {
		return d, err
	}
	raw, err := v.legacy(ctx, "get_dnd_timer")
	if err != nil {
		return d, err
	}
	o, err := object(raw, "get_dnd_timer")
	if err != nil {
		return d, err
	}
	return DND{Enabled: asFloat(o["enabled"]) == 1, StartHour: int(asFloat(o["start_hour"])), StartMinute: int(asFloat(o["start_minute"])),
		EndHour: int(asFloat(o["end_hour"])), EndMinute: int(asFloat(o["end_minute"]))}, nil
}

// SetDND sets and enables the do-not-disturb period (legacy).
func (v *Vacuum) SetDND(ctx context.Context, startHour, startMinute, endHour, endMinute int) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	for _, x := range [][3]int{{startHour, 0, 23}, {startMinute, 0, 59}, {endHour, 0, 23}, {endMinute, 0, 59}} {
		if x[0] < x[1] || x[0] > x[2] {
			return fmt.Errorf("the do-not-disturb time %d is outside %d-%d", x[0], x[1], x[2])
		}
	}
	_, err := v.legacy(ctx, "set_dnd_timer", startHour, startMinute, endHour, endMinute)
	return err
}

// DisableDND turns the do-not-disturb period off (legacy).
func (v *Vacuum) DisableDND(ctx context.Context) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	_, err := v.legacy(ctx, "close_dnd_timer", "")
	return err
}

// VacuumTimer is a scheduled cleaning.
type VacuumTimer struct {
	ID      string // the creation time in Unix milliseconds, as text
	Enabled bool
	// Cron is the schedule, "minute hour day-of-month month day-of-week"
	// in the vacuum's time zone (VacuumTimezone).
	Cron string
	// Action is what runs, as the vacuum reports it (such as
	// "start_clean"), with its parameters.
	Action string
	Params any
}

// Timers lists the scheduled cleanings (legacy).
func (v *Vacuum) Timers(ctx context.Context) ([]VacuumTimer, error) {
	if err := v.legacyOnly(); err != nil {
		return nil, err
	}
	raw, err := v.legacy(ctx, "get_timer", "")
	if err != nil {
		return nil, err
	}
	var recs [][]any
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil, fmt.Errorf("the vacuum answered get_timer with %s", raw)
	}
	out := make([]VacuumTimer, 0, len(recs))
	for _, r := range recs {
		if len(r) < 3 {
			continue
		}
		t := VacuumTimer{Enabled: r[1] == "on"}
		switch id := r[0].(type) {
		case string:
			t.ID = id
		case float64:
			t.ID = strconv.FormatInt(int64(id), 10)
		}
		if sched, ok := r[2].([]any); ok && len(sched) > 0 {
			t.Cron, _ = sched[0].(string)
			if len(sched) > 1 {
				if act, ok := sched[1].([]any); ok && len(act) > 0 {
					t.Action, _ = act[0].(string)
					if len(act) > 1 {
						t.Params = act[1]
					}
				}
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// AddTimer schedules a cleaning by a cron schedule in the vacuum's time
// zone (legacy) and returns its ID; id "" takes the current time in
// Unix milliseconds, as the vendor app does. The vacuum adds it enabled
// on most firmware.
func (v *Vacuum) AddTimer(ctx context.Context, id, cron string) (string, error) {
	if err := v.legacyOnly(); err != nil {
		return "", err
	}
	if id == "" {
		id = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	_, err := v.legacy(ctx, "set_timer", []any{id, []any{cron, []any{"", ""}}})
	return id, err
}

// EnableTimer turns a scheduled cleaning on or off (legacy).
func (v *Vacuum) EnableTimer(ctx context.Context, id string, on bool) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	s := "off"
	if on {
		s = "on"
	}
	_, err := v.legacy(ctx, "upd_timer", id, s)
	return err
}

// DeleteTimer removes a scheduled cleaning (legacy).
func (v *Vacuum) DeleteTimer(ctx context.Context, id string) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	_, err := v.legacy(ctx, "del_timer", id)
	return err
}

// Timezone reads the vacuum's time zone, such as "Asia/Kolkata" (legacy);
// timers and the do-not-disturb period follow it.
func (v *Vacuum) Timezone(ctx context.Context) (string, error) {
	if err := v.legacyOnly(); err != nil {
		return "", err
	}
	raw, err := v.legacy(ctx, "get_timezone")
	if err != nil {
		return "", err
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0], nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	return "", fmt.Errorf("the vacuum answered get_timezone with %s", raw)
}

// Volume reads the voice volume in per cent (legacy).
func (v *Vacuum) Volume(ctx context.Context) (int, error) {
	if err := v.legacyOnly(); err != nil {
		return 0, err
	}
	raw, err := v.legacy(ctx, "get_sound_volume")
	if err != nil {
		return 0, err
	}
	f, err := scalar(raw, "get_sound_volume")
	return int(f), err
}

// SetVolume sets the voice volume in per cent (legacy).
func (v *Vacuum) SetVolume(ctx context.Context, pct int) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	if pct < 0 || pct > 100 {
		return fmt.Errorf("volume %d is outside 0-100", pct)
	}
	_, err := v.legacy(ctx, "change_sound_volume", pct)
	return err
}

// TestVolume plays a sound at the current volume (legacy).
func (v *Vacuum) TestVolume(ctx context.Context) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	_, err := v.legacy(ctx, "test_sound_volume")
	return err
}

// fanNames are the MIoT names of a vacuum's suction setting.
var fanNames = []string{"vacuum.fan-level", "vacuum.suction-level", "vacuum.mode", "fan-level", "mode"}

// FanSpeed reads the suction setting: the legacy custom mode (a per cent
// on early models, a preset number on later ones), or the MIoT fan
// level's value.
func (v *Vacuum) FanSpeed(ctx context.Context) (int, error) {
	if v.Model == nil {
		raw, err := v.legacy(ctx, "get_custom_mode")
		if err != nil {
			return 0, err
		}
		f, err := scalar(raw, "get_custom_mode")
		return int(f), err
	}
	n, ok := v.prop(fanNames...)
	if !ok {
		return 0, ErrUnsupported
	}
	got, err := v.Model.Get(ctx, v.Client, v.DID, n)
	if err != nil {
		return 0, err
	}
	return int(asFloat(got[n])), nil
}

// SetFanSpeed sets the suction setting: a number FanSpeed reads or
// FanSpeedPresets lists.
func (v *Vacuum) SetFanSpeed(ctx context.Context, speed int) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "set_custom_mode", speed)
		return err
	}
	return v.set(ctx, speed, fanNames...)
}

// FanSpeedPresets names the suction settings of a MIoT vacuum, from its
// specification's value list (description → value). Legacy models take
// numbers that differ by model and firmware; this module bundles no
// table of them, so it answers ErrUnsupported there: read the current
// one with FanSpeed.
func (v *Vacuum) FanSpeedPresets() (map[string]int, error) {
	if v.Model == nil {
		return nil, ErrUnsupported
	}
	n, ok := v.prop(fanNames...)
	if !ok {
		return nil, ErrUnsupported
	}
	p, _ := v.Model.Property(n)
	if len(p.ValueList) == 0 {
		return nil, ErrUnsupported
	}
	out := map[string]int{}
	for _, it := range p.ValueList {
		out[it.Description] = it.Value
	}
	return out, nil
}

// CarpetMode is the vacuum's carpet detection: when on, it raises suction
// on carpet, detected by the brush motor's current crossing the
// thresholds (the units are the firmware's own).
type CarpetMode struct {
	Enabled                                             bool
	StallTime, CurrentLow, CurrentHigh, CurrentIntegral int
}

// CarpetMode reads the carpet detection settings (legacy).
func (v *Vacuum) CarpetMode(ctx context.Context) (CarpetMode, error) {
	var c CarpetMode
	if err := v.legacyOnly(); err != nil {
		return c, err
	}
	raw, err := v.legacy(ctx, "get_carpet_mode")
	if err != nil {
		return c, err
	}
	o, err := object(raw, "get_carpet_mode")
	if err != nil {
		return c, err
	}
	return CarpetMode{Enabled: asFloat(o["enable"]) == 1, StallTime: int(asFloat(o["stall_time"])), CurrentLow: int(asFloat(o["current_low"])),
		CurrentHigh: int(asFloat(o["current_high"])), CurrentIntegral: int(asFloat(o["current_integral"]))}, nil
}

// SetCarpetMode turns carpet detection on or off, keeping the vacuum's
// thresholds (it reads them first) (legacy).
func (v *Vacuum) SetCarpetMode(ctx context.Context, on bool) error {
	c, err := v.CarpetMode(ctx)
	if err != nil {
		return err
	}
	c.Enabled = on
	return v.SetCarpetModeSettings(ctx, c)
}

// SetCarpetModeSettings writes all carpet detection settings (legacy).
func (v *Vacuum) SetCarpetModeSettings(ctx context.Context, c CarpetMode) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	en := 0
	if c.Enabled {
		en = 1
	}
	_, err := v.legacy(ctx, "set_carpet_mode", map[string]any{"enable": en, "stall_time": c.StallTime,
		"current_low": c.CurrentLow, "current_high": c.CurrentHigh, "current_integral": c.CurrentIntegral})
	return err
}

// Find makes the vacuum say where it is: the legacy find_me, or the MIoT
// identify action.
func (v *Vacuum) Find(ctx context.Context) error {
	if v.Model == nil {
		_, err := v.legacy(ctx, "find_me", "")
		return err
	}
	return v.do(ctx, "identify.identify", "identify")
}

// Spot cleans a small area around the vacuum (legacy).
func (v *Vacuum) Spot(ctx context.Context) error {
	if err := v.legacyOnly(); err != nil {
		return err
	}
	_, err := v.legacy(ctx, "app_spot")
	return err
}
