package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Per-device tokens: an alternative to the one shared token. Every paired
// device gets its own secret, so one device can be revoked or rotated without
// touching the others. The store is a single JSON file with owner-only
// permissions, written atomically.

const (
	// DevicesFile is the default store location next to the shared token.
	DevicesFile = "devices.json"
)

var (
	ErrDeviceExists   = errors.New("a device with this name already exists")
	ErrDeviceNotFound = errors.New("no such device")
	ErrDeviceName     = errors.New("device name must be 1-64 characters: letters, digits, '-', '_', '.' or space")
)

// Device is one paired peer in the store.
type Device struct {
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	CreatedAt time.Time  `json:"createdAt"`
	RotatedAt *time.Time `json:"rotatedAt,omitempty"`
}

// devicesFile is the on-disk shape.
type devicesFile struct {
	Devices []Device `json:"devices"`
}

// Store manages the per-device token file. Safe for concurrent use.
type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultDevicesPath returns the store location next to the shared token.
func DefaultDevicesPath() (string, error) {
	base, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(base), DevicesFile), nil
}

// OpenStore opens (creating the file on first use) the store at path.
func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("devices path is required")
	}
	s := &Store{path: path}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := readDevices(path)
	if err != nil {
		return nil, err
	}
	if file == nil {
		if err := writeDevices(path, devicesFile{}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func readDevices(path string) (*devicesFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read devices file: %w", err)
	}
	if err := checkMode(path); err != nil {
		return nil, err
	}
	var file devicesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse devices file %s: %w", path, err)
	}
	return &file, nil
}

func writeDevices(path string, file devicesFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode devices file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create devices directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write devices file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace devices file: %w", err)
	}
	return nil
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ' ':
		default:
			return false
		}
	}
	return true
}

func (s *Store) load() (devicesFile, error) {
	file, err := readDevices(s.path)
	if err != nil {
		return devicesFile{}, err
	}
	if file == nil {
		file = &devicesFile{}
	}
	return *file, nil
}

// Create mints a token for a new device. The name must be unique.
func (s *Store) Create(name string) (Device, error) {
	if !validName(name) {
		return Device{}, ErrDeviceName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return Device{}, err
	}
	for _, d := range file.Devices {
		if d.Name == name {
			return Device{}, ErrDeviceExists
		}
	}
	token, err := Generate()
	if err != nil {
		return Device{}, err
	}
	device := Device{Name: name, Token: token, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	file.Devices = append(file.Devices, device)
	if err := writeDevices(s.path, file); err != nil {
		return Device{}, err
	}
	return device, nil
}

// List returns every device, oldest first.
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(file.Devices, func(i, j int) bool { return file.Devices[i].CreatedAt.Before(file.Devices[j].CreatedAt) })
	return file.Devices, nil
}

func (s *Store) remove(file *devicesFile, name string) (*Device, error) {
	for i, d := range file.Devices {
		if d.Name == name {
			device := file.Devices[i]
			file.Devices = append(file.Devices[:i], file.Devices[i+1:]...)
			return &device, writeDevices(s.path, *file)
		}
	}
	return nil, ErrDeviceNotFound
}

// Revoke removes a device; its token stops working immediately.
func (s *Store) Revoke(name string) error {
	if !validName(name) {
		return ErrDeviceName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return err
	}
	if _, err := s.remove(&file, name); err != nil {
		return err
	}
	return nil
}

// Rotate replaces a device's token, keeping its name and history.
func (s *Store) Rotate(name string) (Device, error) {
	if !validName(name) {
		return Device{}, ErrDeviceName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return Device{}, err
	}
	for i := range file.Devices {
		if file.Devices[i].Name == name {
			token, err := Generate()
			if err != nil {
				return Device{}, err
			}
			now := time.Now().UTC().Truncate(time.Second)
			file.Devices[i].Token = token
			file.Devices[i].RotatedAt = &now
			if err := writeDevices(s.path, file); err != nil {
				return Device{}, err
			}
			return file.Devices[i], nil
		}
	}
	return Device{}, ErrDeviceNotFound
}

// Verify reports which device a token belongs to. It walks every stored
// token with a constant-time comparison so timing leaks nothing about
// where a match lives.
func (s *Store) Verify(token string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return Device{}, false
	}
	for _, d := range file.Devices {
		if Equal(token, d.Token) {
			return d, true
		}
	}
	return Device{}, false
}
