package pdfsource

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	pdf "github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

func (a matrix) mul(b matrix) matrix {
	return matrix{a[0]*b[0] + a[1]*b[2], a[0]*b[1] + a[1]*b[3], a[2]*b[0] + a[3]*b[2], a[2]*b[1] + a[3]*b[3], a[4]*b[0] + a[5]*b[2] + b[4], a[4]*b[1] + a[5]*b[3] + b[5]}
}
func (a matrix) rect() []float64 {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range [][2]float64{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		x, y := p[0]*a[0]+p[1]*a[2]+a[4], p[0]*a[1]+p[1]*a[3]+a[5]
		x0 = min(x0, x)
		x1 = max(x1, x)
		y0 = min(y0, y)
		y1 = max(y1, y)
	}
	return []float64{x0, y0, x1, y1}
}

type fragment struct {
	text       string
	x, y, size float64
}
type contentState struct {
	cm, tm, lm    matrix
	font          *fontDecoder
	size, leading float64
}
type pageContent struct {
	ctx                     context.Context
	fragments               []fragment
	positions               map[string][][]float64
	decoded, ops, textBytes int
	fontBytes               int
}

func (c *pageContent) walk(v, resources pdf.Value, cm matrix, path string, depth int) error {
	if depth > 32 {
		return errors.New("content forms nested too deeply")
	}
	if v.IsNull() {
		return nil
	}
	var check func(pdf.Value, int) error
	check = func(stream pdf.Value, depth int) error {
		if depth > 32 {
			return errors.New("content arrays nested too deeply")
		}
		if stream.Kind() == pdf.Array {
			for i := 0; i < stream.Len(); i++ {
				if err := check(stream.Index(i), depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		data, err := readStream(c.ctx, stream)
		if err != nil {
			return err
		}
		c.decoded += len(data)
		if c.decoded > maxDecodedBytes {
			return errors.New("selected content exceeds 16 MiB limit")
		}
		return nil
	}
	if err := check(v, 0); err != nil {
		return err
	}
	if len(resources.Key("Font").Keys()) > 4096 {
		return errors.New("page font resource count exceeds limit")
	}
	fonts := map[string]*fontDecoder{}
	for _, name := range resources.Key("Font").Keys() {
		f := &pdf.Font{V: resources.Key("Font").Key(name)}
		unicode := f.V.Key("ToUnicode")
		var unicodeData []byte
		if !unicode.IsNull() {
			b, err := readStream(c.ctx, unicode)
			if err != nil {
				return err
			}
			unicodeData = b
			if len(b) > 2<<20 {
				return errors.New("font Unicode map exceeds limit")
			}
		}
		decoder, err := newFontDecoder(c.ctx, f, unicodeData)
		if err != nil {
			return err
		}
		c.fontBytes += decoder.cost
		if c.fontBytes > 8<<20 {
			return errors.New("page font mappings exceed 8 MiB limit")
		}
		fonts[name] = decoder
	}
	g := contentState{cm: cm, tm: identity, lm: identity}
	var stack []contentState
	var walkErr error
	decode := func(raw string) string {
		if g.font != nil {
			return g.font.decode(raw)
		}
		return raw
	}
	show := func(text string) {
		m := g.tm.mul(g.cm)
		if text != "" {
			c.textBytes += len(text)
			if c.textBytes > 2<<20 {
				panic("page text exceeds 2 MiB limit")
			}
			c.fragments = append(c.fragments, fragment{text, m[4], m[5], math.Abs(g.size) * math.Hypot(m[0], m[1])})
		}
		advance := float64(len([]rune(text))) * g.size * .5
		g.tm[4] += advance
	}
	pdf.Interpret(v, func(st *pdf.Stack, op string) {
		c.ops++
		if c.ops > 500_000 {
			panic("content operation limit exceeded")
		}
		if err := c.ctx.Err(); err != nil {
			panic(err)
		}
		args := make([]pdf.Value, st.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = st.Pop()
		}
		need := func(n int) {
			if len(args) != n {
				panic(fmt.Sprintf("invalid %s operands", op))
			}
		}
		mat := func() matrix {
			need(6)
			var m matrix
			for i := range m {
				m[i] = args[i].Float64()
			}
			return m
		}
		newline := func() { g.lm = matrix{1, 0, 0, 1, 0, -g.leading}.mul(g.lm); g.tm = g.lm }
		switch op {
		case "q":
			stack = append(stack, g)
		case "Q":
			if len(stack) == 0 {
				panic("unbalanced graphics state")
			}
			g = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case "cm":
			g.cm = mat().mul(g.cm)
		case "BT":
			g.tm = identity
			g.lm = identity
		case "Tf":
			need(2)
			g.font = fonts[args[0].Name()]
			g.size = args[1].Float64()
		case "TL":
			need(1)
			g.leading = args[0].Float64()
		case "Tm":
			g.tm = mat()
			g.lm = g.tm
		case "Td", "TD":
			need(2)
			x, y := args[0].Float64(), args[1].Float64()
			if op == "TD" {
				g.leading = -y
			}
			g.lm = matrix{1, 0, 0, 1, x, y}.mul(g.lm)
			g.tm = g.lm
		case "T*":
			newline()
		case "Tj":
			need(1)
			show(decode(args[0].RawString()))
		case "TJ":
			need(1)
			var b strings.Builder
			for i := 0; i < args[0].Len(); i++ {
				item := args[0].Index(i)
				if item.Kind() == pdf.String {
					b.WriteString(decode(item.RawString()))
				} else if item.Float64() < -150 {
					b.WriteByte(' ')
				}
			}
			show(b.String())
		case "'":
			need(1)
			newline()
			show(decode(args[0].RawString()))
		case "\"":
			need(3)
			newline()
			show(decode(args[2].RawString()))
		case "Do":
			need(1)
			name := args[0].Name()
			obj := resources.Key("XObject").Key(name)
			p := path + "/" + name
			switch obj.Key("Subtype").Name() {
			case "Image":
				c.positions[p] = append(c.positions[p], g.cm.rect())
			case "Form":
				nested := obj.Key("Resources")
				if nested.IsNull() {
					nested = resources
				}
				m := identity
				mv := obj.Key("Matrix")
				if mv.Len() == 6 {
					for i := range m {
						m[i] = mv.Index(i).Float64()
					}
				}
				if err := c.walk(obj, nested, m.mul(g.cm), p, depth+1); err != nil {
					walkErr = err
					panic(err)
				}
			}
		case "BI":
			panic("inline PDF images are not supported")
		}
	})
	return walkErr
}
func nearest(fragments []fragment, r []float64) string {
	best := math.Inf(1)
	var value string
	for _, f := range fragments {
		if strings.TrimSpace(f.text) == "" || f.y < r[3]-f.size {
			continue
		}
		distance := math.Abs(f.x-r[0]) + math.Abs(f.y-r[3])
		if distance < best {
			best = distance
			value = strings.TrimSpace(f.text)
		}
	}
	return value
}
func inherited(v pdf.Value, key string) pdf.Value {
	for range 64 {
		if x := v.Key(key); !x.IsNull() {
			return x
		}
		v = v.Key("Parent")
		if v.IsNull() {
			break
		}
	}
	return pdf.Value{}
}
func extractPage(ctx context.Context, p pdf.Page, cpu *model.Context, number int, totalImages *int) (Page, error) {
	c := &pageContent{ctx: ctx, positions: map[string][][]float64{}}
	if err := c.walk(p.V.Key("Contents"), p.Resources(), identity, "", 0); err != nil {
		return Page{}, err
	}
	page := Page{Number: number, Links: []Link{}, Images: []Image{}}
	box := inherited(p.V, "MediaBox")
	if box.Len() == 4 {
		page.Width = box.Index(2).Float64() - box.Index(0).Float64()
		page.Height = box.Index(3).Float64() - box.Index(1).Float64()
	}
	// Keep content-stream ordering, inserting a newline when text moves to another
	// baseline. This preserves literal HTML/code chunks rather than rewriting them.
	var text strings.Builder
	var last *fragment
	for i := range c.fragments {
		f := &c.fragments[i]
		if last != nil && math.Abs(f.y-last.y) > max(2, f.size*.3) {
			text.WriteByte('\n')
		} else if last != nil && f.x-(last.x+float64(len([]rune(last.text)))*last.size*.5) > max(12, last.size*1.5) {
			text.WriteByte('\t')
		}
		text.WriteString(f.text)
		last = f
	}
	page.Text = text.String()
	page.RequiresOCR = strings.TrimSpace(page.Text) == ""
	annotations := p.V.Key("Annots")
	for i := 0; i < annotations.Len(); i++ {
		a := annotations.Index(i)
		uri := a.Key("A").Key("URI").Text()
		if uri == "" {
			continue
		}
		link := Link{URI: uri, Rect: []float64{}}
		r := a.Key("Rect")
		for j := 0; j < r.Len(); j++ {
			link.Rect = append(link.Rect, r.Index(j).Float64())
		}
		if len(link.Rect) == 4 {
			var labels []string
			for _, f := range c.fragments {
				if f.x >= link.Rect[0]-f.size && f.x <= link.Rect[2] && f.y >= link.Rect[1]-f.size && f.y <= link.Rect[3]+f.size {
					labels = append(labels, f.text)
				}
			}
			link.Text = strings.Join(labels, " ")
		}
		page.Links = append(page.Links, link)
	}
	_, _, attrs, err := cpu.PageDict(number, false)
	if err != nil {
		return Page{}, err
	}
	images, err := extractImages(ctx, cpu, attrs.Resources, c, totalImages)
	if err != nil {
		return Page{}, err
	}
	page.Images = images
	// Deterministic output independent of PDF dictionary iteration.
	slices.SortFunc(page.Images, func(a, b Image) int { return strings.Compare(a.Name, b.Name) })
	return page, nil
}
