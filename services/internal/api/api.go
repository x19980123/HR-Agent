package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/config"
	"github.com/hr-agent/services/internal/feishuauth"
	"github.com/hr-agent/services/internal/pipeline"
	"github.com/hr-agent/services/internal/ratelimit"
	"github.com/hr-agent/services/internal/replytoken"
	"github.com/hr-agent/services/internal/session"
	"github.com/hr-agent/services/web"
)

type Server struct {
	Pipeline   *pipeline.Service
	Cfg        *config.Config
	UploadDir  string
	publicRL   *ratelimit.Limiter
	FeishuAuth *feishuauth.Client
}

func NewServer(p *pipeline.Service, cfg *config.Config, uploadDir string) *Server {
	return &Server{
		Pipeline:   p,
		Cfg:        cfg,
		UploadDir:  uploadDir,
		publicRL:   ratelimit.New(time.Minute, 40),
		FeishuAuth: feishuauth.New(cfg.FeishuAppID, cfg.FeishuAppSecret, cfg.FeishuOAuthRedirect),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	// Auth (Feishu OAuth + session)
	mux.HandleFunc("GET /v1/auth/config", s.authConfig)
	mux.HandleFunc("GET /v1/auth/me", s.authMe)
	mux.HandleFunc("POST /v1/auth/logout", s.authLogout)
	mux.HandleFunc("GET /v1/auth/feishu/login", s.feishuLogin)
	mux.HandleFunc("GET /v1/auth/feishu/callback", s.feishuCallback)

	// Legacy + admin create (HR auth)
	mux.Handle("POST /v1/applications", s.requireHR(http.HandlerFunc(s.createApplication)))
	mux.Handle("GET /v1/applications/{id}", s.requireHR(http.HandlerFunc(s.getApplication)))
	mux.Handle("POST /v1/applications/{id}/replies", s.requireHROrInternal(http.HandlerFunc(s.handleReply)))
	mux.Handle("POST /v1/applications/{id}/human/approve", s.requireHR(http.HandlerFunc(s.humanApprove)))
	mux.Handle("POST /v1/internal/sweep-timeouts", s.requireInternal(http.HandlerFunc(s.sweepTimeouts)))

	// Public candidate reply API
	mux.HandleFunc("GET /v1/public/reply/{token}", s.publicGet)
	mux.HandleFunc("POST /v1/public/reply/{token}", s.publicPost)

	// Admin API
	mux.Handle("GET /v1/admin/stats", s.requireHR(http.HandlerFunc(s.adminStats)))
	mux.Handle("GET /v1/admin/jds", s.requireHR(http.HandlerFunc(s.adminListJDs)))
	mux.Handle("GET /v1/admin/jds/{id}", s.requireHR(http.HandlerFunc(s.adminGetJD)))
	mux.Handle("POST /v1/admin/jds", s.requireHR(http.HandlerFunc(s.adminUpsertJD)))
	mux.Handle("PUT /v1/admin/jds/{id}", s.requireHR(http.HandlerFunc(s.adminUpdateJD)))
	mux.Handle("DELETE /v1/admin/jds/{id}", s.requireHR(http.HandlerFunc(s.adminDeleteJD)))
	mux.Handle("GET /v1/admin/applications", s.requireHR(http.HandlerFunc(s.adminListApps)))
	mux.Handle("GET /v1/admin/applications/{id}", s.requireHR(http.HandlerFunc(s.adminGetApp)))
	mux.Handle("GET /v1/admin/applications/{id}/resume", s.requireHR(http.HandlerFunc(s.adminDownloadResume)))
	mux.Handle("POST /v1/admin/applications", s.requireHR(http.HandlerFunc(s.createApplication)))
	mux.Handle("POST /v1/admin/applications/{id}/human/approve", s.requireHR(http.HandlerFunc(s.humanApprove)))
	mux.Handle("POST /v1/admin/applications/{id}/retry-parse", s.requireHR(http.HandlerFunc(s.retryParse)))
	mux.Handle("GET /v1/admin/applications/{id}/reply-token", s.requireHR(http.HandlerFunc(s.adminReplyToken)))

	mux.Handle("GET /v1/admin/question-bank", s.requireHR(http.HandlerFunc(s.adminListQuestionBank)))
	mux.Handle("GET /v1/admin/question-bank/{id}", s.requireHR(http.HandlerFunc(s.adminGetQuestionBank)))
	mux.Handle("POST /v1/admin/question-bank", s.requireHR(http.HandlerFunc(s.adminUpsertQuestionBank)))
	mux.Handle("PUT /v1/admin/question-bank/{id}", s.requireHR(http.HandlerFunc(s.adminUpdateQuestionBank)))
	mux.Handle("DELETE /v1/admin/question-bank/{id}", s.requireHR(http.HandlerFunc(s.adminDeleteQuestionBank)))
	mux.Handle("POST /v1/admin/question-bank/reindex", s.requireHR(http.HandlerFunc(s.adminReindexQuestionBank)))

	mux.Handle("GET /v1/admin/staff/audit", s.requireAdmin(http.HandlerFunc(s.adminStaffAudit)))
	mux.Handle("GET /v1/admin/staff", s.requireAdmin(http.HandlerFunc(s.adminListStaff)))
	mux.Handle("POST /v1/admin/staff", s.requireAdmin(http.HandlerFunc(s.adminUpsertStaff)))
	mux.Handle("PUT /v1/admin/staff/{open_id}", s.requireAdmin(http.HandlerFunc(s.adminUpdateStaff)))
	mux.Handle("POST /v1/admin/staff/{open_id}/enable", s.requireAdmin(http.HandlerFunc(s.adminEnableStaff)))
	mux.Handle("POST /v1/admin/staff/{open_id}/disable", s.requireAdmin(http.HandlerFunc(s.adminDisableStaff)))
	mux.Handle("GET /v1/admin/staff/join-requests", s.requireAdmin(http.HandlerFunc(s.adminListJoinRequests)))
	mux.Handle("POST /v1/admin/staff/join-requests/{id}/approve", s.requireAdmin(http.HandlerFunc(s.adminApproveJoin)))
	mux.Handle("POST /v1/admin/staff/join-requests/{id}/reject", s.requireAdmin(http.HandlerFunc(s.adminRejectJoin)))

	// Static pages
	mux.HandleFunc("GET /r/{token}", s.serveCandidate)
	mux.HandleFunc("GET /admin", s.serveAdmin)
	mux.HandleFunc("GET /admin/", s.serveAdmin)
	mux.HandleFunc("GET /", s.serveRoot)

	return s.withSecurity(mux)
}

func (s *Server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.ContentLength > 20<<20 {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireHR(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkHR(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.checkHR(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		actor := s.currentActor(r)
		if !s.Pipeline.IsSystemAdmin(r.Context(), actor) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "仅系统管理员可进行成员管理"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkHR(r *http.Request) bool {
	if s.checkBearer(r, s.Cfg.HRAPIToken) {
		return true
	}
	if _, err := session.FromRequest(r, s.Cfg.SessionSecret); err == nil {
		return true
	}
	return false
}

func (s *Server) requireInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.checkInternal(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireHROrInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkHR(r) || s.checkInternal(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (s *Server) checkInternal(r *http.Request) bool {
	if s.Cfg.InternalAPIToken == "" {
		return false
	}
	if r.Header.Get("X-Internal-Token") == s.Cfg.InternalAPIToken {
		return true
	}
	return s.checkBearer(r, s.Cfg.InternalAPIToken)
}

func (s *Server) checkBearer(r *http.Request, expect string) bool {
	if expect == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) == expect
}

func (s *Server) clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"status": "ok"}
	if err := s.Pipeline.PingDB(r.Context()); err != nil {
		out["status"] = "degraded"
		out["mysql"] = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, out)
		return
	}
	out["mysql"] = "ok"
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

func (s *Server) serveCandidate(w http.ResponseWriter, r *http.Request) {
	raw, err := web.FS.ReadFile("candidate/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

func (s *Server) serveAdmin(w http.ResponseWriter, r *http.Request) {
	raw, err := web.FS.ReadFile("admin/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

func (s *Server) publicGet(w http.ResponseWriter, r *http.Request) {
	if !s.publicRL.Allow(s.clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
		return
	}
	token := r.PathValue("token")
	view, err := s.Pipeline.PublicReplyView(r.Context(), token)
	if err != nil {
		code := http.StatusBadRequest
		if err == replytoken.ErrExpired {
			code = http.StatusGone
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) publicPost(w http.ResponseWriter, r *http.Request) {
	if !s.publicRL.Allow(s.clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
		return
	}
	token := r.PathValue("token")
	var body struct {
		Action    string `json:"action"`
		SlotIndex *int   `json:"slot_index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if body.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action required"})
		return
	}
	if err := s.Pipeline.HandlePublicAction(r.Context(), token, body.Action, body.SlotIndex); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	claims, _ := replytoken.Verify(s.Cfg.ReplyTokenSecret, token)
	app, _ := s.Pipeline.GetApplication(r.Context(), claims.AppID)
	view, _ := s.Pipeline.PublicReplyView(r.Context(), token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "application": app, "view": view})
}

func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(20 << 20); err != nil {
			http.Error(w, "invalid multipart", http.StatusBadRequest)
			return
		}
		jdID := r.FormValue("jd_id")
		email := r.FormValue("candidate_email")
		name := r.FormValue("candidate_name")
		file, hdr, err := r.FormFile("resume")
		if err != nil {
			http.Error(w, "resume file required", http.StatusBadRequest)
			return
		}
		defer file.Close()
		_ = os.MkdirAll(s.UploadDir, 0o755)
		ext := filepath.Ext(hdr.Filename)
		path := filepath.Join(s.UploadDir, uuid.NewString()+ext)
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		out, err := os.Create(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, err := s.Pipeline.Start(r.Context(), pipeline.StartInput{
			JDID: jdID, CandidateEmail: email, CandidateName: name, ResumePath: path,
		})
		if err != nil && id == "" {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		app, _ := s.Pipeline.GetApplication(r.Context(), id)
		// 202: pipeline continues in background; client should poll application status.
		writeJSON(w, http.StatusAccepted, map[string]any{"application_id": id, "application": app, "async": true, "warning": errString(err)})
		return
	}

	var body struct {
		JDID           string `json:"jd_id"`
		CandidateEmail string `json:"candidate_email"`
		CandidateName  string `json:"candidate_name"`
		ResumeText     string `json:"resume_text"`
		ResumePath     string `json:"resume_path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	path := body.ResumePath
	if path == "" && body.ResumeText != "" {
		_ = os.MkdirAll(s.UploadDir, 0o755)
		path = filepath.Join(s.UploadDir, uuid.NewString()+".txt")
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if err := os.WriteFile(path, []byte(body.ResumeText), 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				path = abs
			}
		}
	}
	id, err := s.Pipeline.Start(r.Context(), pipeline.StartInput{
		JDID: body.JDID, CandidateEmail: body.CandidateEmail,
		CandidateName: body.CandidateName, ResumePath: path, ResumeText: body.ResumeText,
	})
	if err != nil && id == "" {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	app, _ := s.Pipeline.GetApplication(r.Context(), id)
	writeJSON(w, http.StatusAccepted, map[string]any{"application_id": id, "application": app, "async": true, "warning": errString(err)})
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := s.Pipeline.GetApplication(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		EmailBody string `json:"email_body"`
		ThreadID  string `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.EmailBody == "" {
		http.Error(w, "email_body required", http.StatusBadRequest)
		return
	}
	appID := id
	if appID == "by-thread" || appID == "_" {
		appID = extractThread(body.EmailBody, body.ThreadID)
		if appID == "" {
			http.Error(w, "thread_id not found", http.StatusBadRequest)
			return
		}
	}
	if err := s.Pipeline.HandleReply(r.Context(), appID, body.EmailBody); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	app, _ := s.Pipeline.GetApplication(r.Context(), appID)
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) humanApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Pipeline.HumanApprove(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	app, _ := s.Pipeline.GetApplication(r.Context(), id)
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) retryParse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ResumeText string `json:"resume_text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.Pipeline.RetryParse(r.Context(), id, body.ResumeText); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	app, err := s.Pipeline.GetApplicationDetail(r.Context(), id)
	if err != nil {
		app, _ = s.Pipeline.GetApplication(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) sweepTimeouts(w http.ResponseWriter, r *http.Request) {
	n, err := s.Pipeline.SweepTimeouts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	out, err := s.Pipeline.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminListJDs(w http.ResponseWriter, r *http.Request) {
	items, err := s.Pipeline.ListJDs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminGetJD(w http.ResponseWriter, r *http.Request) {
	jd, err := s.Pipeline.GetJD(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, jd)
}

func (s *Server) adminUpsertJD(w http.ResponseWriter, r *http.Request) {
	var in pipeline.JDInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	id, err := s.Pipeline.UpsertJD(r.Context(), in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) adminUpdateJD(w http.ResponseWriter, r *http.Request) {
	var in pipeline.JDInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	in.ID = r.PathValue("id")
	id, err := s.Pipeline.UpsertJD(r.Context(), in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) adminDeleteJD(w http.ResponseWriter, r *http.Request) {
	if err := s.Pipeline.DeleteJD(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminListQuestionBank(w http.ResponseWriter, r *http.Request) {
	items, err := s.Pipeline.ListQuestionBank(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminGetQuestionBank(w http.ResponseWriter, r *http.Request) {
	item, err := s.Pipeline.GetQuestionBank(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminUpsertQuestionBank(w http.ResponseWriter, r *http.Request) {
	var in pipeline.QuestionBankInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	item, err := s.Pipeline.UpsertQuestionBank(r.Context(), in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminUpdateQuestionBank(w http.ResponseWriter, r *http.Request) {
	var in pipeline.QuestionBankInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	in.ID = r.PathValue("id")
	item, err := s.Pipeline.UpsertQuestionBank(r.Context(), in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminDeleteQuestionBank(w http.ResponseWriter, r *http.Request) {
	if err := s.Pipeline.DeleteQuestionBank(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminReindexQuestionBank(w http.ResponseWriter, r *http.Request) {
	out, err := s.Pipeline.ReindexQuestionBank(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminListStaff(w http.ResponseWriter, r *http.Request) {
	items, err := s.Pipeline.ListStaff(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminUpsertStaff(w http.ResponseWriter, r *http.Request) {
	var in pipeline.StaffInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	item, err := s.Pipeline.UpsertStaff(r.Context(), in, s.currentActor(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminUpdateStaff(w http.ResponseWriter, r *http.Request) {
	var in pipeline.StaffInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	in.OpenID = r.PathValue("open_id")
	item, err := s.Pipeline.UpsertStaff(r.Context(), in, s.currentActor(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminEnableStaff(w http.ResponseWriter, r *http.Request) {
	item, err := s.Pipeline.SetStaffEnabled(r.Context(), r.PathValue("open_id"), true, s.currentActor(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminDisableStaff(w http.ResponseWriter, r *http.Request) {
	item, err := s.Pipeline.SetStaffEnabled(r.Context(), r.PathValue("open_id"), false, s.currentActor(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminStaffAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.Pipeline.ListSystemAudit(r.Context(), 40)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminListJoinRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	items, err := s.Pipeline.ListJoinRequests(r.Context(), status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pending, _ := s.Pipeline.CountPendingJoinRequests(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "pending_count": pending})
}

func (s *Server) adminApproveJoin(w http.ResponseWriter, r *http.Request) {
	var in pipeline.ApproveJoinInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	// Applicants are HR by default; interviewer flag is optional (for freebusy pool only).
	if !in.IsHR {
		in.IsHR = true
	}
	item, err := s.Pipeline.ApproveJoinRequest(r.Context(), r.PathValue("id"), in, s.currentActor(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminRejectJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.Pipeline.RejectJoinRequest(r.Context(), r.PathValue("id"), body.Note, s.currentActor(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminDownloadResume(w http.ResponseWriter, r *http.Request) {
	path, err := s.Pipeline.GetResumePath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Prevent path escape outside upload dir when possible.
	clean := filepath.Clean(path)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(clean)+"\"")
	http.ServeFile(w, r, clean)
}

func (s *Server) adminListApps(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	items, err := s.Pipeline.ListApplications(r.Context(), status, 100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminGetApp(w http.ResponseWriter, r *http.Request) {
	app, err := s.Pipeline.GetApplicationDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) adminReplyToken(w http.ResponseWriter, r *http.Request) {
	tok, err := s.Pipeline.IssueReplyToken(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	base := strings.TrimRight(s.Cfg.PublicBaseURL, "/")
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok,
		"url":   base + "/r/" + tok,
	})
}

func extractThread(body, explicit string) string {
	if explicit != "" {
		return explicit
	}
	const marker = "[thread:"
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, "]")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
