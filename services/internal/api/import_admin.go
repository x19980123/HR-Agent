package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/hr-agent/services/internal/pipeline"
)

func (s *Server) adminCreateImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(80 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart"})
		return
	}
	jdID := r.FormValue("jd_id")
	defaultEmail := strings.TrimSpace(r.FormValue("default_email"))
	if defaultEmail == "" {
		defaultEmail = "candidate@import.local"
	}

	csvMap := map[string]pipeline.CSVMappingRow{}
	for _, key := range []string{"mapping", "mapping_csv"} {
		if csvFiles := r.MultipartForm.File[key]; len(csvFiles) > 0 {
			rc, err := csvFiles[0].Open()
			if err == nil {
				parsed, perr := pipeline.ParseImportCSVMapping(rc)
				rc.Close()
				if perr == nil {
					csvMap = parsed
				}
			}
			break
		}
	}

	type uploadFile struct {
		filename string
		path     string
	}
	var uploads []uploadFile

	files := r.MultipartForm.File["resume"]
	if len(files) == 0 {
		files = r.MultipartForm.File["resume[]"]
	}
	for _, fh := range files {
		rc, err := fh.Open()
		if err != nil {
			continue
		}
		path, err := s.Pipeline.SaveImportResume(fh.Filename, rc)
		rc.Close()
		if err != nil {
			continue
		}
		uploads = append(uploads, uploadFile{filename: fh.Filename, path: path})
	}

	if zips := r.MultipartForm.File["archive"]; len(zips) > 0 {
		rc, err := zips[0].Open()
		if err == nil {
			zipPath, err := s.Pipeline.SaveImportResume(zips[0].Filename, rc)
			rc.Close()
			if err == nil {
				tmpDir := filepath.Join(s.Cfg.UploadDir, "import_zip")
				extracted, _ := pipeline.ExtractZipResumes(zipPath, tmpDir)
				for _, p := range extracted {
					uploads = append(uploads, uploadFile{filename: filepath.Base(p), path: p})
				}
			}
		}
	}

	if len(uploads) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resume files or archive required"})
		return
	}

	var items []pipeline.ImportJobItem
	for i, up := range uploads {
		row, hasCSV := pipeline.LookupCSVMapping(csvMap, up.filename)
		name, email, source, preEmail, preMeta := s.Pipeline.ResolveImportContact(r.Context(), up.path, up.filename, defaultEmail, row, hasCSV, i, len(uploads))
		extID := row.ExternalID
		if extID == "" {
			extID = strings.TrimSuffix(filepath.Base(up.filename), filepath.Ext(up.filename))
		}
		items = append(items, pipeline.ImportJobItem{
			CandidateName:        name,
			CandidateEmail:       email,
			CandidateSource:      source,
			ExternalID:           extID,
			ResumePath:           up.path,
			PreExtractEmail:      preEmail,
			PreExtractMetaJSON:   preMeta,
		})
	}
	jobID, err := s.Pipeline.CreateImportJob(r.Context(), jdID, s.currentActor(r), items)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "total": len(items)})
}

func (s *Server) adminListImportItems(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = r.URL.Query().Get("filter")
	}
	items, err := s.Pipeline.ListImportItems(r.Context(), jobID, status, 100, 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminRetryImportItem(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	itemID := r.PathValue("item_id")
	job, err := s.Pipeline.GetImportJob(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	jdID, _ := job["jd_id"].(string)
	if err := s.Pipeline.RetryImportItem(r.Context(), jobID, itemID, jdID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) adminUpdateAppContact(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.Pipeline.UpdateApplicationContact(r.Context(), r.PathValue("id"), body.Email, body.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
