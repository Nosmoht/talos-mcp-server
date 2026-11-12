package talos

import (
	"strings"
	"testing"
)

func TestParseMetaKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    uint8
		wantErr string
	}{
		{name: "decimal", input: "6", want: 6},
		{name: "decimal max", input: "255", want: 255},
		{name: "decimal zero", input: "0", want: 0},
		{name: "hex lower", input: "0x0c", want: 12},
		{name: "hex upper", input: "0X0C", want: 12},
		{name: "explicit octal", input: "0o17", want: 15},
		{name: "empty", input: "", wantErr: "must not be empty"},
		{name: "leading zero rejected", input: "06", wantErr: "ambiguous leading zero"},
		{name: "leading zero rejected (long)", input: "013", wantErr: "ambiguous leading zero"},
		{name: "overflow", input: "256", wantErr: "exceeds META key range"},
		{name: "non-numeric", input: "abc", wantErr: "invalid syntax"},
		{name: "decimal underscore rejected", input: "1_0", wantErr: "must not contain underscores"},
		{name: "hex underscore rejected", input: "0x_0c", wantErr: "must not contain underscores"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMetaKey(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseMetaPrivilegedKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    []uint8
		wantErr string
	}{
		{name: "empty", input: "", want: nil},
		{name: "whitespace", input: "  \t", want: nil},
		{name: "only commas", input: ",,,", want: nil},
		{name: "single decimal", input: "13", want: []uint8{13}},
		{name: "single hex lower", input: "0x0d", want: []uint8{13}},
		{name: "single hex upper", input: "0X0D", want: []uint8{13}},
		{name: "explicit octal", input: "0o15", want: []uint8{13}},
		{name: "multiple mixed", input: "6, 0x07, 13", want: []uint8{6, 7, 13}},
		{name: "duplicates collapse", input: "6,6,0x06", want: []uint8{6}},
		{name: "max valid", input: "255", want: []uint8{255}},
		{name: "min valid", input: "0", want: []uint8{0}},
		{name: "leading-zero decimal rejected", input: "013", wantErr: "ambiguous leading zero"},
		{name: "overflow decimal", input: "256", wantErr: "exceeds META key range"},
		{name: "negative", input: "-1", wantErr: "invalid syntax"},
		{name: "non-numeric", input: "abc", wantErr: "invalid syntax"},
		{name: "trailing comma whitespace", input: "6, , 7,", want: []uint8{6, 7}},
		{name: "underscore rejected", input: "6, 1_3", wantErr: "must not contain underscores"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMetaPrivilegedKeys(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result %v)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d keys (%v), want %d (%v)", len(got), keysOf(got), len(tc.want), tc.want)
			}
			for _, k := range tc.want {
				if _, ok := got[k]; !ok {
					t.Errorf("missing key %d in result %v", k, keysOf(got))
				}
			}
		})
	}
}

func keysOf(m map[uint8]struct{}) []uint8 {
	out := make([]uint8, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
