package pdfsource

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	pdf "github.com/ledongthuc/pdf"
)

type fontDecoder struct {
	cost    int
	font    *pdf.Font
	ctx     context.Context
	mapping map[string]string
	lengths []int
}

var cmapBlock = regexp.MustCompile(`(?s)begin(bfchar|bfrange)\s*(.*?)\s*end(?:bfchar|bfrange)`)
var cmapToken = regexp.MustCompile(`<([0-9A-Fa-f\s]+)>|\[|\]`)

func utf16Text(b []byte) string {
	if len(b)%2 != 0 {
		panic("invalid UTF-16 font mapping")
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(units))
}
func newFontDecoder(ctx context.Context, font *pdf.Font, data []byte) (*fontDecoder, error) {
	f := &fontDecoder{font: font, ctx: ctx, cost: 128}
	if len(data) == 0 {
		return f, nil
	}
	if strings.Contains(string(data), "usecmap") {
		return nil, errors.New("inherited ToUnicode maps are not supported")
	}
	f.mapping = map[string]string{}
	expanded := 0
	add := func(key, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		expanded += len(value)*2 + len(key) + 64
		if len(value) > 256 || expanded > 2<<20 {
			return errors.New("expanded ToUnicode map exceeds byte limit")
		}
		if len(key) == 0 || len(key) > 4 || len(value) == 0 || len(value)%2 != 0 {
			return errors.New("invalid ToUnicode character mapping")
		}
		if len(f.mapping) >= 65536 {
			return errors.New("ToUnicode map exceeds character limit")
		}
		f.mapping[string(key)] = utf16Text(value)
		if !slices.Contains(f.lengths, len(key)) {
			f.lengths = append(f.lengths, len(key))
		}
		return nil
	}
	for _, block := range cmapBlock.FindAllStringSubmatch(string(data), -1) {
		raw := cmapToken.FindAllString(block[2], -1)
		tokens := make([][]byte, len(raw))
		for i, t := range raw {
			if t == "[" || t == "]" {
				tokens[i] = []byte(t)
				continue
			}
			t = strings.Join(strings.Fields(t[1:len(t)-1]), "")
			b, err := hex.DecodeString(t)
			if err != nil {
				return nil, err
			}
			tokens[i] = b
		}
		for i := 0; i < len(tokens); {
			if block[1] == "bfchar" {
				if i+1 >= len(tokens) {
					return nil, errors.New("truncated ToUnicode bfchar")
				}
				if err := add(tokens[i], tokens[i+1]); err != nil {
					return nil, err
				}
				i += 2
				continue
			}
			if i+2 >= len(tokens) {
				return nil, errors.New("truncated ToUnicode bfrange")
			}
			low, high, dst := tokens[i], tokens[i+1], tokens[i+2]
			i += 3
			if len(low) == 0 || len(low) > 4 || len(low) != len(high) {
				return nil, errors.New("invalid ToUnicode range")
			}
			from, _ := strconv.ParseUint(hex.EncodeToString(low), 16, 32)
			to, _ := strconv.ParseUint(hex.EncodeToString(high), 16, 32)
			if to < from || to-from > 65535 {
				return nil, errors.New("ToUnicode range exceeds limit")
			}
			for n := from; n <= to; n++ {
				var encoded [4]byte
				binary.BigEndian.PutUint32(encoded[:], uint32(n))
				key := encoded[4-len(low):]
				value := dst
				if string(dst) == "[" {
					if i >= len(tokens) || string(tokens[i]) == "]" {
						return nil, errors.New("truncated ToUnicode range array")
					}
					value = tokens[i]
					i++
				}
				if err := add(key, value); err != nil {
					return nil, err
				}
				if string(dst) != "[" {
					dst = slices.Clone(dst)
					for j := len(dst) - 1; j >= 0; j-- {
						dst[j]++
						if dst[j] != 0 {
							break
						}
					}
				}
			}
			if string(dst) == "[" {
				if i >= len(tokens) || string(tokens[i]) != "]" {
					return nil, errors.New("invalid ToUnicode range array")
				}
				i++
			}
		}
	}
	if len(f.mapping) == 0 {
		return nil, errors.New("ToUnicode map has no supported character mappings")
	}
	f.cost += expanded
	slices.Sort(f.lengths)
	return f, nil
}
func (f *fontDecoder) decode(raw string) string {
	if len(raw) > 2<<20 {
		panic("text operand exceeds 2 MiB limit")
	}
	if f.mapping == nil {
		return f.font.Encoder().Decode(raw)
	}
	var out strings.Builder
	for len(raw) > 0 {
		if err := f.ctx.Err(); err != nil {
			panic(err)
		}
		found := false
		for _, n := range f.lengths {
			if n > len(raw) {
				continue
			}
			if text, ok := f.mapping[raw[:n]]; ok {
				if out.Len()+len(text) > 2<<20 {
					panic("decoded text exceeds 2 MiB limit")
				}
				out.WriteString(text)
				raw = raw[n:]
				found = true
				break
			}
		}
		if !found {
			panic(fmt.Sprintf("character missing from ToUnicode map: %x", []byte(raw[:min(len(raw), 4)])))
		}
	}
	return out.String()
}
