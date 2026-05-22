# lynxai v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the v1 of `lynxai` — a self-hostable OSS AI-native headless browser with two REST endpoints (`/fetch`, `/extract`) and an encrypted text-based credential vault, packaged as a single Go binary plus a Docker image.

**Architecture:** Single Go static binary. HTTP API (chi) wires three internal packages: `engine` (chromedp + html→markdown), `creds` (encrypted SQLite vault with audit), `extract` (bridle turn whose tool input schema is the caller's JSON Schema). bridle is an external dependency that owns LLM provider abstraction; default config synthesizes DeepSeek via bridle's openai-api provider.

**Tech Stack:**
- Go 1.25+ (matches bridle/nexus)
- `github.com/chromedp/chromedp` — CDP-direct Chromium driver
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/ncruces/go-sqlite3` — pure-Go SQLite (no CGo)
- `github.com/JohannesKaufmann/html-to-markdown/v2` — HTML→markdown
- `github.com/santhosh-tekuri/jsonschema/v6` — JSON Schema validation
- `golang.org/x/crypto/hkdf` — key derivation
- `github.com/CarriedWorldUniverse/bridle` — LLM harness (extract only)
- `github.com/CarriedWorldUniverse/bridle/provider/openai` — used with `option.WithBaseURL` for DeepSeek default
- stdlib `testing` (no testify — matches sibling project convention)

**Spec:** `docs/superpowers/specs/2026-05-22-lynxai-v1-design.md`

**License:** AGPL-3.0-or-later

**Working directory:** `~/Source/lynxai` (git-initialized, no Go code yet)

---

## Task 1: Project skeleton

**Files:**
- Create: `LICENSE` (AGPL-3.0 full text)
- Create: `README.md` (minimal stub — fleshed out in Task 20)
- Create: `.gitignore`
- Create: `go.mod`
- Create: `cmd/lynxai/main.go` (skeleton; real impl in Task 18)

- [ ] **Step 1: Write `.gitignore`**

```gitignore
# Binaries
/lynxai
/lynxai.exe
/dist/

# Local dev
*.db
*.db-journal
master.key
.lynxai/

# Editor
.idea/
.vscode/
.DS_Store
```

- [ ] **Step 2: Write `LICENSE` (AGPL-3.0)**

Download the canonical AGPL-3.0 text:

```bash
curl -fsSL https://www.gnu.org/licenses/agpl-3.0.txt -o LICENSE
head -5 LICENSE   # should show "GNU AFFERO GENERAL PUBLIC LICENSE / Version 3"
```

- [ ] **Step 3: Write `README.md` stub**

```markdown
# lynxai

Self-hostable, AI-native headless browser. Free alternative to Browserbase.

**Status:** pre-alpha. v1 build in progress.

See `docs/superpowers/specs/2026-05-22-lynxai-v1-design.md` for the design.

## License

AGPL-3.0-or-later. See `LICENSE`.
```

- [ ] **Step 4: Initialize Go module**

```bash
go mod init github.com/CarriedWorldUniverse/lynxai
```

Then verify `go.mod` starts with:
```
module github.com/CarriedWorldUniverse/lynxai
```

- [ ] **Step 5: Write `cmd/lynxai/main.go` skeleton**

```go
// Command lynxai is the self-hostable AI-native headless browser server.
//
// v1 exposes:
//   lynxai serve [--addr :7878] [--data-dir ~/.lynxai] [--bridle-config <path>]
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		fmt.Println("serve: not yet implemented")
		os.Exit(1)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: lynxai <subcommand> [flags]

Subcommands:
  serve   Run the lynxai HTTP server
  help    Show this message`)
}
```

- [ ] **Step 6: Verify it builds and runs**

```bash
go build ./...
./lynxai help
```

Expected: usage text, exit code 0.

- [ ] **Step 7: Commit**

```bash
git add LICENSE README.md .gitignore go.mod go.sum cmd/
git commit -m "feat: project skeleton, AGPL-3.0 license, CLI stub"
```

---

## Task 2: Credential crypto (AES-256-GCM + HKDF)

**Files:**
- Create: `internal/creds/crypto.go`
- Test: `internal/creds/crypto_test.go`

- [ ] **Step 1: Write failing test for key derivation determinism**

`internal/creds/crypto_test.go`:
```go
package creds

import (
	"bytes"
	"testing"
)

func TestDeriveKey_Deterministic(t *testing.T) {
	master := []byte("test-master-key-32-bytes-padded!!")
	k1 := deriveKey(master)
	k2 := deriveKey(master)
	if !bytes.Equal(k1, k2) {
		t.Fatalf("deriveKey not deterministic: %x vs %x", k1, k2)
	}
	if len(k1) != 32 {
		t.Fatalf("want 32-byte key, got %d", len(k1))
	}
}

func TestDeriveKey_DifferentMastersDifferentKeys(t *testing.T) {
	a := deriveKey([]byte("master-a-padded-to-32-bytes-..!!"))
	b := deriveKey([]byte("master-b-padded-to-32-bytes-..!!"))
	if bytes.Equal(a, b) {
		t.Fatal("different masters produced same key")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/creds/...
```
Expected: `undefined: deriveKey`.

- [ ] **Step 3: Implement key derivation**

`internal/creds/crypto.go`:
```go
// Package creds implements lynxai's encrypted credential vault and audit log.
//
// Encryption: AES-256-GCM with per-row nonce. Data key derived via HKDF-SHA256
// from a master key stored at <data_dir>/master.key (0600 perms). HKDF info
// string is fixed at "lynxai.credentials.v1" — bumping it forces re-keying.
package creds

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	hkdfInfo  = "lynxai.credentials.v1"
	nonceLen  = 12
	keyLen    = 32 // AES-256
)

// deriveKey returns a 32-byte data key derived from master via HKDF-SHA256.
func deriveKey(master []byte) []byte {
	r := hkdf.New(sha256.New, master, nil, []byte(hkdfInfo))
	out := make([]byte, keyLen)
	if _, err := io.ReadFull(r, out); err != nil {
		panic(fmt.Errorf("hkdf read: %w", err)) // cannot happen with hkdf.New
	}
	return out
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go mod tidy
go test ./internal/creds/...
```
Expected: PASS.

- [ ] **Step 5: Write failing test for encrypt/decrypt round-trip**

Append to `crypto_test.go`:
```go
func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	plaintext := []byte(`{"host":"example.com","token":"abc"}`)

	ct, nonce, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(nonce) != nonceLen {
		t.Fatalf("nonce len = %d, want %d", len(nonce), nonceLen)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}

	pt, err := decrypt(key, nonce, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("round-trip mismatch: %s vs %s", pt, plaintext)
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	good := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	bad := deriveKey([]byte("master-key-32-bytes-padded-OTHR!"))
	ct, nonce, err := encrypt(good, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decrypt(bad, nonce, ct); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestEncrypt_NoncesAreUnique(t *testing.T) {
	key := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		_, nonce, err := encrypt(key, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		s := string(nonce)
		if seen[s] {
			t.Fatal("duplicate nonce produced")
		}
		seen[s] = true
	}
}
```

- [ ] **Step 6: Run — expect fail**

```bash
go test ./internal/creds/...
```
Expected: `undefined: encrypt`, `undefined: decrypt`.

- [ ] **Step 7: Implement encrypt/decrypt**

Append to `crypto.go`:
```go
// encrypt returns ciphertext + random nonce for plaintext, sealed with key.
func encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm new: %w", err)
	}
	nonce = make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce read: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// ErrDecryptFailed indicates the ciphertext won't open with the given key/nonce.
var ErrDecryptFailed = errors.New("creds: decryption failed")

// decrypt opens ciphertext sealed by encrypt. Returns ErrDecryptFailed on any
// authentication failure (wrong key, tampered ciphertext, wrong nonce).
func decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return pt, nil
}
```

- [ ] **Step 8: Run — all crypto tests pass**

```bash
go mod tidy
go test ./internal/creds/...
```
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/creds/ go.mod go.sum
git commit -m "feat(creds): AES-256-GCM crypto with HKDF key derivation"
```

---

## Task 3: Master key loading

**Files:**
- Create: `internal/creds/masterkey.go`
- Test: `internal/creds/masterkey_test.go`

- [ ] **Step 1: Write failing test for first-run key generation**

`internal/creds/masterkey_test.go`:
```go
package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateMasterKey_CreatesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")

	k1, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("want 32-byte key, got %d", len(k1))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %o, want 0600", info.Mode().Perm())
	}

	k2, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(k1) != string(k2) {
		t.Fatal("second load returned different key")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/creds/... -run MasterKey
```
Expected: `undefined: LoadOrCreateMasterKey`.

- [ ] **Step 3: Implement**

`internal/creds/masterkey.go`:
```go
package creds

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

const masterKeyLen = 32

// LoadOrCreateMasterKey reads a 32-byte master key from path, creating it
// (with 0600 perms) if absent. Returns the raw key bytes.
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != masterKeyLen {
			return nil, fmt.Errorf("master key at %s has wrong length %d (want %d)", path, len(data), masterKeyLen)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key := make([]byte, masterKeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return key, nil
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go test ./internal/creds/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/creds/masterkey.go internal/creds/masterkey_test.go
git commit -m "feat(creds): master-key load/create with 0600 perms"
```

---

## Task 4: Credential bundle types and validators

**Files:**
- Create: `internal/creds/bundles.go`
- Test: `internal/creds/bundles_test.go`

- [ ] **Step 1: Write failing tests for all four bundle kinds**

`internal/creds/bundles_test.go`:
```go
package creds

import (
	"strings"
	"testing"
)

func TestValidateBundle_Basic(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr string // substring; empty = no error expected
	}{
		{"ok", `{"host":"api.example.com","user":"alice","password":"pw"}`, ""},
		{"missing host", `{"user":"alice","password":"pw"}`, "host"},
		{"missing user", `{"host":"x","password":"pw"}`, "user"},
		{"missing password", `{"host":"x","user":"alice"}`, "password"},
		{"junk json", `not json`, "parse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateBundle(KindBasic, []byte(c.json))
			checkErr(t, err, c.wantErr)
		})
	}
}

func TestValidateBundle_Bearer(t *testing.T) {
	cases := []struct {
		name, json, wantErr string
	}{
		{"ok", `{"host":"api.example.com","token":"abc"}`, ""},
		{"missing host", `{"token":"abc"}`, "host"},
		{"missing token", `{"host":"x"}`, "token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkErr(t, ValidateBundle(KindBearer, []byte(c.json)), c.wantErr)
		})
	}
}

