package dzzzr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The engine renders JSON through Smarty templates, so scalar types are not
// stable: a number is usually quoted ("3"), a boolean may be true, "1" or "",
// and an absent value may come as "" or null. The Flex types accept every
// observed form and re-encode as the natural Go type.

// FlexString decodes a JSON string, number, bool or null into a string.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler.
func (s *FlexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*s = ""
		return nil
	}
	if b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = FlexString(v)
		return nil
	}
	*s = FlexString(string(b))
	return nil
}

// String returns the plain string.
func (s FlexString) String() string { return string(s) }

// FlexInt decodes a JSON number, a quoted number, a bool or null into an int.
// Unparseable strings decode to zero rather than failing the whole document.
type FlexInt int

// UnmarshalJSON implements json.Unmarshaler.
func (i *FlexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*i = 0
		return nil
	}
	if b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*i = FlexInt(parseLooseInt(v))
		return nil
	}
	switch string(b) {
	case "true":
		*i = 1
		return nil
	case "false":
		*i = 0
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("dzzzr: FlexInt: %w", err)
	}
	*i = FlexInt(f)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (i FlexInt) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(int(i))), nil }

// Int returns the plain int.
func (i FlexInt) Int() int { return int(i) }

// parseLooseInt reads a leading integer out of a string, tolerating
// surrounding whitespace, a decimal fraction and trailing text.
func parseLooseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f)
	}
	end := 0
	if s[0] == '-' || s[0] == '+' {
		end = 1
	}
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	v, _ := strconv.Atoi(s[:end])
	return v
}

// FlexBool decodes true/false, "1"/"0", "true"/"false", ""/null and numbers
// into a bool. Any non-empty string other than "0" and "false" is true, which
// matches how PHP truthiness produced the value in the first place.
type FlexBool bool

// UnmarshalJSON implements json.Unmarshaler.
func (v *FlexBool) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*v = false
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.ToLower(strings.TrimSpace(s))
		*v = FlexBool(s != "" && s != "0" && s != "false" && s != "null")
		return nil
	}
	switch string(b) {
	case "true":
		*v = true
		return nil
	case "false":
		*v = false
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("dzzzr: FlexBool: %w", err)
	}
	*v = f != 0
	return nil
}

// MarshalJSON implements json.Marshaler.
func (v FlexBool) MarshalJSON() ([]byte, error) { return []byte(strconv.FormatBool(bool(v))), nil }

// Bool returns the plain bool.
func (v FlexBool) Bool() bool { return bool(v) }
