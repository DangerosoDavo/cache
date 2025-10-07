package core

import (
	"context"
)

const (
	// FormatJSON identifies JSON payloads within the serializer abstraction.
	FormatJSON = "json"
)

// Payload contains serialized bytes plus format information so callers can route to the correct decoder.
type Payload struct {
	Format string
	Data   []byte
}

// Serializer defines how structs are converted to and from cached payloads.
type Serializer interface {
	Format() string
	Serialize(ctx context.Context, meta *StructMetadata, value any) (Payload, error)
	Deserialize(ctx context.Context, meta *StructMetadata, payload Payload, out any) error
}
