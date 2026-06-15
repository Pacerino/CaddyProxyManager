package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	c "github.com/Pacerino/CaddyProxyManager/internal/api/context"
)

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	return env
}

func TestResultResponseJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ResultResponseJSON(rec, req, http.StatusOK, map[string]any{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	result := env["result"].(map[string]any)
	if result["hello"] != "world" {
		t.Errorf("result = %v", result)
	}
}

func TestResultResponseJSONPrettyPrint(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), c.PrettyPrintCtxKey, true))
	ResultResponseJSON(rec, req, http.StatusOK, map[string]any{"a": 1})
	// Pretty output contains newlines/indentation.
	if !json.Valid(rec.Body.Bytes()) {
		t.Error("pretty output should still be valid JSON")
	}
}

func TestResultErrorJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ResultErrorJSON(rec, req, http.StatusBadRequest, "bad", map[string]string{"field": "x"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	errObj := env["error"].(map[string]any)
	if errObj["message"] != "bad" {
		t.Errorf("error = %v", errObj)
	}
}

func TestValidateRequestSchema(t *testing.T) {
	schema := `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`

	// Valid payload.
	errs, err := ValidateRequestSchema(schema, []byte(`{"name":"bob"}`))
	if err != nil || len(errs) != 0 {
		t.Fatalf("valid payload: errs=%v err=%v", errs, err)
	}

	// Missing required field.
	errs, _ = ValidateRequestSchema(schema, []byte(`{}`))
	if len(errs) == 0 {
		t.Error("expected schema validation errors for missing field")
	}

	// Non-JSON body.
	if _, err := ValidateRequestSchema(schema, []byte("not json")); err != ErrInvalidJSON {
		t.Errorf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestIsJSON(t *testing.T) {
	if !isJSON([]byte(`{"a":1}`)) {
		t.Error("object should be json")
	}
	if isJSON([]byte("nope")) {
		t.Error("garbage should not be json")
	}
}
