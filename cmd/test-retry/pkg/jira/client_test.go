package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/jira"
)

func TestCreateIssue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		response   map[string]string
		wantErr    bool
		wantKey    string
	}{
		{
			name:       "successful creation",
			statusCode: http.StatusCreated,
			response:   map[string]string{"key": "RHOAIENG-12345", "id": "99999"},
			wantKey:    "RHOAIENG-12345",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   map[string]string{"errorMessages": "internal error"},
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"message": "unauthorized"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/rest/api/2/issue", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				var payload map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&payload)
				require.NoError(t, err)

				fields := payload["fields"].(map[string]interface{})
				project := fields["project"].(map[string]interface{})
				assert.Equal(t, "RHOAIENG", project["key"])
				assert.Equal(t, "Flaky test: TestFoo", fields["summary"])

				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			client := jira.NewClient(srv.URL, "test-token")
			result, err := client.CreateIssue(context.Background(), jira.CreateIssueInput{
				Project:     "RHOAIENG",
				Summary:     "Flaky test: TestFoo",
				Description: "Test is flaky at 30%",
				Labels:      []string{"e2e-flaky-quarantine"},
			})

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKey, result.Key)
			}
		})
	}
}

func TestLoadOptionsFromFile(t *testing.T) {
	t.Parallel()

	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir() + "/jira.json"
		content := `{"server":"https://jira.example.com","token":"secret","project":"PROJ","labels":["flaky"]}`
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))

		opts, err := jira.LoadOptionsFromFile(path)
		require.NoError(t, err)
		assert.Equal(t, "https://jira.example.com", opts.Server)
		assert.Equal(t, "secret", opts.Token)
		assert.Equal(t, "PROJ", opts.Project)
		assert.Equal(t, []string{"flaky"}, opts.Labels)
		assert.True(t, opts.Configured())
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()
		_, err := jira.LoadOptionsFromFile("/nonexistent/path.json")
		assert.Error(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir() + "/bad.json"
		require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0600))

		_, err := jira.LoadOptionsFromFile(path)
		assert.Error(t, err)
	})
}

func TestIsIssueDone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statusCode  int
		categoryKey string
		wantDone    bool
		wantErr     bool
	}{
		{
			name:        "done category",
			statusCode:  http.StatusOK,
			categoryKey: "done",
			wantDone:    true,
		},
		{
			name:        "in-progress category",
			statusCode:  http.StatusOK,
			categoryKey: "indeterminate",
			wantDone:    false,
		},
		{
			name:        "new category",
			statusCode:  http.StatusOK,
			categoryKey: "new",
			wantDone:    false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/rest/api/2/issue/RHOAIENG-100", r.URL.Path)
				assert.Equal(t, "fields=status", r.URL.RawQuery)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"fields": map[string]interface{}{
							"status": map[string]interface{}{
								"name": "Done",
								"statusCategory": map[string]interface{}{
									"key": tt.categoryKey,
								},
							},
						},
					})
				}
			}))
			defer srv.Close()

			client := jira.NewClient(srv.URL, "test-token")
			done, err := client.IsIssueDone(context.Background(), "RHOAIENG-100")

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDone, done)
			}
		})
	}
}

func TestOptionsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts jira.Options
		want bool
	}{
		{
			name: "all set",
			opts: jira.Options{Server: "https://jira.example.com", Token: "tok", Project: "PROJ"},
			want: true,
		},
		{
			name: "missing server",
			opts: jira.Options{Token: "tok", Project: "PROJ"},
			want: false,
		},
		{
			name: "missing token",
			opts: jira.Options{Server: "https://jira.example.com", Project: "PROJ"},
			want: false,
		},
		{
			name: "missing project",
			opts: jira.Options{Server: "https://jira.example.com", Token: "tok"},
			want: false,
		},
		{
			name: "empty",
			opts: jira.Options{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.opts.Configured())
		})
	}
}
