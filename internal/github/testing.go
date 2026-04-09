package github

import gh "github.com/google/go-github/v81/github"

// NewTestClient creates a Client for testing with a custom github.Client.
// This allows tests to point the client at an httptest.Server.
func NewTestClient(client *gh.Client, owner, repo string) *Client {
	return &Client{client: client, owner: owner, repo: repo}
}
