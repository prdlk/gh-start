package main

import "testing"

func TestContinuation(t *testing.T) {
	tests := []struct {
		name, draft, out, want string
	}{
		{"echoed, new word", "Add a dark mode toggle that", "Add a dark mode toggle that persists the choice.", " persists the choice."},
		{"echoed, mid word", "Refac", "Refactor the auth module.", "tor the auth module."},
		{"echoed, draft ends in space", "Add a toggle ", "Add a toggle that persists.", "that persists."},
		{"echoed, draft has trailing newline", "Fix the parser\n", "Fix the parser so empty input returns nil.", "so empty input returns nil."},
		{"bare continuation", "Add a dark mode toggle that", "persists the choice.", " persists the choice."},
		{"bare punctuation", "Add a dark mode toggle", ", persisted in localStorage.", ", persisted in localStorage."},
		{"quoted echo", "Add a toggle that", `"Add a toggle that persists."`, " persists."},
		{"rewrite rejected", "Fix the bug in the login form", "Fix the login form bug where the password field clears.", ""},
		{"no continuation", "Add a toggle", "Add a toggle", ""},
		{"multi-line collapsed", "Add a toggle", "Add a toggle\nthat persists\n\nacross reloads.", " that persists across reloads."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := continuation(tt.draft, tt.out); got != tt.want {
				t.Errorf("continuation(%q, %q) = %q, want %q", tt.draft, tt.out, got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"dark-mode\n":                        "dark-mode",
		"Add CSV export to the reports!":     "add-csv-export-to-the",
		"  `auth-refactor`  ":                "auth-refactor",
		"supercalifragilisticexpialidocious": "supercalifragilisticexpi",
	}
	for in, want := range tests {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellJoin(t *testing.T) {
	got := shellJoin([]string{"omp", "--model", "anthropic/claude-opus-5", "it's here"})
	want := `omp --model anthropic/claude-opus-5 'it'\''s here'`
	if got != want {
		t.Errorf("shellJoin = %s, want %s", got, want)
	}
}
