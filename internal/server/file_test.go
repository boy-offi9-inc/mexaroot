package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

// stubFileRoot is a runtime that exposes a temporary directory as its
// file system, so tests do not touch the process working directory.
type stubFileRoot struct {
	root string
}

func (r *stubFileRoot) Info() runtimes.Info {
	return runtimes.Info{ID: "testfs", Kind: runtimes.KindTermux, Name: "Test FS", State: runtimes.StateAvailable}
}
func (r *stubFileRoot) Exec(context.Context, runtimes.ExecRequest) (runtimes.ExecResult, error) {
	return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
}
func (r *stubFileRoot) Root() string { return r.root }

func newFileTestServerURL(t *testing.T, root string) string {
	t.Helper()
	registry := runtimes.NewRegistry()
	if err := registry.Register(&stubFileRoot{root: root}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Token:    testToken,
		Version:  "test",
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeBinary sends one chunk frame.
func writeBinary(ctx context.Context, t *testing.T, conn *websocket.Conn, stream uint32, data []byte) {
	t.Helper()
	frame, err := wire.EncodeChunk(stream, data)
	if err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("Write binary: %v", err)
	}
}

// readBinary reads one chunk frame.
func readBinary(ctx context.Context, t *testing.T, conn *websocket.Conn) (uint32, []byte) {
	t.Helper()
	conn.SetReadLimit(1 << 22)
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read binary: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Fatalf("expected a binary frame, got %v", msgType)
	}
	stream, chunk, err := wire.DecodeChunk(data)
	if err != nil {
		t.Fatalf("DecodeChunk: %v", err)
	}
	return stream, chunk
}

// readText reads one JSON message and fails the test on a frame error.
func readText(ctx context.Context, t *testing.T, conn *websocket.Conn) wire.Message {
	t.Helper()
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read text: %v", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		msg, err := wire.Decode(data)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return msg
	}
}

// expectFileDone waits for the file.done event on the stream.
func expectFileDone(ctx context.Context, t *testing.T, conn *websocket.Conn, stream uint32) wire.FileDone {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		msg := readText(readCtx, t, conn)
		cancel()
		if msg.Type != wire.TypeFileDone || msg.Stream == nil || *msg.Stream != stream {
			continue
		}
		var payload wire.FileDone
		if err := msg.DecodePayload(&payload); err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}
		return payload
	}
	t.Fatal("no file.done event received")
	return wire.FileDone{}
}

func writeTempFile(t *testing.T, dir string, size int) (string, []byte) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	if size == 0 {
		data = nil
	}
	path := filepath.Join(dir, fmtFile(size))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path, data
}

func fmtFile(size int) string {
	if size == 0 {
		return "empty.bin"
	}
	return "blob-" + hex.EncodeToString([]byte{byte(size), byte(size >> 8), byte(size >> 16)}) + ".bin"
}

