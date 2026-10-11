// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Completing a verification the Mi account asks for. Two kinds occur at
// sign-in (step 2), and both need a person:
//
//   - a captcha: the answer has a captchaUrl; Captcha fetches the image
//     (which also sets the "ick" cookie), a person reads it into
//     CaptchaCode, and Login is called again (a wrong text answers code
//     87001, another captcha);
//   - an identity check: the answer has a notificationUrl whose "context"
//     parameter names the check. SendCode opens it, lists the ways the
//     account offers (identity/list) and asks for a code by e-mail
//     (identity/auth/sendEmailTicket) or SMS (sendPhoneTicket); VerifyCode
//     posts the code (identity/auth/verifyEmail with _flag 8, verifyPhone
//     with _flag 4) and follows the finish address (identity/result/check,
//     then serviceLoginAuth2/end, whose extension-pragma header carries
//     ssecurity, then the sts address that sets the serviceToken cookie).
//
// This follows public descriptions of open-source tools (PROVENANCE.md)
// and is tested against miio-sim's account only, not a real Mi account.

// VerifyError is ErrCloudVerify with what the account asked for.
type VerifyError struct {
	// CaptchaURL is set when a captcha must be read.
	CaptchaURL string
	// WrongCaptcha: the CaptchaCode sent was refused.
	WrongCaptcha bool
	// NotificationURL is set when a code must be sent and entered.
	NotificationURL string
}

func (e *VerifyError) Error() string {
	switch {
	case e.WrongCaptcha:
		return ErrCloudVerify.Error() + ": the captcha text was wrong"
	case e.CaptchaURL != "":
		return ErrCloudVerify.Error() + ": a captcha"
	}
	return ErrCloudVerify.Error() + ": a code sent by e-mail or SMS"
}

// Unwrap makes errors.Is(err, ErrCloudVerify) true.
func (e *VerifyError) Unwrap() error { return ErrCloudVerify }

// Verification ways for SendCode and VerifyCode.
const (
	ByEmail = "email"
	ByPhone = "phone"
)

func (c *Cloud) absURL(u string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(c.AccountBase, "/") + "/" + strings.TrimLeft(u, "/")
}

// Captcha fetches the captcha image the last Login was asked for.
func (c *Cloud) Captcha(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.verify == nil || c.verify.CaptchaURL == "" {
		return nil, errors.New("the Mi account has not asked for a captcha")
	}
	b, resp, err := c.do(ctx, true, http.MethodGet, c.verify.CaptchaURL, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the captcha address answered HTTP %d", resp.StatusCode)
	}
	return b, nil
}

func (c *Cloud) verifyContext() (string, error) {
	if c.verify == nil || c.verify.NotificationURL == "" {
		return "", errors.New("the Mi account has not asked for a code")
	}
	u, err := url.Parse(c.verify.NotificationURL)
	if err != nil {
		return "", err
	}
	return u.Query().Get("context"), nil
}

