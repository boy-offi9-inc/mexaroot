package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	fileChunkSize   = 64 * 1024
	maxTransferSize = 256 << 20
	maxTransfers    = 4
	chunkQueueDepth = 256
	transferTimeout = 10 * time.Minute
)

var ErrPathOutsideRoot = errors.New("path is outside the runtime file system")

type fileTransfer struct {
	chunks chan []byte
	done   chan struct{}
}

func (c *connection) handleFilePush(ctx context.Context, req wire.Message) {
	var params wire.FilePushRequest
	if err := req.DecodePayload(&params); err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	if params.Path == "" {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "path is required"}))
		return
	}
	if params.Size < 0 || params.Size > maxTransferSize {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: fmt.Sprintf("size must be between 0 and %d", maxTransferSize)}))
		return
	}
	_, root, err := c.runtimeRoot(params.Runtime)
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	if _, err := resolveRuntimePath(root, params.Path); err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}

	stream, tr, ok := c.addTransfer()
	if !ok {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "too many concurrent transfers"}))
		return
	}

	resp, err := wire.NewResponse(req, wire.StreamResponse{Stream: stream})
	if err != nil {
		c.removeTransfer(stream)
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.send(ctx, resp)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.receivePush(ctx, stream, tr, root, &params)
	}()
}

func (c *connection) receivePush(ctx context.Context, stream uint32, tr *fileTransfer, root string, params *wire.FilePushRequest) {
	defer c.removeTransfer(stream)

	done := func(err error) { c.doneEvent(ctx, stream, err) }

	rel, err := resolveRuntimeRel(root, params.Path)
	if err != nil {
		done(err)
		return
	}
	// Every operation below goes through os.Root, so even a symlink planted
	// inside the runtime root cannot redirect the write outside of it.
	rd, err := os.OpenRoot(root)
	if err != nil {
		done(fmt.Errorf("open runtime root: %w", err))
		return
	}
	defer rd.Close()
	if dir := path.Dir(rel); dir != "." {
		if err := rd.MkdirAll(dir, 0o755); err != nil {
			done(fmt.Errorf("create directory: %w", err))
			return
		}
	}

	file, err := rd.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode(params.Mode))
	if err != nil {
		done(fileError("create", err))
		return
	}

	hash := sha256.New()
	written, recvErr := receiveChunks(tr, io.MultiWriter(file, hash), params.Size)
	closeErr := file.Close()
	switch {
	case recvErr != nil:
		rd.Remove(rel)
		done(recvErr)
	case closeErr != nil:
		rd.Remove(rel)
		done(fileError("close", closeErr))
	case written != params.Size:
		rd.Remove(rel)
		done(fmt.Errorf("size mismatch: received %d of %d bytes", written, params.Size))
	case hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(params.SHA256):
		rd.Remove(rel)
		done(fmt.Errorf("sha256 mismatch"))
	default:
		done(nil)
	}
}

func (c *connection) handleFilePull(ctx context.Context, req wire.Message) {
	var params wire.FilePullRequest
	if err := req.DecodePayload(&params); err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	if params.Path == "" {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "path is required"}))
		return
	}
	_, root, err := c.runtimeRoot(params.Runtime)
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}

	rel, err := resolveRuntimeRel(root, params.Path)
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	// os.Root confines the open to the runtime root, including refusing
	// symlinks that point outside it.
	rd, err := os.OpenRoot(root)
	if err != nil {
		c.send(ctx, errorResponse(req, fmt.Errorf("open runtime root: %w", err)))
		return
	}
	defer rd.Close()
	file, err := rd.Open(rel)
	if err != nil {
		c.send(ctx, errorResponse(req, fileError("open", err)))
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.send(ctx, errorResponse(req, fileError("stat", err)))
		return
	}
	if info.IsDir() {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "path is a directory"}))
		return
	}
	if info.Size() > maxTransferSize {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: fmt.Sprintf("file exceeds the %d byte transfer limit", maxTransferSize)}))
		return
	}
	// Hash the whole file before announcing the stream so the
	// response always carries a digest the client can verify.
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		c.send(ctx, errorResponse(req, fileError("read", err)))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.send(ctx, errorResponse(req, fileError("read", err)))
		return
	}

	stream := c.nextTransferStream()
	resp, err := wire.NewResponse(req, wire.FilePullResponse{
		Stream: stream,
		Size:   info.Size(),
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Mode:   uint32(info.Mode().Perm()),
	})
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.send(ctx, resp)

	buf := make([]byte, fileChunkSize)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := c.sendChunk(ctx, stream, buf[:n]); err != nil {
				return
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return
		}
	}
	c.sendChunk(ctx, stream, nil)
}

func (c *connection) runtimeRoot(id string) (runtimes.Runtime, string, error) {
	if id == "" {
		return nil, "", &wire.Error{Code: wire.CodeInvalidRequest, Message: "runtime is required"}
	}
	rt, err := c.server.cfg.Registry.Get(id)
	if err != nil {
		return nil, "", &wire.Error{Code: wire.CodeNotFound, Message: err.Error()}
	}
	root, ok := rt.(runtimes.FileRoot)
	if !ok {
		return nil, "", &wire.Error{Code: wire.CodeUnsupported, Message: "runtime does not expose a file system"}
	}
	return rt, root.Root(), nil
}