func TestResolveRuntimePath(t *testing.T) {
	root := "/data/ubuntu"
	for _, path := range []string{"../escape", "a/../../b", "..", ""} {
		if _, err := resolveRuntimePath(root, path); err == nil {
			t.Errorf("expected %q to be rejected", path)
		}
	}
	cases := map[string]string{
		"file.txt":        "/data/ubuntu/file.txt",
		"dir/sub/file":    "/data/ubuntu/dir/sub/file",
		"dir/../file.txt": "/data/ubuntu/file.txt",
		"./file.txt":      "/data/ubuntu/file.txt",
		"/data/ubuntu/x":  "/data/ubuntu/x",
	}
	for path, want := range cases {
		got, err := resolveRuntimePath(root, path)
		if err != nil {
			t.Fatalf("resolveRuntimePath(%q): %v", path, err)
		}
		if got != want {
			t.Fatalf("resolveRuntimePath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFileMode(t *testing.T) {
	if got := fileMode(0); got != 0o644 {
		t.Fatalf("fileMode(0) = %o, want 644", got)
	}
	if got := fileMode(0o755 | 0o170000); got != 0o755 {
		t.Fatalf("fileMode with special bits = %o, want 755", got)
	}
}

func TestFileErrorMapping(t *testing.T) {
	_, err := os.ReadFile(filepath.Join(t.TempDir(), "missing"))
	var werr *wire.Error
	if !errors.As(fileError("open", err), &werr) || werr.Code != wire.CodeNotFound {
		t.Fatalf("expected not_found, got %v", fileError("open", err))
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	err = os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o600)
	if err == nil {
		t.Skip("running as root; permission errors do not apply")
	}
	werr, ok := fileError("write", err).(*wire.Error)
	if !ok || werr.Code == wire.CodeNotFound {
		t.Fatalf("unexpected error mapping for %v", err)
	}
}

func TestFilePushRejectsOversize(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePush, wire.FilePushRequest{
		Runtime: "termux",
		Path:    "ok.txt",
		Size:    1 << 40,
		SHA256:  digest(nil),
	})
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestFilePushMissingPath(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePush, wire.FilePushRequest{Runtime: "termux"})
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestFilePullMissingRuntime(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePull, wire.FilePullRequest{Runtime: "missing", Path: "x"})
	expectError(t, resp, wire.CodeNotFound)
}

func TestFilePushTraversalRejected(t *testing.T) {
	ctx, conn := dial(t, newFileTestServerURL(t, t.TempDir()))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePush, wire.FilePushRequest{
		Runtime: "testfs",
		Path:    "../outside.txt",
		Size:    0,
		SHA256:  digest(nil),
	})
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestFilePushEndToEnd(t *testing.T) {
	root := t.TempDir()
	ctx, conn := dial(t, newFileTestServerURL(t, root))
	authenticate(ctx, t, conn)

	dir := t.TempDir()
	path, data := writeTempFile(t, dir, 200*1024)
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	resp := call(ctx, t, conn, wire.TypeFilePush, wire.FilePushRequest{
		Runtime: "testfs",
		Path:    "dir/uploaded.bin",
		Size:    int64(len(data)),
		SHA256:  digest(data),
		Mode:    uint32(mode),
	})
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("file.push failed: %+v", resp.Error)
	}
	var started wire.StreamResponse
	if err := resp.DecodePayload(&started); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	for offset := 0; offset < len(data); offset += wire.MaxChunkSize {
		end := offset + wire.MaxChunkSize
		if end > len(data) {
			end = len(data)
		}
		writeBinary(ctx, t, conn, started.Stream, data[offset:end])
	}
	writeBinary(ctx, t, conn, started.Stream, nil)

	done := expectFileDone(ctx, t, conn, started.Stream)
	if !done.OK {
		t.Fatalf("file.done reported failure: %+v", done.Error)
	}

	got, err := os.ReadFile(filepath.Join(root, "dir", "uploaded.bin"))
	if err != nil {
		t.Fatalf("read pushed file: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("size mismatch: got %d, want %d", len(got), len(data))
	}
	if info, err := os.Stat(filepath.Join(root, "dir", "uploaded.bin")); err != nil || info.Mode().Perm() != mode {
		t.Fatalf("mode mismatch: %v %v", err, info.Mode().Perm())
	}
}

func TestFilePushSizeMismatch(t *testing.T) {
	root := t.TempDir()
	ctx, conn := dial(t, newFileTestServerURL(t, root))
	authenticate(ctx, t, conn)

	resp := call(ctx, t, conn, wire.TypeFilePush, wire.FilePushRequest{
		Runtime: "testfs",
		Path:    "short.bin",
		Size:    10,
		SHA256:  digest(make([]byte, 10)),
	})
	var started wire.StreamResponse
	if err := resp.DecodePayload(&started); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	writeBinary(ctx, t, conn, started.Stream, []byte("abc"))
	writeBinary(ctx, t, conn, started.Stream, nil)

	done := expectFileDone(ctx, t, conn, started.Stream)
	if done.OK {
		t.Fatal("expected file.done to report a size mismatch")
	}
	if _, err := os.Stat(filepath.Join(root, "short.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file was not removed: %v", err)
	}
}

func TestFilePullEndToEnd(t *testing.T) {
	root := t.TempDir()
	ctx, conn := dial(t, newFileTestServerURL(t, root))
	authenticate(ctx, t, conn)

	_, data := writeTempFile(t, root, 200*1024)
	remote := fmtFile(200 * 1024)

	resp := call(ctx, t, conn, wire.TypeFilePull, wire.FilePullRequest{
		Runtime: "testfs",
		Path:    remote,
	})
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("file.pull failed: %+v", resp.Error)
	}
	var announced wire.FilePullResponse
	if err := resp.DecodePayload(&announced); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if announced.Size != int64(len(data)) || announced.SHA256 != digest(data) {
		t.Fatalf("unexpected announcement: %+v", announced)
	}

	var received []byte
	for {
		stream, chunk := readBinary(ctx, t, conn)
		if stream != announced.Stream {
			t.Fatalf("unexpected stream %d", stream)
		}
		if len(chunk) == 0 {
			break
		}
		received = append(received, chunk...)
	}
	if len(received) != len(data) {
		t.Fatalf("size mismatch: got %d, want %d", len(received), len(data))
	}
	if digest(received) != digest(data) {
		t.Fatal("sha256 mismatch")
	}
}

func TestFilePullMissingFile(t *testing.T) {
	ctx, conn := dial(t, newFileTestServerURL(t, t.TempDir()))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePull, wire.FilePullRequest{
		Runtime: "testfs",
		Path:    "nope.bin",
	})
	expectError(t, resp, wire.CodeNotFound)
}

func TestFilePullDirectoryRejected(t *testing.T) {
	ctx, conn := dial(t, newFileTestServerURL(t, t.TempDir()))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeFilePull, wire.FilePullRequest{
		Runtime: "testfs",
		Path:    ".",
	})
	expectError(t, resp, wire.CodeInvalidRequest)
}

var _ = io.EOF
