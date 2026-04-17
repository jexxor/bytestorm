#include "textflag.h"

// func searchDoubleByteSIMD(data []byte, pattern []byte, out []int32) int
TEXT ·searchDoubleByteSIMD(SB), NOSPLIT, $0-80
	// Register roles:
	// R8=data ptr, R9=data len / last-byte index, R10=pattern ptr, R11=pattern len
	// DI=out ptr (int32 offsets), R12=out len, AX=matches written
	// R15=window count (dLen-pLen+1), R14=current window offset
	MOVQ data_base+0(FP), R8
	MOVQ data_len+8(FP), R9
	MOVQ pattern_base+24(FP), R10
	MOVQ pattern_len+32(FP), R11
	MOVQ out_base+48(FP), DI
	MOVQ out_len+56(FP), R12

	XORQ AX, AX

	// Fast-exit on invalid/empty inputs or no output capacity.
	TESTQ R9, R9
	JE done
	TESTQ R11, R11
	JE done
	CMPQ R11, R9
	JG done
	TESTQ R12, R12
	JE done

	// Search windows = dLen - pLen + 1.
	MOVQ R9, R15
	SUBQ R11, R15
	INCQ R15

limitReady:
	TESTQ R15, R15
	JLE done

	CMPQ R11, $1
	JE oneByte

	// Reuse R9 as index of the pattern's last byte (pLen-1).
	MOVQ R11, R9
	DECQ R9

	// Broadcast first byte to Y0.
	MOVBLZX (R10), BX
	MOVQ $0x0101010101010101, DX
	IMULQ BX, DX
	MOVQ DX, X0
	VPBROADCASTQ X0, Y0

	// Broadcast last byte to Y1.
	MOVBLZX (R10)(R9*1), BX
	MOVQ $0x0101010101010101, DX
	IMULQ BX, DX
	MOVQ DX, X1
	VPBROADCASTQ X1, Y1

	CMPQ R11, $8
	JLE shortPattern
	JMP longPattern

shortPattern:
	// pLen in [2..8]: prefilter by first+last byte, then fixed-width compare.
	XORQ R14, R14

shortChunkCheck:
	// Vector loop scans 32 candidate starts per chunk.
	MOVQ R15, R13
	SUBQ R14, R13
	CMPQ R13, $32
	JL shortTail

	LEAQ (R8)(R14*1), SI
	VMOVDQU (SI), Y2
	VPCMPEQB Y0, Y2, Y2

	LEAQ (SI)(R9*1), CX
	VMOVDQU (CX), Y3
	VPCMPEQB Y1, Y3, Y3

	VPAND Y3, Y2, Y2
	VPMOVMSKB Y2, DX

shortCandLoop:
	// DX bitmask marks lanes where first and last bytes matched.
	TESTQ DX, DX
	JE shortNextChunk

	BSFQ DX, BX
	MOVQ R14, R13
	ADDQ BX, R13
	LEAQ (R8)(R13*1), SI

	// Exact verification for each short pattern length.
	CMPQ R11, $2
	JE shortVerify2
	CMPQ R11, $3
	JE shortVerify3
	CMPQ R11, $4
	JE shortVerify4
	CMPQ R11, $5
	JE shortVerify5
	CMPQ R11, $6
	JE shortVerify6
	CMPQ R11, $7
	JE shortVerify7
	CMPQ R11, $8
	JE shortVerify8
	JMP shortReject

