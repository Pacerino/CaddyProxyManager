package pluginmgr

// JSONSchemaPackage is always part of every build request; CPM needs it to
// generate the DNS provider forms.
const JSONSchemaPackage = "github.com/abiosoft/caddy-json-schema"

// TestedPackages are the plugins CPM ships in its -dns image and has tested
// with the DNS challenge forms. Keep in sync with .github/workflows/ci.yml.
var TestedPackages = []string{
	"github.com/caddy-dns/cloudflare",
	"github.com/caddy-dns/duckdns",
	"github.com/caddy-dns/porkbun",
	"github.com/caddy-dns/route53",
	"github.com/caddy-dns/digitalocean",
	"github.com/caddy-dns/hetzner",
	"github.com/caddy-dns/ovh",
	"github.com/caddy-dns/namecheap",
	"github.com/caddy-dns/gandi",
	"github.com/caddy-dns/desec",
	"github.com/caddy-dns/netcup",
	"github.com/caddy-dns/ionos",
	"github.com/caddy-dns/linode",
	"github.com/caddy-dns/godaddy",
	"github.com/caddy-dns/rfc2136",
	"github.com/caddy-dns/acmedns",
	"github.com/caddy-dns/bunny",
	"github.com/caddy-dns/inwx",
	"github.com/caddy-dns/azure",
	"github.com/caddy-dns/googleclouddns",
	"github.com/caddy-dns/namesilo",
	"github.com/caddy-dns/powerdns",
	"github.com/caddy-dns/transip",
	"github.com/caddy-dns/alidns",
	"github.com/caddy-dns/he",
	JSONSchemaPackage,
}

// IsTested reports whether path is on the tested list.
func IsTested(path string) bool {
	for _, p := range TestedPackages {
		if p == path {
			return true
		}
	}
	return false
}
