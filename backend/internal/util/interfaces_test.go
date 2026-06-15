package util

import "testing"

func TestFindItemInInterface(t *testing.T) {
	obj := map[string]interface{}{
		"a": 1,
		"nested": map[string]interface{}{
			"target": "found",
		},
		"list": []interface{}{
			map[string]interface{}{"deep": "value"},
		},
	}

	if v, ok := FindItemInInterface("a", obj); !ok || v != 1 {
		t.Errorf("top level: got %v ok=%v", v, ok)
	}
	if v, ok := FindItemInInterface("target", obj); !ok || v != "found" {
		t.Errorf("nested map: got %v ok=%v", v, ok)
	}
	if v, ok := FindItemInInterface("deep", obj); !ok || v != "value" {
		t.Errorf("nested array: got %v ok=%v", v, ok)
	}
	if _, ok := FindItemInInterface("missing", obj); ok {
		t.Error("missing key should not be found")
	}
	if _, ok := FindItemInInterface("a", "not a map"); ok {
		t.Error("non-map input should return false")
	}
}
