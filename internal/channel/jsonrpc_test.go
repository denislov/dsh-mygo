package channel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInvalidFrameConformance(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "invalid-frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	var frames []string
	if err := json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	for index, frame := range frames {
		t.Run(fmt.Sprintf("frame-%d", index), func(t *testing.T) {
			if _, err := readMessage(bufio.NewReader(strings.NewReader(frame)), 1024); err == nil {
				t.Fatal("invalid protocol frame accepted")
			}
		})
	}
}

func TestCallAndNotification(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := NewClient(clientConn, clientConn, 1<<20)
	notifications := make(chan string, 1)
	client.OnNotification(func(method string, _ json.RawMessage) {
		notifications <- method
	})
	go func() { _ = client.Run() }()

	go func() {
		reader := bufio.NewReader(serverConn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		var request message
		if json.Unmarshal([]byte(line), &request) != nil {
			return
		}
		fmt.Fprintf(serverConn, "{\"jsonrpc\":\"2.0\",\"method\":\"ready\",\"params\":{}}\n")
		fmt.Fprintf(serverConn, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ok\":true}}\n", request.ID)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := client.Call(ctx, "initialize", map[string]any{"cwd": "."})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var decoded map[string]bool
	if err := json.Unmarshal(result, &decoded); err != nil || !decoded["ok"] {
		t.Fatalf("result = %s, err = %v", result, err)
	}
	select {
	case method := <-notifications:
		if method != "ready" {
			t.Fatalf("notification %q, want ready", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestRPCError(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := NewClient(clientConn, clientConn, 1<<20)
	go func() { _ = client.Run() }()
	go func() {
		reader := bufio.NewReader(serverConn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		var request message
		if json.Unmarshal([]byte(line), &request) != nil {
			return
		}
		fmt.Fprintf(serverConn, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32000,\"message\":\"boom\"}}\n", request.ID)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.Call(ctx, "session/prompt", map[string]any{})
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error = %v, want *RPCError", err)
	}
	if rpcErr.Code != -32000 || rpcErr.Message != "boom" {
		t.Fatalf("error = %+v", rpcErr)
	}
}

func TestFrameTooLarge(t *testing.T) {
	reader, writer := io.Pipe()
	client := NewClient(reader, &bytes.Buffer{}, 64)
	done := make(chan error, 1)
	go func() { done <- client.Run() }()

	if _, err := io.WriteString(writer, strings.Repeat("x", 128)+"\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrFrameTooLarge) {
			t.Fatalf("Run error = %v, want ErrFrameTooLarge", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame error")
	}
	_ = writer.Close()
}

func TestStreamCloseRejectsPending(t *testing.T) {
	reader, writer := io.Pipe()
	client := NewClient(reader, &bytes.Buffer{}, 1<<20)
	go func() { _ = client.Run() }()

	errs := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), "wait", map[string]any{})
		errs <- err
	}()
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("Call succeeded after stream close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pending call rejection")
	}
}

func TestCallAfterClose(t *testing.T) {
	client := NewClient(bytes.NewReader(nil), &bytes.Buffer{}, 1<<20)
	client.Close()
	if _, err := client.Call(context.Background(), "x", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Call error = %v, want ErrClosed", err)
	}
}
