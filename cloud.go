package miio

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Mi account ("Xiaomi cloud") gives the list of an account's devices
// with each one's token, local IP address and model, so nobody has to
// extract tokens by hand. Xiaomi documents none of it; this follows the
// public descriptions of how open-source tools sign in (facts only, our
// own code; PROVENANCE.md):
//
//  1. GET account/pass/serviceLogin?sid=xiaomiio&_json=true answers (after
//     a "&&&START&&&" prefix) JSON with a _sign;
//  2. POST account/pass/serviceLoginAuth2 with the user, the upper-case
//     hex MD5 of the password, the _sign and the xiaomiio callback answers
//     ssecurity, userId and a location; a captcha or an e-mail check
//     (captchaUrl, notificationUrl) cannot be answered by a server;
//  3. GET location sets the serviceToken cookie;
//  4. API calls are POSTs to https://<region>.api.io.mi.com/app/<path>
//     (no prefix for cn) whose parameters are RC4-encrypted with a
//     per-call key: nonce = 8 random bytes ‖ minutes since the epoch
//     (4 bytes, big-endian), key = SHA-256(ssecurity ‖ nonce), each value
//     encrypted with RC4 after dropping 1024 key-stream bytes, signed
//     with base64 SHA-1 over "POST&<path>&k=v…&key"; the answer is
//     encrypted the same way. v2/homeroom/gethome lists the homes,
//     v2/user/get_device_cnt the homes shared with the account, and
//     v2/home/home_device_list a home's devices.
//
// Untested against the real Mi account service: the simulator
// (sim.Cloud) follows the same description.

// Regions are the Mi account's server regions (cn has no prefix; i2 is
// India's).
var Regions = []string{"cn", "de", "us", "ru", "tw", "sg", "in", "i2"}

// CloudDevice is one device of the account.
type CloudDevice struct {
	DID     string `json:"did"`
	Token   string `json:"token"`
	Name    string `json:"name"`
	Model   string `json:"model"`
	LocalIP string `json:"localip"`
	MAC     string `json:"mac"`
	Online  bool   `json:"isOnline"`
	Region  string `json:"region,omitempty"`
}

// Errors of the sign-in.
var (
	// ErrCloudAuth: the account refused the user name or password.
	ErrCloudAuth = errors.New("the Mi account refused the user name or password")
	// ErrCloudVerify: Xiaomi asks for a captcha or an e-mail or SMS code,
	// which a server cannot give.
	ErrCloudVerify = errors.New("the Mi account asks for a verification a server cannot give (a captcha or a code sent by e-mail or SMS)")
)

// Cloud signs in to a Mi account and lists its devices.
type Cloud struct {
	Username, Password string
	// AccountBase is https://account.xiaomi.com (tests change it).
	AccountBase string
	// APIBase gives a region's API address
	// (https://<region>.api.io.mi.com/app).
	APIBase func(region string) string
	HTTP    *http.Client

	mu           sync.Mutex
	jar          *cookiejar.Jar
	deviceID     string
	agent        string
	ssecurity    string
	userID       string
	serviceToken string
}

// DefaultAPIBase is the Mi account's API address of a region.
func DefaultAPIBase(region string) string {
	if region == "cn" || region == "" {
		return "https://api.io.mi.com/app"
	}
	return "https://" + region + ".api.io.mi.com/app"
}

func randLetters(n int, from, span byte) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = from + b[i]%span
	}
	return string(b)
}

func (c *Cloud) init() {
	if c.jar == nil {
		c.jar, _ = cookiejar.New(nil)
		c.deviceID = randLetters(6, 'a', 26)
		c.agent = randLetters(18, 'a', 26) + "-" + randLetters(13, 'A', 5) + " APP/com.xiaomi.mihome APPV/10.5.201"
	}
	if c.AccountBase == "" {
		c.AccountBase = "https://account.xiaomi.com"
	}
	if c.APIBase == nil {
		c.APIBase = DefaultAPIBase
	}
}

func (c *Cloud) client(follow bool) *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{Timeout: 20 * time.Second}
	}
	hc := *base
	hc.Jar = c.jar
	if !follow {
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return &hc
}

// startJSON reads an account answer: JSON after "&&&START&&&".
func startJSON(b []byte) (map[string]any, error) {
	b = bytes.TrimPrefix(bytes.TrimSpace(b), []byte("&&&START&&&"))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("the Mi account's answer is not JSON: %w", err)
	}
	return m, nil
}

func (c *Cloud) do(ctx context.Context, follow bool, method, u string, cookies map[string]string) ([]byte, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := c.client(follow).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return b, resp, err
}

// Login signs in (steps 1 to 3).
func (c *Cloud) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login(ctx)
}

