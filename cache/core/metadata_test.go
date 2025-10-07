package core

import (
	"reflect"
	"testing"
	"time"
)

type testAddress struct {
	Street   string
	City     string
	PostCode string `cache:"zip"`
	Secret   string `cache:"-"` // excluded
	Expires  time.Time
	TTLField string `cache_ttl:"15m"`
}

type testProfile struct {
	ID            string
	Count         int64
	CreatedAt     time.Time
	Nickname      *string `cache:"alias"`
	Address       testAddress
	AddressPtr    *testAddress
	Tags          []string
	Addresses     []testAddress
	AddressPtrSet []*testAddress
	Ignored       string `cache:"-"`
}

func TestGetStructMetadata(t *testing.T) {
	ResetStructMetadataCache()

	meta, err := GetStructMetadata(testProfile{})
	if err != nil {
		t.Fatalf("GetStructMetadata returned error: %v", err)
	}

	if len(meta.Fields) != 9 { // Ignored field is skipped.
		t.Fatalf("expected 9 metadata fields, got %d", len(meta.Fields))
	}

	// Validate primary fields.
	idField := findField(meta, "ID")
	if idField == nil {
		t.Fatalf("expected ID field in metadata")
	}
	if idField.Kind != reflect.String {
		t.Fatalf("expected ID field kind string, got %s", idField.Kind)
	}

	aliasField := findField(meta, "alias")
	if aliasField == nil {
		t.Fatalf("expected alias field in metadata")
	}
	if !aliasField.IsPointer {
		t.Errorf("expected alias field to be pointer")
	}

	addressField := findField(meta, "Address")
	if addressField == nil {
		t.Fatalf("expected Address field")
	}
	if addressField.Nested == nil {
		t.Fatalf("expected nested metadata for Address")
	}
	if len(addressField.Nested.Fields) != 5 {
		t.Fatalf("expected 5 fields for Address metadata, got %d", len(addressField.Nested.Fields))
	}

	ttlField := findField(addressField.Nested, "TTLField")
	if ttlField == nil || ttlField.TTLOverride == nil {
		t.Fatalf("expected TTL override on TTLField")
	}
	if ttlField.TTLOverride != nil && *ttlField.TTLOverride != 15*time.Minute {
		t.Fatalf("expected TTL override of 15m, got %v", ttlField.TTLOverride)
	}

	if created := findField(meta, "CreatedAt"); created == nil || !created.Atomic {
		t.Fatalf("expected CreatedAt to be treated as atomic")
	}

	sliceField := findField(meta, "Addresses")
	if sliceField == nil || sliceField.ElemNested == nil {
		t.Fatalf("expected Addresses slice to have nested metadata")
	}
	if !sliceField.ElemNested.Type.AssignableTo(addressField.Nested.Type) {
		t.Fatalf("slice element metadata mismatch")
	}

	ptrSliceField := findField(meta, "AddressPtrSet")
	if ptrSliceField == nil {
		t.Fatalf("expected AddressPtrSet field")
	}
	if !ptrSliceField.ElemIsPointer {
		t.Fatalf("expected pointer slice elements to be flagged")
	}

	// Ensure cache returns same pointer on subsequent calls.
	meta2, err := GetStructMetadata(testProfile{})
	if err != nil {
		t.Fatalf("second metadata call failed: %v", err)
	}
	if meta != meta2 {
		t.Fatalf("expected metadata cache to return identical pointer")
	}
}

func findField(meta *StructMetadata, cacheName string) *FieldDescriptor {
	for i := range meta.Fields {
		if meta.Fields[i].CacheName == cacheName {
			return &meta.Fields[i]
		}
	}
	return nil
}

func TestGetStructMetadataRejectsNonStruct(t *testing.T) {
	if _, err := GetStructMetadata(42); err == nil {
		t.Fatalf("expected error when requesting metadata for non-struct")
	}
}
