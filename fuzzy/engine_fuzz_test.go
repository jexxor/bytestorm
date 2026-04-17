package fuzzy

import (
	"bytes"
	"context"
	"hash/fnv"
	"math/rand"
	"runtime"
	"runtime/debug"
	"slices"
	"testing"

	"jexxor/bytestorm/core"
)

const (
	maxFuzzDataSize      = 1 << 20
	maxFuzzPatternSize   = 64
	fuzzResultBufferCap  = 64 << 10
	fuzzMemoryIterations = 10_000
)

func FuzzSIMDEngineSearchAgainstOracle(f *testing.F) {
	f.Add([]byte(""), []byte("a"), uint8(0))
	f.Add(bytes.Repeat([]byte("x"), 31), []byte("x"), uint8(1))
	f.Add(bytes.Repeat([]byte("x"), 32), []byte("xx"), uint8(1))
	f.Add(bytes.Repeat([]byte("x"), 33), []byte("xxx"), uint8(1))
	f.Add([]byte("abcdefghij"), []byte("hij"), uint8(2))
	f.Add([]byte("AAAAAAAAAAAAAAAA"), []byte("AAAAA"), uint8(3))

	engine := core.NewSIMDEngine(fuzzResultBufferCap)
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, seedData []byte, seedPattern []byte, selector uint8) {
		data, pattern := buildFuzzCase(seedData, seedPattern, selector)

		got, err := engine.Search(ctx, data, pattern)
		if err != nil {
			t.Fatalf("SIMDEngine.Search() returned error: %v", err)
		}

		want := indexAll(data, pattern)
		if !slices.Equal(got, want) {
			t.Fatalf("index mismatch: data_len=%d pattern_len=%d got=%v want=%v", len(data), len(pattern), got, want)
		}
	})
}

func TestSIMDEnginePoolMemoryStability(t *testing.T) {
	engine := core.NewSIMDEngine(fuzzResultBufferCap)
	rng := rand.New(rand.NewSource(0xBADA55))
	ctx := context.Background()

	runtime.GC()
	debug.FreeOSMemory()

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < fuzzMemoryIterations; i++ {
		dataLen := randomDataLen(rng)
		patternLen := 1 + rng.Intn(maxFuzzPatternSize)

		data := make([]byte, dataLen)
		rng.Read(data)

		pattern := make([]byte, patternLen)
		rng.Read(pattern)

		switch i % 3 {
		case 0:
			if dataLen >= patternLen {
				copy(data[dataLen-patternLen:], pattern)
			}
		case 1:
			if dataLen > 0 {
				byteValue := byte('A')
				for j := range data {
					data[j] = byteValue
				}
				for j := range pattern {
					pattern[j] = byteValue
				}
			}
		}

		if _, err := engine.Search(ctx, data, pattern); err != nil {
			t.Fatalf("SIMDEngine.Search() returned error on iteration %d: %v", i, err)
		}
	}

	runtime.GC()
	debug.FreeOSMemory()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	const maxAllowedHeapGrowth = 96 << 20
	if after.HeapAlloc > before.HeapAlloc+maxAllowedHeapGrowth {
		t.Fatalf(
			"heap grew beyond threshold after %d iterations: before=%d after=%d delta=%d threshold=%d",
			fuzzMemoryIterations,
			before.HeapAlloc,
			after.HeapAlloc,
			after.HeapAlloc-before.HeapAlloc,
			maxAllowedHeapGrowth,
		)
	}
}

func buildFuzzCase(seedData []byte, seedPattern []byte, selector uint8) ([]byte, []byte) {
	rng := rand.New(rand.NewSource(makeSeed(seedData, seedPattern, selector)))
	mode := int(selector % 5)

	switch mode {
	case 1:
		sizes := [...]int{31, 32, 33}
		dataLen := sizes[rng.Intn(len(sizes))]
		patternLen := 1 + rng.Intn(minInt(maxFuzzPatternSize, dataLen))

		data := make([]byte, dataLen)
		rng.Read(data)
		pattern := make([]byte, patternLen)
		rng.Read(pattern)
		return data, pattern

	case 2:
		dataLen := 1 + rng.Intn(maxFuzzDataSize)
		patternLen := 1 + rng.Intn(minInt(maxFuzzPatternSize, dataLen))

		data := make([]byte, dataLen)
		rng.Read(data)
		pattern := make([]byte, patternLen)
		rng.Read(pattern)
		copy(data[dataLen-patternLen:], pattern)
		return data, pattern

	case 3:
		dataLen := 1 + rng.Intn(maxFuzzDataSize)
		patternLen := 1 + rng.Intn(minInt(maxFuzzPatternSize, dataLen))
		repeated := byte('A')

		data := bytes.Repeat([]byte{repeated}, dataLen)
		pattern := bytes.Repeat([]byte{repeated}, patternLen)
		return data, pattern

	case 4:
		dataLen := len(seedData)
		if dataLen > maxFuzzDataSize {
			dataLen = maxFuzzDataSize
		}

		patternLen := len(seedPattern)
		if patternLen == 0 {
			patternLen = 1
		}
		if patternLen > maxFuzzPatternSize {
			patternLen = maxFuzzPatternSize
		}

		data := make([]byte, dataLen)
		copy(data, seedData)
		pattern := make([]byte, patternLen)
		if len(seedPattern) == 0 {
			rng.Read(pattern)
		} else {
			copy(pattern, seedPattern)
		}
		return data, pattern
	}

	dataLen := rng.Intn(maxFuzzDataSize + 1)
	patternLen := 1 + rng.Intn(maxFuzzPatternSize)

	data := make([]byte, dataLen)
	rng.Read(data)
	pattern := make([]byte, patternLen)
	rng.Read(pattern)
	return data, pattern
}

func indexAll(data []byte, pattern []byte) []int64 {
	if len(pattern) == 0 || len(data) == 0 || len(pattern) > len(data) {
		return nil
	}

	limit := len(data) - len(pattern) + 1
	offset := 0
	result := make([]int64, 0, 8)

	for offset < limit {
		idx := bytes.Index(data[offset:], pattern)
		if idx < 0 {
			break
		}

		match := offset + idx
		result = append(result, int64(match))
		offset = match + 1
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func makeSeed(seedData []byte, seedPattern []byte, selector uint8) int64 {
	h := fnv.New64a()
	_, _ = h.Write(seedData)
	_, _ = h.Write([]byte{0xFF})
	_, _ = h.Write(seedPattern)
	_, _ = h.Write([]byte{selector})

	sum := h.Sum64()
	return int64(sum)
}

func randomDataLen(rng *rand.Rand) int {
	n := rng.Intn(100)
	switch {
	case n < 85:
		return rng.Intn((8 << 10) + 1)
	case n < 98:
		return rng.Intn((128 << 10) + 1)
	default:
		return rng.Intn(maxFuzzDataSize + 1)
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
