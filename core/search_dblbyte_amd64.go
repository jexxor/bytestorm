//go:build amd64

package core

// searchDoubleByteSIMD runs the full search hot loop in amd64 assembly.
// It writes int32 match offsets into out and returns the number of matches written.
func searchDoubleByteSIMD(data []byte, pattern []byte, out []int32) int
