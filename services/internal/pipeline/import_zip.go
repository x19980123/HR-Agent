package pipeline

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var importResumeExts = map[string]bool{".pdf": true, ".doc": true, ".docx": true, ".txt": true}

func ExtractZipResumes(zipPath, destDir string) ([]string, error) {
	return extractZipResumes(zipPath, destDir)
}

func extractZipResumes(zipPath, destDir string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !importResumeExts[ext] {
			continue
		}
		base := filepath.Base(f.Name)
		outPath := filepath.Join(destDir, base)
		if err := extractZipFile(f, outPath); err != nil {
			continue
		}
		paths = append(paths, outPath)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("zip contains no supported resumes")
	}
	return paths, nil
}

func extractZipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