func (c *Cloud) login(ctx context.Context) error {
	c.init()
	c.ssecurity, c.serviceToken = "", ""
	base := map[string]string{"sdkVersion": "accountsdk-18.8.15", "deviceId": c.deviceID}
	ck := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	b, resp, err := c.do(ctx, true, http.MethodGet, c.AccountBase+"/pass/serviceLogin?sid=xiaomiio&_json=true", ck(map[string]string{"userId": c.Username}))
	if err != nil {
		return fmt.Errorf("Mi account sign-in: %w", err)
	}
	m, err := startJSON(b)
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Mi account sign-in answered HTTP %d", resp.StatusCode)
	}
	location := ""
	if s, _ := m["ssecurity"].(string); s != "" {
		// Already signed in (a kept session).
		c.ssecurity, location = s, str(m["location"])
		c.userID = numString(m["userId"])
	} else {
		sign, _ := m["_sign"].(string)
		sum := md5.Sum([]byte(c.Password))
		q := url.Values{}
		q.Set("sid", "xiaomiio")
		q.Set("hash", strings.ToUpper(hex.EncodeToString(sum[:])))
		q.Set("callback", "https://sts.api.io.mi.com/sts")
		q.Set("qs", "%3Fsid%3Dxiaomiio%26_json%3Dtrue")
		q.Set("user", c.Username)
		q.Set("_sign", sign)
		q.Set("_json", "true")
		b, resp, err = c.do(ctx, false, http.MethodPost, c.AccountBase+"/pass/serviceLoginAuth2?"+q.Encode(), ck(nil))
		if err != nil {
			return fmt.Errorf("Mi account sign-in: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Mi account sign-in answered HTTP %d", resp.StatusCode)
		}
		m, err = startJSON(b)
		if err != nil {
			return err
		}
		if s, _ := m["ssecurity"].(string); len(s) > 4 {
			c.ssecurity, location = s, str(m["location"])
			c.userID = numString(m["userId"])
		} else if str(m["captchaUrl"]) != "" || str(m["notificationUrl"]) != "" {
			return ErrCloudVerify
		} else {
			return fmt.Errorf("%w (code %v)", ErrCloudAuth, m["code"])
		}
	}
	if location == "" {
		return errors.New("the Mi account gave no service address")
	}
	if _, _, err := c.do(ctx, true, http.MethodGet, location, nil); err != nil {
		return fmt.Errorf("Mi account service token: %w", err)
	}
	if u, err := url.Parse(location); err == nil {
		for _, ck := range c.jar.Cookies(u) {
			if ck.Name == "serviceToken" {
				c.serviceToken = ck.Value
			}
		}
	}
	if c.serviceToken == "" {
		return errors.New("the Mi account gave no service token")
	}
	return nil
}

func str(v any) string { s, _ := v.(string); return s }

func numString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// rc4Crypt encrypts or decrypts with RC4 keyed by key, after dropping the
// first 1024 key-stream bytes.
func rc4Crypt(key, data []byte) []byte {
	c, _ := rc4.NewCipher(key)
	drop := make([]byte, 1024)
	c.XORKeyStream(drop, drop)
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

// SignedNonce is the per-call key: SHA-256(ssecurity ‖ nonce), base64.
func SignedNonce(ssecurity, nonce string) string {
	s, _ := base64.StdEncoding.DecodeString(ssecurity)
	n, _ := base64.StdEncoding.DecodeString(nonce)
	h := sha256.Sum256(append(s, n...))
	return base64.StdEncoding.EncodeToString(h[:])
}

// EncSignature is the SHA-1 signature over the method, the path, the
// parameters in order and the signed nonce.
func EncSignature(method, path string, params [][2]string, signedNonce string) string {
	parts := []string{strings.ToUpper(method), path}
	for _, p := range params {
		parts = append(parts, p[0]+"="+p[1])
	}
	parts = append(parts, signedNonce)
	h := sha1.Sum([]byte(strings.Join(parts, "&")))
	return base64.StdEncoding.EncodeToString(h[:])
}

// RC4Encrypt encrypts a value with a signed nonce (base64 in and out).
func RC4Encrypt(signedNonce, v string) string {
	k, _ := base64.StdEncoding.DecodeString(signedNonce)
	return base64.StdEncoding.EncodeToString(rc4Crypt(k, []byte(v)))
}

// RC4Decrypt reverses RC4Encrypt.
func RC4Decrypt(signedNonce, v string) ([]byte, error) {
	k, _ := base64.StdEncoding.DecodeString(signedNonce)
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return nil, err
	}
	return rc4Crypt(k, b), nil
}

// apiPath is the signed path of an API address: its path without the
// "/app" prefix.
func apiPath(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return u
	}
	p := pu.Path
	if i := strings.Index(p, "/app/"); i >= 0 {
		p = p[i+len("/app"):]
	}
	return p
}