func TestValidateBundle_Cookies(t *testing.T) {
	ok := `{"host":"example.com","cookies":[{"name":"sid","value":"v","domain":".example.com","path":"/","secure":true,"http_only":true}]}`
	cases := []struct {
		name, json, wantErr string
	}{
		{"ok", ok, ""},
		{"empty cookies", `{"host":"x","cookies":[]}`, "at least one"},
		{"cookie missing name", `{"host":"x","cookies":[{"value":"v"}]}`, "name"},
		{"cookie missing value", `{"host":"x","cookies":[{"name":"n"}]}`, "value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkErr(t, ValidateBundle(KindCookies, []byte(c.json)), c.wantErr)
		})
	}
}

func TestValidateBundle_Form(t *testing.T) {
	ok := `{"host":"x","login_url":"https://x/login","method":"POST","fields":{"user_field":"u","pass_field":"p","user":"alice","password":"pw"},"success_cookie_name":"sid"}`
	cases := []struct {
		name, json, wantErr string
	}{
		{"ok", ok, ""},
		{"missing login_url", `{"host":"x","method":"POST","fields":{"user_field":"u","pass_field":"p","user":"a","password":"b"},"success_cookie_name":"sid"}`, "login_url"},
		{"missing success_cookie_name", `{"host":"x","login_url":"https://x/l","method":"POST","fields":{"user_field":"u","pass_field":"p","user":"a","password":"b"}}`, "success_cookie_name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkErr(t, ValidateBundle(KindForm, []byte(c.json)), c.wantErr)
		})
	}
}

func TestValidateBundle_UnknownKind(t *testing.T) {
	err := ValidateBundle("nope", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want unknown-kind error, got %v", err)
	}
}

