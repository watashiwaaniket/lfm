package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

const launchdLabel = "com.lfm"

// launchdPlistTemplate is written to ~/Library/LaunchAgents/com.lfm.plist
const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Binary}}</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>{{.StdoutLog}}</string>
	<key>StandardErrorPath</key>
	<string>{{.StderrLog}}</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`

type launchdPaths struct {
	Label     string
	Binary    string
	Plist     string
	LogDir    string
	StdoutLog string
	StderrLog string
}

func resolveLaunchdPaths() (launchdPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return launchdPaths{}, fmt.Errorf("home dir: %w", err)
	}

	bin, err := os.Executable()
	if err != nil {
		return launchdPaths{}, fmt.Errorf("resolve executable: %w", err)
	}
	// Prefer the real path if this is a symlink (e.g. under /tmp during go run).
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}

	logDir := filepath.Join(home, "Library", "Logs", "lfm")
	return launchdPaths{
		Label:     launchdLabel,
		Binary:    bin,
		Plist:     filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"),
		LogDir:    logDir,
		StdoutLog: filepath.Join(logDir, "stdout.log"),
		StderrLog: filepath.Join(logDir, "stderr.log"),
	}, nil
}

func cmdInstall() error {
	p, err := resolveLaunchdPaths()
	if err != nil {
		return err
	}

	// Refuse go-run temp binaries — they vanish after the process ends.
	if strings.Contains(p.Binary, string(filepath.Separator)+"go-build") ||
		strings.HasPrefix(p.Binary, os.TempDir()) {
		return fmt.Errorf("refusing to install a temporary binary (%s); run `go build -o lfm .` and install with that binary", p.Binary)
	}

	if err := os.MkdirAll(filepath.Dir(p.Plist), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.MkdirAll(p.LogDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	tpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p.Plist, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := tpl.Execute(f, p); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// Ad-hoc sign so launchd does not reject the binary (OS_REASON_CODESIGNING).
	if out, err := exec.Command("codesign", "-s", "-", "-f", p.Binary).CombinedOutput(); err != nil {
		fmt.Printf("note: codesign: %v (%s)\n", err, strings.TrimSpace(string(out)))
	}

	// Replace any existing agent cleanly.
	_ = launchctlBootout(p.Label)

	if err := launchctlBootstrap(p.Plist); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w\nplist written to %s — try: launchctl bootstrap gui/$(id -u) %s", err, p.Plist, p.Plist)
	}

	fmt.Printf("Installed LaunchAgent %s\n", p.Label)
	fmt.Printf("  binary: %s\n", p.Binary)
	fmt.Printf("  plist:  %s\n", p.Plist)
	fmt.Printf("  logs:   %s\n", p.LogDir)
	fmt.Println()
	fmt.Println("It will start now and on login.")
	fmt.Println("Useful commands:")
	fmt.Printf("  launchctl print gui/$(id -u)/%s\n", p.Label)
	fmt.Printf("  tail -f %s\n", p.StderrLog)
	fmt.Println("  lfm uninstall")
	return nil
}

func cmdUninstall() error {
	p, err := resolveLaunchdPaths()
	if err != nil {
		return err
	}

	if err := launchctlBootout(p.Label); err != nil {
		// Not loaded is fine.
		fmt.Printf("note: bootout: %v\n", err)
	}

	if err := os.Remove(p.Plist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	fmt.Printf("Uninstalled LaunchAgent %s\n", p.Label)
	fmt.Printf("  removed: %s\n", p.Plist)
	fmt.Println("Logs left in place:", p.LogDir)
	return nil
}

func guiDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func launchctlBootstrap(plistPath string) error {
	// modern macOS
	cmd := exec.Command("launchctl", "bootstrap", guiDomain(), plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		// fallback for older systems
		cmd2 := exec.Command("launchctl", "load", "-w", plistPath)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("bootstrap: %v (%s); load: %v (%s)", err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)))
		}
	}
	// Ensure it starts immediately.
	_ = exec.Command("launchctl", "kickstart", "-k", guiDomain()+"/"+launchdLabel).Run()
	return nil
}

func launchctlBootout(label string) error {
	cmd := exec.Command("launchctl", "bootout", guiDomain()+"/"+label)
	if out, err := cmd.CombinedOutput(); err != nil {
		cmd2 := exec.Command("launchctl", "unload", "-w",
			filepath.Join(mustHome(), "Library", "LaunchAgents", label+".plist"))
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("bootout: %v (%s); unload: %v (%s)",
				err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

func mustHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
