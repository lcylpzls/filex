package filex

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"
)

func benchStore(b *testing.B) *Store {
	b.Helper()
	s, err := New(Config{DataDir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	_, _ = s.CreateBucket(context.Background(), "bench")
	b.Cleanup(func() { _ = s.Close() })
	return s
}

func BenchmarkPutSmall(b *testing.B) {
	s := benchStore(b)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Put(ctx, "bench", "k", bytes.NewReader(data), PutOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetSmall(b *testing.B) {
	s := benchStore(b)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 1024)
	_, _ = s.Put(ctx, "bench", "k", bytes.NewReader(data), PutOptions{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obj, err := s.Get(ctx, "bench", "k", GetOptions{})
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, obj)
		_ = obj.Close()
	}
}

func BenchmarkList1000(b *testing.B) {
	s := benchStore(b)
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		_, _ = s.Put(ctx, "bench", string(rune('a'+i%26))+strconv.Itoa(i), bytes.NewReader([]byte("x")), PutOptions{})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.List(ctx, "bench", ListOptions{Limit: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}