func (c *Cloud) cookie(base, name string) string {
	u, err := url.Parse(base)
	if err != nil || c.jar == nil {
		return ""
	}
	for _, ck := range c.jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

// SendCode asks the account to send a verification code by e-mail
// (ByEmail) or SMS (ByPhone), after a Login that answered a VerifyError
// with a NotificationURL.
func (c *Cloud) SendCode(ctx context.Context, by string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cx, err := c.verifyContext()
	if err != nil {
		return err
	}
	if _, _, err := c.do(ctx, true, http.MethodGet, c.verify.NotificationURL, nil); err != nil {
		return fmt.Errorf("Mi account verification: %w", err)
	}
	base := strings.TrimRight(c.AccountBase, "/")
	lq := url.Values{"sid": {"xiaomiio"}, "context": {cx}, "_locale": {"en_US"}}
	if _, _, err := c.do(ctx, true, http.MethodGet, base+"/identity/list?"+lq.Encode(), nil); err != nil {
		return fmt.Errorf("Mi account verification: %w", err)
	}
	path := "/identity/auth/sendEmailTicket"
	if by == ByPhone {
		path = "/identity/auth/sendPhoneTicket"
	} else if by != ByEmail {
		return fmt.Errorf("%w: verification by %q (email or phone)", ErrValue, by)
	}
	q := url.Values{"_dc": {strconv.FormatInt(time.Now().UnixMilli(), 10)}, "sid": {"xiaomiio"}, "context": {cx}, "mask": {"0"}, "_locale": {"en_US"}}
	form := url.Values{"retry": {"0"}, "icode": {""}, "_json": {"true"}, "ick": {c.cookie(base, "ick")}}
	b, resp, err := c.doForm(ctx, true, http.MethodPost, base+path+"?"+q.Encode(), nil, form)
	if err != nil {
		return fmt.Errorf("Mi account verification: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the Mi account answered HTTP %d to the code request", resp.StatusCode)
	}
	if m, err := startJSON(b); err == nil {
		if code := numString(m["code"]); code != "" && code != "0" {
			return fmt.Errorf("the Mi account refused to send a code: code %s %s", code, str(m["tips"])+str(m["desc"]))
		}
	}
	return nil
}

var checkURL = regexp.MustCompile(`https?://[^"'\s]+/identity/result/check[^"'\s]*`)

// VerifyCode completes the sign-in with the code a person received (by
// the same way given to SendCode). Devices and later calls then work.
func (c *Cloud) VerifyCode(ctx context.Context, by, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cx, err := c.verifyContext()
	if err != nil {
		return err
	}
	base := strings.TrimRight(c.AccountBase, "/")
	path, flag := "/identity/auth/verifyEmail", "8"
	if by == ByPhone {
		path, flag = "/identity/auth/verifyPhone", "4"
	} else if by != ByEmail {
		return fmt.Errorf("%w: verification by %q (email or phone)", ErrValue, by)
	}
	q := url.Values{"_flag": {flag}, "_json": {"true"}, "sid": {"xiaomiio"}, "context": {cx}, "mask": {"0"}, "_locale": {"en_US"}}
	form := url.Values{"_flag": {flag}, "ticket": {code}, "trust": {"false"}, "_json": {"true"}, "ick": {c.cookie(base, "ick")}}
	b, resp, err := c.doForm(ctx, false, http.MethodPost, base+path+"?"+q.Encode(), nil, form)
	if err != nil {
		return fmt.Errorf("Mi account verification: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode/100 != 3 {
		return fmt.Errorf("the Mi account answered HTTP %d to the code", resp.StatusCode)
	}
	// Where to go next: the answer's location, its Location header, an
	// address in the body, or the check address itself.
	next := ""
	if m, err := startJSON(b); err == nil {
		if code := numString(m["code"]); code != "" && code != "0" {
			return fmt.Errorf("%w: the code was refused (code %s)", ErrCloudAuth, code)
		}
		next = str(m["location"])
	}
	if next == "" {
		next = resp.Header.Get("Location")
	}
	if next == "" {
		next = checkURL.FindString(string(b))
	}
	if next == "" {
		cq := url.Values{"sid": {"xiaomiio"}, "context": {cx}, "_locale": {"en_US"}}
		next = base + "/identity/result/check?" + cq.Encode()
	}
	next = c.absURL(next)
	if strings.Contains(next, "identity/result/check") {
		_, r, err := c.do(ctx, false, http.MethodGet, next, nil)
		if err != nil {
			return fmt.Errorf("Mi account verification: %w", err)
		}
		if l := r.Header.Get("Location"); l != "" {
			next = c.absURL(l)
		}
	}
	// The end of serviceLoginAuth2 gives ssecurity in a header; a tips
	// page in between is asked again once.
	var r *http.Response
	for try := 0; try < 2; try++ {
		_, r, err = c.do(ctx, false, http.MethodGet, next, nil)
		if err != nil {
			return fmt.Errorf("Mi account verification: %w", err)
		}
		if r.StatusCode == http.StatusOK && r.Header.Get("extension-pragma") == "" {
			continue
		}
		break
	}
	var prag struct {
		Ssecurity string `json:"ssecurity"`
	}
	_ = json.Unmarshal([]byte(r.Header.Get("extension-pragma")), &prag)
	if prag.Ssecurity == "" {
		return errors.New("the Mi account's verification gave no ssecurity")
	}
	sts := r.Header.Get("Location")
	if sts == "" {
		return errors.New("the Mi account's verification gave no service address")
	}
	c.ssecurity = prag.Ssecurity
	if c.userID == "" {
		c.userID = c.cookie(base, "userId")
	}
	if err := c.finish(ctx, c.absURL(sts)); err != nil {
		return err
	}
	if c.userID == "" {
		c.userID = c.cookie(c.absURL(sts), "userId")
	}
	c.verify = nil
	return nil
}