func (c *connection) addTransfer() (uint32, *fileTransfer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.transfers) >= maxTransfers {
		return 0, nil, false
	}
	c.nextStream++
	tr := &fileTransfer{
		chunks: make(chan []byte, chunkQueueDepth),
		done:   make(chan struct{}),
	}
	c.transfers[c.nextStream] = tr
	return c.nextStream, tr, true
}

func (c *connection) nextTransferStream() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextStream++
	return c.nextStream
}

func (c *connection) removeTransfer(stream uint32) {
	c.mu.Lock()
	tr, ok := c.transfers[stream]
	if ok {
		delete(c.transfers, stream)
	}
	c.mu.Unlock()
	if ok {
		close(tr.done)
	}
}

func (c *connection) closeTransfers() {
	c.mu.Lock()
	open := make([]*fileTransfer, 0, len(c.transfers))
	for stream, tr := range c.transfers {
		open = append(open, tr)
		delete(c.transfers, stream)
	}
	c.mu.Unlock()
	for _, tr := range open {
		close(tr.done)
	}
}

func (c *connection) sendChunk(ctx context.Context, stream uint32, data []byte) error {
	frame, err := wire.EncodeChunk(stream, data)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageBinary, frame)
}

func (c *connection) doneEvent(ctx context.Context, stream uint32, err error) {
	payload := wire.FileDone{OK: err == nil}
	if err != nil {
		payload.Error = &wire.Error{Code: wire.CodeInternal, Message: err.Error()}
	}
	c.sendEvent(ctx, wire.TypeFileDone, stream, payload)
}

// resolveRuntimeRel resolves a guest path to a slash path relative to the
// runtime root, rejecting anything that escapes it. The result is safe to
// pass to os.Root operations, which enforce the same containment at the
// kernel level (including refusing symlink escapes).
func resolveRuntimeRel(root, guest string) (string, error) {
	if guest == "" {
		return "", &wire.Error{Code: wire.CodeInvalidRequest, Message: "path is required"}
	}
	// Guest paths are POSIX by protocol contract ("dir/sub/file", "/data/..."),
	// even when the daemon runs on a host whose native separator is "\" — so
	// this resolution always uses the POSIX path package. Go's os package
	// accepts forward-slash paths on Windows, so the resolved target opens
	// correctly there too.
	rootSlash := filepath.ToSlash(root)
	if rootSlash == "" {
		rootSlash = "/"
	}
	clean := path.Clean(filepath.ToSlash(guest))
	rel := clean
	if !path.IsAbs(clean) {
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return "", &wire.Error{Code: wire.CodeInvalidRequest, Message: ErrPathOutsideRoot.Error()}
		}
	} else {
		// Absolute guest paths live inside the root: strip the root prefix.
		var err error
		rel, err = filepath.Rel(rootSlash, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", &wire.Error{Code: wire.CodeInvalidRequest, Message: ErrPathOutsideRoot.Error()}
		}
	}
	return rel, nil
}

// resolveRuntimePath returns the host path a guest path resolves to. New
// file operations should prefer resolveRuntimeRel with os.Root, which
// confines every syscall to the runtime root.
func resolveRuntimePath(root, guest string) (string, error) {
	rel, err := resolveRuntimeRel(root, guest)
	if err != nil {
		return "", err
	}
	rootSlash := filepath.ToSlash(root)
	if rootSlash == "" {
		rootSlash = "/"
	}
	return path.Join(rootSlash, rel), nil
}

func receiveChunks(tr *fileTransfer, dst io.Writer, size int64) (int64, error) {
	var written int64
	deadline := time.Now().Add(transferTimeout)
	for written < size {
		select {
		case chunk, ok := <-tr.chunks:
			if !ok {
				return written, errors.New("transfer interrupted: connection closed")
			}
			if len(chunk) == 0 {
				return written, fmt.Errorf("size mismatch: received %d of %d bytes", written, size)
			}
			n, err := dst.Write(chunk)
			written += int64(n)
			if err != nil {
				return written, fmt.Errorf("write: %w", err)
			}
			if int64(n) != int64(len(chunk)) {
				return written, io.ErrShortWrite
			}
		case <-tr.done:
			return written, errors.New("transfer interrupted: connection closed")
		case <-time.After(time.Until(deadline)):
			return written, errors.New("transfer timed out")
		}
	}
	select {
	case chunk, ok := <-tr.chunks:
		if !ok {
			return written, nil
		}
		if len(chunk) == 0 {
			return written, nil
		}
		return written, errors.New("client sent more data than declared")
	case <-tr.done:
		return written, nil
	case <-time.After(time.Until(deadline)):
		return written, nil
	}
}

func fileMode(mode uint32) os.FileMode {
	if mode == 0 {
		return 0o644
	}
	return os.FileMode(mode & 0o777)
}

func fileError(op string, err error) error {
	msg := err.Error()
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &wire.Error{Code: wire.CodeNotFound, Message: msg}
	case errors.Is(err, os.ErrPermission):
		return &wire.Error{Code: wire.CodeInvalidRequest, Message: msg}
	default:
		return &wire.Error{Code: wire.CodeInternal, Message: op + ": " + msg}
	}
}
