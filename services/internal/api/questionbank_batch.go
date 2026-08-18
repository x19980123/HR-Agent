package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/hr-agent/services/internal/pipeline"
)

func (s *Server) adminBatchQuestionBank(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	var items []pipeline.QuestionBankInput

	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart"})
			return
		}
		files := r.MultipartForm.File["file"]
		if len(files) == 0 {
			files = r.MultipartForm.File["csv"]
		}
		if len(files) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "csv file required"})
			return
		}
		rc, err := files[0].Open()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		parsed, perr := pipeline.ParseQuestionBankCSV(rc)
		rc.Close()
		if perr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": perr.Error()})
			return
		}
		items = parsed
	} else {
		var body struct {
			Items []pipeline.QuestionBankInput `json:"items"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		items = body.Items
	}

	out, err := s.Pipeline.BatchUpsertQuestionBank(r.Context(), items)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
