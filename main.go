package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	configPath := DefaultConfigPath()

	// Optional: -c / --config path
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if (args[i] == "-c" || args[i] == "--config") && i+1 < len(args) {
			configPath = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
	}

	switch cmd {
	case "auth":
		if err := cmdAuth(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "auth: %v\n", err)
			os.Exit(1)
		}
	case "run":
		if err := cmdRun(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "run: %v\n", err)
			os.Exit(1)
		}
	case "now":
		if err := cmdNow(); err != nil {
			fmt.Fprintf(os.Stderr, "now: %v\n", err)
			os.Exit(1)
		}
	case "install":
		if err := cmdInstall(); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := cmdUninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `lfm — lastFM scrobbler (Apple Music → Last.fm)

Usage:
  lfm auth [-c config.yaml]   Interactive Last.fm authorization
  lfm run  [-c config.yaml]   Start polling daemon (foreground)
  lfm now                     Print currently playing track
  lfm install                 Install LaunchAgent (start at login)
  lfm uninstall               Remove LaunchAgent
  lfm version

Config: %s
Env:    LASTFM_API_KEY, LASTFM_API_SECRET, LASTFM_SESSION_KEY, POLL_INTERVAL
`, DefaultConfigPath())
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}

func cmdAuth(configPath string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	if err := cfg.ValidateAuthCredentials(); err != nil {
		return err
	}

	client := NewLastFMClient(cfg.LastFM.APIKey, cfg.LastFM.APISecret, "")
	token, err := client.GetToken()
	if err != nil {
		return fmt.Errorf("getToken: %w", err)
	}

	url := client.AuthURL(token)
	fmt.Println("Authorize this application in your browser:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
	fmt.Print("Press Enter after you have authorized... ")

	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	username, sessionKey, err := client.GetSession(token)
	if err != nil {
		return fmt.Errorf("getSession: %w", err)
	}

	if err := SaveSession(configPath, sessionKey, username); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	fmt.Printf("Authenticated as %s. Session key saved to %s\n", username, configPath)
	return nil
}

func cmdRun(configPath string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	if err := cfg.ValidateRun(); err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)
	music := NewAppleScriptMusic()
	lastfm := NewLastFMClient(cfg.LastFM.APIKey, cfg.LastFM.APISecret, cfg.LastFM.SessionKey)
	scrobbler := NewScrobbler(music, lastfm, cfg, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return scrobbler.Run(ctx)
}

func cmdNow() error {
	music := NewAppleScriptMusic()
	track, err := music.GetCurrentTrack()
	if err != nil {
		return err
	}
	fmt.Println(track.String())
	return nil
}
