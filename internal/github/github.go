// Package github provides a minimal client for the GitHub Git Data API,
// scoped to deploying static files into the varianter artifact repos.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	repoOwner    = "varianter"
	repoPublic   = "external-artifacts"
	repoInternal = "vibe-artifacts"
	urlPublic    = "https://share.variant.dev"
	urlInternal  = "https://artifacts.variant.dev"
	apiBase      = "https://api.github.com"
	branch       = "main"
)

// RepoForTarget maps "public" / "internal" to owner + repo name.
// Returns an error for any other value.
func RepoForTarget(target string) (owner, repo string, err error) {
	switch target {
	case "public":
		return repoOwner, repoPublic, nil
	case "internal":
		return repoOwner, repoInternal, nil
	default:
		return "", "", fmt.Errorf("unknown repo target %q: must be \"public\" or \"internal\"", target)
	}
}

// LiveURL returns the hosting URL for the given target and app name.
func LiveURL(target, appName string) string {
	switch target {
	case "public":
		return fmt.Sprintf("%s/%s/", urlPublic, appName)
	default:
		return fmt.Sprintf("%s/%s/", urlInternal, appName)
	}
}

// FileEntry is one file to deploy: a repo-relative path (no leading slash,
// relative to apps/<app-name>/) and its plain-text content.
type FileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// DeployResult holds the outcome of a successful deployment.
type DeployResult struct {
	CommitSHA string
	CommitURL string
	Files     []string
}

// Client is a GitHub API client authenticated with a personal access token.
type Client struct {
	token string
}

// NewClient returns a new Client using the provided PAT.
func NewClient(token string) *Client {
	return &Client{token: token}
}

// AppExists reports whether apps/<appName>/ already exists in the repo.
// Uses the Contents API — read-only, no side effects.
func (c *Client) AppExists(ctx context.Context, owner, repo, appName string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/apps/%s", apiBase, owner, repo, appName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	c.setHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected HTTP %d checking app existence", resp.StatusCode)
	}
}

// blobRef pairs a repo-relative file path with its blob SHA.
type blobRef struct {
	path string
	sha  string
}

// Deploy creates or replaces files under apps/<appName>/ in a single atomic commit
// using the Git Data API. If authorName and authorEmail are both non-empty they are
// set as the commit author; otherwise GitHub uses the token owner's identity.
func (c *Client) Deploy(ctx context.Context, owner, repo, appName, commitMsg, authorName, authorEmail string, files []FileEntry) (DeployResult, error) {
	// Step 1+2: get current HEAD commit SHA and its tree SHA
	headCommitSHA, headTreeSHA, err := c.headSHA(ctx, owner, repo)
	if err != nil {
		return DeployResult{}, fmt.Errorf("fetch HEAD: %w", err)
	}

	// Step 3: create blobs concurrently
	blobs := make([]blobRef, len(files))
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func(i int, f FileEntry) {
			defer wg.Done()
			sha, err := c.createBlob(ctx, owner, repo, f.Content)
			if err != nil {
				errs[i] = err
				return
			}
			blobs[i] = blobRef{
				path: "apps/" + appName + "/" + f.Path,
				sha:  sha,
			}
		}(i, f)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return DeployResult{}, fmt.Errorf("create blob: %w", err)
		}
	}

	// Step 4: create tree
	treeSHA, err := c.createTree(ctx, owner, repo, headTreeSHA, blobs)
	if err != nil {
		return DeployResult{}, fmt.Errorf("create tree: %w", err)
	}

	// Step 5: create commit
	commitSHA, err := c.createCommit(ctx, owner, repo, commitMsg, treeSHA, headCommitSHA, authorName, authorEmail)
	if err != nil {
		return DeployResult{}, fmt.Errorf("create commit: %w", err)
	}

	// Step 6: fast-forward branch ref
	if err := c.updateRef(ctx, owner, repo, commitSHA); err != nil {
		return DeployResult{}, fmt.Errorf("update ref: %w", err)
	}

	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.Path
	}

	return DeployResult{
		CommitSHA: commitSHA,
		CommitURL: fmt.Sprintf("https://github.com/%s/%s/commit/%s", owner, repo, commitSHA),
		Files:     filePaths,
	}, nil
}

// ── internal helpers ───────────────────────────────────────────────────────────

func (c *Client) headSHA(ctx context.Context, owner, repo string) (commitSHA, treeSHA string, err error) {
	// Get HEAD ref
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, branch)
	if err := c.do(ctx, http.MethodGet, url, nil, &refResp); err != nil {
		return "", "", err
	}
	commitSHA = refResp.Object.SHA

	// Get tree SHA from commit
	var commitResp struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	url = fmt.Sprintf("%s/repos/%s/%s/git/commits/%s", apiBase, owner, repo, commitSHA)
	if err := c.do(ctx, http.MethodGet, url, nil, &commitResp); err != nil {
		return "", "", err
	}
	treeSHA = commitResp.Tree.SHA
	return commitSHA, treeSHA, nil
}

func (c *Client) createBlob(ctx context.Context, owner, repo, content string) (string, error) {
	body := map[string]string{
		"content":  base64.StdEncoding.EncodeToString([]byte(content)),
		"encoding": "base64",
	}
	var resp struct {
		SHA string `json:"sha"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/blobs", apiBase, owner, repo)
	if err := c.do(ctx, http.MethodPost, url, body, &resp); err != nil {
		return "", err
	}
	return resp.SHA, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (c *Client) createTree(ctx context.Context, owner, repo, baseTreeSHA string, blobs []blobRef) (string, error) {
	entries := make([]treeEntry, len(blobs))
	for i, b := range blobs {
		entries[i] = treeEntry{
			Path: b.path,
			Mode: "100644",
			Type: "blob",
			SHA:  b.sha,
		}
	}
	body := map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	}
	var resp struct {
		SHA string `json:"sha"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/trees", apiBase, owner, repo)
	if err := c.do(ctx, http.MethodPost, url, body, &resp); err != nil {
		return "", err
	}
	return resp.SHA, nil
}

func (c *Client) createCommit(ctx context.Context, owner, repo, message, treeSHA, parentSHA, authorName, authorEmail string) (string, error) {
	body := map[string]any{
		"message": message,
		"tree":    treeSHA,
		"parents": []string{parentSHA},
	}
	if authorName != "" && authorEmail != "" {
		body["author"] = map[string]string{
			"name":  authorName,
			"email": authorEmail,
		}
	}
	var resp struct {
		SHA string `json:"sha"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/commits", apiBase, owner, repo)
	if err := c.do(ctx, http.MethodPost, url, body, &resp); err != nil {
		return "", err
	}
	return resp.SHA, nil
}

func (c *Client) updateRef(ctx context.Context, owner, repo, commitSHA string) error {
	body := map[string]any{
		"sha":   commitSHA,
		"force": false,
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, branch)
	return c.do(ctx, http.MethodPatch, url, body, nil)
}

func (c *Client) do(ctx context.Context, method, url string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
