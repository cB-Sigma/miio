// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// image is the reference peer's image, built from ./peer.
const image = "sigma-miio-interop:python-miio-9a00e08-py3.12.10-r2"

// envVar turns the tests on.
const envVar = "SIGMA_MIIO_INTEROP"

// needDocker skips the test unless the environment variable is 1 and
// Docker answers, and builds the peer image when it is missing.
func needDocker(t *testing.T) {
	t.Helper()
	if os.Getenv(envVar) != "1" {
		t.Skip("set " + envVar + "=1 to run the interop tests (needs Docker)")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker is not running")
	}
	if exec.Command("docker", "image", "inspect", image).Run() == nil {
		return
	}
	out, err := exec.Command("docker", "build", "-q", "-t", image, "peer").CombinedOutput()
	if err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}
}

// freeUDPPort returns a UDP port free on the loopback interface.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// syncBuf is the container's output.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startPeer runs `docker run --rm --name <unique> <args...>` and stops it
// when the test ends, logging its output when the test failed.
func startPeer(t *testing.T, args ...string) {
	t.Helper()
	name := fmt.Sprintf("sigma-miio-%d", time.Now().UnixNano())
	cmd := exec.Command("docker", append([]string{"run", "--rm", "--name", name}, args...)...)
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() {
		_ = exec.Command("docker", "stop", "-t", "2", name).Run()
		if t.Failed() || os.Getenv(envVar+"_LOG") == "1" {
			t.Logf("peer output:\n%s", out.String())
		}
	})
}
