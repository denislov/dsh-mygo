// Package channel implements the newline-delimited JSON-RPC 2.0 transport used
// between the MyGo UI and the DeepSeek Harness host process.
package channel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// DefaultMaxFrameBytes bounds one inbound or outbound JSON-RPC frame.
// SDK session events can be large because assistant messages embed their
// complete model stream; the custom dsh-mygo channel may use a smaller limit.
const DefaultMaxFrameBytes = 16 << 20

var (
	// ErrFrameTooLarge reports a frame that exceeds the configured limit.
	ErrFrameTooLarge = errors.New("channel: JSON-RPC frame too large")
	// ErrClosed reports use of a client whose read loop has stopped.
	ErrClosed = errors.New("channel: JSON-RPC client closed")
)

// RPCError is a JSON-RPC error response. Code is the wire integer code; Data
// stays raw so callers can decode their own structured payload.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements error.
func (e *RPCError) Error() string {
	if e == nil {
		return "channel: unknown JSON-RPC error"
	}
	return fmt.Sprintf("channel: JSON-RPC error %d: %s", e.Code, e.Message)
}

// NotificationHandler receives a server-to-client notification.
type NotificationHandler func(method string, params json.RawMessage)

// RequestHandler answers a server-to-client request. The SDK protocol does not
// use this direction today; the custom channel will use it for approvals and
// user questions.
type RequestHandler func(ctx context.Context, method string, params json.RawMessage) (any, *RPCError)

// message is the union of the three JSON-RPC frame kinds.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type outboundRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type pendingResponse struct {
	result json.RawMessage
	err    *RPCError
}

// Client is one JSON-RPC endpoint over caller-owned byte streams. Run reads
// frames until the stream ends; Call and Notify write frames safely from any
// goroutine. The client does not own or close the streams.
type Client struct {
	r        *bufio.Reader
	w        io.Writer
	maxFrame int

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan pendingResponse
	closed  bool
	err     error

	nextID atomic.Uint64

	notificationHandler NotificationHandler
	requestHandler      RequestHandler

	done      chan struct{}
	closeOnce sync.Once
}

// NewClient creates a client over r and w. A nil or non-positive maxFrame uses
// DefaultMaxFrameBytes.
func NewClient(r io.Reader, w io.Writer, maxFrame int) *Client {
	if maxFrame <= 0 {
		maxFrame = DefaultMaxFrameBytes
	}
	return &Client{
		r:        bufio.NewReaderSize(r, 64<<10),
		w:        w,
		maxFrame: maxFrame,
		pending:  make(map[string]chan pendingResponse),
		done:     make(chan struct{}),
	}
}

// OnNotification installs the notification handler. It must be set before Run.
func (c *Client) OnNotification(handler NotificationHandler) { c.notificationHandler = handler }

// OnRequest installs the server-to-client request handler. It must be set
// before Run.
func (c *Client) OnRequest(handler RequestHandler) { c.requestHandler = handler }

// Done is closed when the read loop stops.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns the error that stopped the read loop, or nil while running.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Run reads frames until the stream ends or a frame is invalid. It returns the
// terminal error; io.EOF and ErrClosed are normal shutdown outcomes.
func (c *Client) Run() error {
	for {
		msg, err := readMessage(c.r, c.maxFrame)
		if err != nil {
			c.fail(err)
			return err
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			go c.answerRequest(msg)
		case msg.Method != "":
			if c.notificationHandler != nil {
				c.notificationHandler(msg.Method, msg.Params)
			}
		case len(msg.ID) > 0:
			c.resolve(msg)
		}
	}
}

// Call sends a request and waits for its response, cancellation, or closure.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("req_%x", c.nextID.Add(1))
	key := "s:" + id
	ch := make(chan pendingResponse, 1)

	c.mu.Lock()
	if c.closed {
		err := c.err
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrClosed
	}
	c.pending[key] = ch
	c.mu.Unlock()

	if err := c.write(outboundRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.remove(key)
		return nil, err
	}

	select {
	case response := <-ch:
		if response.err != nil {
			return nil, response.err
		}
		return response.result, nil
	case <-ctx.Done():
		c.remove(key)
		return nil, ctx.Err()
	case <-c.done:
		c.remove(key)
		if err := c.Err(); err != nil {
			return nil, err
		}
		return nil, ErrClosed
	}
}

// Notify sends a notification without waiting for a response.
func (c *Client) Notify(method string, params any) error {
	return c.write(outboundNotification{JSONRPC: "2.0", Method: method, Params: params})
}

