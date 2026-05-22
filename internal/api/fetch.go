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
