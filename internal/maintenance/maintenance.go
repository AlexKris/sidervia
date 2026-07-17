package maintenance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
	"modernc.org/sqlite"
)

type DoctorReport struct {
	Status              string `json:"status"`
	DatabasePath        string `json:"database_path"`
	SchemaVersion       int    `json:"schema_version"`
	LatestSchemaVersion int    `json:"latest_schema_version"`
	JournalMode         string `json:"journal_mode"`
	MasterKeyID         string `json:"master_key_id"`
	AdminConfigured     bool   `json:"admin_configured"`
}

type BackupReport struct {
	Path          string `json:"path"`
	ChecksumPath  string `json:"checksum_path"`
	SHA256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	SchemaVersion int    `json:"schema_version"`
	EncryptedRows int    `json:"encrypted_rows_verified"`
}

type RotationReport struct {
	OldKeyID     string `json:"old_key_id"`
	NewKeyID     string `json:"new_key_id"`
	RowsRotated  int    `json:"rows_rotated"`
	SessionsGone int64  `json:"sessions_revoked"`
}

func Doctor(ctx context.Context, dataDir string, cipher *cryptox.Cipher) (DoctorReport, error) {
	database, err := store.OpenReadOnly(ctx, dataDir)
	if err != nil {
		return DoctorReport{}, err
	}
	defer database.Close()
	if err := database.VerifySentinel(ctx, cipher); err != nil {
		return DoctorReport{}, err
	}
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	if version != store.LatestSchemaVersion {
		return DoctorReport{}, fmt.Errorf("database schema is %d; this binary requires %d", version, store.LatestSchemaVersion)
	}
	var journalMode string
	if err := database.DB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return DoctorReport{}, fmt.Errorf("read journal mode: %w", err)
	}
	var adminConfigured bool
	if err := database.DB().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM admin_user WHERE id = 1)").Scan(&adminConfigured); err != nil {
		return DoctorReport{}, fmt.Errorf("inspect administrator: %w", err)
	}
	return DoctorReport{
		Status: "ok", DatabasePath: database.Path(), SchemaVersion: version,
		LatestSchemaVersion: store.LatestSchemaVersion, JournalMode: journalMode,
		MasterKeyID: cipher.KeyID(), AdminConfigured: adminConfigured,
	}, nil
}

func CreateBackup(ctx context.Context, source *store.Store, cipher *cryptox.Cipher, output string) (BackupReport, error) {
	if err := source.VerifySentinel(ctx, cipher); err != nil {
		return BackupReport{}, err
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return BackupReport{}, fmt.Errorf("resolve backup output: %w", err)
	}
	if _, err := os.Lstat(output); err == nil {
		return BackupReport{}, errors.New("backup output already exists")
	} else if !os.IsNotExist(err) {
		return BackupReport{}, fmt.Errorf("inspect backup output: %w", err)
	}
	parent := filepath.Dir(output)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return BackupReport{}, errors.New("backup output parent must be an existing directory, not a symlink")
	}
	temporary, err := os.CreateTemp(parent, ".sidervia-backup-*.tmp")
	if err != nil {
		return BackupReport{}, fmt.Errorf("create backup temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return BackupReport{}, err
	}
	defer os.Remove(temporaryPath)
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return BackupReport{}, fmt.Errorf("secure backup permissions: %w", err)
	}
	if err := onlineBackup(ctx, source.DB(), temporaryPath); err != nil {
		return BackupReport{}, err
	}
	if err := syncFile(temporaryPath); err != nil {
		return BackupReport{}, err
	}
	verified, err := verifyDatabase(ctx, temporaryPath, cipher)
	if err != nil {
		return BackupReport{}, fmt.Errorf("verify created backup: %w", err)
	}
	digest, size, err := fileSHA256(temporaryPath)
	if err != nil {
		return BackupReport{}, err
	}
	if err := os.Link(temporaryPath, output); err != nil {
		return BackupReport{}, fmt.Errorf("publish backup without overwrite: %w", err)
	}
	checksumPath := output + ".sha256"
	if err := writeChecksum(checksumPath, digest, filepath.Base(output)); err != nil {
		_ = os.Remove(output)
		return BackupReport{}, err
	}
	return BackupReport{
		Path: output, ChecksumPath: checksumPath, SHA256: digest, Bytes: size,
		SchemaVersion: verified.SchemaVersion, EncryptedRows: verified.EncryptedRows,
	}, nil
}

