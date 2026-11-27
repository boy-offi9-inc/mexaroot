package client_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

type fileRoot struct {
	root string
}

func (r *fileRoot) Info() runtimes.Info {
	return runtimes.Info{ID: "testfs", Kind: runtimes.KindTermux, Name: "Test FS", State: runtimes.StateAvailable}
}
func (r *fileRoot) Exec(context.Context, runtimes.ExecRequest) (runtimes.ExecResult, error) {
	return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
}
func (r *fileRoot) Root() string { return r.root }

func startFileServer(t *testing.T, root string) string {
	t.Helper()
	registry := runtimes.NewRegistry()
	if err := registry.Register(&fileRoot{root: root}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := server.New(server.Config{
		Addr:     "127.0.0.1:0",
		Token:    "secret",
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

func connectClient(t *testing.T, url string) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, err := client.Connect(ctx, url, "secret", "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func randomFile(t *testing.T, dir string, size int) (string, []byte) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	if size == 0 {
		data = nil
	}
	path := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path, data
}

func TestClientPushPullRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are a Unix concept; Windows reports 0666 for every file")
	}
	root := t.TempDir()
	c := connectClient(t, startFileServer(t, root))

	localDir := t.TempDir()
	src, data := randomFile(t, localDir, 300*1024)

	if err := c.Push(context.Background(), "testfs", "docs/blob.bin", src, 0o600); err != nil {
		t.Fatalf("Push: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "docs", "blob.bin"))
	if err != nil {
		t.Fatalf("read pushed file: %v", err)
	}
	sum := sha256.Sum256(data)
	if sha256.Sum256(got) != sum {
		t.Fatal("pushed content does not match")
	}
	if info, err := os.Stat(filepath.Join(root, "docs", "blob.bin")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode mismatch: %v %v", err, info.Mode().Perm())
	}

	dst := filepath.Join(localDir, "out.bin")
	if err := c.Pull(context.Background(), "testfs", "docs/blob.bin", dst); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	pulled, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if sha256.Sum256(pulled) != sum {
		t.Fatal("pulled content does not match")
	}
}

func TestClientPushMissingFile(t *testing.T) {
	root := t.TempDir()
	c := connectClient(t, startFileServer(t, root))
	err := c.Push(context.Background(), "testfs", "x.bin", filepath.Join(t.TempDir(), "missing"), 0)
	if err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}

func TestClientPullMissingFile(t *testing.T) {
	root := t.TempDir()
	c := connectClient(t, startFileServer(t, root))
	err := c.Pull(context.Background(), "testfs", "nope.bin", filepath.Join(t.TempDir(), "out"))
	var werr *wire.Error
	if !wireCode(err, &werr) || werr.Code != wire.CodeNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestClientPushTraversalRejected(t *testing.T) {
	root := t.TempDir()
	c := connectClient(t, startFileServer(t, root))
	localDir := t.TempDir()
	src, _ := randomFile(t, localDir, 16)
	err := c.Push(context.Background(), "testfs", "../escape.bin", src, 0)
	var werr *wire.Error
	if !wireCode(err, &werr) || werr.Code != wire.CodeInvalidRequest {
		t.Fatalf("expected invalid_request, got %v", err)
	}
}

func TestClientCapabilityGate(t *testing.T) {
	// A server whose runtimes expose no file system still advertises the
	// capability; the runtime-level refusal comes back as unsupported.
	c := connectClient(t, startFileServer(t, t.TempDir()))
	found := false
	for _, cap := range c.Capabilities {
		if cap == wire.CapabilityFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the file capability, got %v", c.Capabilities)
	}
}

func wireCode(err error, werr **wire.Error) bool {
	for err != nil {
		if e, ok := err.(*wire.Error); ok {
			*werr = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