func checkErr(t *testing.T, err error, wantSubstr string) {
	t.Helper()
	if wantSubstr == "" {
		if err != nil {
			t.Fatalf("want no error, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("want error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/creds/... -run Bundle
```
Expected: many `undefined` errors.

- [ ] **Step 3: Implement**

`internal/creds/bundles.go`:
```go
package creds

import (
	"encoding/json"
	"fmt"
)

// Kind is the credential class. The bundle JSON shape is per-kind.
type Kind string

const (
	KindBasic   Kind = "basic"
	KindBearer  Kind = "bearer"
	KindCookies Kind = "cookies"
	KindForm    Kind = "form"
)

func KnownKinds() []Kind {
	return []Kind{KindBasic, KindBearer, KindCookies, KindForm}
}

// BasicBundle: HTTP Basic auth.
type BasicBundle struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// BearerBundle: HTTP Bearer / Authorization header token.
type BearerBundle struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

// Cookie: one entry in a cookie jar credential.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

// CookiesBundle: pre-baked cookie jar applied before navigation.
type CookiesBundle struct {
	Host    string   `json:"host"`
	Cookies []Cookie `json:"cookies"`
}

// FormFields: the input names and values for a form-login POST.
type FormFields struct {
	UserField string `json:"user_field"`
	PassField string `json:"pass_field"`
	User      string `json:"user"`
	Password  string `json:"password"`
}

// FormBundle: form-login credentials. lynxai POSTs to LoginURL once, captures
// SuccessCookieName, and seeds it into the browser context before navigation.
type FormBundle struct {
	Host              string     `json:"host"`
	LoginURL          string     `json:"login_url"`
	Method            string     `json:"method"`
	Fields            FormFields `json:"fields"`
	SuccessCookieName string     `json:"success_cookie_name"`
}

// ValidateBundle verifies that data is a well-formed JSON bundle for kind.
// Returns nil on success, descriptive error otherwise.
func ValidateBundle(kind Kind, data []byte) error {
	switch kind {
	case KindBasic:
		var b BasicBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse basic bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("basic bundle: host required")
		}
		if b.User == "" {
			return fmt.Errorf("basic bundle: user required")
		}
		if b.Password == "" {
			return fmt.Errorf("basic bundle: password required")
		}
		return nil
	case KindBearer:
		var b BearerBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse bearer bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("bearer bundle: host required")
		}
		if b.Token == "" {
			return fmt.Errorf("bearer bundle: token required")
		}
		return nil
	case KindCookies:
		var b CookiesBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse cookies bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("cookies bundle: host required")
		}
		if len(b.Cookies) == 0 {
			return fmt.Errorf("cookies bundle: at least one cookie required")
		}
		for i, c := range b.Cookies {
			if c.Name == "" {
				return fmt.Errorf("cookies bundle: cookie[%d] name required", i)
			}
			if c.Value == "" {
				return fmt.Errorf("cookies bundle: cookie[%d] value required", i)
			}
		}
		return nil
	case KindForm:
		var b FormBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse form bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("form bundle: host required")
		}
		if b.LoginURL == "" {
			return fmt.Errorf("form bundle: login_url required")
		}
		if b.SuccessCookieName == "" {
			return fmt.Errorf("form bundle: success_cookie_name required")
		}
		if b.Fields.UserField == "" || b.Fields.PassField == "" {
			return fmt.Errorf("form bundle: fields.user_field and fields.pass_field required")
		}
		return nil
	default:
		return fmt.Errorf("unknown credential kind: %q (known: %v)", kind, KnownKinds())
	}
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go test ./internal/creds/...
```
Expected: PASS for all bundle tests.

- [ ] **Step 5: Commit**

```bash
git add internal/creds/bundles.go internal/creds/bundles_test.go
git commit -m "feat(creds): bundle types and per-kind validators"
```

---

## Task 5: SQLite vault (Put/Get/Delete/List)

**Files:**
- Create: `internal/creds/vault.go`
- Create: `internal/creds/schema.go`
- Test: `internal/creds/vault_test.go`

- [ ] **Step 1: Write the schema file**

`internal/creds/schema.go`:
```go
package creds

// schemaSQL is applied on Open to create tables if they don't exist.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS credentials (
  name        TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  host        TEXT NOT NULL,
  bundle      BLOB NOT NULL,
  nonce       BLOB NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS credential_audit (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  used_at     INTEGER NOT NULL,
  request_url TEXT NOT NULL,
  outcome     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_name ON credential_audit(name);
`
```

- [ ] **Step 2: Write failing test for vault Put/Get round-trip**

`internal/creds/vault_test.go`:
```go
package creds

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	dir := t.TempDir()
	key, err := LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := OpenVault(filepath.Join(dir, "lynxai.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

func TestVault_PutGet_RoundTrip(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	bundle := []byte(`{"host":"api.example.com","token":"abc"}`)
	if err := v.Put(ctx, "ex-prod", KindBearer, "api.example.com", bundle); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := v.Get(ctx, "ex-prod")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ex-prod" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Kind != KindBearer {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Host != "api.example.com" {
		t.Errorf("Host = %q", got.Host)
	}
	if string(got.Bundle) != string(bundle) {
		t.Errorf("Bundle = %s, want %s", got.Bundle, bundle)
	}
}

func TestVault_Get_NotFound(t *testing.T) {
	v := newTestVault(t)
	_, err := v.Get(context.Background(), "missing")
	if err != ErrCredentialNotFound {
		t.Fatalf("want ErrCredentialNotFound, got %v", err)
	}
}

func TestVault_Put_RejectsInvalidBundle(t *testing.T) {
	v := newTestVault(t)
	err := v.Put(context.Background(), "bad", KindBearer, "x", []byte(`{}`))
	if err == nil {
		t.Fatal("expected validation error for empty bearer bundle")
	}
}

func TestVault_Delete(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	bundle := []byte(`{"host":"x","token":"t"}`)
	if err := v.Put(ctx, "to-delete", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete(ctx, "to-delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "to-delete"); err != ErrCredentialNotFound {
		t.Fatalf("want ErrCredentialNotFound after Delete, got %v", err)
	}
}

func TestVault_List_OmitsBundle(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	bundle := []byte(`{"host":"x","token":"t"}`)
	if err := v.Put(ctx, "a", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "b", KindBearer, "y", bundle); err != nil {
		t.Fatal(err)
	}
	sums, err := v.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 {
		t.Fatalf("want 2 summaries, got %d", len(sums))
	}
	// Summary type must not expose bundles
	for _, s := range sums {
		if s.Name == "" || s.Kind == "" || s.Host == "" {
			t.Errorf("summary missing fields: %+v", s)
		}
	}
}

func TestVault_PutSameNameUpdates(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "same", KindBearer, "x", []byte(`{"host":"x","token":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "same", KindBearer, "x", []byte(`{"host":"x","token":"new"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := v.Get(ctx, "same")
	if string(got.Bundle) != `{"host":"x","token":"new"}` {
		t.Fatalf("update didn't replace bundle: %s", got.Bundle)
	}
}
```

- [ ] **Step 3: Run — expect fail**

```bash
go test ./internal/creds/... -run Vault
```
Expected: `undefined: OpenVault`, `undefined: Vault`, etc.

- [ ] **Step 4: Implement Vault**

`internal/creds/vault.go`:
```go
package creds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// ErrCredentialNotFound is returned when Get/Delete don't find the credential.
var ErrCredentialNotFound = errors.New("creds: credential not found")

// Credential is the decrypted result of Get. Bundle is plaintext JSON.
type Credential struct {
	Name       string
	Kind       Kind
	Host       string
	Bundle     []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	db, err := sql.Open("sqlite", path)
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
```

- [ ] **Step 5: Run — vault tests pass**

```bash
go mod tidy
go test ./internal/creds/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/creds/vault.go internal/creds/schema.go internal/creds/vault_test.go go.mod go.sum
git commit -m "feat(creds): SQLite vault with encrypted Put/Get/Delete/List"
```

---

## Task 6: Audit log

**Files:**
- Modify: `internal/creds/vault.go` (add RecordUse method)
- Test: `internal/creds/audit_test.go`

- [ ] **Step 1: Write failing tests**

`internal/creds/audit_test.go`:
```go
package creds

import (
	"context"
	"testing"
)

func TestVault_RecordUse_WritesRow(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	if err := v.RecordUse(ctx, "name", "https://example.com/x", "ok"); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}

	rows, err := v.db.QueryContext(ctx, `SELECT name, request_url, outcome FROM credential_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var name, url, outcome string
		if err := rows.Scan(&name, &url, &outcome); err != nil {
			t.Fatal(err)
		}
		if name != "name" || url != "https://example.com/x" || outcome != "ok" {
			t.Errorf("row = (%q, %q, %q)", name, url, outcome)
		}
	}
	if count != 1 {
		t.Fatalf("want 1 audit row, got %d", count)
	}
}

func TestVault_RecordUse_DoesNotLeakBundles(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	bundle := []byte(`{"host":"x","token":"SECRET-DO-NOT-LOG"}`)
	if err := v.Put(ctx, "name", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.RecordUse(ctx, "name", "https://x/y", "ok"); err != nil {
		t.Fatal(err)
	}

	// Scan the entire audit table for any occurrence of the secret.
	rows, err := v.db.QueryContext(ctx, `SELECT name, request_url, outcome FROM credential_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n, u, o string
		_ = rows.Scan(&n, &u, &o)
		for _, s := range []string{n, u, o} {
			if containsSecret(s) {
				t.Errorf("audit row contains secret: %q", s)
			}
		}
	}
}

func containsSecret(s string) bool {
	return len(s) >= 6 && (s == "SECRET-DO-NOT-LOG" || (len(s) > 17 && (s[:17] == "SECRET-DO-NOT-LOG" || s[len(s)-17:] == "SECRET-DO-NOT-LOG")))
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/creds/... -run RecordUse
```
Expected: `undefined: (*Vault).RecordUse`.

- [ ] **Step 3: Add RecordUse**

Append to `internal/creds/vault.go`:
```go
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
```

- [ ] **Step 4: Run — audit tests pass**

```bash
go test ./internal/creds/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/creds/vault.go internal/creds/audit_test.go
git commit -m "feat(creds): RecordUse audit log"
```

---

## Task 7: HTML to markdown converter

**Files:**
- Create: `internal/engine/markdown.go`
- Test: `internal/engine/markdown_test.go`

- [ ] **Step 1: Write failing tests for markdown conversion + chrome stripping**

`internal/engine/markdown_test.go`:
```go
package engine

import (
	"strings"
	"testing"
)

func TestHTMLToMarkdown_BasicText(t *testing.T) {
	md, err := HTMLToMarkdown(`<html><body><h1>Hello</h1><p>World</p></body></html>`, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# Hello") {
		t.Errorf("missing h1: %q", md)
	}
	if !strings.Contains(md, "World") {
		t.Errorf("missing p text: %q", md)
	}
}

func TestHTMLToMarkdown_StripsScriptAndStyle(t *testing.T) {
	html := `<html><body><script>alert('x')</script><style>p{}</style><p>visible</p></body></html>`
	md, err := HTMLToMarkdown(html, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "alert") {
		t.Errorf("script content leaked: %q", md)
	}
	if strings.Contains(md, "p{}") {
		t.Errorf("style content leaked: %q", md)
	}
	if !strings.Contains(md, "visible") {
		t.Errorf("body content missing: %q", md)
	}
}

func TestHTMLToMarkdown_StripsChromeByDefault(t *testing.T) {
	html := `<html><body><nav>NAV</nav><header>HDR</header><main><p>BODY</p></main><footer>FOOT</footer></body></html>`
	md, err := HTMLToMarkdown(html, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"NAV", "HDR", "FOOT"} {
		if strings.Contains(md, s) {
			t.Errorf("chrome %s should be stripped: %q", s, md)
		}
	}
	if !strings.Contains(md, "BODY") {
		t.Errorf("main content missing: %q", md)
	}
}

func TestHTMLToMarkdown_IncludeChromeKeepsThem(t *testing.T) {
	html := `<html><body><nav>NAV</nav><main><p>BODY</p></main></body></html>`
	md, err := HTMLToMarkdown(html, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "NAV") {
		t.Errorf("chrome should be kept with includeChrome=true: %q", md)
	}
}

func TestHTMLToMarkdown_Links(t *testing.T) {
	md, err := HTMLToMarkdown(`<a href="https://example.com">click</a>`, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "https://example.com") {
		t.Errorf("link URL missing: %q", md)
	}
	if !strings.Contains(md, "click") {
		t.Errorf("link text missing: %q", md)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/engine/...
```
Expected: `undefined: HTMLToMarkdown`.

- [ ] **Step 3: Implement using html-to-markdown/v2**

`internal/engine/markdown.go`:
```go
// Package engine wraps a headless Chromium (via chromedp) for lynxai's fetch
// operations. Pages are fetched, optionally with credentials applied, and the
// resulting HTML is converted to cleaned markdown for agent consumption.
package engine

import (
	"fmt"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"
)

// chromeTags are elements removed by default when includeChrome=false.
var chromeTags = []string{"nav", "header", "footer", "aside"}

// stripTags are elements always removed.
var stripTags = []string{"script", "style", "noscript", "svg", "template"}

// HTMLToMarkdown converts HTML to cleaned markdown. When includeChrome is false,
// nav/header/footer/aside elements are also stripped.
func HTMLToMarkdown(htmlSrc string, includeChrome bool) (string, error) {
	cleaned, err := stripElements(htmlSrc, includeChrome)
	if err != nil {
		return "", fmt.Errorf("strip: %w", err)
	}
	conv := htmltomd.NewConverter(
		htmltomd.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin()),
	)
	md, err := conv.ConvertString(cleaned)
	if err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	return md, nil
}

func stripElements(htmlSrc string, includeChrome bool) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		return "", err
	}
	toStrip := append([]string{}, stripTags...)
	if !includeChrome {
		toStrip = append(toStrip, chromeTags...)
	}
	stripSet := map[string]bool{}
	for _, t := range toStrip {
		stripSet[t] = true
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		var next *html.Node
		for c := n.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if c.Type == html.ElementNode && stripSet[c.Data] {
				n.RemoveChild(c)
				continue
			}
			walk(c)
		}
	}
	walk(doc)
	var sb strings.Builder
	if err := html.Render(&sb, doc); err != nil {
		return "", err
	}
	return sb.String(), nil
}
```

- [ ] **Step 4: Run — markdown tests pass**

```bash
go mod tidy
go test ./internal/engine/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ go.mod go.sum
git commit -m "feat(engine): HTML→markdown with strip rules"
```

---

## Task 8: Engine — basic Fetch (no credentials)

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go` (integration; behind build tag)

- [ ] **Step 1: Write failing integration test**

`internal/engine/engine_test.go`:
```go
//go:build integration

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEngine_Fetch_StaticPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>T</title></head><body><h1>Hello</h1></body></html>`))
	}))
	defer srv.Close()

	e, err := New(Config{PoolSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := e.Fetch(ctx, FetchRequest{URL: srv.URL, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Status != 200 {
		t.Errorf("Status = %d", res.Status)
	}
	if res.Title != "T" {
		t.Errorf("Title = %q", res.Title)
	}
	if !strings.Contains(res.Markdown, "# Hello") {
		t.Errorf("Markdown missing h1: %q", res.Markdown)
	}
}
```

- [ ] **Step 2: Run — expect fail (undefined symbols, no impl yet)**

```bash
go test -tags integration ./internal/engine/...
```
Expected: `undefined: New`, `undefined: Config`, `undefined: FetchRequest`.

- [ ] **Step 3: Implement Engine**

`internal/engine/engine.go`:
```go
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Config tunes engine behavior.
type Config struct {
	PoolSize     int           // currently unused (single allocator); reserved for v1.1
	DefaultWait  time.Duration // default per-request timeout if FetchRequest.Timeout == 0
}

// FetchRequest is the input to Engine.Fetch.
type FetchRequest struct {
	URL           string
	Credential    *AppliedCredential // pre-resolved by the api layer; nil = unauth
	IncludeChrome bool
	Timeout       time.Duration // 0 = use Config.DefaultWait
}

// AppliedCredential is the engine-level view of a credential — the api layer
// resolves a name+kind into one of these and hands it in.
type AppliedCredential struct {
	Kind    CredKind
	Headers map[string]string // for basic/bearer
	Cookies []CredCookie      // for cookies / form (after login)
}

// CredKind is a flat enum so engine doesn't depend on internal/creds.
type CredKind string

const (
	CredBasic   CredKind = "basic"
	CredBearer  CredKind = "bearer"
	CredCookies CredKind = "cookies"
	CredForm    CredKind = "form"
)

type CredCookie struct {
	Name, Value, Domain, Path string
	Secure, HTTPOnly          bool
}

// FetchResult is the response.
type FetchResult struct {
	Markdown string
	Status   int
	FinalURL string
	Title    string
}

// Engine owns the chromedp allocator and runs fetches.
type Engine struct {
	cfg       Config
	allocCtx  context.Context
	allocDone context.CancelFunc
}

// New constructs an Engine, starting the browser allocator. Caller must Close.
func New(cfg Config) (*Engine, error) {
	if cfg.DefaultWait == 0 {
		cfg.DefaultWait = 30 * time.Second
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	return &Engine{cfg: cfg, allocCtx: ctx, allocDone: cancel}, nil
}

// Close shuts the allocator down.
func (e *Engine) Close() error {
	e.allocDone()
	return nil
}

// Fetch navigates to req.URL and returns cleaned markdown.
func (e *Engine) Fetch(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("engine.Fetch: URL required")
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = e.cfg.DefaultWait
	}

	browserCtx, cancel := chromedp.NewContext(e.allocCtx)
	defer cancel()
	fetchCtx, cancel2 := context.WithTimeout(browserCtx, timeout)
	defer cancel2()

	var (
		status   int64 = 200
		finalURL string
		title    string
		htmlSrc  string
	)

	actions := []chromedp.Action{}
	if req.Credential != nil {
		actions = append(actions, applyCredentialActions(req.Credential)...)
	}
	actions = append(actions,
		chromedp.Navigate(req.URL),
		chromedp.WaitReady("body"),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
		chromedp.OuterHTML("html", &htmlSrc, chromedp.ByQuery),
	)

	if err := chromedp.Run(fetchCtx, actions...); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	md, err := HTMLToMarkdown(htmlSrc, req.IncludeChrome)
	if err != nil {
		return nil, fmt.Errorf("markdown: %w", err)
	}

	return &FetchResult{
		Markdown: md,
		Status:   int(status),
		FinalURL: finalURL,
		Title:    title,
	}, nil
}

// applyCredentialActions returns chromedp actions to apply a pre-resolved credential.
// Implemented in credentials.go (Task 9).
func applyCredentialActions(c *AppliedCredential) []chromedp.Action {
	var acts []chromedp.Action
	if len(c.Headers) > 0 {
		hdrs := network.Headers{}
		for k, v := range c.Headers {
			hdrs[k] = v
		}
		acts = append(acts, network.SetExtraHTTPHeaders(hdrs))
	}
	if len(c.Cookies) > 0 {
		params := make([]*network.CookieParam, 0, len(c.Cookies))
		for _, ck := range c.Cookies {
			params = append(params, &network.CookieParam{
				Name:     ck.Name,
				Value:    ck.Value,
				Domain:   ck.Domain,
				Path:     ck.Path,
				Secure:   ck.Secure,
				HTTPOnly: ck.HTTPOnly,
			})
		}
		acts = append(acts, network.SetCookies(params))
	}
	return acts
}
```

- [ ] **Step 4: Run the integration test (requires Chromium)**

```bash
go mod tidy
# Skip if Chromium isn't installed locally — CI handles it
go test -tags integration ./internal/engine/... -timeout 60s
```
Expected: PASS (or skip-with-note if no Chromium on dev machine).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go go.mod go.sum
git commit -m "feat(engine): chromedp-backed Fetch with credential hook"
```

---

## Task 9: Engine — credential application (basic/bearer/cookies)

This was already wired into Engine.Fetch and applyCredentialActions in Task 8. This task adds the **integration tests** that exercise the credential path end-to-end.

**Files:**
- Modify: `internal/engine/engine_test.go` (add tests)

- [ ] **Step 1: Add basic-auth integration test**

Append to `internal/engine/engine_test.go`:
```go
func TestEngine_Fetch_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>secret</p></body></html>`))
	}))
	defer srv.Close()

	e, _ := New(Config{})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cred := &AppliedCredential{
		Kind:    CredBearer,
		Headers: map[string]string{"Authorization": "Bearer test"},
	}
	res, err := e.Fetch(ctx, FetchRequest{URL: srv.URL, Credential: cred, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(res.Markdown, "secret") {
		t.Errorf("auth not applied; got: %q", res.Markdown)
	}
}

func TestEngine_Fetch_Cookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		if err != nil || c.Value != "MYSESSION" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>logged-in</p></body></html>`))
	}))
	defer srv.Close()

	e, _ := New(Config{})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// httptest gives us 127.0.0.1:NNNNN; the cookie domain must match.
	cred := &AppliedCredential{
		Kind: CredCookies,
		Cookies: []CredCookie{
			{Name: "sid", Value: "MYSESSION", Domain: "127.0.0.1", Path: "/"},
		},
	}
	res, err := e.Fetch(ctx, FetchRequest{URL: srv.URL, Credential: cred, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(res.Markdown, "logged-in") {
		t.Errorf("cookie not applied; got: %q", res.Markdown)
	}
}
```

- [ ] **Step 2: Run integration tests**

```bash
go test -tags integration ./internal/engine/... -timeout 90s
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/engine/engine_test.go
git commit -m "test(engine): integration tests for basic/bearer/cookie credentials"
```

---

## Task 10: Engine — form login

**Files:**
- Create: `internal/engine/formlogin.go`
- Test: `internal/engine/formlogin_test.go`

- [ ] **Step 1: Write failing unit test (with httptest, no Chromium needed)**

`internal/engine/formlogin_test.go`:
```go
package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("username") != "alice" || r.FormValue("password") != "secret" {
			w.WriteHeader(401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "xyz", Path: "/"})
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	ctx := context.Background()
	cookies, err := cache.Login(ctx, "test-cred", FormLoginConfig{
		LoginURL:          srv.URL + "/login",
		Method:            "POST",
		UserField:         "username",
		PassField:         "password",
		User:              "alice",
		Password:          "secret",
		SuccessCookieName: "sessionid",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(cookies) == 0 {
		t.Fatal("no cookies returned")
	}
	var found bool
	for _, c := range cookies {
		if c.Name == "sessionid" && c.Value == "xyz" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sessionid cookie missing in result: %+v", cookies)
	}
}

func TestFormLogin_Cache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "v", Path: "/"})
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	cfg := FormLoginConfig{
		LoginURL: srv.URL, Method: "POST",
		UserField: "u", PassField: "p", User: "a", Password: "b",
		SuccessCookieName: "sessionid",
	}
	for i := 0; i < 3; i++ {
		if _, err := cache.Login(context.Background(), "same-name", cfg); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("login called %d times, want 1 (cache miss)", hits)
	}
}