func VerifyBackup(ctx context.Context, input string, cipher *cryptox.Cipher) (BackupReport, error) {
	input, err := filepath.Abs(input)
	if err != nil {
		return BackupReport{}, fmt.Errorf("resolve backup input: %w", err)
	}
	digest, size, err := fileSHA256(input)
	if err != nil {
		return BackupReport{}, err
	}
	checksumPath := input + ".sha256"
	expected, err := readChecksum(checksumPath, filepath.Base(input))
	if err != nil {
		return BackupReport{}, err
	}
	if !bytes.Equal([]byte(digest), []byte(expected)) {
		return BackupReport{}, errors.New("backup SHA-256 checksum does not match")
	}
	verified, err := verifyDatabase(ctx, input, cipher)
	if err != nil {
		return BackupReport{}, err
	}
	return BackupReport{
		Path: input, ChecksumPath: checksumPath, SHA256: digest, Bytes: size,
		SchemaVersion: verified.SchemaVersion, EncryptedRows: verified.EncryptedRows,
	}, nil
}

func RotateKey(ctx context.Context, database *store.Store, oldCipher, newCipher *cryptox.Cipher) (RotationReport, error) {
	if oldCipher.KeyID() == newCipher.KeyID() {
		return RotationReport{}, errors.New("new master key must differ from current master key")
	}
	if err := database.VerifySentinel(ctx, oldCipher); err != nil {
		return RotationReport{}, err
	}
	tx, err := database.DB().BeginTx(ctx, nil)
	if err != nil {
		return RotationReport{}, err
	}
	defer tx.Rollback()
	columns := []encryptedColumn{
		{Table: "admin_user", IDColumn: "id", Column: "totp_secret_enc"},
		{Table: "admin_user", IDColumn: "id", Column: "totp_pending_secret_enc"},
		{Table: "admin_sessions", IDColumn: "public_id", Column: "csrf_token_enc"},
		{Table: "egress_proxies", IDColumn: "public_id", Column: "username_enc"},
		{Table: "egress_proxies", IDColumn: "public_id", Column: "password_enc"},
		{Table: "accounts", IDColumn: "public_id", Column: "credential_enc"},
		{Table: "crypto_sentinel", IDColumn: "id", Column: "ciphertext"},
	}
	rotated := 0
	for _, column := range columns {
		count, err := rotateColumn(ctx, tx, oldCipher, newCipher, column)
		if err != nil {
			return RotationReport{}, err
		}
		rotated += count
	}
	result, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE revoked_at_ms IS NULL", time.Now().UTC().UnixMilli())
	if err != nil {
		return RotationReport{}, fmt.Errorf("revoke sessions during key rotation: %w", err)
	}
	revoked, _ := result.RowsAffected()
	if _, err := tx.ExecContext(ctx, "UPDATE crypto_sentinel SET key_id = ? WHERE id = 1", newCipher.KeyID()); err != nil {
		return RotationReport{}, fmt.Errorf("update crypto sentinel key identifier: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"schema_version": 1, "old_key_id": oldCipher.KeyID(), "new_key_id": newCipher.KeyID(), "rows_rotated": rotated})
	auditID, err := identifier.NewGenerator().Object("audit")
	if err != nil {
		return RotationReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(public_id, event_type, actor_kind, target_kind,
        target_id, outcome, metadata_json, created_at_ms) VALUES(?, 'system.master_key_rotated', 'local_admin',
        'system', 'crypto', 'success', ?, ?)`, auditID, string(metadata), time.Now().UTC().UnixMilli()); err != nil {
		return RotationReport{}, fmt.Errorf("record key rotation audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RotationReport{}, err
	}
	if err := database.VerifySentinel(ctx, newCipher); err != nil {
		return RotationReport{}, fmt.Errorf("verify rotated master key: %w", err)
	}
	return RotationReport{OldKeyID: oldCipher.KeyID(), NewKeyID: newCipher.KeyID(), RowsRotated: rotated, SessionsGone: revoked}, nil
}

type backupConnection interface {
	NewBackup(string) (*sqlite.Backup, error)
}

func onlineBackup(ctx context.Context, database *sql.DB, destination string) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire SQLite backup connection: %w", err)
	}
	defer connection.Close()
	return connection.Raw(func(driverConnection any) error {
		backuper, ok := driverConnection.(backupConnection)
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		backup, err := backuper.NewBackup(destination)
		if err != nil {
			return fmt.Errorf("start SQLite backup: %w", err)
		}
		_, stepErr := backup.Step(-1)
		finishErr := backup.Finish()
		if stepErr != nil || finishErr != nil {
			return fmt.Errorf("complete SQLite backup: %w", errors.Join(stepErr, finishErr))
		}
		return nil
	})
}

type verifyResult struct {
	SchemaVersion int
	EncryptedRows int
}

func verifyDatabase(ctx context.Context, path string, cipher *cryptox.Cipher) (verifyResult, error) {
	database, err := store.OpenReadOnlyFile(ctx, path)
	if err != nil {
		return verifyResult{}, err
	}
	defer database.Close()
	if err := database.VerifySentinel(ctx, cipher); err != nil {
		return verifyResult{}, err
	}
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return verifyResult{}, err
	}
	if version > store.LatestSchemaVersion || version < 1 {
		return verifyResult{}, fmt.Errorf("unsupported backup schema version %d", version)
	}
	columns := []encryptedColumn{
		{Table: "admin_user", IDColumn: "id", Column: "totp_secret_enc"},
		{Table: "admin_user", IDColumn: "id", Column: "totp_pending_secret_enc"},
		{Table: "admin_sessions", IDColumn: "public_id", Column: "csrf_token_enc"},
		{Table: "egress_proxies", IDColumn: "public_id", Column: "username_enc"},
		{Table: "egress_proxies", IDColumn: "public_id", Column: "password_enc"},
		{Table: "accounts", IDColumn: "public_id", Column: "credential_enc"},
		{Table: "crypto_sentinel", IDColumn: "id", Column: "ciphertext"},
	}
	count := 0
	for _, column := range columns {
		verified, err := verifyColumn(ctx, database.DB(), cipher, column)
		if err != nil {
			return verifyResult{}, err
		}
		count += verified
	}
	return verifyResult{SchemaVersion: version, EncryptedRows: count}, nil
}

type encryptedColumn struct {
	Table    string
	IDColumn string
	Column   string
}

type encryptedRow struct {
	ID    string
	Value []byte
}

func loadEncryptedRows(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, column encryptedColumn) ([]encryptedRow, error) {
	query := "SELECT CAST(" + column.IDColumn + " AS TEXT), " + column.Column + " FROM " + column.Table + " WHERE " + column.Column + " IS NOT NULL"
	rows, err := queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read encrypted %s.%s: %w", column.Table, column.Column, err)
	}
	defer rows.Close()
	result := make([]encryptedRow, 0)
	for rows.Next() {
		var row encryptedRow
		if err := rows.Scan(&row.ID, &row.Value); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func verifyColumn(ctx context.Context, database *sql.DB, cipher *cryptox.Cipher, column encryptedColumn) (int, error) {
	rows, err := loadEncryptedRows(ctx, database, column)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		plain, err := cipher.Open(row.Value, cryptox.AAD(column.Table, row.ID, column.Column))
		if err != nil {
			return 0, fmt.Errorf("decrypt %s.%s row %s: %w", column.Table, column.Column, row.ID, err)
		}
		clearBytes(plain)
	}
	return len(rows), nil
}

func rotateColumn(ctx context.Context, tx *sql.Tx, oldCipher, newCipher *cryptox.Cipher, column encryptedColumn) (int, error) {
	rows, err := loadEncryptedRows(ctx, tx, column)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		aad := cryptox.AAD(column.Table, row.ID, column.Column)
		plain, err := oldCipher.Open(row.Value, aad)
		if err != nil {
			return 0, fmt.Errorf("decrypt %s.%s row %s: %w", column.Table, column.Column, row.ID, err)
		}
		sealed, err := newCipher.Seal(plain, aad)
		clearBytes(plain)
		if err != nil {
			return 0, err
		}
		query := "UPDATE " + column.Table + " SET " + column.Column + " = ? WHERE CAST(" + column.IDColumn + " AS TEXT) = ?"
		result, err := tx.ExecContext(ctx, query, sealed, row.ID)
		if err != nil {
			return 0, fmt.Errorf("update %s.%s row %s: %w", column.Table, column.Column, row.ID, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return 0, fmt.Errorf("update %s.%s row %s affected %d rows", column.Table, column.Column, row.ID, affected)
		}
	}
	return len(rows), nil
}

func fileSHA256(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, fmt.Errorf("inspect backup file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, errors.New("backup must be a regular file, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func writeChecksum(path, digest, name string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup checksum: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%s  %s\n", digest, name); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func readChecksum(path, expectedName string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("read backup checksum: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("backup checksum must be a regular file, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 1024))
	if !scanner.Scan() {
		return "", errors.New("backup checksum file is empty")
	}
	parts := strings.Fields(scanner.Text())
	if len(parts) != 2 || parts[1] != expectedName || len(parts[0]) != sha256.Size*2 {
		return "", errors.New("backup checksum file has an invalid format")
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", errors.New("backup checksum is not valid hexadecimal")
	}
	if scanner.Scan() || scanner.Err() != nil {
		return "", errors.New("backup checksum file must contain exactly one line")
	}
	return strings.ToLower(parts[0]), nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
