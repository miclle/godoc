package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func findGOROOT() string {
	if env := os.Getenv("GOROOT"); env != "" {
		return filepath.Clean(env)
	}
	def := filepath.Clean(runtime.GOROOT())
	if runtime.Compiler == "gccgo" {
		// gccgo has no real GOROOT, and it certainly doesn't
		// depend on the executable's location.
		return def
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return def
	}
	return strings.TrimSpace(string(out))
}