func TestFormLogin_FailureNoCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no Set-Cookie
		w.WriteHeader(200)
		_, _ = w.Write([]byte("bad creds"))
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	_, err := cache.Login(context.Background(), "x", FormLoginConfig{
		LoginURL: srv.URL, Method: "POST",
		UserField: "u", PassField: "p", User: "a", Password: "b",
		SuccessCookieName: "sessionid",
	})
	if err == nil {
		t.Fatal("expected error when success cookie missing")
	}
	if !strings.Contains(err.Error(), "sessionid") {
		t.Errorf("error should mention missing cookie: %v", err)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/engine/... -run FormLogin
```
Expected: `undefined: FormLoginCache` etc.

- [ ] **Step 3: Implement form-login cache**

`internal/engine/formlogin.go`:
```go
package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FormLoginConfig is the input to FormLoginCache.Login.
type FormLoginConfig struct {
	LoginURL          string
	Method            string // "POST" only in v1
	UserField         string
	PassField         string
	User              string
	Password          string
	SuccessCookieName string
}

// FormLoginCache caches the cookies obtained from form logins, keyed by
// credential name. Cache lives for the process lifetime only — persisted
// contexts come in a later spec.
type FormLoginCache struct {
	mu     sync.Mutex
	by     map[string][]CredCookie
	client *http.Client
}

func NewFormLoginCache() *FormLoginCache {
	return &FormLoginCache{
		by:     map[string][]CredCookie{},
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Login returns the cached cookies for credName if present, otherwise performs
// the form-login POST and caches the result.
func (c *FormLoginCache) Login(ctx context.Context, credName string, cfg FormLoginConfig) ([]CredCookie, error) {
	c.mu.Lock()
	if cookies, ok := c.by[credName]; ok {
		c.mu.Unlock()
		return cookies, nil
	}
	c.mu.Unlock()

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookiejar: %w", err)
	}
	cli := *c.client
	cli.Jar = jar

	body := url.Values{}
	body.Set(cfg.UserField, cfg.User)
	body.Set(cfg.PassField, cfg.Password)

	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.LoginURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login POST: %w", err)
	}
	defer resp.Body.Close()

	u, err := url.Parse(cfg.LoginURL)
	if err != nil {
		return nil, fmt.Errorf("parse login url: %w", err)
	}

	// Collect cookies the jar accumulated (covers Set-Cookie + any redirects).
	jarCookies := jar.Cookies(u)
	var found bool
	out := make([]CredCookie, 0, len(jarCookies))
	for _, ck := range jarCookies {
		if ck.Name == cfg.SuccessCookieName {
			found = true
		}
		out = append(out, CredCookie{
			Name: ck.Name, Value: ck.Value,
			Domain: ck.Domain, Path: ck.Path,
			Secure: ck.Secure, HTTPOnly: ck.HttpOnly,
		})
	}
	if !found {
		return nil, fmt.Errorf("form login: success cookie %q not in response (HTTP %d)", cfg.SuccessCookieName, resp.StatusCode)
	}

	c.mu.Lock()
	c.by[credName] = out
	c.mu.Unlock()
	return out, nil
}

// Invalidate drops the cached cookies for a credential (e.g., on 401 retry).
func (c *FormLoginCache) Invalidate(credName string) {
	c.mu.Lock()
	delete(c.by, credName)
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go test ./internal/engine/... -run FormLogin
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/formlogin.go internal/engine/formlogin_test.go
git commit -m "feat(engine): form-login cache (POST + success-cookie capture)"
```

---

## Task 11: JSON Schema validator helper

**Files:**
- Create: `internal/extract/schema.go`
- Test: `internal/extract/schema_test.go`

- [ ] **Step 1: Write failing tests**

`internal/extract/schema_test.go`:
```go
package extract

import (
	"strings"
	"testing"
)

func TestValidateAgainstSchema_OK(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	doc := []byte(`{"name":"alice"}`)
	if err := ValidateAgainstSchema(schema, doc); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateAgainstSchema_MissingRequired(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	doc := []byte(`{}`)
	err := ValidateAgainstSchema(schema, doc)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention missing field: %v", err)
	}
}

func TestValidateAgainstSchema_BadSchema(t *testing.T) {
	err := ValidateAgainstSchema([]byte(`not json`), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/extract/...
```
Expected: `undefined: ValidateAgainstSchema`.

- [ ] **Step 3: Implement**

`internal/extract/schema.go`:
```go
// Package extract runs schema-driven LLM extractions against fetched pages.
package extract

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateAgainstSchema checks that doc (JSON) conforms to schema (JSON Schema).
func ValidateAgainstSchema(schema, doc []byte) error {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("inline://schema", bytes.NewReader(schema)); err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	sch, err := c.Compile("inline://schema")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return fmt.Errorf("parse doc: %w", err)
	}
	if err := sch.Validate(v); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go mod tidy
go test ./internal/extract/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extract/ go.mod go.sum
git commit -m "feat(extract): JSON Schema validation helper"
```

---

## Task 12: Extract via bridle (with mocked Turner)

**Files:**
- Create: `internal/extract/extract.go`
- Test: `internal/extract/extract_test.go`

- [ ] **Step 1: Write failing test for happy path with mocked Turner**

`internal/extract/extract_test.go`:
```go
package extract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	bridle "github.com/CarriedWorldUniverse/bridle"
)

// fakeTurner records the request and returns a canned tool invocation.
type fakeTurner struct {
	gotRequest bridle.TurnRequest
	emit       json.RawMessage // args returned in the tool call
	err        error
}

func (f *fakeTurner) RunTurn(ctx context.Context, req bridle.TurnRequest, runner bridle.ToolRunner, sink bridle.EventSink) (bridle.TurnResult, error) {
	f.gotRequest = req
	if f.err != nil {
		return bridle.TurnResult{}, f.err
	}
	return bridle.TurnResult{
		ToolCalls: []bridle.ToolInvocation{{
			Name: extractToolName,
			Args: f.emit,
		}},
	}, nil
}

func TestExtract_HappyPath(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	emit := json.RawMessage(`{"name":"alice"}`)
	turner := &fakeTurner{emit: emit}

	x := NewExtractor(turner)
	res, err := x.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "## Profile\nName: alice",
		Schema:       schema,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(res, &got)
	if got["name"] != "alice" {
		t.Errorf("got %v", got)
	}

	// Verify the schema was passed to bridle as the tool's input schema.
	if len(turner.gotRequest.Tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(turner.gotRequest.Tools))
	}
	if turner.gotRequest.Tools[0].Name != extractToolName {
		t.Errorf("tool name = %q, want %q", turner.gotRequest.Tools[0].Name, extractToolName)
	}
	if !strings.Contains(string(turner.gotRequest.Tools[0].InputSchema), `"name"`) {
		t.Errorf("schema not forwarded: %s", turner.gotRequest.Tools[0].InputSchema)
	}
	if turner.gotRequest.MaxSteps != 1 {
		t.Errorf("MaxSteps = %d, want 1", turner.gotRequest.MaxSteps)
	}
}

func TestExtract_NoToolCallIsError(t *testing.T) {
	turner := &fakeTurner{} // returns empty ToolCalls
	x := NewExtractor(turner)
	_, err := x.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "x",
		Schema:       json.RawMessage(`{"type":"object"}`),
	})
	if err == nil {
		t.Fatal("expected error when model returned no tool call")
	}
}

