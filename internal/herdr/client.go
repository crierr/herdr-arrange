// Package herdr is a minimal client for the herdr socket API.
//
// The server speaks newline-delimited JSON over a Unix socket and serves
// exactly one request per connection: it writes a single response line and
// closes. So every call dials afresh.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// DefaultTimeout bounds a single request. Every method we use is a local,
// sub-millisecond operation, so a generous ceiling only ever catches a hang.
const DefaultTimeout = 5 * time.Second

// Client talks to one herdr server.
type Client struct {
	// SocketPath is the Unix socket to dial.
	SocketPath string
	// Timeout bounds each request; zero means DefaultTimeout.
	Timeout time.Duration

	seq atomic.Uint64
}

// New returns a client for the server named by $HERDR_SOCKET_PATH, which herdr
// sets for every plugin process.
func New() (*Client, error) {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return nil, errors.New("HERDR_SOCKET_PATH is not set (not running under herdr?)")
	}
	return &Client{SocketPath: path}, nil
}

// APIError is a structured error response from the server.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Code reports the API error code of err, or "" if err is not an APIError.
func Code(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
}

// Call sends one request and decodes the response's `result` object into out.
//
// params may be nil, which is sent as `{}` (the server requires the key).
// out may be nil to discard the result. Every result object carries a "type"
// discriminator plus the payload fields, so out is typically a small struct
// naming just the field you want, e.g. struct{ Layout LayoutDescription }.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if params == nil {
		params = struct{}{}
	}
	id := "arrange-" + strconv.FormatUint(c.seq.Add(1), 10)

	line, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return fmt.Errorf("dial herdr socket: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", method, err)
	}

	// Responses can be large (session.snapshot), so read with a growable buffer.
	reader := bufio.NewReaderSize(conn, 64*1024)
	var resp response
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %w", method, resp.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}
