
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSingleInsuranceCLIAmount(t *testing.T) {
	t.Parallel()

	coin, err := parseSingleInsuranceCLIAmount("25000ulac")
	require.NoError(t, err)
	require.Equal(t, "ulac", coin.Denom)
	require.Equal(t, "25000", coin.Amount.String())
}

func TestParseSingleInsuranceCLIAmountRejectsMultiCoin(t *testing.T) {
	t.Parallel()

	_, err := parseSingleInsuranceCLIAmount("25000ulac,7uother")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly one coin")
}

func TestReadInsuranceParamsFile_AllowsMaxInsuranceParamsFileBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "params.json")
	payload := bytes.Repeat([]byte(" "), int(maxInsuranceParamsFileBytes))
	require.NoError(t, os.WriteFile(path, payload, 0o600))

	got, err := readInsuranceParamsFile(path)
	require.NoError(t, err)
	require.Len(t, got, int(maxInsuranceParamsFileBytes))
}

func TestReadInsuranceParamsFile_RejectsOversizedParamsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "params.json")
	payload := bytes.Repeat([]byte(" "), int(maxInsuranceParamsFileBytes)+1)
	require.NoError(t, os.WriteFile(path, payload, 0o600))

	_, err := readInsuranceParamsFile(path)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "params file exceeds"), "error = %q", err)
}
