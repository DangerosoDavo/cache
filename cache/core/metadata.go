package core

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	cacheTagKey = "cache"
	cacheTTLTag = "cache_ttl"
)

// StructMetadata captures cache-related information for a struct type.
type StructMetadata struct {
	Type   reflect.Type
	Fields []FieldDescriptor
}

// FieldDescriptor stores metadata for an individual struct field.
type FieldDescriptor struct {
	Name        string
	CacheName   string
	Index       []int
	Type        reflect.Type
	BaseType    reflect.Type
	TTLOverride *time.Duration

	// Shape information used at serialization/deserialization time.
	IsPointer   bool
	IsSlice     bool
	IsArray     bool
	IsInterface bool
	Kind        reflect.Kind

	ElemType      reflect.Type
	ElemBaseType  reflect.Type
	ElemKind      reflect.Kind
	ElemIsPointer bool

	Atomic       bool
	ElemAtomic   bool
	Nested       *StructMetadata
	ElemNested   *StructMetadata
	Anonymous    bool
	HasOmitempty bool
}

var structMetadataCache sync.Map // map[reflect.Type]*StructMetadata

// ErrNotStruct indicates the provided value or type is not a struct.
var ErrNotStruct = fmt.Errorf("core: target is not a struct")

// GetStructMetadata returns cached metadata for the provided struct instance or type.
func GetStructMetadata(target any) (*StructMetadata, error) {
	if target == nil {
		return nil, ErrNotStruct
	}

	t, err := normalizeToStructType(target)
	if err != nil {
		return nil, err
	}

	if meta, ok := structMetadataCache.Load(t); ok {
		return meta.(*StructMetadata), nil
	}

	meta, err := buildStructMetadata(t, make(map[reflect.Type]struct{}))
	if err != nil {
		return nil, err
	}

	structMetadataCache.Store(t, meta)
	return meta, nil
}

// ResetStructMetadataCache clears computed metadata; primarily intended for tests.
func ResetStructMetadataCache() {
	structMetadataCache = sync.Map{}
}

func buildStructMetadata(t reflect.Type, visiting map[reflect.Type]struct{}) (*StructMetadata, error) {
	if t.Kind() != reflect.Struct {
		return nil, ErrNotStruct
	}

	if meta, ok := structMetadataCache.Load(t); ok {
		return meta.(*StructMetadata), nil
	}

	if _, seen := visiting[t]; seen {
		return nil, fmt.Errorf("core: detected circular reference in type %s", t.String())
	}

	visiting[t] = struct{}{}
	defer delete(visiting, t)

	meta := &StructMetadata{
		Type:   t,
		Fields: make([]FieldDescriptor, 0, t.NumField()),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if field.PkgPath != "" && !field.Anonymous {
			// Unexported, skip.
			continue
		}

		fd, skip, err := describeField(field, visiting)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		meta.Fields = append(meta.Fields, fd)
	}

	structMetadataCache.Store(t, meta)
	return meta, nil
}

func describeField(field reflect.StructField, visiting map[reflect.Type]struct{}) (FieldDescriptor, bool, error) {
	tagValue := field.Tag.Get(cacheTagKey)
	if tagValue == "-" {
		return FieldDescriptor{}, true, nil
	}

	fd := FieldDescriptor{
		Name:      field.Name,
		CacheName: field.Name,
		Index:     field.Index,
		Type:      field.Type,
		Anonymous: field.Anonymous,
	}

	if tagValue != "" {
		parts := strings.Split(tagValue, ",")
		name := strings.TrimSpace(parts[0])
		if name != "" {
			fd.CacheName = name
		}
		for _, opt := range parts[1:] {
			if strings.TrimSpace(opt) == "omitempty" {
				fd.HasOmitempty = true
			}
		}
	}

	if ttlTag := strings.TrimSpace(field.Tag.Get(cacheTTLTag)); ttlTag != "" {
		dur, err := time.ParseDuration(ttlTag)
		if err != nil {
			return FieldDescriptor{}, false, fmt.Errorf("core: invalid cache_ttl tag on %s.%s: %w", field.Type.String(), field.Name, err)
		}
		fd.TTLOverride = &dur
	}

	fd.BaseType = resolveBaseType(field.Type)
	fd.Kind = fd.BaseType.Kind()
	fd.IsPointer = field.Type.Kind() == reflect.Pointer
	fd.IsSlice = fd.Kind == reflect.Slice
	fd.IsArray = fd.Kind == reflect.Array
	fd.IsInterface = fd.Kind == reflect.Interface
	fd.Atomic = shouldTreatAsAtomic(fd.BaseType)

	if fd.IsSlice || fd.IsArray {
		fd.ElemType = fd.BaseType.Elem()
		fd.ElemBaseType = resolveBaseType(fd.ElemType)
		fd.ElemKind = fd.ElemBaseType.Kind()
		fd.ElemIsPointer = fd.ElemType.Kind() == reflect.Pointer
		fd.ElemAtomic = shouldTreatAsAtomic(fd.ElemBaseType)

		if fd.ElemKind == reflect.Struct && !fd.ElemAtomic {
			nested, err := buildStructMetadata(fd.ElemBaseType, visiting)
			if err != nil {
				return FieldDescriptor{}, false, err
			}
			fd.ElemNested = nested
		}
	}

	if fd.Kind == reflect.Struct && !fd.Atomic {
		nested, err := buildStructMetadata(fd.BaseType, visiting)
		if err != nil {
			return FieldDescriptor{}, false, err
		}
		fd.Nested = nested
	}

	return fd, false, nil
}

func normalizeToStructType(target any) (reflect.Type, error) {
	var t reflect.Type

	switch val := target.(type) {
	case reflect.Type:
		t = val
	default:
		t = reflect.TypeOf(target)
	}

	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == nil || t.Kind() != reflect.Struct {
		return nil, ErrNotStruct
	}

	return t, nil
}

func resolveBaseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func shouldTreatAsAtomic(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return true
	}

	// Treat types implementing json.Marshaler or encoding.TextMarshaler as atomic.
	jsonMarshaler := reflect.TypeOf((*jsonMarshaler)(nil)).Elem()
	textMarshaler := reflect.TypeOf((*textMarshaler)(nil)).Elem()

	if t.Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(jsonMarshaler) {
		return true
	}
	if t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		return true
	}

	// Special-case common stdlib types with private fields.
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return true
	}
	if t.PkgPath() == "time" && t.Name() == "Duration" {
		return true
	}

	return false
}

type jsonMarshaler interface {
	MarshalJSON() ([]byte, error)
}

type textMarshaler interface {
	MarshalText() ([]byte, error)
}
