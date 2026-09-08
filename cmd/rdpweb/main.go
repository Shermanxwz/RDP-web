package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Shermanxwz/RDP-web/internal/app"
	"github.com/Shermanxwz/RDP-web/internal/security"
	"github.com/Shermanxwz/RDP-web/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "reset-password":
			if err := resetPasswordCommand(os.Args[2:], os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "reset-password:", err)
				os.Exit(1)
			}
			return
		case "help", "--help", "-h":
			printUsage(os.Stdout)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			printUsage(os.Stderr)
			os.Exit(2)
		}
	}
	serve()
}

func serve() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := env("RDPWEB_ADDR", ":8080")
	dataDir := env("RDPWEB_DATA_DIR", "./data")
	publicURL := strings.TrimRight(os.Getenv("RDPWEB_PUBLIC_URL"), "/")
	ttl := durationEnv("RDPWEB_SESSION_TTL", 30*24*time.Hour)
	secureCookie := boolEnv("RDPWEB_SECURE_COOKIE", strings.HasPrefix(strings.ToLower(publicURL), "https://"))

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(filepath.Join(dataDir, "rdpweb.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	setupToken := strings.TrimSpace(os.Getenv("RDPWEB_SETUP_TOKEN"))
	count, err := db.UserCount(context.Background())
	if err != nil {
		logger.Error("read setup state", "error", err)
		os.Exit(1)
	}
	if count == 0 && setupToken == "" {
		setupToken, err = security.RandomToken(18)
		if err != nil {
			logger.Error("generate setup token", "error", err)
			os.Exit(1)
		}
		logger.Warn("first-run setup token generated; append it as ?setup=TOKEN when opening RDP Web", "setup_token", setupToken)
	}

	handler, err := app.New(app.Config{
		Store:        db,
		Logger:       logger,
		PublicURL:    publicURL,
		SecureCookie: secureCookie,
		SessionTTL:   ttl,
		SetupToken:   setupToken,
	})
	if err != nil {
		logger.Error("initialize application", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("rdp-web listening", "addr", addr, "secure_cookie", secureCookie)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}

func resetPasswordCommand(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 || args[0] != "--password-stdin" {
		return errors.New("usage: rdpweb reset-password --password-stdin")
	}
	dataDir := env("RDPWEB_DATA_DIR", "./data")
	dbPath := filepath.Join(dataDir, "rdpweb.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("database not found at %s", dbPath)
		}
		return err
	}

	passwordBytes, err := io.ReadAll(io.LimitReader(in, 2049))
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimRight(string(passwordBytes), "\r\n")
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	owner, err := db.Owner(context.Background())
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("owner account does not exist; complete first-run setup instead")
	}
	if err != nil {
		return fmt.Errorf("read owner: %w", err)
	}
	if err := db.ReplaceOwnerPassword(context.Background(), owner.ID, hash); err != nil {
		return fmt.Errorf("replace password: %w", err)
	}
	fmt.Fprintf(out, "owner password reset for %s; all existing web sessions revoked\n", owner.Username)
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "RDP Web")
	fmt.Fprintln(w, "  rdpweb                         start the web service")
	fmt.Fprintln(w, "  rdpweb reset-password --password-stdin")
	fmt.Fprintln(w, "                                 reset the unique owner password from stdin")
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" || strings.EqualFold(value, "auto") {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Hour {
		return fallback
	}
	return parsed
}
