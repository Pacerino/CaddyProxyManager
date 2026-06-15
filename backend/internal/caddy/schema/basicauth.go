package schema

import (
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func init() {
	Register(&Schema{
		ModuleID:    "http.handlers.authentication",
		Title:       "HTTP Basic Authentication",
		Description: "Protect this host with HTTP basic auth. Enter a plaintext password; CPM stores only the bcrypt hash Caddy needs.",
		Scopes:      []Scope{ScopeHost},
		Fields: []Field{
			{
				Key:      "username",
				Label:    "Username",
				Type:     FieldString,
				Required: true,
			},
			{
				Key:         "password",
				Label:       "Password",
				Description: "Plaintext password. It is bcrypt-hashed before being stored.",
				Type:        FieldSecret,
				Required:    true,
			},
		},
		buildHandler: func(values map[string]any) (map[string]any, error) {
			username, _ := values["username"].(string)
			password, _ := values["password"].(string)
			if username == "" || password == "" {
				return nil, fmt.Errorf("username and password are required")
			}

			// Caddy's http_basic expects the account password as a
			// base64-encoded bcrypt hash.
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			encoded := base64.StdEncoding.EncodeToString(hash)

			return map[string]any{
				"handler": "authentication",
				"providers": map[string]any{
					"http_basic": map[string]any{
						"accounts": []any{
							map[string]any{
								"username": username,
								"password": encoded,
							},
						},
					},
				},
			}, nil
		},
	})
}
