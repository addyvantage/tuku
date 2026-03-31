package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCallUnixHonorsContextDeadlineAfterDial(t *testing.T) {
	t.Parallel()

	socketPath := fmt.Sprintf("/tmp/tuku-ipc-%d.sock", time.Now().UnixNano())
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		var req Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			serverErr <- err
			return
		}

		time.Sleep(250 * time.Millisecond)
		serverErr <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = CallUnix(ctx, socketPath, Request{RequestID: "req_test", Method: "test.method"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected CallUnix to fail when the context deadline expires before the response arrives")
	}
	if elapsed > 175*time.Millisecond {
		t.Fatalf("expected context deadline to interrupt the call quickly, elapsed=%s err=%v", elapsed, err)
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode response error, got %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server goroutine failed: %v", err)
	}
}
