package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const httpTimeout = 30 * time.Second

// Client wraps the Jira REST API for issue creation.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Options configures Jira integration for auto-quarantine.
type Options struct {
	Server  string   `json:"server"`  // Jira server URL (e.g. https://redhat.atlassian.net)
	Token   string   `json:"token"`   // API token (PAT or Basic auth token)
	Project string   `json:"project"` // Project key (e.g. RHOAIENG)
	Labels  []string `json:"labels,omitempty"`
}

// Configured returns true when all required Jira fields are set.
func (o *Options) Configured() bool {
	return o.Server != "" && o.Token != "" && o.Project != ""
}

// LoadOptionsFromFile reads Jira options from a JSON config file.
// Fields in the file override zero-value fields in the existing Options.
func LoadOptionsFromFile(path string) (*Options, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading jira config %s: %w", path, err)
	}

	var opts Options
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil, fmt.Errorf("parsing jira config %s: %w", path, err)
	}

	return &opts, nil
}

// NewClient creates a Jira API client.
func NewClient(server, token string) *Client {
	return &Client{
		baseURL: server,
		token:   token,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// CreateIssueInput holds the fields for creating a Jira issue.
type CreateIssueInput struct {
	Project     string
	Summary     string
	Description string
	Labels      []string
	IssueType   string // defaults to "Bug" if empty
}

// CreateIssueResult holds the response from Jira after creating an issue.
type CreateIssueResult struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

// CreateIssue creates a new Jira issue and returns its key.
func (c *Client) CreateIssue(ctx context.Context, input CreateIssueInput) (*CreateIssueResult, error) {
	issueType := input.IssueType
	if issueType == "" {
		issueType = "Bug"
	}

	payload := map[string]interface{}{
		"fields": map[string]interface{}{
			"project":   map[string]string{"key": input.Project},
			"issuetype": map[string]string{"name": issueType},
			"summary":   input.Summary,
			"labels":    input.Labels,
		},
	}

	if input.Description != "" {
		fields := payload["fields"].(map[string]interface{})
		fields["description"] = input.Description
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling issue payload: %w", err)
	}

	url := fmt.Sprintf("%s/rest/api/2/issue", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Jira API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateIssueResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}

// doneCategories are Jira status category keys that indicate an issue is resolved.
var doneCategories = map[string]bool{
	"done": true,
}

// IsIssueDone returns true when the Jira issue's status category is "done"
// (covers Resolved, Closed, Done, and any custom status mapped to that category).
func (c *Client) IsIssueDone(ctx context.Context, issueKey string) (bool, error) {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s?fields=status", c.baseURL, issueKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("Jira API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var issue struct {
		Fields struct {
			Status struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return false, fmt.Errorf("parsing response: %w", err)
	}

	return doneCategories[issue.Fields.Status.StatusCategory.Key], nil
}
