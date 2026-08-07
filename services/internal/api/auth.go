package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/hr-agent/services/internal/feishuauth"
	"github.com/hr-agent/services/internal/pipeline"
	"github.com/hr-agent/services/internal/session"
)

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"feishu_login": s.feishuLoginEnabled(),
		"token_login":  s.Cfg.HRAPIToken != "",
	})
}

func (s *Server) feishuLoginEnabled() bool {
	return s.Cfg.FeishuLoginEnabled && s.FeishuAuth != nil && s.FeishuAuth.Enabled()
}

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	if u, err := session.FromRequest(r, s.Cfg.SessionSecret); err == nil {
		isAdmin := s.Pipeline.IsSystemAdmin(r.Context(), u.OpenID)
		writeJSON(w, http.StatusOK, map[string]any{
			"auth":     "feishu",
			"is_admin": isAdmin,
			"user": map[string]any{
				"open_id":  u.OpenID,
				"name":     u.Name,
				"email":    u.Email,
				"avatar":   u.Avatar,
				"is_admin": isAdmin,
			},
		})
		return
	}
	if s.checkBearer(r, s.Cfg.HRAPIToken) {
		writeJSON(w, http.StatusOK, map[string]any{
			"auth":     "token",
			"is_admin": true,
			"user":     map[string]any{"name": "API Token", "is_admin": true},
		})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	session.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) feishuLogin(w http.ResponseWriter, r *http.Request) {
	if !s.feishuLoginEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feishu login disabled"})
		return
	}
	state, err := randomHex(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "state failed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "hr_oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
		Secure:   strings.HasPrefix(s.Cfg.PublicBaseURL, "https://"),
	})
	http.Redirect(w, r, s.FeishuAuth.AuthURL(state), http.StatusFound)
}

func (s *Server) feishuCallback(w http.ResponseWriter, r *http.Request) {
	if !s.feishuLoginEnabled() {
		http.Error(w, "feishu login disabled", http.StatusBadRequest)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Redirect(w, r, "/admin/?login_error="+errParam, http.StatusFound)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		http.Redirect(w, r, "/admin/?login_error=missing_code", http.StatusFound)
		return
	}
	sc, err := r.Cookie("hr_oauth_state")
	if err != nil || sc.Value == "" || sc.Value != state {
		http.Redirect(w, r, "/admin/?login_error=bad_state", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "hr_oauth_state", Value: "", Path: "/", MaxAge: -1})

	tok, err := s.FeishuAuth.ExchangeCode(r.Context(), code)
	if err != nil {
		http.Redirect(w, r, "/admin/?login_error=token_exchange", http.StatusFound)
		return
	}
	info, err := s.FeishuAuth.UserInfo(r.Context(), tok.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/admin/?login_error=user_info", http.StatusFound)
		return
	}
	email := info.EnterpriseEmail
	if email == "" {
		email = info.Email
	}
	name := info.Name
	if name == "" {
		name = info.EnName
	}
	avatar := firstNonEmpty(info.AvatarURL, info.AvatarThumb)

	if !s.feishuUserAllowed(r, info) {
		// Not yet an HR member → submit join request (open_id comes from Feishu automatically)
		status, err := s.Pipeline.SubmitJoinRequest(r.Context(), pipeline.JoinRequestInput{
			OpenID: info.OpenID,
			Name:   name,
			Email:  email,
			Avatar: avatar,
		})
		if err != nil {
			http.Redirect(w, r, "/admin/?login_error=join_failed", http.StatusFound)
			return
		}
		switch status {
		case "already_member":
			if s.feishuUserAllowed(r, info) {
				// fall through to issue session
				break
			}
			http.Redirect(w, r, "/admin/?login_info=join_pending", http.StatusFound)
			return
		case "already_pending":
			http.Redirect(w, r, "/admin/?login_info=join_pending", http.StatusFound)
			return
		case "disabled":
			http.Redirect(w, r, "/admin/?login_error=account_disabled", http.StatusFound)
			return
		case "not_hr":
			http.Redirect(w, r, "/admin/?login_error=not_hr", http.StatusFound)
			return
		default:
			http.Redirect(w, r, "/admin/?login_info=join_submitted", http.StatusFound)
			return
		}
	}

	s.Pipeline.TouchStaffProfile(r.Context(), info.OpenID, name, email)
	sess, err := session.Issue(s.Cfg.SessionSecret, session.User{
		OpenID: info.OpenID,
		Name:   name,
		Email:  email,
		Avatar: avatar,
	}, 12*time.Hour)
	if err != nil {
		http.Redirect(w, r, "/admin/?login_error=session", http.StatusFound)
		return
	}
	session.SetCookie(w, sess, int((12 * time.Hour).Seconds()), strings.HasPrefix(s.Cfg.PublicBaseURL, "https://"))
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

func (s *Server) feishuUserAllowed(r *http.Request, info *feishuauth.UserInfo) bool {
	if info == nil || info.OpenID == "" {
		return false
	}
	email := strings.ToLower(strings.TrimSpace(info.EnterpriseEmail))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(info.Email))
	}

	ok, err := s.Pipeline.IsHRAllowed(r.Context(), info.OpenID, email)
	if err == nil && ok {
		return true
	}
	if err == nil && !ok {
		return false
	}
	// Empty table or query error → fallback to env seed + allowlists
	if err == sql.ErrNoRows || err != nil {
		if s.envFallbackAllowed(info, email) {
			return true
		}
	}
	return false
}

func (s *Server) envFallbackAllowed(info *feishuauth.UserInfo, email string) bool {
	seed := strings.TrimSpace(s.Cfg.FeishuInterviewerUserID)
	if seed != "" && (info.OpenID == seed || info.UserID == seed || info.UnionID == seed) {
		return true
	}
	allowIDs := s.Cfg.FeishuHRAllowOpenIDs
	allowEmails := s.Cfg.FeishuHRAllowEmails
	if len(allowIDs) == 0 && len(allowEmails) == 0 {
		// When staff table is empty/unavailable, only seed interviewer may enter
		// (safer than open-to-all). If no seed configured, deny.
		return false
	}
	for _, id := range allowIDs {
		if id == info.OpenID || id == info.UserID || id == info.UnionID {
			return true
		}
	}
	for _, e := range allowEmails {
		if strings.ToLower(strings.TrimSpace(e)) == email && email != "" {
			return true
		}
	}
	return false
}

func (s *Server) currentActor(r *http.Request) string {
	if u, err := session.FromRequest(r, s.Cfg.SessionSecret); err == nil && u.OpenID != "" {
		return u.OpenID
	}
	if s.checkBearer(r, s.Cfg.HRAPIToken) {
		return "api_token"
	}
	return "system"
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