func TestExtract_ValidationFailureBubblesUp(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"string"}},"required":["n"]}`)
	turner := &fakeTurner{emit: json.RawMessage(`{}`)} // missing required
	x := NewExtractor(turner)
	_, err := x.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "x", Schema: schema,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/extract/... -run Extract
```
Expected: `undefined: NewExtractor`, etc.

- [ ] **Step 3: Implement Extractor**

`internal/extract/extract.go`:
```go
package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	bridle "github.com/CarriedWorldUniverse/bridle"
)

const (
	extractToolName    = "emit_extraction"
	extractToolDesc    = "Call this exactly once with the structured data extracted from the page. The arguments must conform to the supplied schema."
	extractSystemPrompt = `You are a precise data-extraction assistant. The user will give you a web page's content in markdown. You must call the "emit_extraction" tool exactly once, with arguments that conform to the tool's input schema, populated from facts found in the page. Do not invent data. If a field cannot be determined from the page, omit it (unless required by the schema, in which case use the closest reasonable value from context).`
)

// Turner is the subset of bridle.Harness we use. Mockable in tests.
type Turner interface {
	RunTurn(ctx context.Context, req bridle.TurnRequest, runner bridle.ToolRunner, sink bridle.EventSink) (bridle.TurnResult, error)
}

// ExtractRequest is the input to Extractor.Extract.
type ExtractRequest struct {
	PageMarkdown string
	Schema       json.RawMessage
}

// Extractor runs schema-driven LLM extractions via a bridle turn.
type Extractor struct {
	turner Turner
}

// NewExtractor returns an Extractor backed by the given Turner (bridle.Harness in prod).
func NewExtractor(t Turner) *Extractor {
	return &Extractor{turner: t}
}

// ErrNoToolCall is returned when the model finished a turn without calling our tool.
var ErrNoToolCall = errors.New("extract: model returned no tool invocation")

// Extract runs the turn and returns the JSON args from the tool call.
func (x *Extractor) Extract(ctx context.Context, req ExtractRequest) (json.RawMessage, error) {
	turnReq := bridle.TurnRequest{
		AspectID:     "lynxai-extract",
		SystemPrompt: extractSystemPrompt,
		UserMessage:  req.PageMarkdown,
		Tools: []bridle.ToolDef{{
			Name:        extractToolName,
			Description: extractToolDesc,
			InputSchema: req.Schema,
		}},
		MaxSteps: 1,
	}
	result, err := x.turner.RunTurn(ctx, turnReq, nopRunner{}, nopSink{})
	if err != nil {
		return nil, fmt.Errorf("bridle turn: %w", err)
	}
	if len(result.ToolCalls) == 0 {
		return nil, ErrNoToolCall
	}
	args := result.ToolCalls[0].Args
	if err := ValidateAgainstSchema(req.Schema, args); err != nil {
		return nil, fmt.Errorf("extracted JSON does not match schema: %w", err)
	}
	return args, nil
}

// nopRunner is a ToolRunner that refuses to execute tools — we want the model
// to *emit* one tool call, not actually run it.
type nopRunner struct{}

func (nopRunner) Run(ctx context.Context, call bridle.ToolCall) (json.RawMessage, error) {
	return nil, fmt.Errorf("extract: tool %q should not be invoked; model should emit it once", call.Name)
}

// nopSink discards all stream events. Bridle requires a non-nil sink.
type nopSink struct{}

func (nopSink) Emit(bridle.Event) {}
```

> **Note for implementer:** verify the exact method name on bridle's `EventSink` interface (`Emit`, `Send`, etc.) by reading `~/Source/bridle/events.go`. If different, rename `Emit` accordingly — the rest of the code is unchanged. Same applies to `ToolDef` field names (`Name`, `Description`, `InputSchema`) and `ToolRunner.Run` — these match the spec but bridle is the source of truth.

- [ ] **Step 4: Run — extract tests pass**

```bash
go mod tidy
go test ./internal/extract/...
```
Expected: PASS. If bridle's API names differ, adjust the call sites until tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extract/ go.mod go.sum
git commit -m "feat(extract): bridle-backed schema extraction"
```

---

## Task 13: API error encoding

**Files:**
- Create: `internal/api/errors.go`
- Test: `internal/api/errors_test.go`

- [ ] **Step 1: Write failing tests**

`internal/api/errors_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteError_ShapeAndStatus(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, ErrCodeCredentialNotFound, "cred 'foo' not found", nil)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != string(ErrCodeCredentialNotFound) {
		t.Errorf("code = %q", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Error("message empty")
	}
}

func TestErrorCodeStatusMap(t *testing.T) {
	cases := map[ErrCode]int{
		ErrCodeBadRequest:               400,
		ErrCodeCredentialNotFound:       404,
		ErrCodeCredentialDecryptFailed:  500,
		ErrCodeCredentialApplyFailed:    502,
		ErrCodeNavigationFailed:         502,
		ErrCodeExtractionFailed:         502,
		ErrCodeLLMUnavailable:           503,
		ErrCodeInternal:                 500,
	}
	for code, want := range cases {
		if got := statusFor(code); got != want {
			t.Errorf("statusFor(%q) = %d, want %d", code, got, want)
		}
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/api/...
```
Expected: `undefined: WriteError`, etc.

- [ ] **Step 3: Implement**

`internal/api/errors.go`:
```go
// Package api hosts the lynxai HTTP handlers (chi router).
package api

import (
	"encoding/json"
	"net/http"
)

// ErrCode is the machine-readable error class. See spec §"Error handling".
type ErrCode string

const (
	ErrCodeBadRequest              ErrCode = "bad_request"
	ErrCodeCredentialNotFound      ErrCode = "credential_not_found"
	ErrCodeCredentialDecryptFailed ErrCode = "credential_decrypt_failed"
	ErrCodeCredentialApplyFailed   ErrCode = "credential_apply_failed"
	ErrCodeNavigationFailed        ErrCode = "navigation_failed"
	ErrCodeExtractionFailed        ErrCode = "extraction_failed"
	ErrCodeLLMUnavailable          ErrCode = "llm_unavailable"
	ErrCodeInternal                ErrCode = "internal_error"
)

// errorBody is the on-the-wire shape.
type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    ErrCode        `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func statusFor(code ErrCode) int {
	switch code {
	case ErrCodeBadRequest:
		return http.StatusBadRequest
	case ErrCodeCredentialNotFound:
		return http.StatusNotFound
	case ErrCodeCredentialDecryptFailed, ErrCodeInternal:
		return http.StatusInternalServerError
	case ErrCodeCredentialApplyFailed, ErrCodeNavigationFailed, ErrCodeExtractionFailed:
		return http.StatusBadGateway
	case ErrCodeLLMUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// WriteError writes a structured JSON error response.
func WriteError(w http.ResponseWriter, code ErrCode, msg string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusFor(code))
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorPayload{
		Code: code, Message: msg, Details: details,
	}})
}
```

- [ ] **Step 4: Run — tests pass**

```bash
go test ./internal/api/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/errors.go internal/api/errors_test.go
git commit -m "feat(api): structured error JSON encoding"
```

---

## Task 14: API — credentials handlers

**Files:**
- Create: `internal/api/credentials.go`
- Test: `internal/api/credentials_test.go`

- [ ] **Step 1: Write failing tests**

`internal/api/credentials_test.go`:
```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
)

func newTestServer(t *testing.T) (http.Handler, *creds.Vault) {
	t.Helper()
	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, err := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	h := NewRouter(Deps{Vault: v}) // engine/extractor unused for these tests
	return h, v
}

func TestCredentialsPut_Roundtrip(t *testing.T) {
	h, _ := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"x","kind":"bearer","host":"api.x","bundle":{"host":"api.x","token":"abc"}}`)
	req := httptest.NewRequest("POST", "/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}

	req = httptest.NewRequest("GET", "/credentials", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list status = %d", w.Code)
	}
	var sums []creds.CredentialSummary
	if err := json.NewDecoder(w.Body).Decode(&sums); err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].Name != "x" {
		t.Fatalf("sums = %+v", sums)
	}
}

