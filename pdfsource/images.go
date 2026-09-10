package pdfsource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	_ "golang.org/x/image/tiff"
)

func extractImages(ctx context.Context, cpu *model.Context, resources types.Dict, content *pageContent, total *int) ([]Image, error) {
	images := []Image{}
	seen := map[string]int{}
	active := map[int]bool{}
	var visit func(types.Dict, string, int) error
	visit = func(resources types.Dict, path string, depth int) error {
		if depth > 32 {
			return errors.New("image resources nested too deeply")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		objects, err := cpu.DereferenceDict(resources["XObject"])
		if err != nil {
			return err
		}
		names := make([]string, 0, len(objects))
		for name := range objects {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			obj := objects[name]
			sd, _, err := cpu.DereferenceStreamDict(obj)
			if err != nil {
				return err
			}
			if sd == nil {
				return fmt.Errorf("missing image/form stream %s", name)
			}
			subtype := sd.NameEntry("Subtype")
			if subtype == nil {
				continue
			}
			key := path + "/" + name
			switch *subtype {
			case "Form":
				number := 0
				if ref, ok := obj.(types.IndirectRef); ok {
					number = ref.ObjectNumber.Value()
				}
				if number != 0 && active[number] {
					continue
				}
				active[number] = true
				nested := resources
				if raw, ok := sd.Dict["Resources"]; ok {
					nested, err = cpu.DereferenceDict(raw)
					if err != nil {
						return err
					}
				}
				if err = visit(nested, key, depth+1); err != nil {
					return err
				}
				delete(active, number)
			case "Image":
				width, err := cpu.DereferenceInteger(sd.Dict["Width"])
				if err != nil || width == nil {
					return fmt.Errorf("invalid image width %s", key)
				}
				height, err := cpu.DereferenceInteger(sd.Dict["Height"])
				if err != nil || height == nil {
					return fmt.Errorf("invalid image height %s", key)
				}
				w, h := width.Value(), height.Value()
				if w < 1 || h < 1 || int64(w)*int64(h) > 20_000_000 {
					return fmt.Errorf("image %s exceeds pixel limit", key)
				}
				var data []byte
				mime, extension := "image/png", ".png"
				_, smask := sd.Dict["SMask"]
				_, mask := sd.Dict["Mask"]
				if len(sd.FilterPipeline) == 1 && sd.FilterPipeline[0].Name == "DCTDecode" && !smask && !mask {
					data = bytes.Clone(sd.Raw)
					mime, extension = "image/jpeg", ".jpg"
					config, _, err := image.DecodeConfig(bytes.NewReader(data))
					if err != nil || config.Width != w || config.Height != h {
						return fmt.Errorf("invalid JPEG dimensions %s", key)
					}
				} else {
					if err = sd.DecodeWithLimit(maxDecodedBytes); err != nil {
						return fmt.Errorf("image %s: %w", key, err)
					}
					number := 0
					if ref, ok := obj.(types.IndirectRef); ok {
						number = ref.ObjectNumber.Value()
					}
					decoded, err := pdfcpu.ExtractImage(cpu, sd, false, name, number, false)
					if err != nil {
						return fmt.Errorf("image %s: %w", key, err)
					}
					if decoded == nil || decoded.Reader == nil {
						return fmt.Errorf("image %s could not be decoded", key)
					}
					data, err = io.ReadAll(io.LimitReader(decoded.Reader, maxImageBytes+1))
					if err != nil {
						return err
					}
					if len(data) > maxImageBytes {
						return errors.New("exported image exceeds limit")
					}
					if decoded.FileType != "png" {
						picture, _, err := image.Decode(bytes.NewReader(data))
						if err != nil {
							return fmt.Errorf("unsupported image export %s: %w", decoded.FileType, err)
						}
						var buf bytes.Buffer
						if err = png.Encode(&buf, picture); err != nil {
							return err
						}
						data = buf.Bytes()
					}
				}
				placements := []Placement{}
				for _, rect := range content.positions[key] {
					placements = append(placements, Placement{Rect: rect, NearbyText: nearest(content.fragments, rect)})
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(data))
				if index, ok := seen[digest]; ok {
					images[index].Placements = append(images[index].Placements, placements...)
					continue
				}
				*total += len(data)
				if *total > maxImageBytes {
					return errors.New("selected exported images exceed 32 MiB limit")
				}
				seen[digest] = len(images)
				images = append(images, Image{Name: digest + extension, MIMEType: mime, Data: data, Width: w, Height: h, Placements: placements})
			}
		}
		return nil
	}
	if err := visit(resources, "", 0); err != nil {
		return nil, err
	}
	for i := range images {
		images[i].PlacementUnknown = len(images[i].Placements) == 0
	}
	return images, nil
}
