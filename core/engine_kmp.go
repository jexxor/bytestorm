package core

import "context"

type KMPEngine struct {
	cache *LPSCache
}

const (
	KMPEngineID    = "kmp"
	KMPCtxFreqMask = 0x3FFF // 16KiB
)

func (e *KMPEngine) GetID() string {
	return KMPEngineID
}

func NewKMPEngine() *KMPEngine {
	return &KMPEngine{
		cache: NewLPSCache(),
	}
}

func (e *KMPEngine) Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error) {

	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen {
		return nil, nil
	}

	lps := e.cache.Get(pattern)
	var result []int64

	i, j := 0, 0
	for i < dLen {

		// won't use select because of the overhead
		if i&KMPCtxFreqMask == 0 {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
		}

		if data[i] == pattern[j] {
			i++
			j++
		} else {
			if j != 0 {
				j = lps[j-1]
			} else {
				i++
			}
		}

		if j == pLen {
			result = append(result, int64(i-j))
			j = lps[j-1]
		}
	}

	return result, nil
}
