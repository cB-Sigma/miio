// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Firmware update over the LAN: miIO.ota tells a device to download a
// firmware image from a URL (one the device can reach, usually an HTTP
// server on the caller's machine) and install it after checking its MD5;
// miIO.get_ota_progress answers the download progress in per cent and
// miIO.get_ota_state one of "idle", "downloading", "installing" or
// "failed". The image must be one the vendor made for that model: a
// device refuses or, worse, breaks on anything else.

// OTA states.
const (
	OTAIdle        = "idle"
	OTADownloading = "downloading"
	OTAInstalling  = "installing"
	OTAFailed      = "failed"
)

// ErrOTAFailed means the device reported the update as failed.
var ErrOTAFailed = errors.New("the device reports the firmware update as failed")

// Update asks the device to download the image at url, check it against
// md5Hex (32 hex digits) and install it.
func Update(ctx context.Context, c *Client, url, md5Hex string) error {
	if b, err := hex.DecodeString(md5Hex); err != nil || len(b) != 16 {
		return fmt.Errorf("%w: the MD5 must be 32 hex digits", ErrValue)
	}
	return okAnswer(c.Call(ctx, "miIO.ota", map[string]any{
		"mode": "normal", "install": "1", "app_url": url, "file_md5": md5Hex, "proc": "dnld install",
	}))
}

// UpdateProgress reads the download progress in per cent.
func UpdateProgress(ctx context.Context, c *Client) (int, error) {
	raw, err := c.Call(ctx, "miIO.get_ota_progress", nil)
	if err != nil {
		return 0, err
	}
	var v []any
	if json.Unmarshal(raw, &v) != nil || len(v) == 0 {
		return 0, fmt.Errorf("the device answered miIO.get_ota_progress with %s", raw)
	}
	return int(asFloat(v[0])), nil
}

// UpdateState reads the update state (OTAIdle, OTADownloading,
// OTAInstalling, OTAFailed, or another word a device uses).
func UpdateState(ctx context.Context, c *Client) (string, error) {
	raw, err := c.Call(ctx, "miIO.get_ota_state", nil)
	if err != nil {
		return "", err
	}
	var v []any
	if json.Unmarshal(raw, &v) != nil || len(v) == 0 {
		return "", fmt.Errorf("the device answered miIO.get_ota_state with %s", raw)
	}
	s, _ := v[0].(string)
	return s, nil
}

// WaitUpdate polls the state every interval until the device is idle again
// after an update (nil), reports failure (ErrOTAFailed) or ctx ends. A
// device that restarts to install stops answering for a while; such
// silence is waited out. progress, when not nil, receives each reading.
func WaitUpdate(ctx context.Context, c *Client, interval time.Duration, progress func(state string, pct int)) error {
	busy := false
	for {
		st, err := UpdateState(ctx, c)
		if err == nil {
			pct, _ := UpdateProgress(ctx, c)
			if progress != nil {
				progress(st, pct)
			}
			switch st {
			case OTAFailed:
				return ErrOTAFailed
			case OTAIdle:
				if busy || pct >= 100 {
					return nil
				}
			default:
				busy = true
			}
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("%w (last: %v)", ctx.Err(), err)
			}
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
