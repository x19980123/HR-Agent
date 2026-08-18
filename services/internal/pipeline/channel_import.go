package pipeline

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/hr-agent/services/internal/audit"
)

type ChannelApplicationInput struct {
	Channel        string
	JDID           string
	ExternalID     string
	CandidateName  string
	CandidateEmail string
	ResumePath     string
	ResumeBase64   string
	ResumeFilename string
}

// IngestChannelApplication creates one application from ATS/Boss-style webhooks (internal auth).
func (s *Service) IngestChannelApplication(ctx context.Context, in ChannelApplicationInput) (string, error) {
	in.Channel = strings.TrimSpace(strings.ToLower(in.Channel))
	if in.Channel == "" {
		in.Channel = "generic"
	}
	in.JDID = strings.TrimSpace(in.JDID)
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	if in.JDID == "" {
		return "", fmt.Errorf("jd_id required")
	}
	if in.ExternalID == "" {
		return "", fmt.Errorf("external_id required for channel ingest")
	}

	path := strings.TrimSpace(in.ResumePath)
	if path == "" && in.ResumeBase64 != "" {
		name := strings.TrimSpace(in.ResumeFilename)
		if name == "" {
			name = "resume.pdf"
		}
		raw, err := base64.StdEncoding.DecodeString(in.ResumeBase64)
		if err != nil {
			return "", fmt.Errorf("invalid resume_base64")
		}
		path, err = s.SaveImportResume(name, io.NopCloser(strings.NewReader(string(raw))))
		if err != nil {
			return "", err
		}
	}
	if path == "" {
		return "", fmt.Errorf("resume_path or resume_base64 required")
	}

	sum := sha256File(path)
	if skip, dupID, derr := s.importDedupSkip(ctx, in.JDID, in.CandidateEmail, sum, in.ExternalID); derr != nil {
		return "", derr
	} else if skip && dupID != "" {
		return dupID, nil
	}

	email := strings.TrimSpace(in.CandidateEmail)
	source := ContactSourceHRForm
	if email == "" || s.isPlaceholderEmail(email) {
		if pe, pn, _, _ := s.PreExtractContact(ctx, path); pe != "" && s.validateEmailFormat(pe) {
			email = pe
			if in.CandidateName == "" {
				in.CandidateName = pn
			}
			source = ContactSourcePreExtract
		} else {
			email = ImportItemEmail("candidate@import.local", 0, 1)
			source = ContactSourceImportPlaceholder
		}
	}

	appID, err := s.Start(ctx, StartInput{
		JDID: in.JDID, CandidateName: in.CandidateName, CandidateEmail: email,
		ResumePath: path, ContactSource: source, ExternalID: in.ExternalID, Channel: in.Channel,
	})
	if err != nil {
		return "", err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "channel_application_ingested",
		Detail:        map[string]any{"channel": in.Channel, "external_id": in.ExternalID},
	})
	return appID, nil
}
