package core

import (
	"bytes"
	"context"
)

type StdlibEngine struct{}

const (
	StdlibEngineID    = "stdlib"
	StdlibCtxFreqMask = 0x3FFF // 16KiB
)

func NewStdlibEngine() *StdlibEngine {
	return &StdlibEngine{}
}

func (e *StdlibEngine) GetID() string {
	return StdlibEngineID
}

func (e *StdlibEngine) Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error) {

	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen {
		return nil, nil
	}

	limit := dLen - pLen + 1
	offset := 0
	var result []int64

	for offset < limit {
		if offset&StdlibCtxFreqMask == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		idx := bytes.Index(data[offset:], pattern)
		if idx < 0 {
			break
		}

		match := offset + idx
		result = append(result, int64(match))

		// Shift by one byte to preserve overlapping-match semantics.
		offset = match + 1
	}

	return result, nil
}
