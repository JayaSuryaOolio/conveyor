package main

import "testing"

func TestShellPath(t *testing.T) {
	for out, want := range map[string]string{
		"motd noise\n" + pathMarker + "/opt/homebrew/bin:/usr/bin": "/opt/homebrew/bin:/usr/bin",
		"no marker": "",
	} {
		if got := shellPath(out); got != want {
			t.Errorf("shellPath(%q) = %q, want %q", out, got, want)
		}
	}
}
