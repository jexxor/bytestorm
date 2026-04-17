//go:build !amd64

package core

func searchDoubleByteSIMD(data []byte, pattern []byte, out []int32) int {
	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen || len(out) == 0 {
		return 0
	}

	limit := dLen - pLen + 1
	if limit > len(out) {
		limit = len(out)
	}

	count := 0
	if pLen == 1 {
		target := pattern[0]
		for i := 0; i < limit; i++ {
			if data[i] == target {
				out[count] = int32(i)
				count++
			}
		}
		return count
	}

	first := pattern[0]
	second := pattern[1]
	for i := 0; i < limit; i++ {
		if data[i] != first || data[i+1] != second {
			continue
		}

		matched := true
		for j := 2; j < pLen; j++ {
			if data[i+j] != pattern[j] {
				matched = false
				break
			}
		}

		if matched {
			out[count] = int32(i)
			count++
		}
	}

	return count
}
