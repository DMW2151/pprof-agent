package registry

import (
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/oklog/ulid/v2"

	pprof "github.com/dmw2151/pprof-mcp/pprof"
)

// Entry holds a loaded profile and its metadata.
type Entry struct {
	Profile   *pprof.Profile
	ID        string
	SourceURI string
	LoadedAt  time.Time
	ExpiresAt time.Time
}

// Registry is a TTL+LRU-backed store of loaded profiles.
type Registry struct {
	cache *lru.LRU[string, *Entry]
	ttl   time.Duration
}

// New creates a Registry backed by an expirable LRU cache.
// ttl controls how long a profile stays resident; capacity is the max number of profiles.
func New(ttl time.Duration, capacity int) *Registry {
	r := &Registry{ttl: ttl}
	r.cache = lru.NewLRU[string, *Entry](capacity, nil, ttl)
	return r
}

// LoadBytes parses a profile from raw bytes and registers it.
// sourceName is stored as the SourceURI for display purposes.
func (r *Registry) LoadBytes(data []byte, sourceName string) (string, error) {
	p, err := pprof.Load(data)
	if err != nil {
		return "", fmt.Errorf("parse profile: %w", err)
	}
	id := ulid.Make().String()
	now := time.Now()
	r.cache.Add(id, &Entry{
		Profile:   p,
		ID:        id,
		SourceURI: sourceName,
		LoadedAt:  now,
		ExpiresAt: now.Add(r.ttl),
	})
	return id, nil
}

// Get returns the Entry for a given handle ID, or an error if not found / expired.
func (r *Registry) Get(id string) (*Entry, error) {
	e, ok := r.cache.Get(id)
	if !ok {
		return nil, fmt.Errorf("profile %q not found (expired or never loaded)", id)
	}
	return e, nil
}

// Delete explicitly removes a profile from the registry.
func (r *Registry) Delete(id string) error {
	if _, ok := r.cache.Get(id); !ok {
		return fmt.Errorf("profile %q not found", id)
	}
	r.cache.Remove(id)
	return nil
}

// List returns metadata for all currently loaded profiles.
func (r *Registry) List() []Entry {
	keys := r.cache.Keys()
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		if e, ok := r.cache.Get(k); ok {
			out = append(out, *e)
		}
	}
	return out
}

// Close stops the background eviction ticker inside the LRU cache.
func (r *Registry) Close() {
	r.cache.Purge()
}
