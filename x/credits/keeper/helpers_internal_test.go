package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// --- formatRatioFixed4 ---

func TestFormatRatioFixed4_OneToOne(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(100), sdkmath.NewInt(100))
	require.Equal(t, "1.0000", result)
}

func TestFormatRatioFixed4_HalfRatio(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(1), sdkmath.NewInt(2))
	require.Equal(t, "0.5000", result)
}

func TestFormatRatioFixed4_FractionalResult(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(1), sdkmath.NewInt(3))
	require.Equal(t, "0.3333", result)
}

func TestFormatRatioFixed4_TwoToOne(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(200), sdkmath.NewInt(100))
	require.Equal(t, "2.0000", result)
}

func TestFormatRatioFixed4_ZeroDenominator(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(100), sdkmath.ZeroInt())
	require.Equal(t, "0.0000", result)
}

func TestFormatRatioFixed4_ZeroNumerator(t *testing.T) {
	result := formatRatioFixed4(sdkmath.ZeroInt(), sdkmath.NewInt(100))
	require.Equal(t, "0.0000", result)
}

func TestFormatRatioFixed4_NegativeNumerator(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(-10), sdkmath.NewInt(100))
	require.Equal(t, "0.0000", result)
}

func TestFormatRatioFixed4_NegativeDenominator(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(10), sdkmath.NewInt(-100))
	require.Equal(t, "0.0000", result)
}

func TestFormatRatioFixed4_LargeNumbers(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(999_999_999), sdkmath.NewInt(1_000_000_000))
	require.Equal(t, "0.9999", result)
}

func TestFormatRatioFixed4_SmallFraction(t *testing.T) {
	result := formatRatioFixed4(sdkmath.NewInt(1), sdkmath.NewInt(10000))
	require.Equal(t, "0.0001", result)
}

// --- parseLockSequence ---

func TestParseLockSequence_Valid(t *testing.T) {
	seq, err := parseLockSequence("lock-42")
	require.NoError(t, err)
	require.Equal(t, uint64(42), seq)
}

func TestParseLockSequence_Zero(t *testing.T) {
	seq, err := parseLockSequence("lock-0")
	require.NoError(t, err)
	require.Equal(t, uint64(0), seq)
}

func TestParseLockSequence_LargeNumber(t *testing.T) {
	seq, err := parseLockSequence("lock-999999")
	require.NoError(t, err)
	require.Equal(t, uint64(999999), seq)
}

func TestParseLockSequence_WrongPrefix(t *testing.T) {
	_, err := parseLockSequence("vault-42")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected lock id format")
}

func TestParseLockSequence_TooShort(t *testing.T) {
	_, err := parseLockSequence("lock")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected lock id format")
}

func TestParseLockSequence_Empty(t *testing.T) {
	_, err := parseLockSequence("")
	require.Error(t, err)
}

func TestParseLockSequence_NotANumber(t *testing.T) {
	_, err := parseLockSequence("lock-abc")
	require.Error(t, err)
}

// --- Schema helpers ---

func TestGetSchemaInfo(t *testing.T) {
	info := GetSchemaInfo()
	require.Equal(t, CurrentSchemaVersion, info.Version)
	require.Equal(t, "credits", info.ModuleName)
	require.NotEmpty(t, info.Description)
}

func TestGetConsensusVersion(t *testing.T) {
	v := GetConsensusVersion()
	require.Equal(t, uint64(ConsensusVersion), v)
}

func TestSchemaVersionConstants(t *testing.T) {
	require.Equal(t, uint64(1), SchemaVersion1)
	require.Equal(t, uint64(2), SchemaVersion2)
	require.Equal(t, SchemaVersion2, CurrentSchemaVersion)
}

// --- recoverCredits ---

func TestRecoverCredits_NoError(t *testing.T) {
	var err error
	func() {
		defer recoverCredits("test-action", &err)
		// No panic, so err should remain nil
	}()
	require.NoError(t, err)
}

func TestRecoverCredits_PanicError(t *testing.T) {
	var err error
	func() {
		defer recoverCredits("test-action", &err)
		panic("test panic message")
	}()
	require.Error(t, err)
	require.Contains(t, err.Error(), "test-action panic")
	require.Contains(t, err.Error(), "test panic message")
}

func TestRecoverCredits_PanicErrorType(t *testing.T) {
	var err error
	innerErr := &testError{msg: "inner error"}
	func() {
		defer recoverCredits("test-action", &err)
		panic(innerErr)
	}()
	require.Error(t, err)
	require.Contains(t, err.Error(), "test-action panic")
	require.Contains(t, err.Error(), "inner error")
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
