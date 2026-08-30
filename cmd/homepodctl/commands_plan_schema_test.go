package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParsePlanArgsConsumesSeparateJSONBoolean(t *testing.T) {
	t.Parallel()

	jsonOut, pos, err := parsePlanArgs([]string{"native-run", "--json", "false", "--shortcut", "Example"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	if jsonOut {
		t.Fatal("expected separate false value to disable plan envelope JSON")
	}
	want := []string{"native-run", "--shortcut", "Example"}
	if !slices.Equal(pos, want) {
		t.Fatalf("pos=%v want=%v", pos, want)
	}

	jsonOut, pos, err = parsePlanArgs([]string{"play", "focus", "--json=no"})
	if err != nil {
		t.Fatalf("parsePlanArgs equals value: %v", err)
	}
	if jsonOut || !slices.Equal(pos, []string{"play", "focus"}) {
		t.Fatalf("equals value jsonOut=%t pos=%v", jsonOut, pos)
	}
}

func TestParseAndNormalizePlanTargetPreservesDelimiterSuffix(t *testing.T) {
	t.Parallel()

	_, pos, err := parsePlanArgs([]string{"native-run", "--", "--dry-run=false", "--json=maybe"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	cmd, got, err := normalizePlanTarget(pos[0], pos[1:])
	if err != nil {
		t.Fatalf("normalizePlanTarget: %v", err)
	}
	want := []string{"--dry-run=true", "--json=true", "--", "--dry-run=false", "--json=maybe"}
	if cmd != "native-run" || !slices.Equal(got, want) {
		t.Fatalf("cmd=%q args=%v want=%v", cmd, got, want)
	}
}

func TestParsePlanArgsStripsDelimiterBeforeTarget(t *testing.T) {
	t.Parallel()

	_, pos, err := parsePlanArgs([]string{"--", "native-run", "--shortcut", "Example"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	want := []string{"native-run", "--shortcut", "Example"}
	if !slices.Equal(pos, want) {
		t.Fatalf("pos=%v want=%v", pos, want)
	}
}

func TestParsePlanArgsDoesNotConsumeSeparateShortBoolean(t *testing.T) {
	t.Parallel()

	jsonOut, pos, err := parsePlanArgs([]string{"play", "--json", "f"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	if !jsonOut {
		t.Fatal("expected bare --json to enable plan envelope JSON")
	}
	want := []string{"play", "f"}
	if !slices.Equal(pos, want) {
		t.Fatalf("pos=%v want=%v", pos, want)
	}
}

func TestParsePlanArgsAcceptsLegacyEqualsShortBoolean(t *testing.T) {
	t.Parallel()

	jsonOut, pos, err := parsePlanArgs([]string{"play", "focus", "--json=f"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	if jsonOut || !slices.Equal(pos, []string{"play", "focus"}) {
		t.Fatalf("jsonOut=%t pos=%v", jsonOut, pos)
	}
}

func TestNormalizePlanTargetCanonicalizesOwnedFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		args []string
		want []string
	}{
		{
			name: "missing flags",
			cmd:  "native-run",
			args: []string{"--shortcut", "Example"},
			want: []string{"--dry-run=true", "--json=true", "--shortcut", "Example"},
		},
		{
			name: "false equals",
			cmd:  "native-run",
			args: []string{"--shortcut", "Example", "--dry-run=false", "--json=0"},
			want: []string{"--dry-run=true", "--json=true", "--shortcut", "Example"},
		},
		{
			name: "separate false values",
			cmd:  "native-run",
			args: []string{"--shortcut", "Example", "--dry-run", "false", "--json", "no"},
			want: []string{"--dry-run=true", "--json=true", "--shortcut", "Example"},
		},
		{
			name: "duplicates and explicit true",
			cmd:  "play",
			args: []string{"focus", "--dry-run", "--dry-run=false", "--json=yes", "--json", "false"},
			want: []string{"--dry-run=true", "--json=true", "focus"},
		},
		{
			name: "unrecognized separate value remains positional",
			cmd:  "play",
			args: []string{"--dry-run", "focus"},
			want: []string{"--dry-run=true", "--json=true", "focus"},
		},
		{
			name: "target delimiter suffix stays literal",
			cmd:  "native-run",
			args: []string{"--shortcut", "Example", "--", "--dry-run=false", "--json=maybe"},
			want: []string{"--dry-run=true", "--json=true", "--shortcut", "Example", "--", "--dry-run=false", "--json=maybe"},
		},
		{
			name: "out subcommand stays first",
			cmd:  "out",
			args: []string{"set", "--room", "Bedroom", "--dry-run=false"},
			want: []string{"set", "--dry-run=true", "--json=true", "--room", "Bedroom"},
		},
		{
			name: "automation subcommand stays first",
			cmd:  "automation",
			args: []string{"run", "-f", "routine.yaml", "--json=false"},
			want: []string{"run", "--dry-run=true", "--json=true", "-f", "routine.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := slices.Clone(tt.args)
			cmd, got, err := normalizePlanTarget(tt.cmd, tt.args)
			if err != nil {
				t.Fatalf("normalizePlanTarget: %v", err)
			}
			if cmd != tt.cmd {
				t.Fatalf("cmd=%q want=%q", cmd, tt.cmd)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("args=%v want=%v", got, tt.want)
			}
			if !slices.Equal(tt.args, original) {
				t.Fatalf("input mutated: got=%v want=%v", tt.args, original)
			}
		})
	}
}

func TestNormalizePlanTargetRejectsInvalidOwnedFlagValues(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--dry-run=maybe"},
		{"--json="},
	} {
		_, _, err := normalizePlanTarget("play", args)
		if err == nil || !strings.Contains(err.Error(), "invalid boolean") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestPlanPreservesFlagLookingTargetValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"native-run", "--shortcut", "--json", "--json"}, []string{"--shortcut", "--json"}},
		{[]string{"native-run", "--shortcut", "--dry-run=false", "--json"}, []string{"--shortcut", "--dry-run=false"}},
		{[]string{"native-run", "-shortcut", "--json", "--json"}, []string{"-shortcut", "--json"}},
	} {
		jsonOut, pos, err := parsePlanArgs(tc.args)
		if err != nil || !jsonOut {
			t.Fatalf("parse %v: json=%t pos=%v err=%v", tc.args, jsonOut, pos, err)
		}
		_, target, err := normalizePlanTarget(pos[0], pos[1:])
		want := append([]string{"--dry-run=true", "--json=true"}, tc.want...)
		if err != nil || !slices.Equal(target, want) {
			t.Errorf("normalize %v: target=%v want=%v err=%v", tc.args, target, want, err)
		}
	}
}

func TestPlanCanonicalizesLegacyOwnedFlags(t *testing.T) {
	t.Parallel()
	_, target, err := normalizePlanTarget("native-run", []string{"-shortcut=Example", "-dry-run=f", "-json=f"})
	want := []string{"--dry-run=true", "--json=true", "-shortcut=Example"}
	if err != nil || !slices.Equal(target, want) {
		t.Fatalf("target=%v want=%v err=%v", target, want, err)
	}
}
