package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/entitycache/entitycache/cache/core"
)

var (
	// ErrBackendRequired indicates that a manager cannot operate without a cache backend.
	ErrBackendRequired = errors.New("runtime: cache backend is required")
	// ErrSerializerRequired indicates a serializer must be supplied (directly or via defaults).
	ErrSerializerRequired = errors.New("runtime: serializer is required")
	// ErrNilTarget occurs when a caller provides a nil pointer or non-struct to Register/Update.
	ErrNilTarget = errors.New("runtime: target must be a non-nil pointer to a struct")
	// ErrUnknownKey indicates an operation was attempted on a key that is not registered locally.
	ErrUnknownKey = errors.New("runtime: key is not registered")
)

// Logger represents the logging contract consumed by the manager.
type Logger interface {
	Printf(string, ...any)
}

// Manager orchestrates object registration, serialization, and cache interaction.
type Manager struct {
	backend    core.Cache
	serializer core.Serializer
	namespace  string
	defaultTTL time.Duration

	logger Logger

	registry *objectRegistry

	ctx         context.Context
	cancel      context.CancelFunc
	subMu       sync.Mutex
	subscribers map[string]*subscriptionState
}

// Option configures manager-level behavior.
type Option func(*managerConfig)

type managerConfig struct {
	serializer core.Serializer
	namespace  string
	defaultTTL time.Duration
	logger     Logger
}

// WithSerializer injects a custom serializer implementation.
func WithSerializer(serializer core.Serializer) Option {
	return func(cfg *managerConfig) {
		cfg.serializer = serializer
	}
}

// WithNamespace prepends the provided namespace to all cache keys.
func WithNamespace(namespace string) Option {
	return func(cfg *managerConfig) {
		cfg.namespace = namespace
	}
}

// WithDefaultTTL sets the default TTL applied when writing entries (zero means no TTL).
func WithDefaultTTL(ttl time.Duration) Option {
	return func(cfg *managerConfig) {
		cfg.defaultTTL = ttl
	}
}

// WithLogger sets the logger used for diagnostic messages.
func WithLogger(logger Logger) Option {
	return func(cfg *managerConfig) {
		cfg.logger = logger
	}
}

// RegisterOption customizes registration behavior for a specific object.
type RegisterOption func(*registerConfig)

type registerConfig struct {
	ttl          *time.Duration
	onUpdate     []func()
	onInvalidate []func()
}

// WithRegisterTTL overrides the TTL for a specific registration.
func WithRegisterTTL(ttl time.Duration) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.ttl = &ttl
	}
}

// WithOnUpdate registers a callback that fires when the managed object receives a cache update.
func WithOnUpdate(fn func()) RegisterOption {
	return func(cfg *registerConfig) {
		if fn != nil {
			cfg.onUpdate = append(cfg.onUpdate, fn)
		}
	}
}

// WithOnInvalidate registers a callback that fires when the managed object is invalidated.
func WithOnInvalidate(fn func()) RegisterOption {
	return func(cfg *registerConfig) {
		if fn != nil {
			cfg.onInvalidate = append(cfg.onInvalidate, fn)
		}
	}
}

// NewManager constructs a new Manager instance with the provided backend.
func NewManager(backend core.Cache, opts ...Option) (*Manager, error) {
	if backend == nil {
		return nil, ErrBackendRequired
	}

	cfg := managerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	serializer := cfg.serializer
	if serializer == nil {
		serializer = core.NewJSONSerializer()
	}
	if serializer == nil {
		return nil, ErrSerializerRequired
	}

	logger := cfg.logger
	if logger == nil {
		logger = log.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		backend:     backend,
		serializer:  serializer,
		namespace:   cfg.namespace,
		defaultTTL:  cfg.defaultTTL,
		logger:      logger,
		registry:    newObjectRegistry(),
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[string]*subscriptionState),
	}, nil
}