// Close stops accepting calls and rejects every pending request. The read loop
// may still be blocked on the stream; closing the underlying stream is the
// supervisor's responsibility.
func (c *Client) Close() {
	c.fail(ErrClosed)
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.err == nil {
			c.err = err
		}
		for id, ch := range c.pending {
			delete(c.pending, id)
			ch <- pendingResponse{err: &RPCError{Code: -32000, Message: err.Error()}}
		}
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *Client) remove(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) resolve(msg message) {
	key := idKey(msg.ID)
	c.mu.Lock()
	ch := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if ch == nil {
		return
	}
	ch <- pendingResponse{result: msg.Result, err: msg.Error}
}

func (c *Client) answerRequest(msg message) {
	if c.requestHandler == nil {
		c.writeResponse(outboundResponse{JSONRPC: "2.0", ID: msg.ID,
			Error: &RPCError{Code: -32601, Message: "method not found: " + msg.Method}})
		return
	}
	result, rpcErr := c.requestHandler(context.Background(), msg.Method, msg.Params)
	if rpcErr != nil {
		c.writeResponse(outboundResponse{JSONRPC: "2.0", ID: msg.ID, Error: rpcErr})
		return
	}
	c.writeResponse(outboundResponse{JSONRPC: "2.0", ID: msg.ID, Result: result})
}

func (c *Client) writeResponse(response outboundResponse) {
	if response.Error == nil && response.Result == nil {
		response.Result = json.RawMessage("null")
	}
	_ = c.write(response)
}

func (c *Client) write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("channel: encode JSON-RPC frame: %w", err)
	}
	if len(payload)+1 > c.maxFrame {
		return ErrFrameTooLarge
	}
	payload = append(payload, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(payload); err != nil {
		return fmt.Errorf("channel: write JSON-RPC frame: %w", err)
	}
	return nil
}

// readMessage reads and validates one newline-terminated frame.
func readMessage(r *bufio.Reader, maxFrame int) (message, error) {
	for {
		line, err := readLine(r, maxFrame)
		if err != nil {
			return message{}, err
		}
		if len(line) == 0 || !utf8.Valid(line) {
			return message{}, errors.New("channel: invalid JSON-RPC frame")
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			return message{}, errors.New("channel: invalid JSON-RPC frame")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) != nil || msg.JSONRPC != "2.0" {
			return message{}, errors.New("channel: invalid JSON-RPC version or frame")
		}
		_, hasMethod := fields["method"]
		_, hasResult := fields["result"]
		_, hasError := fields["error"]
		_, hasID := fields["id"]
		if hasID && !validID(msg.ID) {
			return message{}, errors.New("channel: invalid JSON-RPC id")
		}
		if hasMethod {
			if msg.Method == "" || len(msg.Method) > 128 || hasResult || hasError {
				return message{}, errors.New("channel: invalid JSON-RPC request")
			}
			if raw, exists := fields["params"]; exists {
				var params map[string]json.RawMessage
				if json.Unmarshal(raw, &params) != nil || params == nil {
					return message{}, errors.New("channel: invalid JSON-RPC params")
				}
			}
		} else {
			if !hasID || hasResult == hasError || fields["params"] != nil {
				return message{}, errors.New("channel: invalid JSON-RPC response")
			}
			if hasError {
				var failure map[string]json.RawMessage
				if json.Unmarshal(fields["error"], &failure) != nil || msg.Error == nil || failure["code"] == nil || failure["message"] == nil || msg.Error.Message == "" {
					return message{}, errors.New("channel: invalid JSON-RPC error")
				}
			}
		}
		return msg, nil
	}
}

func validID(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch identifier := value.(type) {
	case string:
		return len(identifier) > 0 && len(identifier) <= 128
	case float64:
		return identifier == math.Trunc(identifier) && math.Abs(identifier) <= 9007199254740991
	default:
		return false
	}
}

// readLine reads through the next newline and rejects incomplete final frames.
func readLine(r *bufio.Reader, maxFrame int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > maxFrame {
			return nil, ErrFrameTooLarge
		}
		line = append(line, chunk...)
		if err == nil {
			return bytes.TrimSpace(line), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

// idKey normalizes a JSON-RPC id so string and numeric peers correlate the same
// way. The client only generates string ids; numeric support covers peers that
// choose them.
func idKey(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return "s:" + text
	}
	return "r:" + string(raw)
}
