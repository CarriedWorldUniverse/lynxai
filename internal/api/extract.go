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
		// Credential (if any) was applied — record once regardless of fetch outcome.
		recordOutcome(r.Context(), d, body.Credential, body.URL, "ok")
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
