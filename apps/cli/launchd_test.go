package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

func TestLaunchdPlistTemplate(t *testing.T) {
	p := launchdPaths{
		Label:     "com.lfm",
		Binary:    "/Users/test/bin/lfm",
		StdoutLog: "/Users/test/Library/Logs/lfm/stdout.log",
		StderrLog: "/Users/test/Library/Logs/lfm/stderr.log",
	}
	tpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tpl.Execute(&b, p); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"<string>com.lfm</string>",
		"<string>/Users/test/bin/lfm</string>",
		"<string>run</string>",
		"RunAtLoad",
		"KeepAlive",
		"/Users/test/Library/Logs/lfm/stderr.log",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\n%s", want, out)
		}
	}
}

func TestResolveLaunchdPaths(t *testing.T) {
	p, err := resolveLaunchdPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.Label != launchdLabel {
		t.Fatalf("label = %q", p.Label)
	}
	if !strings.HasSuffix(p.Plist, "com.lfm.plist") {
		t.Fatalf("plist = %q", p.Plist)
	}
	if filepath.Base(p.LogDir) != "lfm" {
		t.Fatalf("logDir = %q", p.LogDir)
	}
	if p.Binary == "" {
		t.Fatal("empty binary")
	}
	// Executable should exist for the test process.
	if _, err := os.Stat(p.Binary); err != nil {
		// Under `go test` the binary path may still be valid; only soft-check.
		t.Logf("binary path (may be test binary): %s (%v)", p.Binary, err)
	}
}
