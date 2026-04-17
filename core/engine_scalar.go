package core

import "context"

type ScalarEngine struct{}

const (
	ScalarEngineID    = "scalar"
	ScalarCtxFreqMask = 0x3FFF // 16KiB
)

func NewScalarEngine() *ScalarEngine {
	return &ScalarEngine{}
}

func (e *ScalarEngine) GetID() string {
	return ScalarEngineID
}

func (e *ScalarEngine) Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error) {
	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen {
		return nil, nil
	}

	limit := dLen - pLen
	result := make([]int64, 0)

	for i := 0; i <= limit; i++ {
		if i&ScalarCtxFreqMask == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		matched := true
		for j := 0; j < pLen; j++ {
			if data[i+j] != pattern[j] {
				matched = false
				break
			}
		}

		if matched {
			result = append(result, int64(i))
		}
	}

	if len(result) == 0 {
		return nil, nil
	}

	return result, nil
}
