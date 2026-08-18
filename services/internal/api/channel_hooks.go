package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/hr-agent/services/internal/pipeline"
)

func (s *Server) hookChannelApplication(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel        string `json:"channel"`
		JDID           string `json:"jd_id"`
		ExternalID     string `json:"external_id"`
		CandidateName  string `json:"candidate_name"`
		CandidateEmail string `json:"candidate_email"`
		ResumePath     string `json:"resume_path"`
		ResumeBase64   string `json:"resume_base64"`
		ResumeFilename string `json:"resume_filename"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	appID, err := s.Pipeline.IngestChannelApplication(r.Context(), pipeline.ChannelApplicationInput{
		Channel: body.Channel, JDID: body.JDID, ExternalID: body.ExternalID,
		CandidateName: body.CandidateName, CandidateEmail: body.CandidateEmail,
		ResumePath: body.ResumePath, ResumeBase64: body.ResumeBase64, ResumeFilename: body.ResumeFilename,
	})
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "duplicate") {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"application_id": appID, "async": true})
}
