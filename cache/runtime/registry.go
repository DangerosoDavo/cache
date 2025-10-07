package runtime

import (
	"fmt"
	"sync"

	"github.com/entitycache/entitycache/cache/core"
)

type objectRegistry struct {
	mu      sync.RWMutex
	entries map[string]*objectEntry
}

type objectEntry struct {
	metadata *core.StructMetadata
	version  int64
	handles  map[*objectHandle]struct{}
}

func newObjectRegistry() *objectRegistry {
	return &objectRegistry{
		entries: make(map[string]*objectEntry),
	}
}

func (r *objectRegistry) prepareRegister(key string, handle *objectHandle, meta *core.StructMetadata) (int64, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[key]
	if entry == nil {
		entry = &objectEntry{
			metadata: meta,
			handles:  make(map[*objectHandle]struct{}),
		}
		r.entries[key] = entry
	} else if entry.metadata != nil && entry.metadata.Type != meta.Type {
		return 0, nil, fmt.Errorf("runtime: metadata type mismatch for key %q", key)
	}
	if entry.metadata == nil {
		entry.metadata = meta
	}

	entry.version++
	version := entry.version
	entry.handles[handle] = struct{}{}

	rollback := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		entry := r.entries[key]
		if entry == nil {
			return
		}
		delete(entry.handles, handle)
		if entry.version > 0 {
			entry.version--
		}
		if len(entry.handles) == 0 {
			delete(r.entries, key)
		}
	}

	return version, rollback, nil
}

func (r *objectRegistry) prepareUpdate(key string) (int64, *core.StructMetadata, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[key]
	if entry == nil {
		return 0, nil, nil, ErrUnknownKey
	}
	if entry.metadata == nil {
		return 0, nil, nil, fmt.Errorf("runtime: metadata missing for key %q", key)
	}

	entry.version++
	version := entry.version
	meta := entry.metadata

	rollback := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		entry := r.entries[key]
		if entry == nil {
			return
		}
		if entry.version > 0 {
			entry.version--
		}
	}

	return version, meta, rollback, nil
}

func (r *objectRegistry) unregister(key string, handle *objectHandle) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[key]
	if entry == nil {
		return false
	}
	delete(entry.handles, handle)
	if len(entry.handles) == 0 {
		delete(r.entries, key)
		return true
	}
	return false
}

func (r *objectRegistry) removeEntry(key string) []*objectHandle {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[key]
	if entry == nil {
		return nil
	}

	handles := make([]*objectHandle, 0, len(entry.handles))
	for handle := range entry.handles {
		handles = append(handles, handle)
	}

	delete(r.entries, key)
	return handles
}

func (r *objectRegistry) hasEntry(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.entries[key]
	return ok
}

type entrySnapshot struct {
	metadata *core.StructMetadata
	version  int64
	handles  []*objectHandle
}

func (r *objectRegistry) snapshot(key string) (entrySnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry := r.entries[key]
	if entry == nil {
		return entrySnapshot{}, false
	}
	handles := make([]*objectHandle, 0, len(entry.handles))
	for handle := range entry.handles {
		handles = append(handles, handle)
	}
	return entrySnapshot{
		metadata: entry.metadata,
		version:  entry.version,
		handles:  handles,
	}, true
}

func (r *objectRegistry) updateVersion(key string, version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[key]
	if entry == nil {
		return
	}
	entry.version = version
}

func (r *objectRegistry) size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
