package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBundledExamples(t *testing.T) {
	for _, file := range []string{"graft", "vscode", "nautilus"} {
		t.Run(file, func(t *testing.T) {
			s := newSvc(t)
			s.Home = t.TempDir()
			bin := t.TempDir()
			for _, tool := range []string{"graft", "code", "nautilus"} {
				if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			if err := s.InstallExample(file); err != nil {
				t.Fatalf("failed to install %s: %v", file, err)
			}
		})
	}
}