func TestCredentialsPut_InvalidBundle(t *testing.T) {
	h, _ := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"x","kind":"bearer","host":"y","bundle":{}}`) // missing token
	req := httptest.NewRequest("POST", "/credentials", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
}

func TestCredentialsGet_NotFound(t *testing.T) {
	h, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/credentials/missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestCredentialsDelete(t *testing.T) {
	h, v := newTestServer(t)
	_ = v.Put(nil, "to-delete", creds.KindBearer, "x", []byte(`{"host":"x","token":"t"}`))

	req := httptest.NewRequest("DELETE", "/credentials/to-delete", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Errorf("status = %d", w.Code)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/api/...
```
Expected: `undefined: NewRouter`, `undefined: Deps`.

- [ ] **Step 3: Create the router skeleton**

`internal/api/router.go`:
```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

// Deps bundles the runtime dependencies the API handlers need.
type Deps struct {
	Vault     *creds.Vault
	Engine    *engine.Engine
	Extractor *extract.Extractor
	Forms     *engine.FormLoginCache
}

// NewRouter wires the chi router with all v1 endpoints.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	r.Route("/credentials", func(r chi.Router) {
		r.Post("/", putCredentialHandler(d))
		r.Get("/", listCredentialsHandler(d))
		r.Get("/{name}", getCredentialHandler(d))
		r.Delete("/{name}", deleteCredentialHandler(d))
	})

	r.Post("/fetch", fetchHandler(d))
	r.Post("/extract", extractHandler(d))
	return r
}
```

- [ ] **Step 4: Implement credentials handlers**

`internal/api/credentials.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
)

type credentialPut struct {
	Name   string          `json:"name"`
	Kind   creds.Kind      `json:"kind"`
	Host   string          `json:"host"`
	Bundle json.RawMessage `json:"bundle"`
}

func putCredentialHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body credentialPut
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, ErrCodeBadRequest, "invalid JSON body: "+err.Error(), nil)
			return
		}
		if body.Name == "" || body.Kind == "" || body.Host == "" || len(body.Bundle) == 0 {
			WriteError(w, ErrCodeBadRequest, "name, kind, host, bundle all required", nil)
			return
		}
		if err := d.Vault.Put(r.Context(), body.Name, body.Kind, body.Host, body.Bundle); err != nil {
			WriteError(w, ErrCodeBadRequest, err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"name": body.Name})
	}
}

func listCredentialsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sums, err := d.Vault.List(r.Context())
		if err != nil {
			WriteError(w, ErrCodeInternal, err.Error(), nil)
			return
		}
		if sums == nil {
			sums = []creds.CredentialSummary{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sums)
	}
}

func getCredentialHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		c, err := d.Vault.Get(r.Context(), name)
		if errors.Is(err, creds.ErrCredentialNotFound) {
			WriteError(w, ErrCodeCredentialNotFound, "credential "+name+" not found", nil)
			return
		}
		if err != nil {
			WriteError(w, ErrCodeInternal, err.Error(), nil)
			return
		}
		// Bundle is intentionally NOT returned — clients identify by name only.
		summary := creds.CredentialSummary{
			Name: c.Name, Kind: c.Kind, Host: c.Host,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(summary)
	}
}

func deleteCredentialHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		err := d.Vault.Delete(r.Context(), name)
		if errors.Is(err, creds.ErrCredentialNotFound) {
			WriteError(w, ErrCodeCredentialNotFound, "credential "+name+" not found", nil)
			return
		}
		if err != nil {
			WriteError(w, ErrCodeInternal, err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
```

- [ ] **Step 5: Add fetch/extract placeholder handlers (filled in next tasks)**

Create `internal/api/fetch.go` with a stub so the router compiles:
```go
package api

import "net/http"

func fetchHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, ErrCodeInternal, "fetch handler not yet implemented", nil)
	}
}
```

Create `internal/api/extract.go` similarly:
```go
package api

import "net/http"

func extractHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, ErrCodeInternal, "extract handler not yet implemented", nil)
	}
}
```

- [ ] **Step 6: Run — credentials tests pass**

```bash
go mod tidy
go test ./internal/api/...
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/router.go internal/api/credentials.go internal/api/credentials_test.go internal/api/fetch.go internal/api/extract.go go.mod go.sum
git commit -m "feat(api): router + credentials CRUD endpoints"
```

---

## Task 15: API — /fetch handler

**Files:**
- Modify: `internal/api/fetch.go`
- Test: `internal/api/fetch_test.go`

- [ ] **Step 1: Write failing test (uses a stub Engine via interface)**

First, introduce an Engine interface in `internal/api/router.go` so we can mock it. Replace the `Engine *engine.Engine` field with:

```go
// FetcherEngine is the subset of *engine.Engine the api layer needs.
type FetcherEngine interface {
	Fetch(ctx context.Context, req engine.FetchRequest) (*engine.FetchResult, error)
}
```

And the Deps field becomes:
```go
Engine FetcherEngine
```

(import `"context"` in router.go)

Then `internal/api/fetch_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
)

type stubEngine struct {
	got engine.FetchRequest
	res *engine.FetchResult
	err error
}

func (s *stubEngine) Fetch(ctx context.Context, req engine.FetchRequest) (*engine.FetchResult, error) {
	s.got = req
	return s.res, s.err
}

func newTestServerWithEngine(t *testing.T, eng FetcherEngine) (http.Handler, *creds.Vault) {
	t.Helper()
	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, _ := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	t.Cleanup(func() { v.Close() })
	return NewRouter(Deps{Vault: v, Engine: eng, Forms: engine.NewFormLoginCache()}), v
}

func TestFetch_NoCredential(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "# hello", Status: 200, FinalURL: "https://x"}}
	h, _ := newTestServerWithEngine(t, eng)

	body := bytes.NewBufferString(`{"url":"https://example.com"}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var got engine.FetchResult
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got.Markdown != "# hello" {
		t.Errorf("md = %q", got.Markdown)
	}
	if eng.got.URL != "https://example.com" {
		t.Errorf("url = %q", eng.got.URL)
	}
	if eng.got.Credential != nil {
		t.Errorf("credential should be nil")
	}
}

func TestFetch_WithBearerCredential(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "ok", Status: 200}}
	h, v := newTestServerWithEngine(t, eng)
	_ = v.Put(context.Background(), "ex", creds.KindBearer, "example.com", []byte(`{"host":"example.com","token":"abc"}`))

	body := bytes.NewBufferString(`{"url":"https://example.com/x","credential":{"name":"ex"}}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if eng.got.Credential == nil || eng.got.Credential.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("credential not applied: %+v", eng.got.Credential)
	}
}

func TestFetch_CredentialNotFound(t *testing.T) {
	eng := &stubEngine{}
	h, _ := newTestServerWithEngine(t, eng)
	body := bytes.NewBufferString(`{"url":"https://x","credential":{"name":"missing"}}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d", w.Code)
	}
}
```

(Note: add `import "net/http"` to the test file.)

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/api/... -run Fetch
```
Expected: fetch handler returns 500 "not yet implemented".

- [ ] **Step 3: Implement resolver shared by both /fetch and /extract**

Create `internal/api/resolve.go`:
```go
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
)

// errorResponse is what resolveCredential returns when something goes wrong.
// Handlers map it onto WriteError. Keeps the resolver IO-free.
type errorResponse struct {
	Code    ErrCode
	Message string
}

type credentialRef struct {
	Name string `json:"name"`
}

// resolveCredential loads a credential, decrypts it, builds an
// engine.AppliedCredential, and (for form-login) runs the cached login. On any
// failure it returns a populated *errorResponse along with audit-friendly
// outcome string already written via Vault.RecordUse.
//
// On success: (*AppliedCredential, nil). On failure: (nil, *errorResponse).
func resolveCredential(ctx context.Context, d Deps, name, requestURL string) (*engine.AppliedCredential, *errorResponse) {
	c, err := d.Vault.Get(ctx, name)
	if errors.Is(err, creds.ErrCredentialNotFound) {
		_ = d.Vault.RecordUse(ctx, name, requestURL, "not_found")
		return nil, &errorResponse{ErrCodeCredentialNotFound, "credential " + name + " not found"}
	}
	if errors.Is(err, creds.ErrDecryptFailed) {
		_ = d.Vault.RecordUse(ctx, name, requestURL, "decrypt_failed")
		return nil, &errorResponse{ErrCodeCredentialDecryptFailed, "decrypt failed"}
	}
	if err != nil {
		return nil, &errorResponse{ErrCodeInternal, err.Error()}
	}

	switch c.Kind {
	case creds.KindBasic:
		var b creds.BasicBundle
		if err := json.Unmarshal(c.Bundle, &b); err != nil {
			return nil, &errorResponse{ErrCodeInternal, "decode basic bundle: " + err.Error()}
		}
		token := base64.StdEncoding.EncodeToString([]byte(b.User + ":" + b.Password))
		return &engine.AppliedCredential{
			Kind:    engine.CredBasic,
			Headers: map[string]string{"Authorization": "Basic " + token},
		}, nil

	case creds.KindBearer:
		var b creds.BearerBundle
		if err := json.Unmarshal(c.Bundle, &b); err != nil {
			return nil, &errorResponse{ErrCodeInternal, "decode bearer bundle: " + err.Error()}
		}
		return &engine.AppliedCredential{
			Kind:    engine.CredBearer,
			Headers: map[string]string{"Authorization": "Bearer " + b.Token},
		}, nil

	case creds.KindCookies:
		var b creds.CookiesBundle
		if err := json.Unmarshal(c.Bundle, &b); err != nil {
			return nil, &errorResponse{ErrCodeInternal, "decode cookies bundle: " + err.Error()}
		}
		ck := make([]engine.CredCookie, 0, len(b.Cookies))
		for _, src := range b.Cookies {
			ck = append(ck, engine.CredCookie{
				Name: src.Name, Value: src.Value, Domain: src.Domain, Path: src.Path,
				Secure: src.Secure, HTTPOnly: src.HTTPOnly,
			})
		}
		return &engine.AppliedCredential{Kind: engine.CredCookies, Cookies: ck}, nil

	case creds.KindForm:
		var b creds.FormBundle
		if err := json.Unmarshal(c.Bundle, &b); err != nil {
			return nil, &errorResponse{ErrCodeInternal, "decode form bundle: " + err.Error()}
		}
		cookies, lerr := d.Forms.Login(ctx, name, engine.FormLoginConfig{
			LoginURL: b.LoginURL, Method: b.Method,
			UserField: b.Fields.UserField, PassField: b.Fields.PassField,
			User: b.Fields.User, Password: b.Fields.Password,
			SuccessCookieName: b.SuccessCookieName,
		})
		if lerr != nil {
			_ = d.Vault.RecordUse(ctx, name, requestURL, "apply_failed")
			return nil, &errorResponse{ErrCodeCredentialApplyFailed, lerr.Error()}
		}
		return &engine.AppliedCredential{Kind: engine.CredForm, Cookies: cookies}, nil

	default:
		return nil, &errorResponse{ErrCodeInternal, "unknown credential kind " + string(c.Kind)}
	}
}
```

