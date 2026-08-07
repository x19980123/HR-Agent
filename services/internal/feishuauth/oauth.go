package feishuauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	tokenURL     = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	userInfoURL  = "https://open.feishu.cn/open-apis/authen/v1/user_info"
)

type Client struct {
	AppID       string
	AppSecret   string
	RedirectURI string
	HTTP        *http.Client
	Scopes      []string
}

func New(appID, appSecret, redirectURI string) *Client {
	return &Client{
		AppID:       appID,
		AppSecret:   appSecret,
		RedirectURI: redirectURI,
		HTTP:        &http.Client{Timeout: 20 * time.Second},
		// Match Feishu console scopes (user_access_token tab). Avoid requesting
		// scopes that are not applied, or authorize page returns 20027.
		Scopes: []string{"auth:user_access_token:read"},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.AppID != "" && c.AppSecret != "" && c.RedirectURI != ""
}

func (c *Client) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.AppID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("state", state)
	if len(c.Scopes) > 0 {
		q.Set("scope", strings.Join(c.Scopes, " "))
	}
	return authorizeURL + "?" + q.Encode()
}

type TokenResponse struct {
	Code                 int    `json:"code"`
	AccessToken          string `json:"access_token"`
	ExpiresIn            int    `json:"expires_in"`
	RefreshToken         string `json:"refresh_token"`
	RefreshTokenExpiresIn int   `json:"refresh_token_expires_in"`
	TokenType            string `json:"token_type"`
	Scope                string `json:"scope"`
	Error                string `json:"error"`
	ErrorDescription     string `json:"error_description"`
}

func (c *Client) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     c.AppID,
		"client_secret": c.AppSecret,
		"code":          code,
		"redirect_uri":  c.RedirectURI,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out TokenResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode token: %w body=%s", err, string(b))
	}
	if out.Code != 0 || out.AccessToken == "" {
		msg := out.ErrorDescription
		if msg == "" {
			msg = out.Error
		}
		if msg == "" {
			msg = string(b)
		}
		return nil, fmt.Errorf("feishu token: code=%d %s", out.Code, msg)
	}
	return &out, nil
}

type UserInfo struct {
	OpenID        string `json:"open_id"`
	UnionID       string `json:"union_id"`
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	EnName        string `json:"en_name"`
	Email         string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
	AvatarURL     string `json:"avatar_url"`
	AvatarThumb   string `json:"avatar_thumb"`
}

func (c *Client) UserInfo(ctx context.Context, userAccessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+userAccessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var wrap struct {
		Code int      `json:"code"`
		Msg  string   `json:"msg"`
		Data UserInfo `json:"data"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, fmt.Errorf("decode user_info: %w body=%s", err, string(b))
	}
	if wrap.Code != 0 || wrap.Data.OpenID == "" {
		return nil, fmt.Errorf("feishu user_info: code=%d %s body=%s", wrap.Code, wrap.Msg, string(b))
	}
	return &wrap.Data, nil
}
