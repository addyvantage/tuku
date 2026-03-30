package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func CallUnix(ctx context.Context, socketPath string, req Request) (Response, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, fmt.Errorf("dial daemon: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(bufio.NewReader(conn))

	if err := encoder.Encode(req); err != nil {
		return Response{}, fmt.Errorf("encode request: %w", err)
	}
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK && resp.Error != nil {
		return resp, fmt.Errorf("daemon error [%s]: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
}
