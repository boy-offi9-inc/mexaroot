package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s
}

func TestDevicesCreateAndVerify(t *testing.T) {
	s := testStore(t)
	first, err := s.Create("phone", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.Token == "" || first.Name != "phone" {
		t.Fatalf("bad device: %+v", first)
	}
	device, ok := s.Verify(first.Token)
	if !ok || device.Name != "phone" {
		t.Fatalf("Verify: got %+v ok=%v", device, ok)
	}
	if _, ok := s.Verify("nope"); ok {
		t.Fatal("unknown token must not verify")
	}
}

func TestDevicesDuplicateNameRejected(t *testing.T) {
	s := testStore(t)
	if _, err := s.Create("phone", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Create("phone", nil); !errors.Is(err, ErrDeviceExists) {
		t.Fatalf("want ErrDeviceExists, got %v", err)
	}
	// The failed create must not have corrupted the store.
	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List after duplicate: %v, %d devices", err, len(list))
	}
}

func TestDevicesNames(t *testing.T) {
	s := testStore(t)
	for _, name := range []string{"", "with\nnewline", "emoji😀", string(make([]byte, 65))} {
		if _, err := s.Create(name, nil); !errors.Is(err, ErrDeviceName) {
			t.Fatalf("name %q: want ErrDeviceName, got %v", name, err)
		}
	}
	for _, name := range []string{"laptop", "Pixel-7", "a.b_c d", strings.Repeat("a", 64)} {
		if _, err := s.Create(name, nil); err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	}
}

func TestDevicesRevoke(t *testing.T) {
	s := testStore(t)
	device, err := s.Create("phone", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke("phone"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := s.Verify(device.Token); ok {
		t.Fatal("revoked token must stop verifying")
	}
	if err := s.Revoke("phone"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("double revoke: want ErrDeviceNotFound, got %v", err)
	}
	if err := s.Revoke("nope"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("revoke unknown: want ErrDeviceNotFound, got %v", err)
	}
}

func TestDevicesRotate(t *testing.T) {
	s := testStore(t)
	device, err := s.Create("phone", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rotated, err := s.Rotate("phone")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.Token == device.Token {
		t.Fatal("rotate must mint a new token")
	}
	if rotated.Name != "phone" || rotated.RotatedAt == nil {
		t.Fatalf("rotate must keep the name and set rotatedAt: %+v", rotated)
	}
	if _, ok := s.Verify(device.Token); ok {
		t.Fatal("old token must stop verifying after rotate")
	}
	if _, ok := s.Verify(rotated.Token); !ok {
		t.Fatal("new token must verify after rotate")
	}
	if _, err := s.Rotate("nope"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("rotate unknown: want ErrDeviceNotFound, got %v", err)
	}
}

func TestDevicesListOrderAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	a, _ := s.Create("alpha", nil)
	b, _ := s.Create("beta", nil)
	c, _ := s.Create("gamma", nil)

	// A second handle on the same file sees everything.
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	list, err := reopened.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 || list[0].Name != "alpha" || list[1].Name != "beta" || list[2].Name != "gamma" {
		t.Fatalf("list order: %+v", list)
	}
	if _, err := reopened.Rotate("beta"); err != nil {
		t.Fatalf("rotate via second handle: %v", err)
	}
	if _, ok := s.Verify(b.Token); ok {
		t.Fatal("first handle must see the second handle's revoke effect")
	}
	if _, ok := s.Verify(c.Token); !ok {
		t.Fatal("unrelated device must keep verifying")
	}
	_ = a
}

func TestDevicesFilePermissions(t *testing.T) {
	if os.Getenv("GOOS_TEST_WINDOWS") == "1" {
		t.Skip("permissions are a Unix concept")
	}
	path := filepath.Join(t.TempDir(), "devices.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := s.Create("phone", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("devices file mode = %o, want owner-only", info.Mode().Perm())
	}
}

func TestDevicesRejectsLooseFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte(`{"devices":[]}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := OpenStore(path); err == nil {
		t.Fatal("a world-readable devices file must be refused")
	}
}

func TestDevicesGarbageFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := OpenStore(path)
	if err == nil {
		_ = s
		t.Fatal("garbage devices file must be refused")
	}
}

func TestDevicesCapabilitiesExpand(t *testing.T) {
	caps, err := ExpandCapabilities([]string{"files", "exec", "files"})
	if err != nil {
		t.Fatalf("ExpandCapabilities: %v", err)
	}
	want := []string{CapFilesRead, CapFilesWrite, CapExec}
	if len(caps) != len(want) {
		t.Fatalf("got %v, want %v", caps, want)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Fatalf("got %v, want %v", caps, want)
		}
	}
	if _, err := ExpandCapabilities([]string{"teleport"}); !errors.Is(err, ErrDeviceCap) {
		t.Fatalf("unknown capability: want ErrDeviceCap, got %v", err)
	}
	caps, err = ExpandCapabilities([]string{GroupAll})
	if err != nil || len(caps) != len(AllCapabilities()) {
		t.Fatalf("all: %v, %d caps", err, len(caps))
	}
}

func TestDevicesCapabilitiesStoreRoundTrip(t *testing.T) {
	s := testStore(t)
	ro, err := s.Create("spectator", []string{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ro.Capabilities == nil || len(ro.Capabilities) != 0 {
		t.Fatalf("read-only device must carry an empty non-nil list: %v", ro.Capabilities)
	}
	if HasCapability(ro.Capabilities, "anything") {
		t.Fatal("empty whitelist must grant nothing")
	}
	full, _ := s.Create("legacy", nil)
	if !HasCapability(full.Capabilities, CapShell) {
		t.Fatal("nil whitelist means legacy full access")
	}
	if _, err := s.SetCapabilities("spectator", []string{CapShell}); err != nil {
		t.Fatalf("SetCapabilities: %v", err)
	}
	list, _ := s.List()
	for _, d := range list {
		if d.Name == "spectator" && (len(d.Capabilities) != 1 || d.Capabilities[0] != CapShell) {
			t.Fatalf("capabilities not persisted: %+v", d)
		}
	}
	if _, err := s.SetCapabilities("nope", []string{}); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("set unknown: %v", err)
	}
}
