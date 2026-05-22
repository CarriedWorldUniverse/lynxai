package api

import "net/http"

func fetchHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, ErrCodeInternal, "fetch handler not yet implemented", nil)
	}
}
