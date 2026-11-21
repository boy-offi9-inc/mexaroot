package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

var errForwardUsage = errors.New("usage: mexaroot forward LOCAL_PORT DAEMON_HOST DAEMON_PORT  (example: mexaroot forward 8080 127.0.0.1 3000)")

// forwardCommand listens on a local TCP port and relays every accepted
// connection to daemonHost:daemonPort reachable from the daemon. Multiple
// simultaneous connections are supported; Ctrl-C tears everything down.
func forwardCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
	if len(args) != 3 {
		return 2, errForwardUsage
	}
	localPort, err := strconv.Atoi(args[0])
	if err != nil || localPort <= 0 || localPort > 65535 {
		return 2, fmt.Errorf("invalid local port %q", args[0])
	}
	host := args[1]
	daemonPort, err := strconv.Atoi(args[2])
	if err != nil || daemonPort <= 0 || daemonPort > 65535 {
		return 2, fmt.Errorf("invalid daemon port %q", args[2])
	}
	if !hasCapability(c, wire.CapabilityForward) {
		return 1, errors.New("the daemon does not offer port forwarding (update the daemon or check its forward settings)")
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
	if err != nil {
		return 1, fmt.Errorf("listen on 127.0.0.1:%d: %w", localPort, err)
	}
	defer ln.Close()
	fmt.Printf("forwarding 127.0.0.1:%d -> %s:%d (through the daemon); Ctrl-C to stop\n", localPort, host, daemonPort)

	// Event pump: forward.data / forward.closed events are dispatched to the
	// channel they belong to.
	type chanState struct {
		conn net.Conn
	}
	var mu sync.Mutex
	channels := make(map[uint32]*chanState)

	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for {
			select {
			case ev, ok := <-c.Events():
				if !ok {
					return
				}
				switch ev.Type {
				case wire.TypeForwardData:
					if ev.Stream == nil {
						continue
					}
					var payload wire.SessionOutput
					if err := ev.DecodePayload(&payload); err != nil {
						continue
					}
					data, err := base64.StdEncoding.DecodeString(payload.Data)
					if err != nil {
						continue
					}
					mu.Lock()
					st := channels[*ev.Stream]
					mu.Unlock()
					if st != nil {
						st.conn.Write(data) //nolint:errcheck — client side went away; close path handles it
					}
				case wire.TypeForwardClosed, wire.TypeForwardFailure:
					if ev.Stream == nil {
						continue
					}
					mu.Lock()
					st := channels[*ev.Stream]
					delete(channels, *ev.Stream)
					mu.Unlock()
					if st != nil {
						st.conn.Close()
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	type result struct{ err error }
	acceptErr := make(chan result, 1)
	go func() {
		for {
			lconn, err := ln.Accept()
			if err != nil {
				acceptErr <- result{err}
				return
			}
			go func() {
				stream, openErr := openForward(ctx, c, host, daemonPort)
				if openErr != nil {
					lconn.Close()
					fmt.Printf("forward: %v\n", openErr)
					return
				}
				mu.Lock()
				channels[stream] = &chanState{conn: lconn}
				mu.Unlock()

				// Relay client -> daemon until the local socket ends.
				buf := make([]byte, 32*1024)
				for {
					n, rerr := lconn.Read(buf)
					if n > 0 {
						req, _ := wire.NewRequest("", wire.TypeForwardData, wire.SessionInput{
							Stream: stream,
							Data:   base64.StdEncoding.EncodeToString(buf[:n]),
						})
						// forward.data is a request-shaped message with a stream field.
						req.Stream = &stream
						if err := c.WriteMessage(ctx, req); err != nil {
							break
						}
					}
					if rerr != nil {
						break
					}
				}
				closeReq, _ := wire.NewRequest("", wire.TypeForwardClose, wire.ForwardClose{Stream: stream})
				closeReq.Stream = &stream
				_ = c.WriteMessage(ctx, closeReq)
				lconn.Close()
				mu.Lock()
				delete(channels, stream)
				mu.Unlock()
			}()
		}
	}()

	select {
	case <-ctx.Done():
		ln.Close()
		<-eventsDone
		return 0, nil
	case r := <-acceptErr:
		<-eventsDone
		if r.err != nil && !errors.Is(r.err, net.ErrClosed) && ctx.Err() == nil {
			return 1, fmt.Errorf("accept: %w", r.err)
		}
		return 0, nil
	}
}

func openForward(ctx context.Context, c *client.Client, host string, port int) (uint32, error) {
	var resp wire.ForwardOpenResponse
	if err := c.Call(ctx, wire.TypeForwardOpen, wire.ForwardOpenRequest{Host: host, Port: port}, &resp); err != nil {
		return 0, err
	}
	return resp.Stream, nil
}

func hasCapability(c *client.Client, cap string) bool {
	for _, v := range c.Capabilities {
		if strings.EqualFold(v, cap) {
			return true
		}
	}
	return false
}