Replace `internal/api/fetch.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
)

type fetchRequest struct {
	URL           string         `json:"url"`
	Credential    *credentialRef `json:"credential,omitempty"`
	IncludeChrome bool           `json:"include_chrome,omitempty"`
}

func fetchHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body fetchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, ErrCodeBadRequest, "invalid JSON: "+err.Error(), nil)
			return
		}
		if body.URL == "" {
			WriteError(w, ErrCodeBadRequest, "url required", nil)
			return
		}

		var applied *engine.AppliedCredential
		if body.Credential != nil {
			a, errResp := resolveCredential(r.Context(), d, body.Credential.Name, body.URL)
			if errResp != nil {
				WriteError(w, errResp.Code, errResp.Message, nil)
				return
			}
			applied = a
		}

		res, err := d.Engine.Fetch(r.Context(), engine.FetchRequest{
			URL:           body.URL,
			Credential:    applied,
			IncludeChrome: body.IncludeChrome,
		})
		if err != nil {
			recordOutcome(r.Context(), d, body.Credential, body.URL, "apply_failed")
			WriteError(w, ErrCodeNavigationFailed, err.Error(), nil)
			return
		}
		recordOutcome(r.Context(), d, body.Credential, body.URL, "ok")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}

// recordOutcome writes an audit row only when a credential was used.
func recordOutcome(ctx context.Context, d Deps, ref *credentialRef, url, outcome string) {
	if ref == nil {
		return
	}
	_ = d.Vault.RecordUse(ctx, ref.Name, url, outcome)
}
```

- [ ] **Step 4: Run — fetch tests pass**

```bash
go test ./internal/api/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/ go.mod go.sum
git commit -m "feat(api): /fetch handler with credential resolution + audit"
```

---

## Task 16: API — /extract handler

**Files:**
- Modify: `internal/api/extract.go`
- Test: `internal/api/extract_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/extract_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	bridle "github.com/CarriedWorldUniverse/bridle"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

type stubTurner struct {
	emit json.RawMessage
}

func (s *stubTurner) RunTurn(ctx context.Context, req bridle.TurnRequest, runner bridle.ToolRunner, sink bridle.EventSink) (bridle.TurnResult, error) {
	return bridle.TurnResult{
		ToolCalls: []bridle.ToolInvocation{{Name: "emit_extraction", Args: s.emit}},
	}, nil
}

func TestExtract_HandlerWiresEverything(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "# Hello\nname: alice", Status: 200}}
	xtr := extract.NewExtractor(&stubTurner{emit: json.RawMessage(`{"name":"alice"}`)})

	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, _ := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	t.Cleanup(func() { v.Close() })
	h := NewRouter(Deps{Vault: v, Engine: eng, Extractor: xtr, Forms: engine.NewFormLoginCache()})

	body := bytes.NewBufferString(`{
		"url": "https://x",
		"schema": {"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}
	}`)
	req := httptest.NewRequest("POST", "/extract", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got["json"] == nil {
		t.Fatalf("response missing 'json': %+v", got)
	}
	js, _ := json.Marshal(got["json"])
	if string(js) != `{"name":"alice"}` {
		t.Errorf("got %s", js)
	}
}
```

- [ ] **Step 2: Update Deps and router to plumb Extractor**

Ensure `internal/api/router.go` has `Extractor *extract.Extractor` (or an `ExtractorIface`) in `Deps`. It already does from Task 14.

- [ ] **Step 3: Run — expect fail**

```bash
go test ./internal/api/... -run Extract
```
Expected: extract handler stub returns 500.

- [ ] **Step 4: Implement /extract handler**

Replace `internal/api/extract.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

type extractRequest struct {
	URL           string          `json:"url"`
	Credential    *credentialRef  `json:"credential,omitempty"`
	IncludeChrome bool            `json:"include_chrome,omitempty"`
	Schema        json.RawMessage `json:"schema"`
}

type extractResponse struct {
	JSON     json.RawMessage `json:"json"`
	Status   int             `json:"status"`
	FinalURL string          `json:"final_url"`
}

func extractHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body extractRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, ErrCodeBadRequest, "invalid JSON: "+err.Error(), nil)
			return
		}
		if body.URL == "" || len(body.Schema) == 0 {
			WriteError(w, ErrCodeBadRequest, "url and schema required", nil)
			return
		}

		var applied *engine.AppliedCredential
		if body.Credential != nil {
			a, errResp := resolveCredential(r.Context(), d, body.Credential.Name, body.URL)
			if errResp != nil {
				WriteError(w, errResp.Code, errResp.Message, nil)
				return
			}
			applied = a
		}

		page, err := d.Engine.Fetch(r.Context(), engine.FetchRequest{
			URL:           body.URL,
			Credential:    applied,
			IncludeChrome: body.IncludeChrome,
		})
		if err != nil {
			WriteError(w, ErrCodeNavigationFailed, err.Error(), nil)
			return
		}

		js, err := d.Extractor.Extract(r.Context(), extract.ExtractRequest{
			PageMarkdown: page.Markdown,
			Schema:       body.Schema,
		})
		if err != nil {
			WriteError(w, ErrCodeExtractionFailed, err.Error(), nil)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(extractResponse{
			JSON: js, Status: page.Status, FinalURL: page.FinalURL,
		})
	}
}
```

(`resolveCredential` and `errorResponse` are defined in `internal/api/resolve.go` from Task 15.)

- [ ] **Step 5: Run — extract handler test passes**

```bash
go test ./internal/api/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/extract.go internal/api/extract_test.go
git commit -m "feat(api): /extract handler wired through engine + extractor"
```

---

## Task 17: bridle config synthesis (DeepSeek default)

**Files:**
- Create: `internal/bridlecfg/config.go`
- Test: `internal/bridlecfg/config_test.go`

- [ ] **Step 1: Write failing test for default DeepSeek-via-openai-api harness**

`internal/bridlecfg/config_test.go`:
```go
package bridlecfg

import (
	"testing"
)

func TestSynthesizeDefault_DeepSeek(t *testing.T) {
	t.Setenv("LYNXAI_LLM_API_KEY", "sk-test")
	cfg, err := Synthesize("") // empty path => synthesize default
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://api.deepseek.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "deepseek-chat" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey not picked up from env")
	}
}

func TestSynthesizeDefault_MissingKeyIsError(t *testing.T) {
	t.Setenv("LYNXAI_LLM_API_KEY", "")
	_, err := Synthesize("")
	if err == nil {
		t.Fatal("expected error when no key provided")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/bridlecfg/...
```
Expected: package missing.

- [ ] **Step 3: Implement**

`internal/bridlecfg/config.go`:
```go
// Package bridlecfg loads (or synthesizes) the bridle configuration used by
// lynxai's extract pipeline. v1 supports two modes:
//   - operator-supplied config file path (full bridle config, all options)
//   - zero-config default: DeepSeek via openai-api, API key from env
package bridlecfg

import (
	"fmt"
	"os"
)

// Config is the subset of bridle config lynxai builds the harness from.
type Config struct {
	Provider string // "openai-api" | "claude-api" | "ollama-local" | ...
	BaseURL  string // for openai-api: e.g. https://api.deepseek.com
	Model    string
	APIKey   string
}

// Synthesize returns a Config. If path is non-empty, it's parsed (real bridle
// config file). If path is empty, the default DeepSeek config is synthesized
// from the LYNXAI_LLM_API_KEY env var.
func Synthesize(path string) (*Config, error) {
	if path != "" {
		return loadFile(path)
	}
	key := os.Getenv("LYNXAI_LLM_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("no bridle config and LYNXAI_LLM_API_KEY not set (need either)")
	}
	return &Config{
		Provider: "openai-api",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-chat",
		APIKey:   key,
	}, nil
}

// loadFile parses a bridle config file. v1 supports a minimal JSON shape that
// covers the Config struct above; richer bridle config support can come later.
func loadFile(path string) (*Config, error) {
	// Minimal v1 implementation. Engineer: read the file, JSON-unmarshal into
	// Config, return errors. If/when bridle ships a config loader, swap to it.
	return nil, fmt.Errorf("bridle config file loading: not yet implemented (use LYNXAI_LLM_API_KEY for default DeepSeek)")
}
```

- [ ] **Step 4: Run — config tests pass**

```bash
go test ./internal/bridlecfg/...
```
Expected: PASS.

- [ ] **Step 5: Add helper that turns Config into a live bridle.Harness**

Append to `internal/bridlecfg/config.go`:
```go
// (Same package. Separate function so tests above don't need to import bridle.)
```

Create `internal/bridlecfg/harness.go`:
```go
package bridlecfg

import (
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	bridle "github.com/CarriedWorldUniverse/bridle"
	openaiProv "github.com/CarriedWorldUniverse/bridle/provider/openai"
)

// NewHarness constructs a *bridle.Harness for cfg. Only openai-api in v1.
func NewHarness(cfg *Config) (*bridle.Harness, error) {
	switch cfg.Provider {
	case "openai-api":
		client := openai.NewClient(
			option.WithAPIKey(cfg.APIKey),
			option.WithBaseURL(cfg.BaseURL),
		)
		return bridle.NewHarness(openaiProv.NewWithClient(client)), nil
	default:
		return nil, fmt.Errorf("bridlecfg: provider %q not supported in v1 (use openai-api)", cfg.Provider)
	}
}
```

- [ ] **Step 6: Commit**

```bash
go mod tidy
git add internal/bridlecfg/ go.mod go.sum
git commit -m "feat(bridlecfg): default DeepSeek-via-openai-api harness"
```

---

## Task 18: `lynxai serve` main wiring

**Files:**
- Modify: `cmd/lynxai/main.go`
- Create: `cmd/lynxai/serve.go`

- [ ] **Step 1: Write the serve subcommand**

`cmd/lynxai/serve.go`:
```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/CarriedWorldUniverse/lynxai/internal/api"
	"github.com/CarriedWorldUniverse/lynxai/internal/bridlecfg"
	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7878", "address to bind")
	dataDir := fs.String("data-dir", defaultDataDir(), "directory for master.key and lynxai.db")
	bridleCfg := fs.String("bridle-config", os.Getenv("LYNXAI_BRIDLE_CONFIG"), "path to bridle config (empty => synthesize default from LYNXAI_LLM_API_KEY)")
	_ = fs.Parse(args)

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	masterKey, err := creds.LoadOrCreateMasterKey(filepath.Join(*dataDir, "master.key"))
	if err != nil {
		return err
	}
	vault, err := creds.OpenVault(filepath.Join(*dataDir, "lynxai.db"), masterKey)
	if err != nil {
		return err
	}
	defer vault.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		return err
	}
	defer eng.Close()

	cfg, err := bridlecfg.Synthesize(*bridleCfg)
	if err != nil {
		return fmt.Errorf("bridle config: %w", err)
	}
	harness, err := bridlecfg.NewHarness(cfg)
	if err != nil {
		return err
	}

	router := api.NewRouter(api.Deps{
		Vault:     vault,
		Engine:    eng,
		Extractor: extract.NewExtractor(harness),
		Forms:     engine.NewFormLoginCache(),
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("lynxai serving on http://%s (data-dir=%s, llm=%s/%s)", *addr, *dataDir, cfg.Provider, cfg.Model)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func defaultDataDir() string {
	if v := os.Getenv("LYNXAI_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".lynxai"
	}
	return filepath.Join(home, ".lynxai")
}
```

- [ ] **Step 2: Wire serve into main.go**

Update `cmd/lynxai/main.go` — replace the existing `case "serve":` body:
```go
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
```

- [ ] **Step 3: Build and smoke-test (manual)**

```bash
go build -o lynxai ./cmd/lynxai
LYNXAI_LLM_API_KEY=sk-test ./lynxai serve --addr 127.0.0.1:7879 --data-dir /tmp/lynxai-smoke &
SERVER_PID=$!
sleep 1
curl -s http://127.0.0.1:7879/healthz   # expect: ok
curl -s -X POST http://127.0.0.1:7879/credentials \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke","kind":"bearer","host":"x","bundle":{"host":"x","token":"t"}}'
echo
curl -s http://127.0.0.1:7879/credentials
kill $SERVER_PID
rm -rf /tmp/lynxai-smoke
```
Expected: `ok\n`, `{"name":"smoke"}`, `[{"name":"smoke",...}]`.

- [ ] **Step 4: Commit**

```bash
git add cmd/lynxai/
git commit -m "feat(cmd): lynxai serve subcommand wiring all deps"
```

---

## Task 19: Dockerfile

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1: Write `.dockerignore`**

```dockerignore
.git
*.db
*.db-journal
master.key
.lynxai/
dist/
docs/
*.md
!README.md
```

- [ ] **Step 2: Write the Dockerfile**

Multi-stage: build the binary with Go, ship it on an image that already has Chromium installed.

```dockerfile
# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lynxai ./cmd/lynxai

FROM chromedp/headless-shell:latest
# headless-shell ships a Chromium-equivalent binary; chromedp finds it on PATH.
COPY --from=build /out/lynxai /usr/local/bin/lynxai
ENV LYNXAI_DATA_DIR=/data
VOLUME /data
EXPOSE 7878
ENTRYPOINT ["/usr/local/bin/lynxai"]
CMD ["serve", "--addr", "0.0.0.0:7878", "--data-dir", "/data"]
```

- [ ] **Step 3: Build the image locally**

```bash
docker build -t lynxai:dev .
docker images lynxai:dev
```
Expected: image builds successfully.

- [ ] **Step 4: Smoke-test the container**

```bash
docker run -d --name lynxai-smoke -p 7878:7878 -e LYNXAI_LLM_API_KEY=sk-test lynxai:dev
sleep 2
curl -s http://127.0.0.1:7878/healthz
docker logs lynxai-smoke | head -20
docker rm -f lynxai-smoke
```
Expected: `ok`, log line "lynxai serving on http://0.0.0.0:7878".

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "feat: multi-stage Dockerfile with bundled headless Chromium"
```

---

## Task 20: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace `README.md` with the real content**

```markdown
# lynxai

> Self-hostable, AI-native headless browser. The access layer for AI agents in tools where the only door is a human's browser session.

[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL%20v3%2B-blue.svg)](LICENSE)

