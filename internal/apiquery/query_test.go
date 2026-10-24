package apiquery

import (
	"math"
	"net/url"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestFormat(t *testing.T) {
	for _, tc := range [][2]string{
		{Format(int64(-42)), "-42"},
		{Format(int64(math.MaxInt64)), "9223372036854775807"},
		{Format(1200.5), "1200.5"},
		{Format(30780.0), "30780"},
		{Format(1e21), "1000000000000000000000"},
		{Format(true), "true"},
		{Format(false), "false"},
		{Format("a b,c"), "a b,c"},
	} {
		if tc[0] != tc[1] {
			t.Errorf("got %q, want %q", tc[0], tc[1])
		}
	}
}

func TestSetAndSetList(t *testing.T) {
	q := url.Values{}
	Set(q, "f", ptr(1200.5))
	Set(q, "whole", ptr(30780.0))
	Set(q, "zero", ptr(int64(0)))
	Set(q, "no", ptr(false))
	Set[string](q, "unset", nil)
	SetList(q, "ints", []int64{1, 2})
	SetList(q, "bools", []bool{true, false})
	SetList(q, "lwin", []string{"100000120151200750", "100000220161200750"})
	SetList[string](q, "empty", []string{})
	SetList[string](q, "nil", nil)
	want := "bools=true%2Cfalse&f=1200.5&ints=1%2C2&lwin=100000120151200750%2C100000220161200750&no=false&whole=30780&zero=0"
	if q.Encode() != want {
		t.Errorf("encoded %s\nwant    %s", q.Encode(), want)
	}
	Set(q, "f", ptr(1.0))
	if q.Get("f") != "1" || len(q["f"]) != 1 {
		t.Errorf("a second Set did not replace the value: %v", q["f"])
	}
}
