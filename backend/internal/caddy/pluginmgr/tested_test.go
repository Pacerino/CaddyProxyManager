package pluginmgr

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestTestedPackages_MatchCI guards against drift between the -dns image's
// provider list in ci.yml and TestedPackages.
func TestTestedPackages_MatchCI(t *testing.T) {
	f, err := os.Open("../../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var ci []string
	inDNS := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "- flavor:") {
			inDNS = strings.TrimSpace(strings.TrimPrefix(line, "- flavor:")) == "dns"
			continue
		}
		if inDNS && strings.HasPrefix(line, "github.com/caddy-dns/") {
			ci = append(ci, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	var want []string
	for _, p := range TestedPackages {
		if p != JSONSchemaPackage {
			want = append(want, p)
		}
	}
	sort.Strings(ci)
	sort.Strings(want)
	if len(ci) == 0 || strings.Join(ci, "\n") != strings.Join(want, "\n") {
		t.Errorf("ci.yml dns providers differ from TestedPackages\nci:   %v\ngo:   %v", ci, want)
	}
}

func TestIsTested(t *testing.T) {
	if !IsTested("github.com/caddy-dns/cloudflare") || !IsTested(JSONSchemaPackage) {
		t.Error("expected tested")
	}
	if IsTested("github.com/x/unknown") {
		t.Error("unexpected tested")
	}
}
