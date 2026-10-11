// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package miio

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Device pushes work through the devices' local automations ("scenes"):
// the listener poses as a miIO device on UDP 54321, and a scene installed
// on the watched device with send_data_frame names the listener as the
// target, so the device sends a miIO request to it whenever the trigger
// fires. The request is encrypted with the watched device's own token; its
// method is "<listener model>.<action>:<source id>" (dots of the source
// ID written as underscores) and the listener answers {"result": 0}.
// miIO.xdel removes a scene. Which triggers a device has (the "extra"
// string) is model-specific and found from the vendor app's own scenes.

// EventInfo describes one event to subscribe to.
type EventInfo struct {
	// Action is the name the pushes arrive under (our choice).
	Action string
	// Extra identifies the trigger, such as "[1,6,1,0,[0,1],2,0]".
	Extra string
	// Event is the event name in the trigger; Action when empty.
	Event string
	// CommandExtra is passed back with each push.
	CommandExtra string
	// TriggerValue is a threshold, for triggers that have one.
	TriggerValue any
	// TriggerToken is needed by protected triggers (a gateway's alarm).
	TriggerToken string
	// SourceDID and SourceModel name the source; the watched device when
	// empty. Gateway sub-devices are named by their sid and zigbee model.
	SourceDID, SourceModel string
}

// Push is one event a device sent.
type Push struct {
	Device string          // the sender's IP address
	Source string          // the source's ID (a device ID or a sub-device sid)
	Action string          // EventInfo.Action
	Params json.RawMessage // what the device sent with it
}

// PushServer receives device pushes.
type PushServer struct {
	// Addr is where it listens; ":54321" when empty. Devices always send to
	// port 54321 of ServerIP.
	Addr string
	// DeviceID and Model are what the listener poses as; 100000001 and
	// chuangmi.plug.v3 (a model devices accept as a scene target) when
	// zero.
	DeviceID uint32
	Model    string
	// ServerIP is the address devices send to; when empty, the local
	// address that routes to each device.
	ServerIP string

	mu      sync.Mutex
	conn    *net.UDPConn
	devices map[string]*pushDevice // by IP
	nextID  int
	start   time.Time
}

type pushDevice struct {
	token   []byte
	handler func(Push)
	events  []string
}

// Start listens.
func (s *PushServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return errors.New("the push server is already started")
	}
	addr := s.Addr
	if addr == "" {
		addr = ":" + strconv.Itoa(Port)
	}
	ua, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return err
	}
	c, err := net.ListenUDP("udp4", ua)
	if err != nil {
		return err
	}
	if s.DeviceID == 0 {
		s.DeviceID = 100000001
	}
	if s.Model == "" {
		s.Model = "chuangmi.plug.v3"
	}
	if s.devices == nil {
		s.devices = map[string]*pushDevice{}
	}
	if s.nextID == 0 {
		n, _ := rand.Int(rand.Reader, big.NewInt(900000))
		s.nextID = 1000000 + int(n.Int64())
	}
	s.conn, s.start = c, time.Now()
	go s.serve(c)
	return nil
}

// LocalAddr is the listening address.
func (s *PushServer) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr()
}

// Close stops listening. Scenes stay on the devices: Unsubscribe them
// first.
func (s *PushServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}

func hostIP(addr string) (string, error) {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	ips, err := net.LookupIP(h)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 address for %s", h)
}

