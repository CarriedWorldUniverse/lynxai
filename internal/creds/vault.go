package creds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// ErrCredentialNotFound is returned when Get/Delete don't find the credential.
var ErrCredentialNotFound = errors.New("creds: credential not found")

// Credential is the decrypted result of Get. Bundle is plaintext JSON.
type Credential struct {
	Name      string
	Kind      Kind
	Host      string
	Bundle    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CredentialSummary is what List returns — no bundle data.
type CredentialSummary struct {
	Name      string    `json:"name"`
	Kind      Kind      `json:"kind"`
	Host      string    `json:"host"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Vault is the encrypted credential store.
type Vault struct {
	db  *sql.DB
	key []byte // derived data key (HKDF of master)
}

// OpenVault opens (or creates) the SQLite DB at path, applies the schema, and
// returns a Vault keyed by master (the bytes from LoadOrCreateMasterKey).
func OpenVault(path string, master []byte) (*Vault, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Vault{db: db, key: deriveKey(master)}, nil
}

func (v *Vault) Close() error { return v.db.Close() }

// Put stores or replaces a credential. Validates the bundle against kind.
func (v *Vault) Put(ctx context.Context, name string, kind Kind, host string, bundle []byte) error {
	if err := ValidateBundle(kind, bundle); err != nil {
		return err
	}
	ct, nonce, err := encrypt(v.key, bundle)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	now := time.Now().Unix()
	_, err = v.db.ExecContext(ctx, `
		INSERT INTO credentials (name, kind, host, bundle, nonce, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			kind=excluded.kind,
			host=excluded.host,
			bundle=excluded.bundle,
			nonce=excluded.nonce,
			updated_at=excluded.updated_at
	`, name, string(kind), host, ct, nonce, now, now)
	if err != nil {
		return fmt.Errorf("insert credential: %w", err)
	}
	return nil
}

// Get retrieves and decrypts a credential by name.
func (v *Vault) Get(ctx context.Context, name string) (*Credential, error) {
	var (
		kindStr   string
		host      string
		ct, nonce []byte
		createdAt int64
		updatedAt int64
	)
	row := v.db.QueryRowContext(ctx, `
		SELECT kind, host, bundle, nonce, created_at, updated_at
		FROM credentials WHERE name = ?`, name)
	err := row.Scan(&kindStr, &host, &ct, &nonce, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan credential: %w", err)
	}
	pt, err := decrypt(v.key, nonce, ct)
	if err != nil {
		return nil, err
	}
	return &Credential{
		Name:      name,
		Kind:      Kind(kindStr),
		Host:      host,
		Bundle:    pt,
		CreatedAt: time.Unix(createdAt, 0),
		UpdatedAt: time.Unix(updatedAt, 0),
	}, nil
}

// List returns all credential summaries (no bundles).
func (v *Vault) List(ctx context.Context) ([]CredentialSummary, error) {
	rows, err := v.db.QueryContext(ctx, `
		SELECT name, kind, host, created_at, updated_at
		FROM credentials ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []CredentialSummary
	for rows.Next() {
		var s CredentialSummary
		var kindStr string
		var createdAt, updatedAt int64
		if err := rows.Scan(&s.Name, &kindStr, &s.Host, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		s.Kind = Kind(kindStr)
		s.CreatedAt = time.Unix(createdAt, 0)
		s.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete removes a credential by name. Returns ErrCredentialNotFound if absent.
func (v *Vault) Delete(ctx context.Context, name string) error {
	res, err := v.db.ExecContext(ctx, `DELETE FROM credentials WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// RecordUse writes one audit row. requestURL is the URL being fetched, NOT any
// part of the credential bundle. outcome is one of "ok", "not_found",
// "decrypt_failed", "apply_failed".
func (v *Vault) RecordUse(ctx context.Context, name, requestURL, outcome string) error {
	_, err := v.db.ExecContext(ctx, `
		INSERT INTO credential_audit (name, used_at, request_url, outcome)
		VALUES (?, ?, ?, ?)`,
		name, time.Now().Unix(), requestURL, outcome)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}
