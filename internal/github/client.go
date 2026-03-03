package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/go-github/v81/github"
)

// Client wraps the GitHub API client with additional functionality.
type Client struct {
	client *github.Client
	owner  string
	repo   string
}

// NewClient creates a new GitHub API client with authentication.
func NewClient(ctx context.Context, token, owner, repo string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}

	if owner == "" {
		return nil, fmt.Errorf("owner cannot be empty")
	}

	if repo == "" {
		return nil, fmt.Errorf("repo cannot be empty")
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &retryTransport{
			underlying: http.DefaultTransport,
			maxRetries: 3,
		},
	}

	// Create GitHub client with token authentication
	client := github.NewClient(httpClient).WithAuthToken(token)

	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
	}, nil
}

// GetClient returns the underlying GitHub client.
func (c *Client) GetClient() *github.Client {
	return c.client
}

// Owner returns the repository owner.
func (c *Client) Owner() string {
	return c.owner
}

// Repo returns the repository name.
func (c *Client) Repo() string {
	return c.repo
}

// retryTransport implements http.RoundTripper with retry logic.
type retryTransport struct {
	underlying http.RoundTripper
	maxRetries int
}

// RoundTrip implements http.RoundTripper with exponential backoff.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for i := 0; i <= t.maxRetries; i++ {
		resp, err = t.underlying.RoundTrip(req)

		// Success or non-retryable error
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}

		// Rate limit - don't retry, let caller handle
		if resp != nil && resp.StatusCode == 429 {
			return resp, err
		}

		// Last attempt
		if i == t.maxRetries {
			return resp, err
		}

		// Exponential backoff: 1s, 2s, 4s
		backoff := time.Duration(1<<uint(i)) * time.Second
		time.Sleep(backoff)
	}

	return resp, err
}
