package runtime

import (
	"reflect"
	"sync"

	"github.com/entitycache/entitycache/cache/core"
)

// Handle represents a tracked object registration.
type Handle interface {
	Unregister()
	OnUpdate(func())
	OnInvalidate(func())
}

type objectHandle struct {
	key         string
	originalKey string
	value       reflect.Value
	metadata    *core.StructMetadata
	manager     *Manager

	mu           sync.RWMutex
	active       bool
	onUpdateFns  []func()
	onInvalidate []func()
}

func newObjectHandle(fullKey, originalKey string, value reflect.Value, meta *core.StructMetadata, manager *Manager) *objectHandle {
	return &objectHandle{
		key:         fullKey,
		originalKey: originalKey,
		value:       value,
		metadata:    meta,
		manager:     manager,
		active:      true,
	}
}

func (h *objectHandle) Unregister() {
	h.mu.Lock()
	if !h.active {
		h.mu.Unlock()
		return
	}
	h.active = false
	h.mu.Unlock()
	if removed := h.manager.registry.unregister(h.key, h); removed {
		h.manager.stopSubscription(h.key)
	}
}

func (h *objectHandle) OnUpdate(fn func()) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	if h.active {
		h.onUpdateFns = append(h.onUpdateFns, fn)
	}
	h.mu.Unlock()
}

func (h *objectHandle) OnInvalidate(fn func()) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	if h.active {
		h.onInvalidate = append(h.onInvalidate, fn)
	}
	h.mu.Unlock()
}

func (h *objectHandle) notifyUpdate() {
	h.mu.RLock()
	if !h.active {
		h.mu.RUnlock()
		return
	}
	callbacks := make([]func(), len(h.onUpdateFns))
	copy(callbacks, h.onUpdateFns)
	h.mu.RUnlock()

	for _, cb := range callbacks {
		cb()
	}
}

func (h *objectHandle) notifyInvalidate() {
	h.mu.RLock()
	callbacks := make([]func(), len(h.onInvalidate))
	copy(callbacks, h.onInvalidate)
	h.mu.RUnlock()

	for _, cb := range callbacks {
		cb()
	}
}

func (h *objectHandle) detach() {
	h.mu.Lock()
	h.active = false
	h.mu.Unlock()
}