// Register stores the provided object in the cache and tracks it for future updates.
func (m *Manager) Register(ctx context.Context, key string, obj any, opts ...RegisterOption) (Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	value, err := ensureStructPointer(obj)
	if err != nil {
		return nil, err
	}

	meta, err := core.GetStructMetadata(obj)
	if err != nil {
		return nil, err
	}

	handle := newObjectHandle(m.fullKey(key), key, value, meta, m)

	version, rollback, err := m.registry.prepareRegister(handle.key, handle, meta)
	if err != nil {
		return nil, err
	}

	payload, err := m.serializer.Serialize(ctx, meta, obj)
	if err != nil {
		rollback()
		return nil, err
	}

	cfg := registerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	ttl := m.defaultTTL
	if cfg.ttl != nil {
		ttl = *cfg.ttl
	}
	for _, fn := range cfg.onUpdate {
		handle.OnUpdate(fn)
	}
	for _, fn := range cfg.onInvalidate {
		handle.OnInvalidate(fn)
	}

	if err := m.ensureSubscription(handle.key); err != nil {
		rollback()
		m.stopSubscriptionIfEmpty(handle.key)
		return nil, err
	}

	if err := m.backend.Set(ctx, handle.key, payload, core.Metadata{
		TTL:     ttl,
		Version: version,
		Format:  payload.Format,
	}); err != nil {
		rollback()
		m.stopSubscriptionIfEmpty(handle.key)
		return nil, err
	}

	if err := m.backend.Publish(ctx, handle.key, core.Message{
		Key:     handle.key,
		Type:    core.MessageTypeUpdate,
		Version: version,
		Format:  payload.Format,
	}); err != nil {
		m.logger.Printf("runtime: publish after register for %s failed: %v", handle.key, err)
	}

	return handle, nil
}

// Update re-serializes the object and persists the changes to the cache.
func (m *Manager) Update(ctx context.Context, key string, obj any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	value, err := ensureStructPointer(obj)
	if err != nil {
		return err
	}

	fullKey := m.fullKey(key)
	version, meta, rollback, err := m.registry.prepareUpdate(fullKey)
	if err != nil {
		return err
	}

	payload, err := m.serializer.Serialize(ctx, meta, value.Interface())
	if err != nil {
		rollback()
		return err
	}

	if err := m.ensureSubscription(fullKey); err != nil {
		rollback()
		return err
	}

	if err := m.backend.Set(ctx, fullKey, payload, core.Metadata{
		TTL:     m.defaultTTL,
		Version: version,
		Format:  payload.Format,
	}); err != nil {
		rollback()
		return err
	}

	if err := m.backend.Publish(ctx, fullKey, core.Message{
		Key:     fullKey,
		Type:    core.MessageTypeUpdate,
		Version: version,
		Format:  payload.Format,
	}); err != nil {
		m.logger.Printf("runtime: publish after update for %s failed: %v", fullKey, err)
	}

	return nil
}

// Invalidate removes the cached entry and stops tracking registered handles.
func (m *Manager) Invalidate(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fullKey := m.fullKey(key)

	if !m.registry.hasEntry(fullKey) {
		return ErrUnknownKey
	}

	if err := m.backend.Delete(ctx, fullKey); err != nil {
		return err
	}

	handles := m.registry.removeEntry(fullKey)
	m.stopSubscription(fullKey)
	for _, handle := range handles {
		handle.detach()
		handle.notifyInvalidate()
	}

	if err := m.backend.Publish(ctx, fullKey, core.Message{
		Key:  fullKey,
		Type: core.MessageTypeInvalidate,
	}); err != nil {
		m.logger.Printf("runtime: publish after invalidate for %s failed: %v", fullKey, err)
	}

	return nil
}

func (m *Manager) fullKey(key string) string {
	if m.namespace == "" {
		return key
	}
	if key == "" {
		return m.namespace
	}
	return fmt.Sprintf("%s:%s", m.namespace, key)
}

// Close terminates subscription processing and releases resources.
func (m *Manager) Close() error {
	m.cancel()

	m.subMu.Lock()
	subs := make([]*subscriptionState, 0, len(m.subscribers))
	for _, state := range m.subscribers {
		subs = append(subs, state)
	}
	m.subscribers = make(map[string]*subscriptionState)
	m.subMu.Unlock()

	for _, state := range subs {
		state.cancel()
		state.wg.Wait()
	}

	return nil
}

