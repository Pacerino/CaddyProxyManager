package database

import (
	"encoding/json"
	"testing"
)

func TestJSONValue(t *testing.T) {
	// Empty -> "null".
	v, err := JSON(nil).Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != "null" {
		t.Errorf("empty Value() = %v, want null", v)
	}

	v, err = JSON(`{"a":1}`).Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != `{"a":1}` {
		t.Errorf("Value() = %v", v)
	}
}

func TestJSONScan(t *testing.T) {
	var j JSON
	if err := j.Scan([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if string(j) != `{"a":1}` {
		t.Errorf("Scan bytes = %s", j)
	}

	if err := j.Scan(`{"b":2}`); err != nil {
		t.Fatal(err)
	}
	if string(j) != `{"b":2}` {
		t.Errorf("Scan string = %s", j)
	}

	if err := j.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if string(j) != "null" {
		t.Errorf("Scan nil = %s, want null", j)
	}

	if err := j.Scan(12345); err == nil {
		t.Error("expected error scanning unsupported type")
	}
}

func TestJSONMarshalUnmarshal(t *testing.T) {
	// Marshalling embeds raw JSON rather than a base64 byte array.
	type wrapper struct {
		Data JSON `json:"data"`
	}
	out, err := json.Marshal(wrapper{Data: JSON(`{"x":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"data":{"x":true}}` {
		t.Errorf("marshal = %s", out)
	}

	// Empty marshals to null.
	out, _ = json.Marshal(wrapper{})
	if string(out) != `{"data":null}` {
		t.Errorf("empty marshal = %s", out)
	}

	var w wrapper
	if err := json.Unmarshal([]byte(`{"data":{"y":5}}`), &w); err != nil {
		t.Fatal(err)
	}
	if string(w.Data) != `{"y":5}` {
		t.Errorf("unmarshal captured %s", w.Data)
	}
}
