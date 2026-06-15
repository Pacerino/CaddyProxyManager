// Package schema describes how known Caddy modules (plugins) are configured
// through CPM. Each registered Schema drives a typed form in the UI and is
// validated before being applied. Modules without a registered schema fall
// back to a raw JSON editor.
package schema

import (
	"encoding/json"
	"fmt"
	"sort"
)

// FieldType enumerates the input types a schema field can render as.
type FieldType string

const (
	FieldString FieldType = "string"
	FieldInt    FieldType = "int"
	FieldBool   FieldType = "bool"
	// FieldSecret is a string that the UI should mask.
	FieldSecret FieldType = "secret"
)

// Scope describes where a plugin is configured.
type Scope string

const (
	// ScopeGlobal: configured once for the whole Caddy instance (e.g. a DNS
	// provider used for TLS issuance). Managed on the Plugins page.
	ScopeGlobal Scope = "global"
	// ScopeHost: configured per host, rendered as a route handler attached to
	// the host's route. Managed inside the host editor.
	ScopeHost Scope = "host"
)

// Field describes a single configurable property of a module.
type Field struct {
	// Key is the JSON property name within the module's config object.
	Key string `json:"key"`
	// Label and Description are human-facing UI hints.
	Label       string    `json:"label"`
	Description string    `json:"description,omitempty"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Default     any       `json:"default,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
}

// Schema is the typed configuration descriptor for one Caddy module.
type Schema struct {
	// ModuleID is the dotted Caddy module name, e.g. "dns.providers.cloudflare".
	ModuleID string `json:"moduleId"`
	// Title and Description describe the plugin to the user.
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Scopes lists where this plugin can be configured (global, host or both).
	Scopes []Scope `json:"scopes"`
	// Path is the default admin API config path the rendered config is
	// patched into, relative to /config/. Only used for global scope.
	Path string `json:"path,omitempty"`
	// Fields are the typed inputs presented in the UI.
	Fields []Field `json:"fields"`
	// build converts validated form values into the global Caddy JSON
	// fragment. When nil, the values map is emitted as-is.
	build func(values map[string]any) (any, error)
	// buildHandler converts validated form values into a Caddy HTTP route
	// handler object, injected into a host's route. Required for host scope.
	buildHandler func(values map[string]any) (map[string]any, error)
}

// HasScope reports whether the schema supports the given scope.
func (s *Schema) HasScope(scope Scope) bool {
	for _, sc := range s.Scopes {
		if sc == scope {
			return true
		}
	}
	return false
}

var registry = map[string]*Schema{}

// Register adds a schema to the registry. It is intended to be called from
// package init functions of individual module schema files.
func Register(s *Schema) {
	registry[s.ModuleID] = s
}

// Get returns the schema for a module ID and whether one is registered.
func Get(moduleID string) (*Schema, bool) {
	s, ok := registry[moduleID]
	return s, ok
}

// All returns every registered schema, sorted by module ID.
func All() []*Schema {
	out := make([]*Schema, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleID < out[j].ModuleID })
	return out
}

// WithScope returns the registered schemas supporting the given scope, sorted
// by module ID.
func WithScope(scope Scope) []*Schema {
	out := make([]*Schema, 0)
	for _, s := range All() {
		if s.HasScope(scope) {
			out = append(out, s)
		}
	}
	return out
}

// Validate checks form values against the schema's field definitions,
// returning a map of field key -> error message for any problems.
func (s *Schema) Validate(values map[string]any) map[string]string {
	errs := map[string]string{}
	for _, f := range s.Fields {
		v, present := values[f.Key]
		if !present || v == nil || v == "" {
			if f.Required {
				errs[f.Key] = "is required"
			}
			continue
		}
		if msg := checkType(f, v); msg != "" {
			errs[f.Key] = msg
		}
	}
	return errs
}

// Build validates and converts form values into the global Caddy JSON fragment.
func (s *Schema) Build(values map[string]any) (json.RawMessage, error) {
	if errs := s.Validate(values); len(errs) > 0 {
		return nil, &ValidationError{Fields: errs}
	}
	var fragment any = values
	if s.build != nil {
		built, err := s.build(values)
		if err != nil {
			return nil, err
		}
		fragment = built
	}
	return json.Marshal(fragment)
}

// BuildHandler validates form values and renders the Caddy route handler
// object for a host-scoped plugin.
func (s *Schema) BuildHandler(values map[string]any) (map[string]any, error) {
	if s.buildHandler == nil {
		return nil, fmt.Errorf("module %q does not support host scope", s.ModuleID)
	}
	if errs := s.Validate(values); len(errs) > 0 {
		return nil, &ValidationError{Fields: errs}
	}
	return s.buildHandler(values)
}

// ValidationError carries per-field validation failures.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("schema validation failed: %v", e.Fields)
}

func checkType(f Field, v any) string {
	switch f.Type {
	case FieldString, FieldSecret:
		if _, ok := v.(string); !ok {
			return "must be a string"
		}
	case FieldInt:
		// JSON numbers decode to float64.
		switch n := v.(type) {
		case float64:
			if n != float64(int64(n)) {
				return "must be an integer"
			}
		case int, int64:
		default:
			return "must be an integer"
		}
	case FieldBool:
		if _, ok := v.(bool); !ok {
			return "must be a boolean"
		}
	}
	return ""
}
