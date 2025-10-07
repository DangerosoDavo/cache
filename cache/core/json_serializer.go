package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// JSONSerializer serializes structs into canonical JSON using StructMetadata.
type JSONSerializer struct{}

// NewJSONSerializer constructs a JSON serializer instance.
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// Format returns the format identifier for this serializer.
func (s *JSONSerializer) Format() string {
	return FormatJSON
}

// Serialize converts the provided struct into canonical JSON, respecting cache tags.
func (s *JSONSerializer) Serialize(ctx context.Context, meta *StructMetadata, value any) (Payload, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Payload{}, err
		}
	}

	if value == nil {
		return Payload{}, errors.New("core: cannot serialize nil value")
	}

	var err error
	if meta == nil {
		meta, err = GetStructMetadata(value)
		if err != nil {
			return Payload{}, err
		}
	}

	rootValue := reflect.ValueOf(value)
	if !rootValue.IsValid() {
		return Payload{}, errors.New("core: invalid value")
	}

	object, err := buildObjectFromValue(meta, rootValue)
	if err != nil {
		return Payload{}, err
	}

	data, err := marshalCanonicalJSON(object)
	if err != nil {
		return Payload{}, err
	}

	return Payload{
		Format: FormatJSON,
		Data:   data,
	}, nil
}

// Deserialize populates out using the provided payload.
func (s *JSONSerializer) Deserialize(ctx context.Context, meta *StructMetadata, payload Payload, out any) error {
	if payload.Format != "" && payload.Format != FormatJSON {
		return fmt.Errorf("core: unsupported format %q for JSON serializer", payload.Format)
	}

	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	if out == nil {
		return errors.New("core: output target is nil")
	}

	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("core: Deserialize target must be a non-nil pointer")
	}

	var err error
	if meta == nil {
		meta, err = GetStructMetadata(out)
		if err != nil {
			return err
		}
	}

	target := rv.Elem()
	if target.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	var raw map[string]json.RawMessage
	if len(payload.Data) == 0 {
		raw = map[string]json.RawMessage{}
	} else {
		if err := json.Unmarshal(payload.Data, &raw); err != nil {
			return err
		}
	}

	return applyRawMapToStruct(meta, raw, target)
}

func buildObjectFromValue(meta *StructMetadata, value reflect.Value) (map[string]any, error) {
	v, err := resolveValue(value)
	if err != nil {
		return nil, err
	}
	if v.Kind() != reflect.Struct {
		return nil, ErrNotStruct
	}

	result := make(map[string]any, len(meta.Fields))
	for _, fd := range meta.Fields {
		fieldVal := v.FieldByIndex(fd.Index)
		extracted, include, err := extractFieldValue(fd, fieldVal)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		result[fd.CacheName] = extracted
	}

	return result, nil
}

func resolveValue(v reflect.Value) (reflect.Value, error) {
	if !v.IsValid() {
		return reflect.Value{}, errors.New("core: invalid value")
	}

	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, errors.New("core: nil pointer encountered")
		}
		v = v.Elem()
	}
	return v, nil
}

func extractFieldValue(fd FieldDescriptor, fieldVal reflect.Value) (any, bool, error) {
	if !fieldVal.IsValid() {
		return nil, false, nil
	}

	if fd.IsPointer {
		if fieldVal.IsNil() {
			return nil, true, nil
		}
		fieldVal = fieldVal.Elem()
	}

	if fd.Kind == reflect.Struct && fd.Nested != nil && !fd.Atomic {
		if fieldVal.Kind() == reflect.Pointer {
			if fieldVal.IsNil() {
				return nil, true, nil
			}
			fieldVal = fieldVal.Elem()
		}
		nested, err := buildObjectFromValue(fd.Nested, fieldVal)
		return nested, true, err
	}

	if fd.Kind == reflect.Slice || fd.Kind == reflect.Array {
		if fieldVal.Kind() == reflect.Pointer {
			if fieldVal.IsNil() {
				return nil, true, nil
			}
			fieldVal = fieldVal.Elem()
		}
		if fieldVal.IsNil() {
			return nil, true, nil
		}

		length := fieldVal.Len()
		items := make([]any, 0, length)
		for i := 0; i < length; i++ {
			elem := fieldVal.Index(i)
			item, err := extractSliceElement(fd, elem)
			if err != nil {
				return nil, false, err
			}
			items = append(items, item)
		}
		return items, true, nil
	}

	if fd.IsInterface {
		if fieldVal.IsNil() {
			return nil, true, nil
		}
		return fieldVal.Interface(), true, nil
	}

	return fieldVal.Interface(), true, nil
}

