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
