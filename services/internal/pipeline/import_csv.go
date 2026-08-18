package pipeline

import (
	"encoding/csv"
	"io"
	"path/filepath"
	"strings"
)

type CSVMappingRow struct {
	Filename   string
	Email      string
	Name       string
	ExternalID string
}

func ParseImportCSVMapping(r io.Reader) (map[string]CSVMappingRow, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]CSVMappingRow{}
	for i, rec := range records {
		if len(rec) == 0 {
			continue
		}
		for len(rec) < 3 {
			rec = append(rec, "")
		}
		fn := strings.TrimSpace(rec[0])
		em := strings.TrimSpace(rec[1])
		nm := strings.TrimSpace(rec[2])
		ext := ""
		if len(rec) > 3 {
			ext = strings.TrimSpace(rec[3])
		}
		if i == 0 && (strings.EqualFold(fn, "filename") || strings.EqualFold(fn, "file")) {
			continue
		}
		if fn == "" {
			continue
		}
		key := normalizeImportFilename(fn)
		out[key] = CSVMappingRow{Filename: fn, Email: em, Name: nm, ExternalID: ext}
	}
	return out, nil
}

func LookupCSVMapping(m map[string]CSVMappingRow, uploadName string) (CSVMappingRow, bool) {
	if len(m) == 0 {
		return CSVMappingRow{}, false
	}
	key := normalizeImportFilename(uploadName)
	row, ok := m[key]
	return row, ok
}

func normalizeImportFilename(name string) string {
	name = strings.TrimSpace(name)
	name = filepath.Base(name)
	return strings.ToLower(name)
}