// Register accepts pushes from the client's device and hands them to
// handler (on the server's goroutine: return quickly).
func (s *PushServer) Register(c *Client, handler func(Push)) error {
	ip, err := hostIP(c.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices == nil {
		s.devices = map[string]*pushDevice{}
	}
	d := s.devices[ip]
	if d == nil {
		d = &pushDevice{}
		s.devices[ip] = d
	}
	d.token, d.handler = append([]byte(nil), c.Token...), handler
	return nil
}

// TokenEnc is how a scene target carries the target's token: the first 16
// bytes of the token encrypted with itself, in hex.
func TokenEnc(token []byte) string {
	return hex.EncodeToString(Encrypt(token, token)[:16])
}

// Subscribe installs a scene on the client's device that pushes the event
// to this server, and returns the scene's ID. The device must be
// Registered.
func (s *PushServer) Subscribe(ctx context.Context, c *Client, ev EventInfo) (string, error) {
	ip, err := hostIP(c.Addr)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	d := s.devices[ip]
	started := s.conn != nil
	s.mu.Unlock()
	if d == nil || !started {
		return "", errors.New("start the push server and register the device before subscribing")
	}
	if ev.SourceModel == "" || ev.SourceDID == "" {
		raw, err := c.Call(ctx, "miIO.info", nil)
		if err != nil {
			return "", err
		}
		var info struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &info)
		if ev.SourceModel == "" {
			ev.SourceModel = info.Model
		}
		if ev.SourceDID == "" {
			ev.SourceDID = strconv.FormatUint(uint64(c.DeviceID()), 10)
		}
	}
	if ev.Event == "" {
		ev.Event = ev.Action
	}
	server := s.ServerIP
	if server == "" {
		conn, err := net.Dial("udp4", net.JoinHostPort(ip, strconv.Itoa(Port)))
		if err != nil {
			return "", err
		}
		server = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	s.mu.Lock()
	s.nextID++
	id := "x.scene." + strconv.Itoa(s.nextID)
	model, did := s.Model, s.DeviceID
	s.mu.Unlock()
	command := model + "." + ev.Action + ":" + strings.ReplaceAll(ev.SourceDID, ".", "_")
	if len(command) > 49 {
		return "", fmt.Errorf("%w: the push command %q is longer than the 49 characters devices keep", ErrValue, command)
	}
	trigger := map[string]any{
		"did": ev.SourceDID, "extra": ev.Extra, "key": "event." + ev.SourceModel + "." + ev.Event,
		"model": ev.SourceModel, "src": "device", "timespan": []string{"0 0 * * 0,1,2,3,4,5,6", "0 0 * * 0,1,2,3,4,5,6"},
		"token": ev.TriggerToken,
	}
	if ev.TriggerValue != nil {
		trigger["value"] = ev.TriggerValue
	}
	target := map[string]any{
		"command": command, "did": strconv.FormatUint(uint64(did), 10), "extra": ev.CommandExtra, "id": 0,
		"ip": server, "model": model, "token": TokenEnc(c.Token), "value": "",
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(50000000))
	scene := []any{[]any{id, []any{"1.0", 1590161094 + n.Int64(), []any{"0", trigger}, []any{target}}}}
	data, _ := json.Marshal(scene)
	if err := okAnswer(c.Call(ctx, "send_data_frame", map[string]any{
		"cur": 0, "data": string(data), "data_tkn": 29576, "total": 1, "type": "scene",
	})); err != nil {
		return "", fmt.Errorf("the device did not take the scene: %w", err)
	}
	s.mu.Lock()
	d.events = append(d.events, id)
	s.mu.Unlock()
	return id, nil
}

// Unsubscribe removes a scene from the client's device.
func (s *PushServer) Unsubscribe(ctx context.Context, c *Client, id string) error {
	if err := okAnswer(c.Call(ctx, "miIO.xdel", []any{id})); err != nil {
		return err
	}
	if ip, err := hostIP(c.Addr); err == nil {
		s.mu.Lock()
		if d := s.devices[ip]; d != nil {
			for i, e := range d.events {
				if e == id {
					d.events = append(d.events[:i], d.events[i+1:]...)
					break
				}
			}
		}
		s.mu.Unlock()
	}
	return nil
}

// Subscriptions lists the scene IDs installed through this server on a
// device.
func (s *PushServer) Subscriptions(c *Client) []string {
	ip, err := hostIP(c.Addr)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.devices[ip]; d != nil {
		return append([]string(nil), d.events...)
	}
	return nil
}

func (s *PushServer) serve(c *net.UDPConn) {
	buf := make([]byte, 64<<10)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := buf[:n]
		s.mu.Lock()
		did, stamp := s.DeviceID, uint32(time.Since(s.start)/time.Second)
		d := s.devices[from.IP.To4().String()]
		var token []byte
		var handler func(Push)
		if d != nil {
			token, handler = d.token, d.handler
		}
		model := s.Model
		s.mu.Unlock()
		if h, err := ParseHeader(pkt); err == nil && n == 32 && h.Unknown == 0xffffffff {
			ack := make([]byte, 32)
			binary.BigEndian.PutUint16(ack[0:], magic)
			binary.BigEndian.PutUint16(ack[2:], 32)
			binary.BigEndian.PutUint32(ack[8:], did)
			binary.BigEndian.PutUint32(ack[12:], stamp)
			_, _ = c.WriteToUDP(ack, from)
			continue
		}
		if d == nil {
			continue
		}
		_, plain, err := Open(token, pkt)
		if err != nil {
			continue
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(plain, &req) != nil || req.Method == "" {
			continue
		}
		ans, _ := json.Marshal(map[string]any{"id": req.ID, "result": 0})
		_, _ = c.WriteToUDP(Packet(token, did, stamp, ans), from)
		action, source := req.Method, ""
		if i := strings.LastIndex(req.Method, ":"); i >= 0 {
			action, source = req.Method[:i], strings.ReplaceAll(req.Method[i+1:], "_", ".")
		}
		action = strings.TrimPrefix(action, model+".")
		if handler != nil {
			handler(Push{Device: from.IP.String(), Source: source, Action: action, Params: req.Params})
		}
	}
}