func (m *Manager) ensureSubscription(key string) error {
	m.subMu.Lock()
	if state, ok := m.subscribers[key]; ok {
		ctxErr := state.ctx.Err()
		m.subMu.Unlock()
		if ctxErr != nil {
			state.wg.Wait()
			return m.ensureSubscription(key)
		}
		return nil
	}
	m.subMu.Unlock()

	ctx, cancel := context.WithCancel(m.ctx)
	sub, err := m.backend.Subscribe(ctx, key)
	if err != nil {
		cancel()
		return err
	}

	state := &subscriptionState{
		ctx:          ctx,
		cancel:       cancel,
		subscription: sub,
	}
	state.wg.Add(1)

	m.subMu.Lock()
	if existing, ok := m.subscribers[key]; ok {
		m.subMu.Unlock()
		state.cancel()
		state.wg.Done()
		_ = sub.Close()
		ctxErr := existing.ctx.Err()
		if ctxErr != nil {
			existing.wg.Wait()
			return m.ensureSubscription(key)
		}
		return nil
	}
	m.subscribers[key] = state
	m.subMu.Unlock()

	go m.runSubscription(state, key)

	return nil
}

func (m *Manager) runSubscription(state *subscriptionState, key string) {
	defer func() {
		m.removeSubscriber(key, state)
		state.cancel()
		_ = state.subscription.Close()
		state.wg.Done()
	}()

	ch := state.subscription.Channel()
	for {
		select {
		case <-state.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if stop := m.handleMessage(state.ctx, key, msg); stop {
				return
			}
		}
	}
}

func (m *Manager) handleMessage(ctx context.Context, key string, msg core.Message) bool {
	snapshot, ok := m.registry.snapshot(key)
	if !ok || len(snapshot.handles) == 0 {
		return true
	}

	switch msg.Type {
	case core.MessageTypeInvalidate:
		handles := m.registry.removeEntry(key)
		for _, handle := range handles {
			handle.detach()
			handle.notifyInvalidate()
		}
		return true
	case core.MessageTypeUpdate, "":
		if snapshot.metadata == nil {
			m.logger.Printf("runtime: metadata missing for key %s; skipping update", key)
			return false
		}
		if msg.Version > 0 && msg.Version <= snapshot.version {
			return false
		}
		payload, meta, err := m.backend.Get(ctx, key)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				handles := m.registry.removeEntry(key)
				for _, handle := range handles {
					handle.detach()
					handle.notifyInvalidate()
				}
				return true
			}
			m.logger.Printf("runtime: backend get for key %s failed: %v", key, err)
			return false
		}

		version := msg.Version
		if meta.Version > version {
			version = meta.Version
		}
		if version > 0 && version <= snapshot.version {
			return false
		}

		for _, handle := range snapshot.handles {
			if err := m.serializer.Deserialize(ctx, snapshot.metadata, payload, handle.value.Interface()); err != nil {
				m.logger.Printf("runtime: deserialize for key %s failed: %v", key, err)
				continue
			}
			handle.notifyUpdate()
		}

		if version > 0 {
			m.registry.updateVersion(key, version)
		}
		return false
	default:
		m.logger.Printf("runtime: unrecognized message type %q for key %s", msg.Type, key)
		return false
	}
}

func (m *Manager) removeSubscriber(key string, state *subscriptionState) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if current, ok := m.subscribers[key]; ok && current == state {
		delete(m.subscribers, key)
	}
}

func (m *Manager) stopSubscription(key string) {
	m.subMu.Lock()
	state, ok := m.subscribers[key]
	m.subMu.Unlock()
	if !ok {
		return
	}
	state.cancel()
	state.wg.Wait()
}

func (m *Manager) stopSubscriptionIfEmpty(key string) {
	if m.registry.hasEntry(key) {
		return
	}
	m.stopSubscription(key)
}

func ensureStructPointer(obj any) (reflect.Value, error) {
	if obj == nil {
		return reflect.Value{}, ErrNilTarget
	}

	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Pointer || val.IsNil() {
		return reflect.Value{}, ErrNilTarget
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return reflect.Value{}, ErrNilTarget
	}

	return val, nil
}

type subscriptionState struct {
	ctx          context.Context
	cancel       context.CancelFunc
	subscription core.Subscription
	wg           sync.WaitGroup
}
