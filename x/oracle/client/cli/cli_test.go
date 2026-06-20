//go:build cosmos

package cli

import (
	"testing"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

func TestGetQueryCmd_HasSubcommands(t *testing.T) {
	t.Parallel()
	cmd := GetQueryCmd()
	if cmd.Use != types.ModuleName {
		t.Errorf("Use = %q, want %q", cmd.Use, types.ModuleName)
	}
	subs := cmd.Commands()
	if len(subs) != 4 {
		t.Fatalf("expected 4 query subcommands, got %d", len(subs))
	}
	names := map[string]bool{}
	for _, sub := range subs {
		names[sub.Name()] = true
	}
	for _, want := range []string{"params", "price-feed", "all-price-feeds", "aggregated-price"} {
		if !names[want] {
			t.Errorf("missing query subcommand %q", want)
		}
	}
}
