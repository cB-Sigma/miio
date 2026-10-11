package miio

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/cB-Sigma/lanbcast"
)

// The miIO protocol: UDP port 54321, a 32-byte header (magic 0x2131,
// length, 4 unknown bytes, device ID, stamp, and a 16-byte MD5 checksum
// over the header, with the token in the checksum's place, and the data),
// and JSON-RPC data encrypted with AES-128-CBC whose key is MD5(token) and
// IV MD5(key ‖ token). A hello (32 bytes, all 0xff after the length)
// answers with the device ID and its stamp, which later packets carry.
// Xiaomi documents none of this publicly; the facts come from public
// descriptions by open-source clients (PROVENANCE.md).

// Port is miIO's UDP port.
const Port = 54321

const magic = 0x2131

// Hello is the discovery and handshake packet.
func Hello() []byte {
	b := bytes.Repeat([]byte{0xff}, 32)
	binary.BigEndian.PutUint16(b[0:], magic)
	binary.BigEndian.PutUint16(b[2:], 32)
	return b
}

// Keys derives the AES key and IV from a 16-byte token.
func Keys(token []byte) (key, iv []byte) {
	k := md5.Sum(token)
	v := md5.Sum(append(append([]byte(nil), k[:]...), token...))
	return k[:], v[:]
}

// Encrypt encrypts data for a token.
func Encrypt(token, plain []byte) []byte {
	key, iv := Keys(token)
	blk, _ := aes.NewCipher(key)
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	p := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(blk, iv).CryptBlocks(out, p)
	return out
}

// Decrypt reverses Encrypt.
func Decrypt(token, ct []byte) ([]byte, error) {
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("miIO data is not whole AES blocks")
	}
	key, iv := Keys(token)
	blk, _ := aes.NewCipher(key)
	p := make([]byte, len(ct))
	cipher.NewCBCDecrypter(blk, iv).CryptBlocks(p, ct)
	n := int(p[len(p)-1])
	if n < 1 || n > aes.BlockSize {
		return nil, errors.New("miIO data with bad padding (a wrong token?)")
	}
	// Some devices end their JSON with a NUL before the padding.
	return bytes.TrimRight(p[:len(p)-n], "\x00"), nil
}

// Packet builds a packet with encrypted data.
func Packet(token []byte, deviceID, stamp uint32, plain []byte) []byte {
	data := Encrypt(token, plain)
	b := make([]byte, 32, 32+len(data))
	binary.BigEndian.PutUint16(b[0:], magic)
	binary.BigEndian.PutUint16(b[2:], uint16(32+len(data)))
	binary.BigEndian.PutUint32(b[8:], deviceID)
	binary.BigEndian.PutUint32(b[12:], stamp)
	copy(b[16:], token)
	b = append(b, data...)
	sum := md5.Sum(b)
	copy(b[16:], sum[:])
	return b
}

// Header is a parsed packet header.
type Header struct {
	Length   uint16
	Unknown  uint32
	DeviceID uint32
	Stamp    uint32
	Checksum []byte
}

// ParseHeader reads a packet's header.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < 32 || binary.BigEndian.Uint16(b) != magic {
		return Header{}, errors.New("not a miIO packet")
	}
	h := Header{Length: binary.BigEndian.Uint16(b[2:]), Unknown: binary.BigEndian.Uint32(b[4:]), DeviceID: binary.BigEndian.Uint32(b[8:]),
		Stamp: binary.BigEndian.Uint32(b[12:]), Checksum: append([]byte(nil), b[16:32]...)}
	if int(h.Length) != len(b) {
		return h, fmt.Errorf("a miIO packet of %d bytes says it has %d", len(b), h.Length)
	}
	return h, nil
}

// Open checks a packet's checksum and decrypts its data.
func Open(token, b []byte) (Header, []byte, error) {
	h, err := ParseHeader(b)
	if err != nil {
		return h, nil, err
	}
	c := append([]byte(nil), b...)
	copy(c[16:], token)
	sum := md5.Sum(c)
	if !bytes.Equal(sum[:], h.Checksum) {
		return h, nil, errors.New("the miIO checksum does not match (a wrong token?)")
	}
	if len(b) == 32 {
		return h, nil, nil
	}
	p, err := Decrypt(token, b[32:])
	return h, p, err
}

