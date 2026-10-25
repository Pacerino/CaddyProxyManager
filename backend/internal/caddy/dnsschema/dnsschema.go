// Package dnsschema generates form fields for Caddy DNS provider plugins from
// Caddy's JSON schema file (preferred) or the caddyserver.com docs API.
package dnsschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

const (
	providerPrefix = "dns.providers."

	// SourceBinary, SourceDocs, SourceOverride and SourceNone say where a
	// provider's fields came from. SourceNone means raw JSON editing.
	SourceBinary   = "binary"
	SourceDocs     = "docs"
	SourceOverride = "override"
	SourceNone     = "none"

	defaultDocsBaseURL = "https://caddyserver.com/api/docs/config/apps/tls/automation/policies/issuers/acme/challenges/dns/provider/"
)

// Provider describes one DNS provider module and its configurable fields.
type Provider struct {
	ModuleID    string         `json:"moduleId"`
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Fields      []schema.Field `json:"fields"`
	Source      string         `json:"source"`
}

var labelWords = map[string]string{
	"api": "API", "id": "ID", "url": "URL", "dns": "DNS", "ttl": "TTL",
}

// humanize turns "api_token" into "API Token".
func humanize(s string) string {
	words := strings.Split(s, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		if special, ok := labelWords[strings.ToLower(w)]; ok {
			words[i] = special
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// IsSecretKey reports whether a config key looks like a credential: it contains
// "token", "secret", "password" or "key" (case-insensitive).
func IsSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, hint := range []string{"token", "secret", "password", "key"} {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// fieldType picks Secret for keys that look like credentials, else the given
// type when it is a plain string.
func fieldType(key string, t schema.FieldType) schema.FieldType {
	if t == schema.FieldString && IsSecretKey(key) {
		return schema.FieldSecret
	}
	return t
}

func newProvider(moduleID, source string) Provider {
	name := strings.TrimPrefix(moduleID, providerPrefix)
	return Provider{ModuleID: moduleID, Name: name, Title: humanize(name), Source: source, Fields: []schema.Field{}}
}

// schemaProp is a property of a definition in Caddy's JSON schema.
type schemaProp struct {
	Description string          `json:"description"`
	Type        json.RawMessage `json:"type"`
	Items       *schemaProp     `json:"items"`
}

type schemaDef struct {
	Description string                `json:"description"`
	Properties  map[string]schemaProp `json:"properties"`
}

// jsonSchemaType maps a JSON schema property to a field type.
func jsonSchemaType(p schemaProp) schema.FieldType {
	var single string
	var multi []string
	if json.Unmarshal(p.Type, &single) == nil {
		multi = []string{single}
	} else if json.Unmarshal(p.Type, &multi) != nil {
		return schema.FieldJSON
	}
	t := ""
	for _, m := range multi {
		if m != "null" {
			t = m
			break
		}
	}
	switch t {
	case "string":
		return schema.FieldString
	case "number", "integer":
		return schema.FieldInt
	case "boolean":
		return schema.FieldBool
	case "array":
		if p.Items != nil && jsonSchemaType(*p.Items) == schema.FieldString {
			return schema.FieldStringList
		}
	}
	return schema.FieldJSON
}

// cleanDescription drops the "<key>: <type>" and "Module: <id>" header lines
// and bare pkg.go.dev URL lines Caddy prepends to descriptions.
func cleanDescription(desc string) string {
	var kept []string
	for i, line := range strings.Split(desc, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case i == 0 && strings.Contains(trimmed, ": "):
			continue
		case strings.HasPrefix(trimmed, "Module: "):
			continue
		case strings.HasPrefix(trimmed, "https://pkg.go.dev/") && !strings.Contains(trimmed, " "):
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// ParseSchemaFile extracts the dns.providers.* definitions from Caddy's JSON
// schema, keyed by module ID.
func ParseSchemaFile(data []byte) (map[string]Provider, error) {
	var doc struct {
		Definitions map[string]schemaDef `json:"definitions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing caddy schema: %w", err)
	}
	out := map[string]Provider{}
	for id, def := range doc.Definitions {
		if !strings.HasPrefix(id, providerPrefix) {
			continue
		}
		p := newProvider(id, SourceBinary)
		p.Description = cleanDescription(def.Description)
		keys := make([]string, 0, len(def.Properties))
		for k := range def.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			prop := def.Properties[k]
			p.Fields = append(p.Fields, schema.Field{
				Key:         k,
				Label:       humanize(k),
				Description: cleanDescription(prop.Description),
				Type:        fieldType(k, jsonSchemaType(prop)),
			})
		}
		out[id] = p
	}
	return out, nil
}

// docsType is a type node in the caddyserver.com docs API.
type docsType struct {
	Type  string    `json:"type"`
	Elems *docsType `json:"elems"`
}

// ParseDocsAPI builds a provider from a caddyserver.com docs API response.
func ParseDocsAPI(moduleID string, data []byte) (Provider, error) {
	var doc struct {
		Result struct {
			Structure struct {
				Fields []struct {
					Key   string   `json:"key"`
					Value docsType `json:"value"`
				} `json:"struct_fields"`
				Doc string `json:"doc"`
			} `json:"structure"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Provider{}, fmt.Errorf("parsing docs response: %w", err)
	}
	p := newProvider(moduleID, SourceDocs)
	p.Description = strings.TrimSpace(doc.Result.Structure.Doc)
	for _, f := range doc.Result.Structure.Fields {
		field := schema.Field{Key: f.Key, Label: humanize(f.Key)}
		switch f.Value.Type {
		case "string":
			field.Type = schema.FieldString
		case "bool":
			field.Type = schema.FieldBool
		case "int", "float":
			field.Type = schema.FieldInt
		case "duration":
			field.Type = schema.FieldString
			field.Placeholder = "30s"
		case "array":
			field.Type = schema.FieldJSON
			if f.Value.Elems != nil && f.Value.Elems.Type == "string" {
				field.Type = schema.FieldStringList
			}
		default:
			field.Type = schema.FieldJSON
		}
		field.Type = fieldType(f.Key, field.Type)
		p.Fields = append(p.Fields, field)
	}
	return p, nil
}

// Resolver discovers DNS provider modules in the running Caddy build and
// resolves their fields: registered override, then the schema file, then the
// docs API, else raw JSON.
type Resolver struct {
	SchemaFile  string
	DocsBaseURL string
	Client      *http.Client
	ListModules func() ([]caddy.Module, error)

	// override looks up a registered schema; nil means schema.Get. Tests
	// replace it so the built-in registrations don't shadow fixtures.
	override func(moduleID string) (*schema.Schema, bool)

	mu        sync.Mutex
	docsCache map[string]*Provider // nil value caches a miss
}

// NewResolver returns a Resolver using the configured schema file and the
// public docs API.
func NewResolver() *Resolver {
	return &Resolver{
		SchemaFile:  config.Configuration.Caddy.SchemaFile,
		DocsBaseURL: defaultDocsBaseURL,
		Client:      &http.Client{Timeout: 5 * time.Second},
		ListModules: caddy.ListModules,
	}
}

// Providers returns one entry per dns.providers.* module in the build, sorted
// by module ID.
func (r *Resolver) Providers(ctx context.Context) ([]Provider, error) {
	modules, err := r.ListModules()
	if err != nil {
		return nil, err
	}
	fromFile := r.loadSchemaFile()

	var out []Provider
	for _, m := range modules {
		if !strings.HasPrefix(m.ID, providerPrefix) {
			continue
		}
		out = append(out, r.resolve(ctx, m.ID, fromFile))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleID < out[j].ModuleID })
	return out, nil
}

// loadSchemaFile returns the providers in the schema file. A missing or
// unreadable file is not an error: resolution falls through to the docs API.
func (r *Resolver) loadSchemaFile() map[string]Provider {
	if r.SchemaFile == "" {
		return nil
	}
	data, err := os.ReadFile(r.SchemaFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("dnsschema: reading %s: %v", r.SchemaFile, err)
		}
		return nil
	}
	providers, err := ParseSchemaFile(data)
	if err != nil {
		logger.Warn("dnsschema: %v", err)
		return nil
	}
	return providers
}

func (r *Resolver) resolve(ctx context.Context, id string, fromFile map[string]Provider) Provider {
	lookup := r.override
	if lookup == nil {
		lookup = schema.Get
	}
	if s, ok := lookup(id); ok {
		p := newProvider(id, SourceOverride)
		if s.Title != "" {
			p.Title = s.Title
		}
		p.Description = s.Description
		p.Fields = s.Fields
		return p
	}
	if p, ok := fromFile[id]; ok {
		return p
	}
	if p := r.fromDocs(ctx, id); p != nil {
		return *p
	}
	return newProvider(id, SourceNone)
}

// fromDocs fetches a provider from the docs API. Hits and definitive misses
// are cached; transient failures (transport errors, cancelled contexts) are not.
func (r *Resolver) fromDocs(ctx context.Context, id string) *Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.docsCache[id]; ok {
		return p
	}
	p, definitive := r.fetchDocs(ctx, id)
	if definitive && ctx.Err() == nil {
		if r.docsCache == nil {
			r.docsCache = map[string]*Provider{}
		}
		r.docsCache[id] = p
	}
	return p
}

// fetchDocs returns the provider (nil on a miss) and whether the outcome is
// definitive, i.e. safe to cache.
func (r *Resolver) fetchDocs(ctx context.Context, id string) (*Provider, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.DocsBaseURL+strings.TrimPrefix(id, providerPrefix), nil)
	if err != nil {
		return nil, false
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, true
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false
	}
	p, err := ParseDocsAPI(id, body)
	if err != nil {
		return nil, true
	}
	return &p, true
}
