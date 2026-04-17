package core

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

var benchmarkResult int64

type engineFactory func() Engine

func allEngineFactories() map[string]engineFactory {
	return map[string]engineFactory{
		ScalarEngineID: func() Engine { return NewScalarEngine() },
		KMPEngineID:    func() Engine { return NewKMPEngine() },
		SIMDEngineID:   func() Engine { return NewSIMDEngine(0) },
		StdlibEngineID: func() Engine { return NewStdlibEngine() },
	}
}

func TestSearchEnginesSameCases(t *testing.T) {
	longPattern := []byte("abcdefghij")
	longPatternData := make([]byte, 0, len(longPattern)*20)
	longPatternExpect := make([]int64, 0, 20)
	for i := 0; i < 20; i++ {
		longPatternExpect = append(longPatternExpect, int64(i*len(longPattern)))
		longPatternData = append(longPatternData, longPattern...)
	}

	cases := []struct {
		name    string
		data    []byte
		pattern []byte
		expect  []int64
	}{
		{
			name:    "single match",
			data:    []byte("haystack needle haystack"),
			pattern: []byte("needle"),
			expect:  []int64{9},
		},
		{
			name:    "multiple matches",
			data:    []byte("abcxxabcxxabc"),
			pattern: []byte("abc"),
			expect:  []int64{0, 5, 10},
		},
		{
			name:    "overlapping matches",
			data:    []byte("aaaaa"),
			pattern: []byte("aa"),
			expect:  []int64{0, 1, 2, 3},
		},
		{
			name:    "no match",
			data:    []byte("abcdef"),
			pattern: []byte("zzz"),
			expect:  nil,
		},
		{
			name:    "binary payload",
			data:    []byte{0, 1, 2, 0, 1, 2, 0, 1},
			pattern: []byte{0, 1},
			expect:  []int64{0, 3, 6},
		},
		{
			name:    "empty pattern",
			data:    []byte("abcdef"),
			pattern: []byte{},
			expect:  nil,
		},
		{
			name:    "pattern longer than text",
			data:    []byte("abc"),
			pattern: []byte("abcd"),
			expect:  nil,
		},
		{
			name:    "long pattern many matches",
			data:    longPatternData,
			pattern: longPattern,
			expect:  longPatternExpect,
		},
	}

	ctx := context.Background()
	for engineID, factory := range allEngineFactories() {
		engineID := engineID
		factory := factory

		t.Run(engineID, func(t *testing.T) {
			engine := factory()

			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					got, err := engine.Search(ctx, tc.data, tc.pattern)
					if err != nil {
						t.Fatalf("Search returned error: %v", err)
					}

					if !slices.Equal(got, tc.expect) {
						t.Fatalf("Search(%q, %q) = %v, want %v", tc.data, tc.pattern, got, tc.expect)
					}
				})
			}
		})
	}
}

func BenchmarkEngineComparison(b *testing.B) {
	engines := []Engine{
		NewScalarEngine(),
		NewKMPEngine(),
		NewSIMDEngine(0),
		NewStdlibEngine(),
	}
	pattern := []byte("needle")
	sizes := []int{4 << 10, 64 << 10, 1 << 20, 16 << 20}

	for _, size := range sizes {
		size := size
		data := makeBenchmarkData(size)

		for _, engine := range engines {
			engine := engine
			b.Run(fmt.Sprintf("%s/size=%d", engine.GetID(), size), func(b *testing.B) {
				ctx := context.Background()
				b.ReportAllocs()

				// Warmup run keeps cache and code paths hot before timing.
				warmupMatches, err := engine.Search(ctx, data, pattern)
				if err != nil {
					b.Fatalf("warmup Search returned error: %v", err)
				}
				benchmarkResult += int64(len(warmupMatches))
				if len(warmupMatches) > 0 {
					benchmarkResult += int64(warmupMatches[0])
				}

				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					matches, err := engine.Search(ctx, data, pattern)
					if err != nil {
						b.Fatalf("Search returned error: %v", err)
					}

					benchmarkResult += int64(len(matches))
					if len(matches) > 0 {
						benchmarkResult += int64(matches[0])
					}
				}
			})
		}
	}
}

func makeBenchmarkData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte((i * 17) & 0xFF)
	}

	needle := []byte("needle")
	if size >= len(needle) {
		for i := 0; i+len(needle) <= size; i += 4096 {
			copy(data[i:], needle)
		}
	}

	return data
}
