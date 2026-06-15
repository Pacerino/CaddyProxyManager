package schema

import (
	"encoding/json"
	"testing"
)

func TestRegistryHasKnownPlugins(t *testing.T) {
	if _, ok := Get("dns.providers.cloudflare"); !ok {
		t.Fatal("expected cloudflare schema to be registered")
	}
	if len(All()) < 2 {
		t.Fatalf("expected at least 2 registered schemas, got %d", len(All()))
	}
}

func TestValidateRequired(t *testing.T) {
	sc, _ := Get("dns.providers.cloudflare")
	errs := sc.Validate(map[string]any{})
	if errs["api_token"] == "" {
		t.Fatal("expected api_token required error")
	}
}

func TestValidateType(t *testing.T) {
	sc, _ := Get("dns.providers.cloudflare")
	errs := sc.Validate(map[string]any{"api_token": 123})
	if errs["api_token"] == "" {
		t.Fatal("expected type error for non-string secret")
	}
}

func TestBuildRendersFragment(t *testing.T) {
	sc, _ := Get("dns.providers.cloudflare")
	raw, err := sc.Build(map[string]any{"api_token": "secret-token"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal built fragment: %v", err)
	}
	issuers, ok := obj["issuers"].([]any)
	if !ok || len(issuers) != 1 {
		t.Fatalf("expected 1 issuer, got %v", obj["issuers"])
	}
}

func TestScopes(t *testing.T) {
	cf, _ := Get("dns.providers.cloudflare")
	if !cf.HasScope(ScopeGlobal) || cf.HasScope(ScopeHost) {
		t.Errorf("cloudflare should be global-only, scopes=%v", cf.Scopes)
	}
	auth, _ := Get("http.handlers.authentication")
	if !auth.HasScope(ScopeHost) || auth.HasScope(ScopeGlobal) {
		t.Errorf("auth should be host-only, scopes=%v", auth.Scopes)
	}

	if len(WithScope(ScopeHost)) < 1 {
		t.Error("expected at least one host-scope schema")
	}
	if len(WithScope(ScopeGlobal)) < 1 {
		t.Error("expected at least one global-scope schema")
	}
}

func TestBuildHandlerHashesPassword(t *testing.T) {
	auth, _ := Get("http.handlers.authentication")
	handler, err := auth.BuildHandler(map[string]any{"username": "bob", "password": "s3cret"})
	if err != nil {
		t.Fatalf("BuildHandler: %v", err)
	}
	providers := handler["providers"].(map[string]any)
	basic := providers["http_basic"].(map[string]any)
	accounts := basic["accounts"].([]any)
	acc := accounts[0].(map[string]any)
	if acc["username"] != "bob" {
		t.Errorf("username = %v", acc["username"])
	}
	pw, _ := acc["password"].(string)
	if pw == "" || pw == "s3cret" {
		t.Error("password should be a non-plaintext hash")
	}
}

func TestBuildHandlerOnGlobalSchemaFails(t *testing.T) {
	cf, _ := Get("dns.providers.cloudflare")
	if _, err := cf.BuildHandler(map[string]any{"api_token": "x"}); err == nil {
		t.Error("expected error building handler for a global-only schema")
	}
}

func TestCheckTypeAllFieldTypes(t *testing.T) {
	s := &Schema{
		ModuleID: "test.types",
		Scopes:   []Scope{ScopeGlobal},
		Fields: []Field{
			{Key: "s", Type: FieldString, Required: true},
			{Key: "i", Type: FieldInt, Required: true},
			{Key: "b", Type: FieldBool, Required: true},
			{Key: "sec", Type: FieldSecret, Required: true},
		},
	}

	// All correct types -> no errors.
	ok := s.Validate(map[string]any{
		"s": "txt", "i": float64(3), "b": true, "sec": "secret",
	})
	if len(ok) != 0 {
		t.Fatalf("expected no errors, got %v", ok)
	}

	// All wrong types -> one error each.
	bad := s.Validate(map[string]any{
		"s": 1, "i": 1.5, "b": "nope", "sec": 2,
	})
	for _, k := range []string{"s", "i", "b", "sec"} {
		if bad[k] == "" {
			t.Errorf("expected type error for %q", k)
		}
	}
}

func TestValidationErrorMessage(t *testing.T) {
	ve := &ValidationError{Fields: map[string]string{"x": "is required"}}
	if ve.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBuildReturnsValidationError(t *testing.T) {
	sc, _ := Get("dns.providers.cloudflare")
	_, err := sc.Build(map[string]any{})
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Fields["api_token"] == "" {
		t.Fatal("expected api_token in validation error fields")
	}
}
