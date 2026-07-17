package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := PrepareDataDir(dataDir); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "sidervia.db")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: strings.Join([]string{
		"_txlock=immediate",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
	}, "&")}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	s := &Store{db: db, path: path}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := s.quickCheck(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure database permissions: %w", err)
	}
	return s, nil
}

func OpenReadOnly(ctx context.Context, dataDir string) (*Store, error) {
	info, err := os.Lstat(dataDir)
	if err != nil {
		return nil, fmt.Errorf("inspect data directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("data directory must be a directory, not a symlink")
	}
	return OpenReadOnlyFile(ctx, filepath.Join(dataDir, "sidervia.db"))
}

func OpenReadOnlyFile(ctx context.Context, path string) (*Store, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect sqlite database: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("sqlite database must be a regular file, not a symlink")
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: strings.Join([]string{
		"mode=ro",
		"_pragma=foreign_keys(1)",
		"_pragma=query_only(1)",
		"_pragma=busy_timeout(5000)",
	}, "&")}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite read-only: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: path}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite read-only: %w", err)
	}
	if err := s.quickCheck(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	return s.db.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return errors.New("unexpected sqlite readiness result")
	}
	return nil
}

const LatestSchemaVersion = 1

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func (s *Store) quickCheck(ctx context.Context) error {
	var result string
	if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("sqlite quick_check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("sqlite quick_check failed: %s", result)
	}
	return nil
}

func PrepareDataDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	entry, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
		return errors.New("data directory must be a directory, not a symlink")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure data directory permissions: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("data directory path is not a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("data directory must not be accessible by group or other")
	}
	return nil
}
