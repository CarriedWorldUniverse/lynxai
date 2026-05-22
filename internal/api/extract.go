package api

import "net/http"

func extractHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, ErrCodeInternal, "extract handler not yet implemented", nil)
	}
}
