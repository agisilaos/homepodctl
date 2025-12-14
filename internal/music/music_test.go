package music

import "testing"

func TestParseFloatLoose(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want float64
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"  12.5 ", 12.5},
		{"264,0", 264},
		{"not-a-number", 0},
	}

	for _, tc := range cases {
		if got := parseFloatLoose(tc.in); got != tc.want {
			t.Fatalf("parseFloatLoose(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalizeName(t *testing.T) {
	t.Parallel()

	// U+FE0F variation selector, plus zero-width joiner U+200D and weird spacing.
	in := "Example  Playlist \uFE0F \u200D🎶"
	got := canonicalizeName(in)
	want := "Example Playlist 🎶"
	if got != want {
		t.Fatalf("canonicalizeName(%q) = %q, want %q", in, got, want)
	}
}

func TestParseBool(t *testing.T) {
	t.Parallel()

	if !parseBool("true") || !parseBool(" yes ") || !parseBool("1") {
		t.Fatalf("expected truthy values to parse true")
	}
	if parseBool("false") || parseBool("") || parseBool("0") || parseBool("no") {
		t.Fatalf("expected falsy values to parse false")
	}
}
