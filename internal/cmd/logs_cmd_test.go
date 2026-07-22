//go:build unit

package cmd

import (
	"strings"
	"testing"
)

func TestLogsCmd_Flags(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		defValue string // expected default, empty to skip
		usage    string // expected substring in usage, empty to skip
	}{
		{name: "since", flag: "since", usage: "RFC3339"},
		{name: "last", flag: "last"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flag := logsCmd.Flags().Lookup(tc.flag)
			if flag == nil {
				t.Fatalf("expected --%s flag on logs command", tc.flag)
			}
			if tc.usage != "" && !strings.Contains(flag.Usage, tc.usage) {
				t.Errorf("--%s flag usage should mention %q, got: %s", tc.flag, tc.usage, flag.Usage)
			}
			if tc.defValue != "" && flag.DefValue != tc.defValue {
				t.Errorf("--%s flag default should be %q, got: %q", tc.flag, tc.defValue, flag.DefValue)
			}
		})
	}
}