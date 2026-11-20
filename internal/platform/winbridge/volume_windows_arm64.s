#include "textflag.h"

TEXT ·volumeThunk(SB),NOSPLIT,$0-8
	MOVD $·setVolumeABI(SB), R0
	MOVD R0, ret+0(FP)
	RET

// Windows ARM64 passes the first floating-point argument in S0.
TEXT ·setVolumeABI(SB),NOSPLIT|NOFRAME,$0-0
	MOVD R0, R9
	MOVD R1, R0
	FMOVS R2, F0
	MOVD ZR, R1
	JMP (R9)