func extractSliceElement(fd FieldDescriptor, elem reflect.Value) (any, error) {
	for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Interface {
		if elem.IsNil() {
			return nil, nil
		}
		elem = elem.Elem()
	}

	if fd.ElemKind == reflect.Struct && fd.ElemNested != nil && !fd.ElemAtomic {
		return buildObjectFromValue(fd.ElemNested, elem)
	}

	return elem.Interface(), nil
}

func marshalCanonicalJSON(value any) ([]byte, error) {
	switch v := value.(type) {
	case map[string]any:
		if v == nil {
			return []byte("null"), nil
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		var buf bytes.Buffer
		buf.Grow(len(keys) * 32) // heuristic
		buf.WriteByte('{')
		for idx, key := range keys {
			if idx > 0 {
				buf.WriteByte(',')
			}
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')
			child, err := marshalCanonicalJSON(v[key])
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		if v == nil {
			return []byte("null"), nil
		}
		var buf bytes.Buffer
		buf.Grow(len(v) * 16)
		buf.WriteByte('[')
		for idx, item := range v {
			if idx > 0 {
				buf.WriteByte(',')
			}
			child, err := marshalCanonicalJSON(item)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case nil:
		return []byte("null"), nil
	default:
		return json.Marshal(v)
	}
}

func applyRawMapToStruct(meta *StructMetadata, raw map[string]json.RawMessage, target reflect.Value) error {
	if target.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	for _, fd := range meta.Fields {
		field := target.FieldByIndex(fd.Index)
		if !field.CanSet() {
			continue
		}

		rawValue, ok := raw[fd.CacheName]
		if !ok {
			if fd.HasOmitempty {
				field.Set(reflect.Zero(field.Type()))
			}
			continue
		}

		if rawValue == nil {
			setNilField(field)
			continue
		}

		if err := decodeRawToField(fd, rawValue, field); err != nil {
			return err
		}
	}

	return nil
}

func decodeRawToField(fd FieldDescriptor, raw json.RawMessage, field reflect.Value) error {
	if fd.Kind == reflect.Struct && fd.Nested != nil && !fd.Atomic {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			return err
		}

		var target reflect.Value
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				field.Set(reflect.New(fd.BaseType))
			}
			target = field.Elem()
		} else {
			target = field
		}
		return applyRawMapToStruct(fd.Nested, nested, target)
	}

	if fd.Kind == reflect.Slice || fd.Kind == reflect.Array {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		return decodeSliceField(fd, items, field)
	}

	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(fd.BaseType))
		}
		return json.Unmarshal(raw, field.Interface())
	}

	tmp := reflect.New(field.Type())
	if err := json.Unmarshal(raw, tmp.Interface()); err != nil {
		return err
	}
	field.Set(tmp.Elem())
	return nil
}

func decodeSliceField(fd FieldDescriptor, items []json.RawMessage, field reflect.Value) error {
	var sliceTarget reflect.Value
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(fd.BaseType))
		}
		sliceTarget = field.Elem()
	} else {
		sliceTarget = field
	}

	slice := reflect.MakeSlice(sliceTarget.Type(), 0, len(items))
	for _, item := range items {
		if isJSONNull(item) {
			slice = reflect.Append(slice, reflect.Zero(sliceTarget.Type().Elem()))
			continue
		}

		elemVal, err := decodeSliceElement(fd, item, sliceTarget.Type().Elem())
		if err != nil {
			return err
		}
		slice = reflect.Append(slice, elemVal)
	}

	sliceTarget.Set(slice)
	return nil
}

func decodeSliceElement(fd FieldDescriptor, raw json.RawMessage, elemType reflect.Type) (reflect.Value, error) {
	if fd.ElemKind == reflect.Struct && fd.ElemNested != nil && !fd.ElemAtomic {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			return reflect.Value{}, err
		}

		var elem reflect.Value
		if fd.ElemIsPointer {
			elem = reflect.New(fd.ElemBaseType)
			if err := applyRawMapToStruct(fd.ElemNested, nested, elem.Elem()); err != nil {
				return reflect.Value{}, err
			}
			return elem, nil
		}

		elem = reflect.New(fd.ElemBaseType).Elem()
		if err := applyRawMapToStruct(fd.ElemNested, nested, elem); err != nil {
			return reflect.Value{}, err
		}
		return elem, nil
	}

	target := reflect.New(elemType)
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return target.Elem(), nil
}

func setNilField(field reflect.Value) {
	switch field.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		field.Set(reflect.Zero(field.Type()))
	default:
		field.Set(reflect.Zero(field.Type()))
	}
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
