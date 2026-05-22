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
// failure it returns a populated *errorResponse and writes an audit row via
// Vault.RecordUse with the outcome string.
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
