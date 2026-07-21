package config

import "testing"

func TestParseExecutionModes(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"disabled", []string{}},
		{"", []string{}},
		{"local", []string{"local"}},
		{"LOCAL", []string{"local"}},
		{" local , google_calendar ", []string{"local", "google_calendar"}},
		{"local,disabled", []string{"local"}},
	}
	for _, tc := range cases {
		got := parseExecutionModes(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("parseExecutionModes(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parseExecutionModes(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		}
	}
}

func TestHasExecutionMode(t *testing.T) {
	c := Config{ExecutionModes: []string{"local", "google_calendar"}}
	if !c.HasExecutionMode("local") {
		t.Fatal("expected local enabled")
	}
	if !c.HasExecutionMode("GOOGLE_CALENDAR") {
		t.Fatal("expected case-insensitive match")
	}
	if c.HasExecutionMode("http") {
		t.Fatal("http must not be enabled")
	}
	if (Config{}).HasExecutionMode("local") {
		t.Fatal("empty set must enable nothing")
	}
}
