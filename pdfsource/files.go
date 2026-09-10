package pdfsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileExtraction struct {
	Path            string `json:"path"`
	Bytes           int    `json:"bytes"`
	SourceSHA256    string `json:"source_sha256"`
	PageCount       int    `json:"page_count"`
	ReferencedLines int    `json:"referenced_lines"`
	IgnoredLines    int    `json:"ignored_lines"`
}

func rootedPath(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsLocal(path) {
		return "", errors.New("path must stay inside DZZZR_FILES_ROOT")
	}
	return path, nil
}

func readRootFile(root, path string, limit int64) ([]byte, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	path, err = rootedPath(root, path)
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	info, err := dir.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	f, err := dir.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("input must be a regular file up to %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("input size limit exceeded")
	}
	return data, nil
}

func IndexFile(ctx context.Context, root, path string, opts Options) (*IndexedDocument, error) {
	data, err := readRootFile(root, path, MaxPDFBytes)
	if err != nil {
		return nil, err
	}
	return Index(ctx, data, opts)
}

func ExtractFiles(ctx context.Context, root, pdfPath, mappingPath, outputPath string) (*FileExtraction, error) {
	data, err := readRootFile(root, pdfPath, MaxPDFBytes)
	if err != nil {
		return nil, err
	}
	mapping, err := loadMapping(root, mappingPath)
	if err != nil {
		return nil, err
	}
	result, err := Extract(ctx, data, mapping)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	outputPath, err = rootedPath(root, outputPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".json") {
		return nil, errors.New("output must have .json extension")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	parent, err := dir.OpenRoot(filepath.Dir(outputPath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := filepath.Base(outputPath)
	f, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(result.Data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return nil, errors.Join(err, parent.Remove(name))
	}
	return &FileExtraction{Path: filepath.Join(root, outputPath), Bytes: len(result.Data), SourceSHA256: result.SourceSHA256, PageCount: result.PageCount, ReferencedLines: result.ReferencedLines, IgnoredLines: result.IgnoredLines}, nil
}
