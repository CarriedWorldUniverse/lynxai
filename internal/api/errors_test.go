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