shortVerify2:
	MOVWLZX (SI), BX
	MOVWLZX (R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify3:
	MOVWLZX (SI), BX
	MOVWLZX (R10), CX
	CMPQ BX, CX
	JNE shortReject
	MOVBLZX 2(SI), BX
	MOVBLZX 2(R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify4:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify5:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortReject
	MOVBLZX 4(SI), BX
	MOVBLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify6:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortReject
	MOVWLZX 4(SI), BX
	MOVWLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify7:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortReject
	MOVWLZX 4(SI), BX
	MOVWLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortReject
	MOVBLZX 6(SI), BX
	MOVBLZX 6(R10), CX
	CMPQ BX, CX
	JNE shortReject
	JMP shortStore

shortVerify8:
	MOVQ (SI), BX
	CMPQ BX, (R10)
	JNE shortReject

shortStore:
	// Emit int32 offset and advance output cursor.
	CMPQ AX, R12
	JGE done
	MOVL R13, (DI)
	ADDQ $4, DI
	INCQ AX

shortReject:
	// Clear the lowest set bit and continue with next candidate.
	LEAQ -1(DX), CX
	ANDQ CX, DX
	JMP shortCandLoop

shortNextChunk:
	ADDQ $32, R14
	JMP shortChunkCheck

shortTail:
	// Scalar tail for remaining windows (<32).
	CMPQ R14, R15
	JGE done

shortTailLoop:
	LEAQ (R8)(R14*1), SI
	MOVBLZX (SI), BX
	MOVBLZX (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVBLZX (SI)(R9*1), BX
	MOVBLZX (R10)(R9*1), CX
	CMPQ BX, CX
	JNE shortTailNext

	MOVQ R14, R13

	CMPQ R11, $2
	JE shortTailVerify2
	CMPQ R11, $3
	JE shortTailVerify3
	CMPQ R11, $4
	JE shortTailVerify4
	CMPQ R11, $5
	JE shortTailVerify5
	CMPQ R11, $6
	JE shortTailVerify6
	CMPQ R11, $7
	JE shortTailVerify7
	CMPQ R11, $8
	JE shortTailVerify8
	JMP shortTailNext

shortTailVerify2:
	MOVWLZX (SI), BX
	MOVWLZX (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify3:
	MOVWLZX (SI), BX
	MOVWLZX (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVBLZX 2(SI), BX
	MOVBLZX 2(R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify4:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify5:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVBLZX 4(SI), BX
	MOVBLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify6:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVWLZX 4(SI), BX
	MOVWLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify7:
	MOVL (SI), BX
	MOVL (R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVWLZX 4(SI), BX
	MOVWLZX 4(R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	MOVBLZX 6(SI), BX
	MOVBLZX 6(R10), CX
	CMPQ BX, CX
	JNE shortTailNext
	JMP shortTailStore

shortTailVerify8:
	MOVQ (SI), BX
	CMPQ BX, (R10)
	JNE shortTailNext

shortTailStore:
	CMPQ AX, R12
	JGE done
	MOVL R13, (DI)
	ADDQ $4, DI
	INCQ AX

shortTailNext:
	INCQ R14
	CMPQ R14, R15
	JL shortTailLoop
	JMP done

longPattern:
	// pLen >= 9: same prefilter, then full compare (qwords + bytes).
	XORQ R14, R14

longChunkCheck:
	MOVQ R15, R13
	SUBQ R14, R13
	CMPQ R13, $32
	JL longTail

	LEAQ (R8)(R14*1), SI
	VMOVDQU (SI), Y2
	VPCMPEQB Y0, Y2, Y2

	LEAQ (SI)(R9*1), CX
	VMOVDQU (CX), Y3
	VPCMPEQB Y1, Y3, Y3

	VPAND Y3, Y2, Y2
	VPMOVMSKB Y2, DX

longCandLoop:
	// Candidate start = R14 + bit-index from mask.
	TESTQ DX, DX
	JE longNextChunk

	BSFQ DX, BX
	ADDQ R14, BX
	LEAQ (R8)(BX*1), SI
	MOVQ R10, CX
	MOVQ R11, R13

longVerifyQword:
	// Bulk compare 8 bytes at a time.
	CMPQ R13, $8
	JL longVerifyBytes
	MOVQ (SI), BX
	CMPQ BX, (CX)
	JNE longReject
	ADDQ $8, SI
	ADDQ $8, CX
	SUBQ $8, R13
	JMP longVerifyQword

longVerifyBytes:
	// Compare trailing bytes when pLen is not multiple of 8.
	TESTQ R13, R13
	JE longStore
	MOVBLZX (SI), BX
	CMPB BL, (CX)
	JNE longReject
	INCQ SI
	INCQ CX
	DECQ R13
	JMP longVerifyBytes

longStore:
	// SI advanced by pLen during verify, so start = (SI - data) - pLen.
	CMPQ AX, R12
	JGE done
	MOVQ SI, BX
	SUBQ R8, BX
	SUBQ R11, BX
	MOVL BX, (DI)
	ADDQ $4, DI
	INCQ AX

longReject:
	// Remove processed candidate bit and keep scanning this chunk.
	LEAQ -1(DX), CX
	ANDQ CX, DX
	JMP longCandLoop

longNextChunk:
	ADDQ $32, R14
	JMP longChunkCheck

longTail:
	// Scalar tail for long-pattern path.
	CMPQ R14, R15
	JGE done

longTailLoop:
	LEAQ (R8)(R14*1), SI
	MOVBLZX (SI), BX
	MOVBLZX (R10), CX
	CMPQ BX, CX
	JNE longTailNext
	MOVBLZX (SI)(R9*1), BX
	MOVBLZX (R10)(R9*1), CX
	CMPQ BX, CX
	JNE longTailNext

	LEAQ (R8)(R14*1), SI
	MOVQ R10, CX
	MOVQ R11, R13

longTailVerifyQword:
	CMPQ R13, $8
	JL longTailVerifyBytes
	MOVQ (SI), BX
	CMPQ BX, (CX)
	JNE longTailNext
	ADDQ $8, SI
	ADDQ $8, CX
	SUBQ $8, R13
	JMP longTailVerifyQword

longTailVerifyBytes:
	TESTQ R13, R13
	JE longTailStore
	MOVBLZX (SI), BX
	CMPB BL, (CX)
	JNE longTailNext
	INCQ SI
	INCQ CX
	DECQ R13
	JMP longTailVerifyBytes

longTailStore:
	CMPQ AX, R12
	JGE done
	MOVL R14, (DI)
	ADDQ $4, DI
	INCQ AX

longTailNext:
	INCQ R14
	CMPQ R14, R15
	JL longTailLoop
	JMP done

oneByte:
	// Special case pLen == 1: only one-byte equality checks.
	MOVBLZX (R10), BX
	MOVQ $0x0101010101010101, DX
	IMULQ BX, DX
	MOVQ DX, X0
	VPBROADCASTQ X0, Y0

	XORQ R14, R14

oneByteChunkCheck:
	MOVQ R15, R13
	SUBQ R14, R13
	CMPQ R13, $32
	JL oneByteTail

	LEAQ (R8)(R14*1), SI
	VMOVDQU (SI), Y2
	VPCMPEQB Y0, Y2, Y2
	VPMOVMSKB Y2, DX

oneByteCandLoop:
	// Every set bit in DX is a match offset in this 32-byte chunk.
	TESTQ DX, DX
	JE oneByteNextChunk

	BSFQ DX, BX
	ADDQ R14, BX
	CMPQ AX, R12
	JGE done
	MOVL BX, (DI)
	ADDQ $4, DI
	INCQ AX

	LEAQ -1(DX), CX
	ANDQ CX, DX
	JMP oneByteCandLoop

oneByteNextChunk:
	ADDQ $32, R14
	JMP oneByteChunkCheck

oneByteTail:
	// Scalar tail for pLen==1.
	CMPQ R14, R15
	JGE done

oneByteTailLoop:
	LEAQ (R8)(R14*1), SI
	MOVBLZX (SI), BX
	MOVBLZX (R10), CX
	CMPQ BX, CX
	JNE oneByteTailNext
	CMPQ AX, R12
	JGE done
	MOVL R14, (DI)
	ADDQ $4, DI
	INCQ AX

oneByteTailNext:
	INCQ R14
	CMPQ R14, R15
	JL oneByteTailLoop

done:
	// Clear upper YMM state before returning to Go code.
	VZEROUPPER
	MOVQ AX, ret+72(FP)
	RET
