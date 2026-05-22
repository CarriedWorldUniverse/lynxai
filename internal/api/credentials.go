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
		if err := d.Vault.Put(r.Context(), body.Name, body.Kind, body.Host, []byte(body.Bundle)); err != nil {
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
