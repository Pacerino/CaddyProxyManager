package supervisor

import (
	"os"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

// TestMain silences the warnings crash and fallback tests log on purpose.
func TestMain(m *testing.M) {
	_ = logger.Configure(&logger.Config{LogThreshold: logger.ErrorLevel, Formatter: "json"})
	os.Exit(m.Run())
}
