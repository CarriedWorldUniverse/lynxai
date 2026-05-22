package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

// FetcherEngine is the subset of *engine.Engine the api layer needs.
// Defined as an interface so handlers can be unit-tested with a stub.
type FetcherEngine interface {
	Fetch(ctx context.Context, req engine.FetchRequest) (*engine.FetchResult, error)
}

// Deps bundles the runtime dependencies the API handlers need.
type Deps struct {
	Vault     *creds.Vault
	Engine    FetcherEngine
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
