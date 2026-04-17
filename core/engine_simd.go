package core

import (
	"context"
	"runtime"
	"sync"

	"golang.org/x/sys/cpu"
)

const SIMDEngineID = "simd"

const defaultSIMDResultBufferCap = 64 << 10
const simdSmallInputThreshold = 16 << 10

var simdAvailable = runtime.GOARCH == "amd64" && cpu.X86.HasAVX2

type SIMDEngine struct {
	resultPool sync.Pool
	maxResults int
	fallback   Engine
	smallInput Engine
}

func SIMDEnabled() bool {
	return simdAvailable
}

func NewSIMDEngine(bufferCap int) *SIMDEngine {
	if bufferCap <= 0 {
		bufferCap = defaultSIMDResultBufferCap
	}

	engine := &SIMDEngine{
		maxResults: bufferCap,
		fallback:   NewKMPEngine(),
		smallInput: NewStdlibEngine(),
	}

	engine.resultPool.New = func() any {
		return make([]int32, engine.maxResults)
	}

	return engine
}

func (e *SIMDEngine) GetID() string {
	return SIMDEngineID
}

func (e *SIMDEngine) Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error) {
	if len(data) < simdSmallInputThreshold {
		return e.smallInput.Search(ctx, data, pattern)
	}

	if !SIMDEnabled() {
		return e.fallback.Search(ctx, data, pattern)
	}

	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen {
		return nil, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bufferAny := e.resultPool.Get()
	buffer, ok := bufferAny.([]int32)
	if !ok || len(buffer) != e.maxResults || cap(buffer) != e.maxResults {
		buffer = make([]int32, e.maxResults)
	}
	batch := buffer[:e.maxResults]
	defer e.resultPool.Put(batch)

	limit := int64(dLen - pLen + 1)
	offset := int64(0)
	var result []int64

	for offset < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		window := data[int(offset):]
		windowLimit := len(window) - pLen + 1
		if windowLimit <= 0 {
			break
		}

		chunkCap := e.maxResults
		if windowLimit < chunkCap {
			chunkCap = windowLimit
		}

		count := searchDoubleByteSIMD(window, pattern, batch[:chunkCap])
		if count == 0 {
			break
		}

		if result == nil {
			result = make([]int64, 0, count)
		}

		neededLen := len(result) + count
		if cap(result) < neededLen {
			newCap := cap(result) * 2
			if newCap < neededLen {
				newCap = neededLen
			}

			grown := make([]int64, len(result), newCap)
			copy(grown, result)
			result = grown
		}

		start := len(result)
		result = result[:neededLen]
		base := offset
		for i := 0; i < count; i++ {
			result[start+i] = base + int64(batch[i])
		}

		if count < chunkCap {
			break
		}

		offset += int64(batch[count-1]) + 1
	}

	if len(result) == 0 {
		return nil, nil
	}

	return result, nil
}
