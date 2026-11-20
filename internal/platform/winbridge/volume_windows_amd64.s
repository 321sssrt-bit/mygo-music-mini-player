#include "textflag.h"

TEXT ·volumeThunk(SB),NOSPLIT,$0-8
	LEAQ ·setVolumeABI(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// Windows x64 passes the second argument's float in XMM1.
TEXT ·setVolumeABI(SB),NOSPLIT|NOFRAME,$0-0
	MOVQ CX, AX
	MOVQ DX, CX
	MOVD R8, X1
	XORQ R8, R8
	JMP AX
