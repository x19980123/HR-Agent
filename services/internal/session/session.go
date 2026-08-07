package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const CookieName = "hr_admin_session"

var (
	ErrInvalid = errors.New("invalid session")
	ErrExpired = errors.New("session expired")
)

type User struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	Avatar string `json:"avatar,omitempty"`
	Exp    int64  `json:"exp"`
}

func Issue(secret string, u User, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("session secret empty")
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	u.Exp = time.Now().Add(ttl).Unix()
	raw, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(p))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return p + "." + sig, nil
}

func Parse(secret, token string) (User, error) {
	var zero User
	if secret == "" || token == "" {
		return zero, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return zero, ErrInvalid
	}
	p, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(p))
	expect := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expect), []byte(sig)) {
		return zero, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return zero, ErrInvalid
	}
	var u User
	if err := json.Unmarshal(raw, &u); err != nil || u.OpenID == "" {
		return zero, ErrInvalid
	}
	if time.Now().Unix() > u.Exp {
		return zero, ErrExpired
	}
	return u, nil
}

func SetCookie(w http.ResponseWriter, value string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   maxAge,
	})
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func FromRequest(r *http.Request, secret string) (User, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return User{}, ErrInvalid
	}
	return Parse(secret, c.Value)
}
