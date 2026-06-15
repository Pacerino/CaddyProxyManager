package util

import (
	"reflect"
	"testing"
)

func TestSliceContainsItem(t *testing.T) {
	s := []string{"a", "b", "c"}
	if !SliceContainsItem(s, "b") {
		t.Error("expected to find b")
	}
	if SliceContainsItem(s, "z") {
		t.Error("did not expect to find z")
	}
	if SliceContainsItem(nil, "a") {
		t.Error("nil slice should contain nothing")
	}
}

func TestSliceContainsInt(t *testing.T) {
	s := []int{1, 2, 3}
	if !SliceContainsInt(s, 2) {
		t.Error("expected to find 2")
	}
	if SliceContainsInt(s, 9) {
		t.Error("did not expect to find 9")
	}
}

func TestConvertIntSliceToString(t *testing.T) {
	tests := []struct {
		in   []int
		want string
	}{
		{nil, ""},
		{[]int{}, ""},
		{[]int{1}, "1"},
		{[]int{1, 2, 3}, "1,2,3"},
	}
	for _, tt := range tests {
		if got := ConvertIntSliceToString(tt.in); got != tt.want {
			t.Errorf("ConvertIntSliceToString(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConvertStringSliceToInterface(t *testing.T) {
	got := ConvertStringSliceToInterface([]string{"a", "b"})
	want := []interface{}{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if len(ConvertStringSliceToInterface(nil)) != 0 {
		t.Error("nil input should produce empty slice")
	}
}
