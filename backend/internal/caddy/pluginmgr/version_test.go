package pluginmgr

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperation_PreviousCaddyVersion(t *testing.T) {
	e := newEnv(t)
	e.builds.version = "v2.99.0"
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepDone || op.PreviousCaddyVersion != shippedVer || op.CaddyVersion != "v2.99.0" {
		t.Fatalf("op: %+v", op)
	}
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"previousCaddyVersion":"`+shippedVer+`"`) {
		t.Fatalf("json: %s", data)
	}
}

func TestOperation_PreviousCaddyVersionOnUpdate(t *testing.T) {
	e := newEnv(t)
	old := shippedBin()
	old.Version = "v2.10.0"
	promoteFake(t, e, old)
	op := e.run(t, OpUpdate, "")
	if op.Step != StepDone || op.PreviousCaddyVersion != "v2.10.0" || op.CaddyVersion != shippedVer {
		t.Fatalf("op: %+v", op)
	}
}
