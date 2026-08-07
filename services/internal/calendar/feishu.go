package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const feishuBase = "https://open.feishu.cn/open-apis"

// FeishuConfig holds Feishu calendar credentials.
type FeishuConfig struct {
	AppID             string
	AppSecret         string
	CalendarID        string // optional; empty => create/use shared "HR面试排期"
	InterviewerUserID string // seed / fallback single interviewer
	InterviewerName   string // display name in event summary/description
	InterviewerIDs    []string // runtime pool (from staff_members); overrides single ID when set
	UserIDType        string   // open_id | user_id | union_id
	Timezone          string
	Location          string
}

// FeishuProvider implements Provider against Feishu Calendar OpenAPI.
type FeishuProvider struct {
	cfg        FeishuConfig
	http       *http.Client
	mu         sync.Mutex
	token      string
	tokenExp   time.Time
	calendarID string
	holds      map[string]Slot // eventID -> slot
}

func NewFeishuProvider(cfg FeishuConfig) (*FeishuProvider, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu: APP_ID and APP_SECRET required")
	}
	if cfg.UserIDType == "" {
		cfg.UserIDType = "open_id"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Shanghai"
	}
	if cfg.Location == "" {
		cfg.Location = "飞书会议 / 线上面试"
	}
	p := &FeishuProvider{
		cfg:        cfg,
		http:       &http.Client{Timeout: 30 * time.Second},
		calendarID: cfg.CalendarID,
		holds:      map[string]Slot{},
	}
	return p, nil
}

func (p *FeishuProvider) ListSlots(ctx context.Context, c Constraints) ([]Slot, error) {
	if c.Duration == 0 {
		c.Duration = time.Hour
	}
	if c.Limit <= 0 {
		c.Limit = 3
	}
	loc, err := time.LoadLocation(p.cfg.Timezone)
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	after := c.After
	if after.IsZero() {
		after = time.Now().In(loc).Add(24 * time.Hour)
	} else {
		after = after.In(loc)
	}

	// candidate window ~ 14 days
	windowEnd := after.Add(14 * 24 * time.Hour)
	busy, err := p.listBusy(ctx, after, windowEnd)
	if err != nil {
		// freebusy optional; continue without if interviewer not configured
		if len(p.interviewerIDs()) > 0 {
			return nil, err
		}
		busy = nil
	}

	t := time.Date(after.Year(), after.Month(), after.Day(), 10, 0, 0, 0, loc)
	if !t.After(after) {
		t = t.Add(24 * time.Hour)
	}

	var out []Slot
	for len(out) < c.Limit && t.Before(windowEnd) {
		for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			t = t.Add(24 * time.Hour)
		}
		end := t.Add(c.Duration)
		if end.Hour() > 18 || (end.Hour() == 18 && end.Minute() > 0) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 10, 0, 0, 0, loc)
			continue
		}
		if !overlapsBusy(t, end, busy) {
			out = append(out, Slot{
				ID:       uuid.NewString(),
				StartsAt: t,
				EndsAt:   end,
				Location: p.cfg.Location,
			})
		}
		next := t.Add(2 * time.Hour)
		if next.Hour() >= 18 {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 10, 0, 0, 0, loc)
		} else {
			t = next
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("feishu: no free slots in next 14 days")
	}
	return out, nil
}

func (p *FeishuProvider) Hold(ctx context.Context, slot Slot, applicationID string) (BookResult, error) {
	// Called only after candidate confirms — creates the real interview event.
	calID, err := p.ensureCalendar(ctx)
	if err != nil {
		return BookResult{}, err
	}
	interviewer := p.cfg.InterviewerName
	if interviewer == "" {
		interviewer = p.cfg.InterviewerUserID
	}
	loc := slot.Location
	if loc == "" {
		loc = p.cfg.Location
	}
	summary := "面试已确认"
	if interviewer != "" {
		summary = fmt.Sprintf("面试（%s）%s", interviewer, slot.StartsAt.Format("01-02 15:04"))
	} else {
		summary = fmt.Sprintf("面试 %s", slot.StartsAt.Format("01-02 15:04"))
	}
	desc := fmt.Sprintf("候选人已确认面试安排（HR-Agent）\napplication_id=%s\n面试官：%s\n地点：%s",
		applicationID, interviewer, loc)
	eventID, meetingURL, err := p.createEvent(ctx, calID, summary, desc, slot.StartsAt, slot.EndsAt)
	if err != nil {
		return BookResult{}, err
	}
	// Best-effort: event is usable even if attendee add fails (check calendar ACL / open_id).
	_ = p.addAttendees(ctx, calID, eventID)
	slot.EventID = eventID
	p.mu.Lock()
	p.holds[eventID] = slot
	p.mu.Unlock()
	return BookResult{EventID: eventID, MeetingURL: meetingURL, Location: loc}, nil
}

