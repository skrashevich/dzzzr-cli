package dzzzr

import (
	"encoding/json"
	"testing"
)

func TestFlexIntDecodes(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`"3"`, 3}, {`3`, 3}, {`3.0`, 3}, {`""`, 0}, {`null`, 0}, {`true`, 1}, {`false`, 0},
		{`" 12 "`, 12}, {`"-5"`, -5}, {`"7 мин"`, 7}, {`"abc"`, 0}, {`"2.9"`, 2},
	}
	for _, c := range cases {
		var v FlexInt
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Fatalf("FlexInt(%s): %v", c.in, err)
		}
		if v.Int() != c.want {
			t.Errorf("FlexInt(%s) = %d, want %d", c.in, v, c.want)
		}
	}
	b, _ := json.Marshal(FlexInt(42))
	if string(b) != "42" {
		t.Errorf("Marshal = %s, want 42", b)
	}
}

func TestFlexBoolDecodes(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`"1"`, true}, {`""`, false}, {`"0"`, false}, {`true`, true}, {`false`, false},
		{`"true"`, true}, {`"false"`, false}, {`0`, false}, {`1`, true}, {`null`, false}, {`"yes"`, true},
	}
	for _, c := range cases {
		var v FlexBool
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Fatalf("FlexBool(%s): %v", c.in, err)
		}
		if v.Bool() != c.want {
			t.Errorf("FlexBool(%s) = %v, want %v", c.in, v, c.want)
		}
	}
	b, _ := json.Marshal(FlexBool(true))
	if string(b) != "true" {
		t.Errorf("Marshal = %s, want true", b)
	}
}

func TestFlexStringDecodes(t *testing.T) {
	cases := map[string]string{`"a"`: "a", `5`: "5", `null`: "", `true`: "true"}
	for in, want := range cases {
		var v FlexString
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("FlexString(%s): %v", in, err)
		}
		if v.String() != want {
			t.Errorf("FlexString(%s) = %q, want %q", in, v, want)
		}
	}
}

func TestFlexInStruct(t *testing.T) {
	var s struct {
		N FlexInt  `json:"n"`
		B FlexBool `json:"b"`
	}
	if err := json.Unmarshal([]byte(`{"n":"12","b":"1"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.N != 12 || !s.B {
		t.Errorf("got %+v", s)
	}
}
