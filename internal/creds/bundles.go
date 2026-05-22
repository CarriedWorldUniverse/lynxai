package creds

import (
	"encoding/json"
	"fmt"
)

// Kind is the credential class. The bundle JSON shape is per-kind.
type Kind string

const (
	KindBasic   Kind = "basic"
	KindBearer  Kind = "bearer"
	KindCookies Kind = "cookies"
	KindForm    Kind = "form"
)

func KnownKinds() []Kind {
	return []Kind{KindBasic, KindBearer, KindCookies, KindForm}
}

// BasicBundle: HTTP Basic auth.
type BasicBundle struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// BearerBundle: HTTP Bearer / Authorization header token.
type BearerBundle struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

// Cookie: one entry in a cookie jar credential.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

// CookiesBundle: pre-baked cookie jar applied before navigation.
type CookiesBundle struct {
	Host    string   `json:"host"`
	Cookies []Cookie `json:"cookies"`
}

// FormFields: the input names and values for a form-login POST.
type FormFields struct {
	UserField string `json:"user_field"`
	PassField string `json:"pass_field"`
	User      string `json:"user"`
	Password  string `json:"password"`
}

// FormBundle: form-login credentials. lynxai POSTs to LoginURL once, captures
// SuccessCookieName, and seeds it into the browser context before navigation.
type FormBundle struct {
	Host              string     `json:"host"`
	LoginURL          string     `json:"login_url"`
	Method            string     `json:"method"`
	Fields            FormFields `json:"fields"`
	SuccessCookieName string     `json:"success_cookie_name"`
}

// ValidateBundle verifies that data is a well-formed JSON bundle for kind.
// Returns nil on success, descriptive error otherwise.
func ValidateBundle(kind Kind, data []byte) error {
	switch kind {
	case KindBasic:
		var b BasicBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse basic bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("basic bundle: host required")
		}
		if b.User == "" {
			return fmt.Errorf("basic bundle: user required")
		}
		if b.Password == "" {
			return fmt.Errorf("basic bundle: password required")
		}
		return nil
	case KindBearer:
		var b BearerBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse bearer bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("bearer bundle: host required")
		}
		if b.Token == "" {
			return fmt.Errorf("bearer bundle: token required")
		}
		return nil
	case KindCookies:
		var b CookiesBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse cookies bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("cookies bundle: host required")
		}
		if len(b.Cookies) == 0 {
			return fmt.Errorf("cookies bundle: at least one cookie required")
		}
		for i, c := range b.Cookies {
			if c.Name == "" {
				return fmt.Errorf("cookies bundle: cookie[%d] name required", i)
			}
			if c.Value == "" {
				return fmt.Errorf("cookies bundle: cookie[%d] value required", i)
			}
		}
		return nil
	case KindForm:
		var b FormBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse form bundle: %w", err)
		}
		if b.Host == "" {
			return fmt.Errorf("form bundle: host required")
		}
		if b.LoginURL == "" {
			return fmt.Errorf("form bundle: login_url required")
		}
		if b.SuccessCookieName == "" {
			return fmt.Errorf("form bundle: success_cookie_name required")
		}
		if b.Fields.UserField == "" || b.Fields.PassField == "" {
			return fmt.Errorf("form bundle: fields.user_field and fields.pass_field required")
		}
		return nil
	default:
		return fmt.Errorf("unknown credential kind: %q (known: %v)", kind, KnownKinds())
	}
}
