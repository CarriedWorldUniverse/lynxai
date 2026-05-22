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
