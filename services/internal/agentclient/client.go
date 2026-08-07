package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: stringsTrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func stringsTrimRight(s, cut string) string {
	for len(s) > 0 && stringsHasSuffix(s, cut) {
		s = s[:len(s)-len(cut)]
	}
	return s
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

type PipelineRequest struct {
	ApplicationID string         `json:"application_id"`
	ResumePath    string         `json:"resume_path"`
	ResumeText    string         `json:"resume_text,omitempty"`
	JD            map[string]any `json:"jd"`
}

type PipelineResponse struct {
	Profile        map[string]any `json:"profile"`
	Screen         map[string]any `json:"screen"`
	Questions      []any          `json:"questions"`
	NeedsHuman     bool           `json:"needs_human"`
	Rejected       bool           `json:"rejected"`
	LangsmithRunID string         `json:"langsmith_run_id,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type QuestionsRequest struct {
	ApplicationID string         `json:"application_id"`
	Profile       map[string]any `json:"profile"`
	JD            map[string]any `json:"jd"`
}

type QuestionsResponse struct {
	Questions      []any  `json:"questions"`
	LangsmithRunID string `json:"langsmith_run_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type ClassifyRequest struct {
	ApplicationID string         `json:"application_id"`
	EmailBody     string         `json:"email_body"`
	Context       map[string]any `json:"context,omitempty"`
}

type ClassifyResponse struct {
	Intent            string   `json:"intent"`
	Confidence        float64  `json:"confidence"`
	PreferredWindows  []string `json:"preferred_windows,omitempty"`
	SelectedSlotIndex *int     `json:"selected_slot_index,omitempty"`
	LangsmithRunID    string   `json:"langsmith_run_id,omitempty"`
	Error             string   `json:"error,omitempty"`
}

func (c *Client) RunParseScreen(ctx context.Context, req PipelineRequest) (*PipelineResponse, error) {
	var out PipelineResponse
	if err := c.post(ctx, "/v1/pipeline/parse_screen", req, &out); err != nil {
		return nil, err
	}
	// needs_human responses may include a human-readable reason in Error; not a transport failure.
	if out.Error != "" && !out.NeedsHuman {
		return &out, fmt.Errorf("agent parse_screen: %s", out.Error)
	}
	return &out, nil
}

// RunPipeline is a legacy alias for parse+screen (questions deferred).
func (c *Client) RunPipeline(ctx context.Context, req PipelineRequest) (*PipelineResponse, error) {
	return c.RunParseScreen(ctx, req)
}

func (c *Client) GenerateQuestions(ctx context.Context, req QuestionsRequest) (*QuestionsResponse, error) {
	var out QuestionsResponse
	if err := c.post(ctx, "/v1/pipeline/generate_questions", req, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return &out, fmt.Errorf("agent generate_questions: %s", out.Error)
	}
	return &out, nil
}

func (c *Client) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error) {
	var out ClassifyResponse
	if err := c.post(ctx, "/v1/pipeline/classify", req, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return &out, fmt.Errorf("agent classify: %s", out.Error)
	}
	return &out, nil
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent health %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) RAGUpsert(ctx context.Context, item map[string]any) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, "/v1/rag/upsert", map[string]any{"item": item}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) RAGDelete(ctx context.Context, id string) error {
	var out map[string]any
	return c.post(ctx, "/v1/rag/delete", map[string]any{"id": id}, &out)
}

func (c *Client) RAGReindex(ctx context.Context, items []map[string]any) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, "/v1/rag/reindex", map[string]any{"items": items}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, in any, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("agent %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}
