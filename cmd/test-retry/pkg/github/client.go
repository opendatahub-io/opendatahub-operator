package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v67/github"
	"golang.org/x/oauth2"
)

// GitHubClient interface for GitHub operations (allows mocking)
type GitHubClient interface {
	AddLabel(ctx context.Context, owner, repo string, prNumber int, label string) error
	AddComment(ctx context.Context, owner, repo string, prNumber int, comment string) error
}

// Client wraps the GitHub API client
type Client struct {
	client *github.Client
}

// NewClient creates a new GitHub client with authentication
func NewClient(token string) *Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return &Client{
		client: github.NewClient(tc),
	}
}

// AddLabel adds a label to a GitHub pull request
func (c *Client) AddLabel(ctx context.Context, owner, repo string, prNumber int, label string) error {
	_, _, err := c.client.Issues.AddLabelsToIssue(ctx, owner, repo, prNumber, []string{label})
	if err != nil {
		return fmt.Errorf("failed to add label '%s' to PR #%d: %w", label, prNumber, err)
	}
	return nil
}

// AddComment adds a comment to a GitHub pull request
func (c *Client) AddComment(ctx context.Context, owner, repo string, prNumber int, comment string) error {
	issueComment := &github.IssueComment{
		Body: &comment,
	}
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, prNumber, issueComment)
	if err != nil {
		return fmt.Errorf("failed to add comment to PR #%d: %w", prNumber, err)
	}
	return nil
}