`lynxai` is a free alternative to hosted browser infrastructure (Browserbase, etc.) for AI agents. It runs as a small HTTP server you self-host, with an encrypted credential vault for the sites your agent needs to access, and LLM-driven extraction so agents get clean JSON instead of HTML.

## Why

Most AI agent integrations stop at "what has an API or MCP." The human developer has a much larger surface — every SaaS tool they log into. lynxai opens that surface to the agent: store the credentials once, fetch and extract on demand.

The bootstrapping case is the most interesting: an agent uses lynxai's future `drive` endpoint to obtain an API key from a vendor's UI, lynxai stores the key, and from then on the agent calls the vendor's API directly. The expensive browser-driven bootstrap runs once; cheap API calls run forever after.

See [`docs/superpowers/specs/2026-05-22-lynxai-v1-design.md`](docs/superpowers/specs/2026-05-22-lynxai-v1-design.md) for the full design.

## Quickstart (Docker)

```bash
docker run -d -p 7878:7878 \
  -e LYNXAI_LLM_API_KEY=sk-...  \
  -v lynxai-data:/data \
  ghcr.io/carriedworlduniverse/lynxai:latest
```

The default LLM provider is DeepSeek (cheap, OpenAI-API-compatible). Drop in a DeepSeek API key and you're running.

## Quickstart (binary)

```bash
go install github.com/CarriedWorldUniverse/lynxai/cmd/lynxai@latest
export LYNXAI_LLM_API_KEY=sk-...
lynxai serve --addr 127.0.0.1:7878
```

You'll need Chromium installed on PATH (or available in the default chromedp lookup locations).

## API (v1)

Two endpoints do the work:

```bash
# Fetch — page as cleaned markdown
curl -X POST http://localhost:7878/fetch \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'

# Extract — LLM-driven structured extraction
curl -X POST http://localhost:7878/extract \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://news.ycombinator.com",
    "schema": {
      "type": "object",
      "properties": {
        "stories": {
          "type": "array",
          "items": {"type":"object","properties":{"title":{"type":"string"},"url":{"type":"string"}}}
        }
      }
    }
  }'
```

Plus credential CRUD:

```bash
# Store a bearer token, scoped to a host
curl -X POST http://localhost:7878/credentials \
  -d '{"name":"github-mine","kind":"bearer","host":"api.github.com","bundle":{"host":"api.github.com","token":"ghp_..."}}'

# Use it
curl -X POST http://localhost:7878/fetch \
  -d '{"url":"https://api.github.com/user","credential":{"name":"github-mine"}}'
```

Supported credential kinds (v1): `basic`, `bearer`, `cookies`, `form`.

## Security

- The API has **no built-in authentication.** Bind to loopback (default) or put it behind your own reverse proxy.
- Credentials are stored encrypted at rest (AES-256-GCM, HKDF-derived from a `master.key` file with 0600 perms).
- Bundle data never leaves the server: clients reference credentials by name only on `/fetch` and `/extract`.
- Every credential use writes an audit row (name, request URL, outcome) — bundle contents are never logged.

## License

AGPL-3.0-or-later. See [`LICENSE`](LICENSE).

If you fork lynxai and run it as a network-accessible service, you must publish your source. See [the license rationale in the spec](docs/superpowers/specs/2026-05-22-lynxai-v1-design.md#license-rationale-agpl-30-or-later).

## Related

- [`bridle`](https://github.com/CarriedWorldUniverse/bridle) — the Go LLM harness lynxai uses for the `extract` endpoint
- [lynx](https://lynx.invisible-island.net/) — the 25-year-old text browser whose `-dump` mode is lynxai's design ancestor. We maintain a separate fork for upstream contributions (bugs/security patches).
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README with quickstart, API examples, security notes"
```

---

## Post-implementation: verification

Before declaring v1 done, run end-to-end:

- [ ] All unit tests pass: `go test ./...`
- [ ] Integration tests pass (Chromium required): `go test -tags integration ./...`
- [ ] Docker image builds: `docker build -t lynxai:dev .`
- [ ] Container responds to `/healthz`
- [ ] Manual smoke test of `/fetch` against a public page (e.g., `https://example.com`)
- [ ] Manual smoke test of `/extract` against the same page with a `{"type":"object","properties":{"title":{"type":"string"}}}` schema
- [ ] Credential CRUD round-trip via curl

---

## Notes for the implementer

- **Do not skip the TDD cycle.** Even when a task feels mechanical (handler glue), write the failing test first. The test corpus is the proof that v1 works.
- **Tests against bridle and chromedp are the most likely to drift.** Bridle's exported API is the active project's current state; if a symbol name has changed, fix the call site to match — the *intent* of the test (input shape, expected output) doesn't change.
- **Commit messages follow the conventional-commits style** used in the examples (`feat:`, `test:`, `docs:`, `fix:`, scoped where useful).
- **All files written by Edit/Write — no `cat <<EOF`.** Use the Edit tool for any changes to files already created.
- **Do not delete the `.gitignore` entries for `*.db` / `master.key`.** Several smoke-test commands generate those locally.
