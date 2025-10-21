package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddLabel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// Create a test server to mock GitHub API
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true

			// Verify request method and path
			expectedPath := "/repos/test-owner/test-repo/issues/123/labels"
			require.Equal(t, expectedPath, r.URL.Path)
			require.Equal(t, http.MethodPost, r.Method)

			var labels []string
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			if err := json.Unmarshal(b, &labels); err != nil {
				require.NoError(t, err)
			}

			require.Equal(t, 1, len(labels))
			require.Equal(t, []string{"test-label"}, labels)

			// Return successful response
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"name": "test-label"}]`))
		}))
		defer server.Close()

		client := getClient(t, server.URL, "test-token")

		err := client.AddLabel(context.Background(), "test-owner", "test-repo", 123, "test-label")
		require.NoError(t, err)
		require.True(t, called)
	})

	t.Run("error", func(t *testing.T) {
		// Create a test server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
		}))
		defer server.Close()

		// Create client with test server URL
		client := getClient(t, server.URL, "test-token")

		err := client.AddLabel(context.Background(), "test-owner", "test-repo", 123, "test-label")
		require.ErrorContains(t, err, "failed to add label 'test-label' to PR #123: ")
	})
}

func TestAddComment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// Create a test server to mock GitHub API
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true

			// Verify request method and path
			expectedPath := "/repos/test-owner/test-repo/issues/123/comments"
			require.Equal(t, expectedPath, r.URL.Path)
			require.Equal(t, http.MethodPost, r.Method)

			// Read and verify the request body
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var comment map[string]string
			err = json.Unmarshal(b, &comment)
			require.NoError(t, err)
			require.Equal(t, "test comment", comment["body"])

			// Return successful response
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id": 1, "body": "test comment"}`))
		}))
		defer server.Close()

		client := getClient(t, server.URL, "test-token")

		err := client.AddComment(context.Background(), "test-owner", "test-repo", 123, "test comment")
		require.NoError(t, err)
		require.True(t, called)
	})

	t.Run("error", func(t *testing.T) {
		// Create a test server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
		}))
		defer server.Close()

		// Create client with test server URL
		client := getClient(t, server.URL, "test-token")

		err := client.AddComment(context.Background(), "test-owner", "test-repo", 123, "test comment")
		require.ErrorContains(t, err, "failed to add comment to PR #123: ")
	})
}

func getClient(t *testing.T, url, token string) *Client {
	t.Helper()

	client := NewClient(token)
	client.client.BaseURL.Host = strings.TrimPrefix(url, "http://")
	client.client.BaseURL.Scheme = "http"
	return client
}
