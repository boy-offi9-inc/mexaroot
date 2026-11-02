package runtimes

import (
	"context"
	"errors"
	"testing"
)

type fakeRuntime struct {
	info Info
}

func (f fakeRuntime) Info() Info {
	return f.info
}

func (f fakeRuntime) Exec(context.Context, ExecRequest) (ExecResult, error) {
	return ExecResult{}, nil
}

func newFake(id string) fakeRuntime {
	return fakeRuntime{info: Info{ID: id, Kind: KindTermux, Name: id, State: StateAvailable}}
}

func TestRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(newFake("termux")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	rt, err := reg.Get("termux")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rt.Info().ID != "termux" {
		t.Fatalf("unexpected runtime: %+v", rt.Info())
	}
}

func TestRegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(newFake("termux")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := reg.Register(newFake("termux"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestRegisterWithoutID(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(newFake(""))
	if !errors.Is(err, ErrNoID) {
		t.Fatalf("expected ErrNoID, got %v", err)
	}
}

func TestGetUnknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListIsSorted(t *testing.T) {
	reg := NewRegistry()
	for _, id := range []string{"ubuntu", "alpine", "termux"} {
		if err := reg.Register(newFake(id)); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}
	infos := reg.List()
	want := []string{"alpine", "termux", "ubuntu"}
	if len(infos) != len(want) {
		t.Fatalf("unexpected list length: %d", len(infos))
	}
	for i, id := range want {
		if infos[i].ID != id {
			t.Fatalf("position %d: got %s, want %s", i, infos[i].ID, id)
		}
	}
}