// Client talks to one device.
type Client struct {
	Addr    string // host:port
	Token   []byte
	Timeout time.Duration

	mu       sync.Mutex
	deviceID uint32
	stamp    uint32
	stampAt  time.Time
	nextID   int
}

// ErrToken means the device's answer could not be opened with the token.
var ErrToken = errors.New("the device's answer does not open with the token")

func (c *Client) exchange(ctx context.Context, pkt []byte) ([]byte, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp4", c.Addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(c.Timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	buf := make([]byte, 64<<10)
	// UDP: send again twice while no answer comes.
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := conn.Write(pkt); err != nil {
			return nil, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(c.Timeout / 3))
		n, err := conn.Read(buf)
		if err == nil {
			return append([]byte(nil), buf[:n]...), nil
		}
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			return nil, err
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return nil, fmt.Errorf("%w from %s", errNoAnswer, c.Addr)
}

// errNoAnswer is a request the device did not answer.
var errNoAnswer = errors.New("no answer")

// handshake sends a hello and keeps the device ID and stamp.
func (c *Client) handshake(ctx context.Context) error {
	b, err := c.exchange(ctx, Hello())
	if err != nil {
		return err
	}
	h, err := ParseHeader(b)
	if err != nil {
		return err
	}
	c.deviceID, c.stamp, c.stampAt = h.DeviceID, h.Stamp, time.Now()
	return nil
}

// DeviceID is the ID the device gave in its last answer.
func (c *Client) DeviceID() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deviceID
}

// Call sends a JSON-RPC request and returns its result, or the whole
// answer when it has neither "result" nor "error". Request IDs
// start at a random value (devices ignore an ID they answered recently, so
// a restarted client must not begin at 1 again); when a request is not
// answered, the IDs move on by 100 and it is asked once more.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if params == nil {
		params = []any{}
	}
	if c.nextID == 0 {
		c.nextID = 1 + mrand.IntN(9000)
	}
	var b []byte
	for attempt := 0; ; attempt++ {
		if c.stampAt.IsZero() || time.Since(c.stampAt) > time.Minute {
			if err := c.handshake(ctx); err != nil {
				return nil, err
			}
		}
		c.nextID++
		req, _ := json.Marshal(map[string]any{"id": c.nextID, "method": method, "params": params})
		stamp := c.stamp + uint32(time.Since(c.stampAt)/time.Second)
		var err error
		b, err = c.exchange(ctx, Packet(c.Token, c.deviceID, stamp, req))
		if err == nil {
			break
		}
		c.stampAt = time.Time{}
		if attempt > 0 || ctx.Err() != nil || !errors.Is(err, errNoAnswer) {
			return nil, err
		}
		c.nextID += 100
	}
	h, plain, err := Open(c.Token, b)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrToken, err)
	}
	c.stamp, c.stampAt = h.Stamp, time.Now()
	var out struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("the device's answer is not JSON: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("the device refused %s: %d %s", method, out.Error.Code, out.Error.Message)
	}
	if out.Result == nil {
		// An answer without "result" (python-miio's simulators answer
		// miIO.info so): the answer itself, as python-miio takes it.
		return json.RawMessage(plain), nil
	}
	return out.Result, nil
}

// Discover broadcasts a hello and returns device ID → address.
func Discover(ctx context.Context, bcast string, wait time.Duration) (map[uint32]string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	ra, err := net.ResolveUDPAddr("udp4", bcast)
	if err != nil {
		return nil, err
	}
	if err := lanbcast.WriteBroadcast(conn, Hello(), ra); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	out := map[uint32]string{}
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return out, nil
		}
		if h, err := ParseHeader(buf[:n]); err == nil && n == 32 {
			out[h.DeviceID] = from.String()
		}
	}
}
