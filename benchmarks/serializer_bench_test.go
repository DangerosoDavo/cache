package benchmarks

import (
	"context"
	"testing"
	"time"

	"github.com/entitycache/entitycache/cache/core"
)

type benchAddress struct {
	Street string
	City   string
	Zip    string
}

type benchUser struct {
	ID        string
	Name      string
	Email     string
	CreatedAt time.Time
	Tags      []string
	Addresses []benchAddress
}

func newBenchUser() benchUser {
	return benchUser{
		ID:        "user-123",
		Name:      "Benchmark",
		Email:     "benchmark@example.com",
		CreatedAt: time.Now().UTC(),
		Tags:      []string{"alpha", "beta", "gamma"},
		Addresses: []benchAddress{{Street: "1 Main", City: "Benchville", Zip: "12345"}, {Street: "2 Side", City: "Benchville", Zip: "67890"}},
	}
}

func BenchmarkJSONSerializerSerialize(b *testing.B) {
	serializer := core.NewJSONSerializer()
	user := newBenchUser()
	meta, err := core.GetStructMetadata(user)
	if err != nil {
		b.Fatalf("metadata error: %v", err)
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := serializer.Serialize(ctx, meta, user); err != nil {
			b.Fatalf("serialize error: %v", err)
		}
	}
}

func BenchmarkJSONSerializerDeserialize(b *testing.B) {
	serializer := core.NewJSONSerializer()
	user := newBenchUser()
	meta, err := core.GetStructMetadata(user)
	if err != nil {
		b.Fatalf("metadata error: %v", err)
	}

	ctx := context.Background()
	payload, err := serializer.Serialize(ctx, meta, user)
	if err != nil {
		b.Fatalf("serialize error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := newBenchUser()
		if err := serializer.Deserialize(ctx, meta, payload, &target); err != nil {
			b.Fatalf("deserialize error: %v", err)
		}
	}
}
