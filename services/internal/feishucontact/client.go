package feishucontact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const feishuBase = "https://open.feishu.cn/open-apis"

type Client struct {
	AppID     string
	AppSecret string
	HTTP      *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

type User struct {
	OpenID     string `json:"open_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Department string `json:"department"`
}

func New(appID, appSecret string) *Client {
	return &Client{
		AppID:     strings.TrimSpace(appID),
		AppSecret: strings.TrimSpace(appSecret),
		HTTP:      &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.AppID != "" && c.AppSecret != ""
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("飞书应用未配置 FEISHU_APP_ID/SECRET")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}
	body := map[string]string{"app_id": c.AppID, "app_secret": c.AppSecret}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := c.doJSON(ctx, http.MethodPost, feishuBase+"/auth/v3/tenant_access_token/internal", "", body, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu token: code=%d msg=%s", out.Code, out.Msg)
	}
	c.token = out.TenantAccessToken
	exp := out.Expire
	if exp <= 0 {
		exp = 7200
	}
	c.tokenExp = time.Now().Add(time.Duration(exp-120) * time.Second)
	return c.token, nil
}

// SearchUsers finds Feishu users by name keyword or email.
// Uses tenant_access_token only (app identity). Name search walks visible departments
// via contact/v3 — do NOT call search/v1/user (that API requires user_access_token).
func (c *Client) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("q required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	if strings.Contains(q, "@") {
		users, err := c.searchByEmail(ctx, token, q)
		if err != nil {
			return nil, wrapPerm(err)
		}
		return users, nil
	}

	users, err := c.searchByName(ctx, token, q, limit)
	if err != nil {
		return nil, wrapPerm(err)
	}
	return users, nil
}

func wrapPerm(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "99991672") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "41050") ||
		strings.Contains(strings.ToLower(msg), "scope") ||
		strings.Contains(msg, "permission") ||
		strings.Contains(msg, "无权限") ||
		strings.Contains(msg, "no user authority") ||
		strings.Contains(msg, "no dept authority") {
		return fmt.Errorf("%w；请在飞书开放平台为应用开通通讯录读权限（contact:user.base:readonly 等）并创建版本发布；数据权限「通讯录权限范围」需覆盖目标成员/部门", err)
	}
	return err
}

func (c *Client) searchByEmail(ctx context.Context, token, email string) ([]User, error) {
	body := map[string]any{
		"emails":           []string{email},
		"include_resigned": false,
	}
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
				Email  string `json:"email"`
			} `json:"user_list"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, feishuBase+"/contact/v3/users/batch_get_id?user_id_type=open_id", token, body, &raw); err != nil {
		return nil, err
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("feishu batch_get_id: code=%d msg=%s", raw.Code, raw.Msg)
	}
	var out []User
	for _, u := range raw.Data.UserList {
		if u.UserID == "" {
			continue
		}
		info, err := c.getUser(ctx, token, u.UserID)
		if err != nil {
			out = append(out, User{OpenID: u.UserID, Email: u.Email})
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

func (c *Client) searchByName(ctx context.Context, token, query string, limit int) ([]User, error) {
	qLower := strings.ToLower(query)
	deptIDs, directUsers, err := c.contactScopes(ctx, token)
	if err != nil {
		// scopes API may need permission; fall back to root department
		deptIDs = []string{"0"}
	}
	if len(deptIDs) == 0 && len(directUsers) == 0 {
		deptIDs = []string{"0"}
	}

	seen := map[string]bool{}
	var out []User

	for _, oid := range directUsers {
		if len(out) >= limit {
			break
		}
		info, gerr := c.getUser(ctx, token, oid)
		if gerr != nil {
			continue
		}
		if !nameMatch(info.Name, qLower) {
			continue
		}
		if seen[info.OpenID] {
			continue
		}
		seen[info.OpenID] = true
		out = append(out, info)
	}

	// Cap department walk to avoid long requests on huge orgs.
	maxDepts := 40
	if len(deptIDs) > maxDepts {
		deptIDs = deptIDs[:maxDepts]
	}
	for _, deptID := range deptIDs {
		if len(out) >= limit {
			break
		}
		users, lerr := c.listDepartmentUsers(ctx, token, deptID, 5)
		if lerr != nil {
			// keep going; root "0" without all-staff scope often fails
			continue
		}
		for _, u := range users {
			if len(out) >= limit {
				break
			}
			if u.OpenID == "" || seen[u.OpenID] {
				continue
			}
			if !nameMatch(u.Name, qLower) {
				continue
			}
			seen[u.OpenID] = true
			out = append(out, u)
		}
	}

	if len(out) == 0 {
		// Try expanding child departments of root / first scoped dept once
		seed := "0"
		if len(deptIDs) > 0 {
			seed = deptIDs[0]
		}
		children, cerr := c.listChildDepartments(ctx, token, seed)
		if cerr == nil {
			for _, child := range children {
				if len(out) >= limit {
					break
				}
				users, lerr := c.listDepartmentUsers(ctx, token, child, 3)
				if lerr != nil {
					continue
				}
				for _, u := range users {
					if len(out) >= limit {
						break
					}
					if u.OpenID == "" || seen[u.OpenID] || !nameMatch(u.Name, qLower) {
						continue
					}
					seen[u.OpenID] = true
					out = append(out, u)
				}
			}
		}
	}

	if len(out) == 0 && err != nil {
		return nil, err
	}
	return out, nil
}

func nameMatch(name, qLower string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || qLower == "" {
		return false
	}
	return strings.Contains(n, qLower)
}

func (c *Client) contactScopes(ctx context.Context, token string) (deptIDs, userIDs []string, err error) {
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			DepartmentIDs []string `json:"department_ids"`
			UserIDs       []string `json:"user_ids"`
			OpenIDs       []string `json:"open_ids"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, feishuBase+"/contact/v3/scopes?user_id_type=open_id&department_id_type=open_department_id", token, nil, &raw); err != nil {
		return nil, nil, err
	}
	if raw.Code != 0 {
		return nil, nil, fmt.Errorf("feishu contact scopes: code=%d msg=%s", raw.Code, raw.Msg)
	}
	deptIDs = raw.Data.DepartmentIDs
	userIDs = raw.Data.UserIDs
	if len(userIDs) == 0 {
		userIDs = raw.Data.OpenIDs
	}
	return deptIDs, userIDs, nil
}

func (c *Client) listDepartmentUsers(ctx context.Context, token, deptID string, maxPages int) ([]User, error) {
	if maxPages <= 0 {
		maxPages = 3
	}
	pageToken := ""
	var out []User
	for page := 0; page < maxPages; page++ {
		q := url.Values{}
		q.Set("department_id", deptID)
		q.Set("user_id_type", "open_id")
		q.Set("department_id_type", "open_department_id")
		q.Set("page_size", "50")
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		path := feishuBase + "/contact/v3/users/find_by_department?" + q.Encode()
		var raw struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
				Items     []struct {
					OpenID          string   `json:"open_id"`
					Name            string   `json:"name"`
					Email           string   `json:"email"`
					EnterpriseEmail string   `json:"enterprise_email"`
					DepartmentIDs   []string `json:"department_ids"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &raw); err != nil {
			return out, err
		}
		if raw.Code != 0 {
			return out, fmt.Errorf("feishu find_by_department: code=%d msg=%s", raw.Code, raw.Msg)
		}
		for _, it := range raw.Data.Items {
			email := it.Email
			if email == "" {
				email = it.EnterpriseEmail
			}
			dept := ""
			if len(it.DepartmentIDs) > 0 {
				dept = it.DepartmentIDs[0]
			}
			out = append(out, User{
				OpenID:     it.OpenID,
				Name:       it.Name,
				Email:      email,
				Department: dept,
			})
		}
		if !raw.Data.HasMore || raw.Data.PageToken == "" {
			break
		}
		pageToken = raw.Data.PageToken
	}
	return out, nil
}

func (c *Client) listChildDepartments(ctx context.Context, token, parentID string) ([]string, error) {
	q := url.Values{}
	q.Set("parent_department_id", parentID)
	q.Set("department_id_type", "open_department_id")
	q.Set("page_size", "50")
	path := feishuBase + "/contact/v3/departments/" + url.PathEscape(parentID) + "/children?" + q.Encode()
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				OpenDepartmentID string `json:"open_department_id"`
				DepartmentID     string `json:"department_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &raw); err != nil {
		return nil, err
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("feishu dept children: code=%d msg=%s", raw.Code, raw.Msg)
	}
	var ids []string
	for _, it := range raw.Data.Items {
		id := it.OpenDepartmentID
		if id == "" {
			id = it.DepartmentID
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *Client) getUser(ctx context.Context, token, openID string) (User, error) {
	path := fmt.Sprintf("%s/contact/v3/users/%s?user_id_type=open_id&department_id_type=open_department_id",
		feishuBase, url.PathEscape(openID))
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				OpenID          string   `json:"open_id"`
				Name            string   `json:"name"`
				Email           string   `json:"email"`
				EnterpriseEmail string   `json:"enterprise_email"`
				DepartmentIDs   []string `json:"department_ids"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &raw); err != nil {
		return User{}, err
	}
	if raw.Code != 0 {
		return User{}, fmt.Errorf("feishu get user: code=%d msg=%s", raw.Code, raw.Msg)
	}
	u := raw.Data.User
	email := u.Email
	if email == "" {
		email = u.EnterpriseEmail
	}
	dept := ""
	if len(u.DepartmentIDs) > 0 {
		dept = u.DepartmentIDs[0]
		if name, err := c.getDepartmentName(ctx, token, dept); err == nil && name != "" {
			dept = name
		}
	}
	return User{
		OpenID:     u.OpenID,
		Name:       u.Name,
		Email:      email,
		Department: dept,
	}, nil
}

func (c *Client) getDepartmentName(ctx context.Context, token, deptID string) (string, error) {
	path := fmt.Sprintf("%s/contact/v3/departments/%s?department_id_type=open_department_id",
		feishuBase, url.PathEscape(deptID))
	var raw struct {
		Code int `json:"code"`
		Data struct {
			Department struct {
				Name string `json:"name"`
			} `json:"department"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &raw); err != nil {
		return "", err
	}
	if raw.Code != 0 {
		return "", fmt.Errorf("dept code=%d", raw.Code)
	}
	return raw.Data.Department.Name, nil
}

func (c *Client) doJSON(ctx context.Context, method, urlStr, token string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// Feishu often returns business errors with HTTP 400 + JSON {code,msg}.
	// Prefer decoding so callers can surface code/msg instead of raw HTTP text.
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			if resp.StatusCode >= 300 {
				return fmt.Errorf("feishu http %d: %s", resp.StatusCode, truncate(string(raw), 280))
			}
			return err
		}
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("feishu http %d: %s", resp.StatusCode, truncate(string(raw), 280))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
