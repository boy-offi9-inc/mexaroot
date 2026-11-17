package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	fileChunkSize   = 64 * 1024
	maxTransferSize = 256 << 20
	transferTimeout = 10 * time.Minute
)

var ErrUnsupported = errors.New("the daemon does not offer file transfer")

type transferEvent struct {
	err error
}

type chunkFrame struct {
	stream uint32
	data   []byte
	err    error
}

// Push sends a local file to path inside the runtime's file system.
func (c *Client) Push(ctx context.Context, runtimeID, path, source string, mode uint32) error {
	if err := c.checkFileCapability(); err != nil {
		return err
	}

	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", source)
	}
	if info.Size() > maxTransferSize {
		return fmt.Errorf("file exceeds the %d byte transfer limit", maxTransferSize)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}

	var started wire.StreamResponse
	err = c.Call(ctx, wire.TypeFilePush, wire.FilePushRequest{
		Runtime: runtimeID,
		Path:    path,
		Size:    info.Size(),
		SHA256:  hex.EncodeToString(hasher.Sum(nil)),
		Mode:    mode,
	}, &started)
	if err != nil {
		return err
	}

	return c.sendChunks(ctx, started.Stream, file, info.Size())
}

// Pull receives the file at path inside the runtime's file system and
// writes it to target. An existing target is overwritten.
func (c *Client) Pull(ctx context.Context, runtimeID, path, target string) error {
	if err := c.checkFileCapability(); err != nil {
		return err
	}

	events := make(chan transferEvent, 1)
	stop := c.watchFileDone(events)
	defer stop()

	var announced wire.FilePullResponse
	if err := c.Call(ctx, wire.TypeFilePull, wire.FilePullRequest{Runtime: runtimeID, Path: path}, &announced); err != nil {
		return err
	}
	if announced.Size > maxTransferSize {
		return fmt.Errorf("file exceeds the %d byte transfer limit", maxTransferSize)
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(announced.Mode&0o777))
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	hasher := sha256.New()
	written, chunkErr := c.receiveChunks(ctx, announced.Stream, io.MultiWriter(file, hasher), announced.Size)
	closeErr := file.Close()
	if chunkErr != nil || closeErr != nil {
		os.Remove(target)
	}
	switch {
	case chunkErr != nil:
		return chunkErr
	case closeErr != nil:
		return fmt.Errorf("close %s: %w", target, closeErr)
	}

	var evErr error
	select {
	case ev := <-events:
		evErr = ev.err
	default:
	}
	if evErr != nil {
		os.Remove(target)
		return evErr
	}

	if written != announced.Size {
		os.Remove(target)
		return fmt.Errorf("size mismatch: received %d of %d bytes", written, announced.Size)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != strings.ToLower(announced.SHA256) {
		os.Remove(target)
		return fmt.Errorf("sha256 mismatch: expected %s, received %s", strings.ToLower(announced.SHA256), got)
	}
	return nil
}

func (c *Client) checkFileCapability() error {
	for _, cap := range c.Capabilities {
		if cap == wire.CapabilityFile {
			return nil
		}
	}
	return ErrUnsupported
}

func (c *Client) sendChunks(ctx context.Context, stream uint32, file *os.File, size int64) error {
	buf := make([]byte, fileChunkSize)
	var written int64
	for written < size {
		n, err := file.Read(buf)
		if n > 0 {
			frame, encErr := wire.EncodeChunk(stream, buf[:n])
			if encErr != nil {
				return encErr
			}
			if err := c.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
				return fmt.Errorf("send chunk: %w", err)
			}
			written += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
	}
	frame, err := wire.EncodeChunk(stream, nil)
	if err != nil {
		return err
	}
	if err := c.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return fmt.Errorf("send end marker: %w", err)
	}
	return c.waitFileDone(ctx)
}

func (c *Client) receiveChunks(ctx context.Context, stream uint32, dst io.Writer, size int64) (int64, error) {
	var written int64
	deadline := time.Now().Add(transferTimeout)
	for written < size {
		frame, err := c.nextChunk(ctx, stream, deadline)
		if err != nil {
			return written, err
		}
		if len(frame) == 0 {
			return written, fmt.Errorf("size mismatch: received %d of %d bytes", written, size)
		}
		n, err := dst.Write(frame)
		written += int64(n)
		if err != nil {
			return written, fmt.Errorf("write: %w", err)
		}
		if int64(n) != int64(len(frame)) {
			return written, io.ErrShortWrite
		}
	}
	for {
		frame, err := c.nextChunk(ctx, stream, deadline)
		if err != nil {
			return written, err
		}
		if len(frame) == 0 {
			return written, nil
		}
		return written, errors.New("daemon sent more data than declared")
	}
}

// nextChunk returns the next chunk for stream, skipping frames that
// belong to other transfers.
func (c *Client) nextChunk(ctx context.Context, stream uint32, deadline time.Time) ([]byte, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		select {
		case frame, ok := <-c.chunks:
			if !ok {
				return nil, ErrClosed
			}
			if frame.err != nil {
				return nil, frame.err
			}
			if frame.stream != stream {
				continue
			}
			return frame.data, nil
		case <-timer.C:
			return nil, errors.New("transfer timed out")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Client) waitFileDone(ctx context.Context) error {
	events := make(chan transferEvent, 1)
	stop := c.watchFileDone(events)
	defer stop()

	select {
	case ev := <-events:
		return ev.err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(transferTimeout):
		return errors.New("transfer timed out waiting for file.done")
	}
}

// watchFileDone consumes file.done events until stop is called.
func (c *Client) watchFileDone(events chan<- transferEvent) func() {
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			select {
			case ev, ok := <-c.Events():
				if !ok {
					events <- transferEvent{err: errors.New("connection closed before file.done")}
					return
				}
				if ev.Type == wire.TypeFileDone {
					var payload wire.FileDone
					if err := ev.DecodePayload(&payload); err != nil {
						events <- transferEvent{err: err}
						return
					}
					if payload.OK {
						events <- transferEvent{}
					} else if payload.Error != nil {
						events <- transferEvent{err: payload.Error}
					} else {
						events <- transferEvent{err: errors.New("transfer failed")}
					}
					return
				}
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
