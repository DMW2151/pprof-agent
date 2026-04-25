package registry_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dmw2151/pprof-mcp/registry"
)

const (
	defaultTTL      = 30 * time.Minute
	defaultCapacity = 256
)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "prof", "inv.build.cpu.pprof")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestLoadBytes(t *testing.T) {
	r := registry.New(defaultTTL, defaultCapacity)
	defer r.Close()

	id, err := r.LoadBytes(fixtureBytes(t), "test.pprof")
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty profile ID")
	}

	e, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.Profile == nil {
		t.Fatal("entry.Profile is nil")
	}
	if e.Profile.SampleCount() == 0 {
		t.Error("expected non-zero sample count")
	}
	if e.SourceURI != "test.pprof" {
		t.Errorf("unexpected SourceURI: %q", e.SourceURI)
	}
	t.Logf("loaded id=%s samples=%d types=%v", id, e.Profile.SampleCount(), e.Profile.SampleTypes)
}

func TestLoadBytes_Invalid(t *testing.T) {
	r := registry.New(defaultTTL, defaultCapacity)
	defer r.Close()

	_, err := r.LoadBytes([]byte("not a pprof file"), "bad.pprof")
	if err == nil {
		t.Fatal("expected error for invalid data")
	}
}

func TestGet_NotFound(t *testing.T) {
	r := registry.New(defaultTTL, defaultCapacity)
	defer r.Close()

	_, err := r.Get("doesnotexist")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestDelete(t *testing.T) {
	r := registry.New(defaultTTL, defaultCapacity)
	defer r.Close()

	id, err := r.LoadBytes(fixtureBytes(t), "test.pprof")
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if err := r.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(id); err == nil {
		t.Fatal("expected error after Delete")
	}
}

func TestList(t *testing.T) {
	r := registry.New(defaultTTL, defaultCapacity)
	defer r.Close()

	data := fixtureBytes(t)
	id1, _ := r.LoadBytes(data, "a.pprof")
	id2, _ := r.LoadBytes(data, "b.pprof")

	entries := r.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	ids := map[string]bool{id1: true, id2: true}
	for _, e := range entries {
		if !ids[e.ID] {
			t.Errorf("unexpected entry id %q", e.ID)
		}
	}
}

func TestTTLExpiry(t *testing.T) {
	r := registry.New(50*time.Millisecond, defaultCapacity)
	defer r.Close()

	id, _ := r.LoadBytes(fixtureBytes(t), "test.pprof")
	time.Sleep(200 * time.Millisecond)
	if _, err := r.Get(id); err == nil {
		t.Error("expected profile to have expired")
	}
}

func TestLRUEviction(t *testing.T) {
	r := registry.New(defaultTTL, 2)
	defer r.Close()

	data := fixtureBytes(t)
	id1, _ := r.LoadBytes(data, "a.pprof")
	r.LoadBytes(data, "b.pprof")
	r.LoadBytes(data, "c.pprof")

	if _, err := r.Get(id1); err == nil {
		t.Error("expected id1 to be evicted by LRU")
	}
}
