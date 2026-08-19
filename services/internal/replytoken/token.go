package replytoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PurposeCandidateReply  = "candidate_reply"
	PurposeInterviewerPack = "interviewer_pack"
)

var (
	ErrInvalid = errors.New("invalid reply token")
	ErrExpired = errors.New("reply token expired")
)

type Claims struct {
	AppID      string `json:"app_id"`
	Exp        int64  `json:"exp"`
	Purpose    string `json:"purpose,omitempty"`
	OpenID     string `json:"open_id,omitempty"`
	RoundIndex int    `json:"round_index,omitempty"`
}

func Issue(secret, appID string, ttl time.Duration) (string, error) {
	return issueClaims(secret, Claims{AppID: appID, Exp: expUnix(ttl)}, ttl)
}

func IssueInterviewerPack(secret, appID, openID string, roundIndex int, ttl time.Duration) (string, error) {
	if strings.TrimSpace(openID) == "" {
		return "", fmt.Errorf("open_id required for interviewer pack token")
	}
	return issueClaims(secret, Claims{
		AppID: appID, Exp: expUnix(ttl), Purpose: PurposeInterviewerPack,
		OpenID: openID, RoundIndex: roundIndex,
	}, ttl)
}

func issueClaims(secret string, c Claims, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("REPLY_TOKEN_SECRET is empty")
	}
	if c.AppID == "" {
		return "", fmt.Errorf("app_id required")
	}
	if c.Exp == 0 {
		if ttl <= 0 {
			ttl = 48 * time.Hour
		}
		c.Exp = time.Now().Add(ttl).Unix()
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(p))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return p + "." + sig, nil
}

func expUnix(ttl time.Duration) int64 {
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return time.Now().Add(ttl).Unix()
}

func Verify(secret, token string) (Claims, error) {
	var zero Claims
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
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil || c.AppID == "" {
		return zero, ErrInvalid
	}
	if time.Now().Unix() > c.Exp {
		return zero, ErrExpired
	}
	return c, nil
}