func (p *FeishuProvider) Confirm(ctx context.Context, eventID string) error {
	calID, err := p.ensureCalendar(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	slot, ok := p.holds[eventID]
	p.mu.Unlock()
	interviewer := p.cfg.InterviewerName
	if interviewer == "" {
		interviewer = p.cfg.InterviewerUserID
	}
	summary := "面试已确认"
	if ok {
		if interviewer != "" {
			summary = fmt.Sprintf("面试（%s）%s", interviewer, slot.StartsAt.Format("01-02 15:04"))
		} else {
			summary = fmt.Sprintf("面试 %s", slot.StartsAt.Format("01-02 15:04"))
		}
	}
	_ = p.addAttendees(ctx, calID, eventID) // ensure interviewer is on the confirmed event
	return p.patchEvent(ctx, calID, eventID, map[string]any{
		"summary": summary,
		"description": fmt.Sprintf("候选人已确认面试安排（HR-Agent）\n面试官：%s\n地点：%s", interviewer, p.cfg.Location),
	})
}

func (p *FeishuProvider) Release(ctx context.Context, eventID string) error {
	if eventID == "" {
		return nil
	}
	calID, err := p.ensureCalendar(ctx)
	if err != nil {
		return err
	}
	if err := p.deleteEvent(ctx, calID, eventID); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.holds, eventID)
	p.mu.Unlock()
	return nil
}

// --- Feishu HTTP helpers ---

type feishuResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (p *FeishuProvider) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.tokenExp) {
		return p.token, nil
	}
	body := map[string]string{
		"app_id":     p.cfg.AppID,
		"app_secret": p.cfg.AppSecret,
	}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := p.doJSON(ctx, http.MethodPost, feishuBase+"/auth/v3/tenant_access_token/internal", "", body, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu token: code=%d msg=%s", out.Code, out.Msg)
	}
	p.token = out.TenantAccessToken
	exp := out.Expire
	if exp <= 0 {
		exp = 7200
	}
	p.tokenExp = time.Now().Add(time.Duration(exp-120) * time.Second)
	return p.token, nil
}

func (p *FeishuProvider) ensureCalendar(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.calendarID != "" {
		id := p.calendarID
		p.mu.Unlock()
		return id, nil
	}
	p.mu.Unlock()

	token, err := p.accessToken(ctx)
	if err != nil {
		return "", err
	}
	reqBody := map[string]any{
		"summary":     "HR面试排期",
		"description": "HR-Agent 自动创建的共享日历",
		"permissions": "private",
	}
	var raw feishuResp
	if err := p.doJSON(ctx, http.MethodPost, feishuBase+"/calendar/v4/calendars", token, reqBody, &raw); err != nil {
		return "", err
	}
	if raw.Code != 0 {
		return "", fmt.Errorf("feishu create calendar: code=%d msg=%s", raw.Code, raw.Msg)
	}
	var data struct {
		Calendar struct {
			CalendarID string `json:"calendar_id"`
		} `json:"calendar"`
	}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		return "", err
	}
	if data.Calendar.CalendarID == "" {
		return "", fmt.Errorf("feishu create calendar: empty calendar_id")
	}
	p.mu.Lock()
	p.calendarID = data.Calendar.CalendarID
	p.mu.Unlock()
	return data.Calendar.CalendarID, nil
}

type busyRange struct {
	Start time.Time
	End   time.Time
}

func (p *FeishuProvider) interviewerIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cfg.InterviewerIDs) > 0 {
		out := make([]string, 0, len(p.cfg.InterviewerIDs))
		for _, id := range p.cfg.InterviewerIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	if p.cfg.InterviewerUserID != "" {
		return []string{p.cfg.InterviewerUserID}
	}
	return nil
}

func (p *FeishuProvider) SetInterviewerUserIDs(ids []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	clean := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	p.cfg.InterviewerIDs = clean
}

