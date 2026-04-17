package core

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParallelSearchMappedOverlapAndMerge(t *testing.T) {
	t.Parallel()

	svc := NewSearchService(KMPEngineID)
	pattern := []byte("aba")
	data := []byte("xxxxxxababaxxxxxxxaba")

	got, err := svc.parallelSearchMapped(context.Background(), data, pattern, 8, 3)
	if err != nil {
		t.Fatalf("parallelSearchMapped returned error: %v", err)
	}

	want := []int64{6, 8, 18}
	if !slices.Equal(got, want) {
		t.Fatalf("parallelSearchMapped() = %v, want %v", got, want)
	}
}

func TestParallelSearchWithMMap(t *testing.T) {
	t.Parallel()

	svc := NewSearchService(KMPEngineID)
	pattern := []byte("needle")

	dataLen := DefaultParallelChunkSize + 4096
	data := make([]byte, dataLen)
	for i := range data {
		data[i] = 'x'
	}

	positions := []int{
		DefaultParallelChunkSize - 2,
		DefaultParallelChunkSize + 123,
	}
	for _, pos := range positions {
		copy(data[pos:], pattern)
	}

	path := filepath.Join(t.TempDir(), "parallel-search.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := svc.ParallelSearch(context.Background(), path, pattern)
	if err != nil {
		t.Fatalf("ParallelSearch returned error: %v", err)
	}

	want := []int64{int64(positions[0]), int64(positions[1])}
	if !slices.Equal(got, want) {
		t.Fatalf("ParallelSearch() = %v, want %v", got, want)
	}
}

// Run scaling checks with:
// go test ./core -bench BenchmarkParallelSearch -benchmem -cpu 1,2,4,8,14
func BenchmarkParallelSearch(b *testing.B) {
	pattern := []byte("needle")
	data := makeBenchmarkData(64 << 20)

	path := filepath.Join(b.TempDir(), "parallel-benchmark.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatalf("failed to write benchmark file: %v", err)
	}

	svc := NewSearchService(SIMDEngineID)
	ctx := context.Background()

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		matches, err := svc.ParallelSearch(ctx, path, pattern)
		if err != nil {
			b.Fatalf("ParallelSearch returned error: %v", err)
		}

		benchmarkResult += int64(len(matches))
		if len(matches) > 0 {
			benchmarkResult += matches[0]
		}
	}
}
