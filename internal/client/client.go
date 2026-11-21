package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	readLimit   = 1 << 20
	eventBuffer = 256
)

var ErrClosed = errors.New("connection closed")

type Client struct {
	Capabilities []string
	Daemon       string
	Protocol     int

	conn      *websocket.Conn
	events    chan wire.Message
	chunks    chan chunkFrame
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	next    int
	pending map[string]chan wire.Message
}

func Connect(ctx context.Context, url, token, name string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	conn.SetReadLimit(readLimit)

	c := &Client{
		conn:    conn,
		events:  make(chan wire.Message, eventBuffer),
		chunks:  make(chan chunkFrame, eventBuffer),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
		pending: make(map[string]chan wire.Message),
	}
	go c.readLoop()

	var hello wire.HelloResponse
	err = c.Call(ctx, wire.TypeHello, wire.HelloRequest{
		Client:   name,
		Protocol: wire.SupportedVersions,
		Token:    token,
	}, &hello)
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	c.Capabilities = hello.Capabilities
	c.Daemon = hello.Daemon
	c.Protocol = hello.Protocol
	return c, nil
}

func (c *Client) Events() <-chan wire.Message {
	return c.events
}

func (c *Client) Call(ctx context.Context, msgType string, payload, out any) error {
	id, ch := c.register()

	req, err := wire.NewRequest(id, msgType, payload)
	if err != nil {
		c.unregister(id)
		return err
	}
	if err := c.write(ctx, req); err != nil {
		c.unregister(id)
		return fmt.Errorf("send %s: %w", msgType, err)
	}

	select {
	case resp := <-ch:
		return decodeResponse(resp, out)
	case <-c.done:
		select {
		case resp := <-ch:
			return decodeResponse(resp, out)
		default:
		}
		return fmt.Errorf("%s: %w", msgType, ErrClosed)
	case <-ctx.Done():
		c.unregister(id)
		return ctx.Err()
	}
}

func (c *Client) Send(ctx context.Context, msgType string, payload any) error {
	c.mu.Lock()
	c.next++
	id := strconv.Itoa(c.next)
	c.mu.Unlock()

	req, err := wire.NewRequest(id, msgType, payload)
	if err != nil {
		return err
	}
	if err := c.write(ctx, req); err != nil {
		return fmt.Errorf("send %s: %w", msgType, err)
	}
	return nil
}

// WriteMessage writes an already-built message without a pending-call id.
// Used for stream-scoped requests (forward.data, forward.close) whose replies
// are handled through events or are ignored.
func (c *Client) WriteMessage(ctx context.Context, msg wire.Message) error {
	return c.write(ctx, msg)
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *Client) write(ctx context.Context, msg wire.Message) error {
	data, err := wire.Encode(msg)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) register() (string, chan wire.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := strconv.Itoa(c.next)
	ch := make(chan wire.Message, 1)
	c.pending[id] = ch
	return id, ch
}

func (c *Client) unregister(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *Client) readLoop() {
	defer close(c.done)
	defer close(c.events)
	defer close(c.chunks)

	for {
		msgType, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		if msgType == websocket.MessageBinary {
			stream, chunk, cerr := wire.DecodeChunk(data)
			select {
			case c.chunks <- chunkFrame{stream: stream, data: chunk, err: cerr}:
			case <-c.closed:
				return
			}
			continue
		}
		msg, err := wire.Decode(data)
		if err != nil {
			continue
		}

		if msg.ID == "" && msg.Stream != nil {
			select {
			case c.events <- msg:
			case <-c.closed:
				return
			}
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok {
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

func decodeResponse(resp wire.Message, out any) error {
	if resp.OK == nil || !*resp.OK {
		if resp.Error != nil {
			return resp.Error
		}
		return errors.New("request failed")
	}
	if out == nil {
		return nil
	}
	return resp.DecodePayload(out)
}
