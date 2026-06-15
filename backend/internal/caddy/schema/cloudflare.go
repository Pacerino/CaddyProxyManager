package schema

func init() {
	Register(&Schema{
		ModuleID:    "dns.providers.cloudflare",
		Title:       "Cloudflare DNS",
		Description: "DNS provider used for ACME DNS-01 challenges via the Cloudflare API.",
		Scopes:      []Scope{ScopeGlobal},
		Path:        "apps/tls/automation/policies",
		Fields: []Field{
			{
				Key:         "api_token",
				Label:       "API Token",
				Description: "Cloudflare API token with DNS edit permissions.",
				Type:        FieldSecret,
				Required:    true,
			},
		},
		build: func(values map[string]any) (any, error) {
			// Caddy's tls automation policy referencing the cloudflare DNS
			// provider for the ACME issuer's DNS-01 challenge.
			return map[string]any{
				"issuers": []any{
					map[string]any{
						"module": "acme",
						"challenges": map[string]any{
							"dns": map[string]any{
								"provider": map[string]any{
									"name":      "cloudflare",
									"api_token": values["api_token"],
								},
							},
						},
					},
				},
			}, nil
		},
	})
}