func (p *FeishuProvider) listBusy(ctx context.Context, from, to time.Time) ([]busyRange, error) {
	ids := p.interviewerIDs()
	if len(ids) == 0 {
		return nil, nil
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var out []busyRange
	q := url.Values{}
	q.Set("user_id_type", p.cfg.UserIDType)
	path := feishuBase + "/calendar/v4/freebusy/list?" + q.Encode()
	for _, uid := range ids {
		body := map[string]any{
			"time_min":  from.Format(time.RFC3339),
			"time_max":  to.Format(time.RFC3339),
			"user_id":   uid,
			"only_busy": true,
		}
		var raw feishuResp
		if err := p.doJSON(ctx, http.MethodPost, path, token, body, &raw); err != nil {
			return nil, err
		}
		if raw.Code != 0 {
			return nil, fmt.Errorf("feishu freebusy: code=%d msg=%s", raw.Code, raw.Msg)
		}
		var data struct {
			FreebusyList []struct {
				StartTime string `json:"start_time"`
				EndTime   string `json:"end_time"`
			} `json:"freebusy_list"`
		}
		if len(raw.Data) > 0 {
			_ = json.Unmarshal(raw.Data, &data)
		}
		for _, b := range data.FreebusyList {
			s, e1 := time.Parse(time.RFC3339, b.StartTime)
			e, e2 := time.Parse(time.RFC3339, b.EndTime)
			if e1 != nil || e2 != nil {
				continue
			}
			out = append(out, busyRange{Start: s, End: e})
		}
	}
	return out, nil
}

func overlapsBusy(start, end time.Time, busy []busyRange) bool {
	for _, b := range busy {
		if start.Before(b.End) && end.After(b.Start) {
			return true
		}
	}
	return false
}

func (p *FeishuProvider) createEvent(ctx context.Context, calID, summary, desc string, start, end time.Time) (eventID, meetingURL string, err error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return "", "", err
	}
	body := map[string]any{
		"summary":     summary,
		"description": desc,
		"start_time": map[string]string{
			"timestamp": strconv.FormatInt(start.Unix(), 10),
			"timezone":  p.cfg.Timezone,
		},
		"end_time": map[string]string{
			"timestamp": strconv.FormatInt(end.Unix(), 10),
			"timezone":  p.cfg.Timezone,
		},
		"free_busy_status": "busy",
		"attendee_ability": "can_modify_event",
		"vchat": map[string]string{
			"vc_type": "vc",
		},
	}
	if p.cfg.Location != "" {
		body["location"] = map[string]string{"name": p.cfg.Location}
	}
	var raw feishuResp
	path := fmt.Sprintf("%s/calendar/v4/calendars/%s/events?user_id_type=%s",
		feishuBase, url.PathEscape(calID), url.QueryEscape(p.cfg.UserIDType))
	if err := p.doJSON(ctx, http.MethodPost, path, token, body, &raw); err != nil {
		return "", "", err
	}
	if raw.Code != 0 {
		return "", "", fmt.Errorf("feishu create event: code=%d msg=%s", raw.Code, raw.Msg)
	}
	var data struct {
		Event struct {
			EventID string `json:"event_id"`
			Vchat   struct {
				MeetingURL string `json:"meeting_url"`
				MeetingNo  string `json:"meeting_no"`
			} `json:"vchat"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		return "", "", err
	}
	if data.Event.EventID == "" {
		return "", "", fmt.Errorf("feishu create event: empty event_id")
	}
	return data.Event.EventID, data.Event.Vchat.MeetingURL, nil
}

func (p *FeishuProvider) addAttendees(ctx context.Context, calID, eventID string) error {
	ids := p.interviewerIDs()
	if len(ids) == 0 || eventID == "" {
		return nil
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	atts := make([]map[string]any, 0, len(ids))
	for _, uid := range ids {
		atts = append(atts, map[string]any{
			"type":        "user",
			"is_optional": false,
			"user_id":     uid,
		})
	}
	body := map[string]any{
		"attendees":         atts,
		"need_notification": true,
	}
	var raw feishuResp
	path := fmt.Sprintf("%s/calendar/v4/calendars/%s/events/%s/attendees?user_id_type=%s",
		feishuBase, url.PathEscape(calID), url.PathEscape(eventID), url.QueryEscape(p.cfg.UserIDType))
	if err := p.doJSON(ctx, http.MethodPost, path, token, body, &raw); err != nil {
		return err
	}
	// 190004 / similar: attendee already exists — treat as success
	if raw.Code != 0 && raw.Code != 190004 && raw.Code != 191003 {
		return fmt.Errorf("feishu add attendees: code=%d msg=%s", raw.Code, raw.Msg)
	}
	return nil
}

func (p *FeishuProvider) EnsureCalendarACL(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	calID, err := p.ensureCalendar(ctx)
	if err != nil {
		return err
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{
		"role": "writer",
		"scope": map[string]any{
			"type": "user",
			"user_id": userID,
		},
	}
	var raw feishuResp
	path := fmt.Sprintf("%s/calendar/v4/calendars/%s/acls?user_id_type=%s",
		feishuBase, url.PathEscape(calID), url.QueryEscape(p.cfg.UserIDType))
	if err := p.doJSON(ctx, http.MethodPost, path, token, body, &raw); err != nil {
		return err
	}
	// already exists / duplicate — ok
	if raw.Code != 0 && raw.Code != 190002 && raw.Code != 191001 && raw.Code != 191003 {
		return fmt.Errorf("feishu create acl: code=%d msg=%s", raw.Code, raw.Msg)
	}
	return nil
}

func (p *FeishuProvider) RemoveCalendarACL(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	calID, err := p.ensureCalendar(ctx)
	if err != nil {
		return err
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	// List ACLs and delete matching user
	listPath := fmt.Sprintf("%s/calendar/v4/calendars/%s/acls?user_id_type=%s&page_size=50",
		feishuBase, url.PathEscape(calID), url.QueryEscape(p.cfg.UserIDType))
	var raw feishuResp
	if err := p.doJSON(ctx, http.MethodGet, listPath, token, nil, &raw); err != nil {
		return err
	}
	if raw.Code != 0 {
		return fmt.Errorf("feishu list acl: code=%d msg=%s", raw.Code, raw.Msg)
	}
	var data struct {
		Acls []struct {
			ACLID string `json:"acl_id"`
			Scope struct {
				Type   string `json:"type"`
				UserID string `json:"user_id"`
			} `json:"scope"`
		} `json:"acls"`
	}
	if len(raw.Data) > 0 {
		_ = json.Unmarshal(raw.Data, &data)
	}
	for _, a := range data.Acls {
		if a.Scope.Type == "user" && a.Scope.UserID == userID && a.ACLID != "" {
			delPath := fmt.Sprintf("%s/calendar/v4/calendars/%s/acls/%s",
				feishuBase, url.PathEscape(calID), url.PathEscape(a.ACLID))
			var delRaw feishuResp
			if err := p.doJSON(ctx, http.MethodDelete, delPath, token, nil, &delRaw); err != nil {
				return err
			}
			if delRaw.Code != 0 && delRaw.Code != 191002 {
				return fmt.Errorf("feishu delete acl: code=%d msg=%s", delRaw.Code, delRaw.Msg)
			}
		}
	}
	return nil
}

func (p *FeishuProvider) patchEvent(ctx context.Context, calID, eventID string, patch map[string]any) error {
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	var raw feishuResp
	path := fmt.Sprintf("%s/calendar/v4/calendars/%s/events/%s", feishuBase, url.PathEscape(calID), url.PathEscape(eventID))
	if err := p.doJSON(ctx, http.MethodPatch, path, token, patch, &raw); err != nil {
		return err
	}
	if raw.Code != 0 {
		return fmt.Errorf("feishu patch event: code=%d msg=%s", raw.Code, raw.Msg)
	}
	return nil
}

func (p *FeishuProvider) deleteEvent(ctx context.Context, calID, eventID string) error {
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	var raw feishuResp
	path := fmt.Sprintf("%s/calendar/v4/calendars/%s/events/%s", feishuBase, url.PathEscape(calID), url.PathEscape(eventID))
	if err := p.doJSON(ctx, http.MethodDelete, path, token, nil, &raw); err != nil {
		return err
	}
	// 404-ish: treat as already gone
	if raw.Code != 0 && raw.Code != 191002 /* maybe not found variants */ {
		// still ok if event already deleted
		if raw.Code == 190002 || raw.Code == 191001 {
			return nil
		}
		return fmt.Errorf("feishu delete event: code=%d msg=%s", raw.Code, raw.Msg)
	}
	return nil
}

func (p *FeishuProvider) doJSON(ctx context.Context, method, urlStr, token string, body any, out any) error {
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
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("feishu http %d: %s", resp.StatusCode, string(raw))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}