// call runs one encrypted API call.
func (c *Cloud) call(ctx context.Context, region, path, data string) (map[string]any, error) {
	u := c.APIBase(region) + path
	var nb [12]byte
	_, _ = rand.Read(nb[:8])
	binary.BigEndian.PutUint32(nb[8:], uint32(time.Now().UnixMilli()/60000))
	nonce := base64.StdEncoding.EncodeToString(nb[:])
	sn := SignedNonce(c.ssecurity, nonce)
	sp := apiPath(u)
	params := [][2]string{{"data", data}}
	params = append(params, [2]string{"rc4_hash__", EncSignature("POST", sp, params, sn)})
	for i := range params {
		params[i][1] = RC4Encrypt(sn, params[i][1])
	}
	q := url.Values{}
	for _, p := range params {
		q.Set(p[0], p[1])
	}
	q.Set("signature", EncSignature("POST", sp, params, sn))
	q.Set("ssecurity", c.ssecurity)
	q.Set("_nonce", nonce)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("x-xiaomi-protocal-flag-cli", "PROTOCAL-HTTP2")
	req.Header.Set("MIOT-ENCRYPT-ALGORITHM", "ENCRYPT-RC4")
	for k, v := range map[string]string{"userId": c.userID, "yetAnotherServiceToken": c.serviceToken, "serviceToken": c.serviceToken,
		"locale": "en_GB", "timezone": "GMT+05:30", "is_daylight": "0", "dst_offset": "0", "channel": "MI_APP_STORE"} {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrCloudAuth
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Mi account API answered HTTP %d", resp.StatusCode)
	}
	plain, err := RC4Decrypt(sn, string(b))
	if err != nil {
		return nil, fmt.Errorf("the Mi account API's answer could not be read: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("the Mi account API's answer is not JSON (a wrong key?): %w", err)
	}
	if code, _ := out["code"].(float64); code != 0 {
		if code == 2 || code == 3 {
			return nil, ErrCloudAuth
		}
		return nil, fmt.Errorf("the Mi account API answered code %v: %v", code, out["message"])
	}
	return out, nil
}

// Devices signs in when needed and lists the account's devices in the
// regions (every region when empty), with the homes shared with it.
func (c *Cloud) Devices(ctx context.Context, regions []string) ([]CloudDevice, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.serviceToken == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	if len(regions) == 0 {
		regions = Regions
	}
	var out []CloudDevice
	var firstErr error
	seen := map[string]bool{}
	for _, region := range regions {
		list, err := c.regionDevices(ctx, region)
		if errors.Is(err, ErrCloudAuth) {
			// The session ended: sign in again once.
			if err = c.login(ctx); err == nil {
				list, err = c.regionDevices(ctx, region)
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("region %s: %w", region, err)
			}
			continue
		}
		for _, d := range list {
			if !seen[d.DID] {
				seen[d.DID] = true
				out = append(out, d)
			}
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (c *Cloud) regionDevices(ctx context.Context, region string) ([]CloudDevice, error) {
	homes, err := c.call(ctx, region, "/v2/homeroom/gethome", `{"fg": true, "fetch_share": true, "fetch_share_dev": true, "limit": 300, "app_ver": 7}`)
	if err != nil {
		return nil, err
	}
	type home struct{ id, owner string }
	var hs []home
	res, _ := homes["result"].(map[string]any)
	hl, _ := res["homelist"].([]any)
	for _, h := range hl {
		hm, _ := h.(map[string]any)
		hs = append(hs, home{numString(hm["id"]), c.userID})
	}
	if cnt, err := c.call(ctx, region, "/v2/user/get_device_cnt", `{ "fetch_own": true, "fetch_share": true}`); err == nil {
		r, _ := cnt["result"].(map[string]any)
		sh, _ := r["share"].(map[string]any)
		fam, _ := sh["share_family"].([]any)
		for _, f := range fam {
			fm, _ := f.(map[string]any)
			hs = append(hs, home{numString(fm["home_id"]), numString(fm["home_owner"])})
		}
	}
	var out []CloudDevice
	for _, h := range hs {
		data := fmt.Sprintf(`{"home_owner": %s,"home_id": %s,  "limit": 200,  "get_split_device": true, "support_smart_home": true}`, h.owner, h.id)
		devs, err := c.call(ctx, region, "/v2/home/home_device_list", data)
		if err != nil {
			return nil, err
		}
		r, _ := devs["result"].(map[string]any)
		list, _ := r["device_info"].([]any)
		for _, d := range list {
			b, _ := json.Marshal(d)
			var cd CloudDevice
			if json.Unmarshal(b, &cd) == nil && cd.DID != "" {
				cd.Region = region
				out = append(out, cd)
			}
		}
	}
	return out, nil
}
