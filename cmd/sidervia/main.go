package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/AlexKris/sidervia/internal/app"
	"github.com/AlexKris/sidervia/internal/auth"
	"github.com/AlexKris/sidervia/internal/buildinfo"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/config"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/maintenance"
	"github.com/AlexKris/sidervia/internal/processlock"
	"github.com/AlexKris/sidervia/internal/safelog"
	"github.com/AlexKris/sidervia/internal/store"
	webassets "github.com/AlexKris/sidervia/web"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "sidervia:", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	arguments, dev := extractDev(arguments)
	if containsHelpFlag(arguments) {
		printUsage(stdout)
		return nil
	}
	command := "serve"
	if len(arguments) > 0 && !stringsHasFlagPrefix(arguments[0]) {
		command, arguments = arguments[0], arguments[1:]
	}
	if command == "help" || command == "-h" || command == "--help" {
		printUsage(stdout)
		return nil
	}
	if command == "version" {
		if len(arguments) != 0 {
			return errors.New("version does not accept arguments")
		}
		return writeJSON(stdout, struct {
			buildinfo.Info
			SchemaMax int `json:"schema_max"`
		}{Info: buildinfo.Current(), SchemaMax: store.LatestSchemaVersion})
	}

	cfg, err := config.Load(dev)
	if err != nil {
		return err
	}
	logger := safelog.New(stderr, cfg.LogLevel)
	ctx := context.Background()

	switch command {
	case "serve":
		set := newFlagSet("serve", stderr)
		if err := set.Parse(arguments); err != nil {
			return err
		}
		if set.NArg() != 0 {
			return errors.New("serve does not accept positional arguments")
		}
		signalContext, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return app.Serve(signalContext, cfg, webassets.Handler(), logger)
	case "doctor":
		set := newFlagSet("doctor", stderr)
		healthcheck := set.Bool("healthcheck", false, "check the running process readiness endpoint")
		if err := set.Parse(arguments); err != nil {
			return err
		}
		if *healthcheck {
			return app.Healthcheck(ctx, cfg)
		}
		cipher, _, err := loadCipher(cfg.MasterKeyFile)
		if err != nil {
			return err
		}
		report, err := maintenance.Doctor(ctx, cfg.DataDir, cipher)
		if err != nil {
			return err
		}
		return writeJSON(stdout, report)
	case "backup":
		return runBackup(ctx, cfg, arguments, stdout, stderr)
	case "key":
		return runKey(ctx, cfg, arguments, stdout, stderr)
	case "admin":
		return runAdmin(ctx, cfg, arguments, stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runBackup(ctx context.Context, cfg config.Config, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("backup requires create or verify")
	}
	subcommand, arguments := arguments[0], arguments[1:]
	cipher, _, err := loadCipher(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	switch subcommand {
	case "create":
		set := newFlagSet("backup create", stderr)
		output := set.String("output", "", "new backup database path")
		if err := set.Parse(arguments); err != nil {
			return err
		}
		if *output == "" || set.NArg() != 0 {
			return errors.New("backup create requires --output and no positional arguments")
		}
		database, err := store.OpenReadOnly(ctx, cfg.DataDir)
		if err != nil {
			return err
		}
		defer database.Close()
		report, err := maintenance.CreateBackup(ctx, database, cipher, *output)
		if err != nil {
			return err
		}
		return writeJSON(stdout, report)
	case "verify":
		set := newFlagSet("backup verify", stderr)
		input := set.String("input", "", "backup database path")
		if err := set.Parse(arguments); err != nil {
			return err
		}
		if *input == "" || set.NArg() != 0 {
			return errors.New("backup verify requires --input and no positional arguments")
		}
		report, err := maintenance.VerifyBackup(ctx, *input, cipher)
		if err != nil {
			return err
		}
		return writeJSON(stdout, report)
	default:
		return fmt.Errorf("unknown backup command %q", subcommand)
	}
}

func runKey(ctx context.Context, cfg config.Config, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "rotate" {
		return errors.New("key requires rotate")
	}
	set := newFlagSet("key rotate", stderr)
	newKeyFile := set.String("new-key-file", "", "secure file containing the new base64 master key")
	backupPath := set.String("backup", "", "verified pre-rotation backup database")
	if err := set.Parse(arguments[1:]); err != nil {
		return err
	}
	if *newKeyFile == "" || *backupPath == "" || set.NArg() != 0 {
		return errors.New("key rotate requires --new-key-file and --backup")
	}
	oldCipher, _, err := loadCipher(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	newCipher, _, err := loadCipher(*newKeyFile)
	if err != nil {
		return fmt.Errorf("load new master key: %w", err)
	}
	if _, err := maintenance.VerifyBackup(ctx, *backupPath, oldCipher); err != nil {
		return fmt.Errorf("verify required pre-rotation backup: %w", err)
	}
	if err := store.PrepareDataDir(cfg.DataDir); err != nil {
		return err
	}
	lock, err := processlock.Acquire(filepath.Join(cfg.DataDir, "sidervia.lock"))
	if err != nil {
		return fmt.Errorf("key rotation requires Sidervia to be stopped: %w", err)
	}
	defer lock.Close()
	database, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer database.Close()
	report, err := maintenance.RotateKey(ctx, database, oldCipher, newCipher)
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, report); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stderr, "master key rotation complete; atomically replace SIDERVIA_MASTER_KEY_FILE with the new key before restarting")
	return nil
}

func runAdmin(ctx context.Context, cfg config.Config, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "reset-password" {
		return errors.New("admin requires reset-password")
	}
	set := newFlagSet("admin reset-password", stderr)
	passwordFile := set.String("password-file", "", "secure file containing the new password")
	disableTOTP := set.Bool("disable-totp", false, "also disable TOTP recovery")
	if err := set.Parse(arguments[1:]); err != nil {
		return err
	}
	if *passwordFile == "" || set.NArg() != 0 {
		return errors.New("admin reset-password requires --password-file")
	}
	cipher, key, err := loadCipher(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	if err := store.PrepareDataDir(cfg.DataDir); err != nil {
		return err
	}
	lock, err := processlock.Acquire(filepath.Join(cfg.DataDir, "sidervia.lock"))
	if err != nil {
		return fmt.Errorf("password reset requires Sidervia to be stopped: %w", err)
	}
	defer lock.Close()
	database, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.VerifySentinel(ctx, cipher); err != nil {
		return err
	}
	service := auth.NewService(database.DB(), cipher, clock.Real{}, identifier.NewGenerator(), auth.NewPasswordHasher(), key, cfg.PublicURL.Hostname())
	if err := service.ResetPasswordFromFile(ctx, *passwordFile, *disableTOTP); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{"status": "ok", "totp_disabled": *disableTOTP, "sessions_revoked": true})
}

func loadCipher(path string) (*cryptox.Cipher, []byte, error) {
	key, err := cryptox.LoadMasterKey(path)
	if err != nil {
		return nil, nil, err
	}
	cipher, err := cryptox.NewCipher(key)
	return cipher, key, err
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(output)
	return set
}

func extractDev(arguments []string) ([]string, bool) {
	result := make([]string, 0, len(arguments))
	dev := false
	for _, argument := range arguments {
		if argument == "--dev" {
			dev = true
			continue
		}
		result = append(result, argument)
	}
	return result, dev
}

func stringsHasFlagPrefix(value string) bool {
	return len(value) > 0 && value[0] == '-'
}

func containsHelpFlag(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	return false
}

func printUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, `Sidervia - self-hosted AI gateway control plane

Usage:
  sidervia [--dev] serve
  sidervia [--dev] doctor [--healthcheck]
  sidervia backup create --output PATH
  sidervia backup verify --input PATH
  sidervia key rotate --new-key-file PATH --backup PATH
  sidervia admin reset-password --password-file PATH [--disable-totp]
  sidervia version`)
}
