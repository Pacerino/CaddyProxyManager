package pluginmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseInstalled_Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/list-modules.json")
	if err != nil {
		t.Fatal(err)
	}
	pkgs := ParseInstalled(data)

	var duck bool
	for i, p := range pkgs {
		if p.Path == "github.com/caddyserver/caddy/v2" {
			t.Fatalf("standard package must be excluded: %+v", p)
		}
		if i > 0 && pkgs[i-1].Path > p.Path {
			t.Fatalf("not sorted: %s before %s", pkgs[i-1].Path, p.Path)
		}
		if p.Path == "github.com/caddy-dns/duckdns" {
			duck = true
			if p.Version != "v0.5.0" || len(p.Modules) != 1 || p.Modules[0] != "dns.providers.duckdns" {
				t.Fatalf("unexpected duckdns entry: %+v", p)
			}
		}
	}
	if !duck {
		t.Fatal("duckdns missing")
	}
	// caddy-json-schema registers only a CLI command, not a module, so it is
	// not in list-modules output; ExecInspector.Installed adds it from build-info.
}

func TestParseInstalled_Invalid(t *testing.T) {
	if got := ParseInstalled([]byte("nope")); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

const fakeCaddy = `#!/bin/sh
case "$1" in
  version) echo "v2.11.4 h1:abcdef="; ;;
  list-modules)
    echo '[{"module_name":"http","module_type":"standard","version":"v2.11.4","package_url":"github.com/caddyserver/caddy/v2"},{"module_name":"dns.providers.duckdns","module_type":"non-standard","version":"v0.5.0","package_url":"github.com/caddy-dns/duckdns"}]'
    ;;
  build-info)
    printf 'path\tcaddy\nmod\tgithub.com/caddyserver/caddy/v2\tv2.11.4\th1:x\ndep\tgithub.com/abiosoft/caddy-json-schema\tv0.0.0-2022\th1:y\ndep\tgithub.com/caddy-dns/duckdns\tv0.5.0\th1:z\ndep\tgithub.com/other/lib\tv1.0.0\t=>\tgithub.com/fork/lib\tv1.0.1\n'
    ;;
  validate)
    if [ "$2" != "--config" ] || [ "$4" != "--adapter" ] || [ -n "$5" ]; then echo "bad args: $*" >&2; exit 2; fi
    case "$3" in *.json) ;; *) echo "not json file" >&2; exit 2;; esac
    if grep -q '"bad"' "$3"; then echo "Error: decoding config: unknown field bad" >&2; exit 1; fi
    echo "Valid configuration"
    ;;
esac
`

func writeFake(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "caddy")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecInspector_Version(t *testing.T) {
	v, err := ExecInspector{}.Version(context.Background(), writeFake(t, fakeCaddy))
	if err != nil || v != "v2.11.4" {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

func TestExecInspector_Installed(t *testing.T) {
	pkgs, err := ExecInspector{}.Installed(context.Background(), writeFake(t, fakeCaddy))
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 || pkgs[0].Path != JSONSchemaPackage || pkgs[0].Version != "v0.0.0-2022" || pkgs[0].Modules != nil {
		t.Fatalf("unexpected: %+v", pkgs)
	}
	if pkgs[1].Path != "github.com/caddy-dns/duckdns" || pkgs[1].Version != "v0.5.0" {
		t.Fatalf("unexpected: %+v", pkgs)
	}
}

func TestExecInspector_Installed_BuildInfoFails(t *testing.T) {
	bin := writeFake(t, "#!/bin/sh\ncase \"$1\" in list-modules) echo '[]';; *) echo nobuild >&2; exit 1;; esac\n")
	_, err := ExecInspector{}.Installed(context.Background(), bin)
	if err == nil || !strings.Contains(err.Error(), "nobuild") {
		t.Fatalf("got %v", err)
	}
}

func TestParseBuildInfoDeps_Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/build-info.txt")
	if err != nil {
		t.Fatal(err)
	}
	deps := ParseBuildInfoDeps(data)
	if deps[JSONSchemaPackage] != "v0.0.0-20220621031927-c4d6e132f3af" {
		t.Fatalf("json-schema: %q", deps[JSONSchemaPackage])
	}
	if deps["github.com/caddy-dns/duckdns"] != "v0.5.0" {
		t.Fatalf("duckdns: %q", deps["github.com/caddy-dns/duckdns"])
	}
	if got := ParseBuildInfoDeps([]byte("dep\ta\tv1\t=>\tb\tv2\n"))["a"]; got != "v1" {
		t.Fatalf("replacement: %q", got)
	}
}

func TestExecInspector_Validate(t *testing.T) {
	bin := writeFake(t, fakeCaddy)
	if err := (ExecInspector{}).Validate(context.Background(), bin, []byte(`{"apps":{}}`)); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	err := ExecInspector{}.Validate(context.Background(), bin, []byte(`{"bad":1}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field bad") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestExecInspector_ErrorIncludesStderr(t *testing.T) {
	bin := writeFake(t, "#!/bin/sh\necho boom >&2\nexit 3\n")
	_, err := ExecInspector{}.Version(context.Background(), bin)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

func TestExecInspector_Timeout(t *testing.T) {
	old := inspectTimeout
	inspectTimeout = 200 * time.Millisecond
	defer func() { inspectTimeout = old }()

	bin := writeFake(t, "#!/bin/sh\nsleep 5\n")
	start := time.Now()
	_, err := ExecInspector{}.Version(context.Background(), bin)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("did not time out promptly: %v", time.Since(start))
	}
}
