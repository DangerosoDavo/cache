package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJSONSerializerSerializeExcludesFields(t *testing.T) {
	ResetStructMetadataCache()

	profile := sampleProfile()
	serializer := NewJSONSerializer()

	payload, err := serializer.Serialize(context.Background(), nil, profile)
	if err != nil {
		t.Fatalf("Serialize returned error: %v", err)
	}
	if payload.Format != FormatJSON {
		t.Fatalf("expected FormatJSON, got %q", payload.Format)
	}
	jsonStr := string(payload.Data)
	if strings.Contains(jsonStr, "Secret") {
		t.Fatalf("expected secret field to be omitted, got %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"zip"`) {
		t.Fatalf("expected zip field key to be present in output: %s", jsonStr)
	}

	// Canonical output should be consistent across invocations.
	payloadRepeat, err := serializer.Serialize(context.Background(), nil, profile)
	if err != nil {
		t.Fatalf("second Serialize call failed: %v", err)
	}
	if string(payload.Data) != string(payloadRepeat.Data) {
		t.Fatalf("expected canonical JSON output to be deterministic")
	}
}

func TestJSONSerializerRoundTrip(t *testing.T) {
	ResetStructMetadataCache()

	profile := sampleProfile()
	serializer := NewJSONSerializer()

	payload, err := serializer.Serialize(context.Background(), nil, profile)
	if err != nil {
		t.Fatalf("Serialize returned error: %v", err)
	}

	var restored testProfile
	if err := serializer.Deserialize(context.Background(), nil, payload, &restored); err != nil {
		t.Fatalf("Deserialize returned error: %v", err)
	}

	if !profilesEqual(profile, restored) {
		t.Fatalf("round-trip mismatch:\noriginal: %#v\nrestored: %#v", profile, restored)
	}
}

func TestJSONSerializerNilPointerError(t *testing.T) {
	serializer := NewJSONSerializer()
	var profile *testProfile
	if _, err := serializer.Serialize(context.Background(), nil, profile); err == nil {
		t.Fatalf("expected error when serializing nil pointer")
	}
}

func TestJSONSerializerDeserializeFormatMismatch(t *testing.T) {
	serializer := NewJSONSerializer()
	payload := Payload{Format: "binary", Data: []byte("data")}
	var out testProfile
	if err := serializer.Deserialize(context.Background(), nil, payload, &out); err == nil {
		t.Fatalf("expected format mismatch error")
	}
}

func sampleProfile() testProfile {
	nickname := "gopher"
	baseTime := time.Date(2024, time.March, 1, 10, 0, 0, 0, time.UTC)

	addressPrimary := testAddress{
		Street:   "1 Main St",
		City:     "Gopherville",
		PostCode: "12345",
		Secret:   "skip",
		Expires:  baseTime,
		TTLField: "primary",
	}

	addressSecondary := testAddress{
		Street:   "2 Side St",
		City:     "Gopherville",
		PostCode: "67890",
		Secret:   "skip",
		Expires:  baseTime.Add(2 * time.Hour),
		TTLField: "secondary",
	}

	addressPtr := addressPrimary

	ptrSliceFirst := testAddress{
		Street:   "3 Tertiary",
		City:     "Gophertown",
		PostCode: "11111",
		Expires:  baseTime.Add(4 * time.Hour),
		TTLField: "ptrslice",
	}

	return testProfile{
		ID:            "user-1",
		Count:         7,
		CreatedAt:     baseTime,
		Nickname:      &nickname,
		Address:       addressPrimary,
		AddressPtr:    &addressPtr,
		Tags:          []string{"alpha", "beta"},
		Addresses:     []testAddress{addressPrimary, addressSecondary},
		AddressPtrSet: []*testAddress{&ptrSliceFirst, nil},
	}
}

func profilesEqual(a, b testProfile) bool {
	if a.ID != b.ID || a.Count != b.Count {
		return false
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}
	if !equalStringPtr(a.Nickname, b.Nickname) {
		return false
	}
	if !addressesEqual(a.Address, b.Address) {
		return false
	}
	if !equalAddressPtr(a.AddressPtr, b.AddressPtr) {
		return false
	}
	if len(a.Tags) != len(b.Tags) {
		return false
	}
	for i := range a.Tags {
		if a.Tags[i] != b.Tags[i] {
			return false
		}
	}
	if len(a.Addresses) != len(b.Addresses) {
		return false
	}
	for i := range a.Addresses {
		if !addressesEqual(a.Addresses[i], b.Addresses[i]) {
			return false
		}
	}
	if len(a.AddressPtrSet) != len(b.AddressPtrSet) {
		return false
	}
	for i := range a.AddressPtrSet {
		if !equalAddressPtr(a.AddressPtrSet[i], b.AddressPtrSet[i]) {
			return false
		}
	}
	return true
}

func addressesEqual(a, b testAddress) bool {
	return a.Street == b.Street &&
		a.City == b.City &&
		a.PostCode == b.PostCode &&
		a.TTLField == b.TTLField &&
		a.Expires.Equal(b.Expires)
}

func equalStringPtr(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a != nil && b != nil:
		return *a == *b
	default:
		return false
	}
}

func equalAddressPtr(a, b *testAddress) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a != nil && b != nil:
		return addressesEqual(*a, *b)
	default:
		return false
	}
}
