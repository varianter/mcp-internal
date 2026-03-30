package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/varianter/internal-mcp/internal/flowcase"
	"github.com/varianter/internal-mcp/internal/github"
	"github.com/varianter/internal-mcp/internal/secrets"
)

// NewGithubDeployAppTool deploys one or more files to apps/<app_name>/ in a
// Variant artifact repo using a single atomic Git commit.
func NewGithubDeployAppTool(loader *secrets.Loader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("github-deploy-app",
		mcp.WithDescription(`Deploy files to apps/<app_name>/ in a Variant artifact repo via a single atomic Git commit.
Supports static HTML/CSS/JS and Vite project files. All files land in one commit — either all succeed or none.

Repo targets:
  "public"   → varianter/external-artifacts → https://share.variant.dev/<app_name>/
  "internal" → varianter/vibe-artifacts     → https://artifacts.variant.dev/<app_name>/ (Variant employees only)

Only writes inside apps/ — never touches anything else in the repo.
Commit message is "deploy: <app_name>" for new apps, "update: <app_name>" when replacing.`),
		mcp.WithString("app_name",
			mcp.Required(),
			mcp.Description(`App identifier in kebab-case (e.g. "budget-tracker"). Becomes apps/<app_name>/ in the repo. Must not contain "/" or "..".`),
		),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description(`Deployment target: "public" (share.variant.dev) or "internal" (artifacts.variant.dev, employees only).`),
		),
		mcp.WithString("files",
			mcp.Required(),
			mcp.Description(`JSON array of files to deploy. Each entry: {"path": "relative/path/file.html", "content": "plain text content"}.
Paths are relative to apps/<app_name>/ with no leading slash. Content is plain UTF-8 text.
Example: [{"path":"index.html","content":"<html>...</html>"},{"path":"src/App.jsx","content":"..."}]`),
		),
		mcp.WithString("author_name",
			mcp.Description(`Full name of the person deploying the app (e.g. "Mikael Brevik"). Used as the Git commit author. If omitted, the token owner's identity is used.`),
		),
		mcp.WithString("author_email",
			mcp.Description(`Email of the person deploying the app (e.g. "mikael@variant.no"). Used as the Git commit author. Required if author_name is provided.`),
		),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appName := strings.TrimSpace(req.GetString("app_name", ""))
		repoTarget := strings.TrimSpace(req.GetString("repo", ""))
		filesJSON := strings.TrimSpace(req.GetString("files", ""))
		authorName := strings.TrimSpace(req.GetString("author_name", ""))
		authorEmail := strings.TrimSpace(req.GetString("author_email", ""))

		// Validate app_name
		if appName == "" {
			return mcp.NewToolResultError("app_name is required"), nil
		}
		if strings.Contains(appName, "..") || strings.Contains(appName, "/") {
			return mcp.NewToolResultError(`app_name must not contain ".." or "/"`), nil
		}

		// Validate repo
		if repoTarget != "public" && repoTarget != "internal" {
			return mcp.NewToolResultError(`repo must be "public" or "internal"`), nil
		}

		// Parse files
		if filesJSON == "" {
			return mcp.NewToolResultError("files is required"), nil
		}
		var entries []github.FileEntry
		if err := json.Unmarshal([]byte(filesJSON), &entries); err != nil {
			return mcp.NewToolResultError("files must be a valid JSON array: " + err.Error()), nil
		}
		if len(entries) == 0 {
			return mcp.NewToolResultError("files must contain at least one entry"), nil
		}

		// Validate each file path
		for _, f := range entries {
			if f.Path == "" {
				return mcp.NewToolResultError("each file entry must have a non-empty path"), nil
			}
			if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
				return mcp.NewToolResultError(fmt.Sprintf("invalid file path %q: must not start with '/' or contain '..'", f.Path)), nil
			}
		}

		token, err := flowcase.LoadSecret(ctx, loader, "GITHUB_TOKEN", "mcp-github-token")
		if err != nil {
			slog.Error("github-deploy-app: failed to load token", "error", err)
			return mcp.NewToolResultError("GitHub token not configured"), nil
		}

		owner, repoName, err := github.RepoForTarget(repoTarget)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client := github.NewClient(token)

		// Check existence to pick commit message prefix
		slog.Info("github-deploy-app: checking app existence", "app", appName, "repo", repoTarget)
		exists, err := client.AppExists(ctx, owner, repoName, appName)
		if err != nil {
			slog.Error("github-deploy-app: existence check failed", "error", err)
			return mcp.NewToolResultError("GitHub API error: " + err.Error()), nil
		}

		commitMsg := "deploy: " + appName
		if exists {
			commitMsg = "update: " + appName
		}

		slog.Info("github-deploy-app: deploying", "app", appName, "repo", repoTarget, "files", len(entries), "new", !exists)

		result, err := client.Deploy(ctx, owner, repoName, appName, commitMsg, authorName, authorEmail, entries)
		if err != nil {
			slog.Error("github-deploy-app: deploy failed", "error", err)
			return mcp.NewToolResultError("Deploy failed: " + err.Error()), nil
		}

		liveURL := github.LiveURL(repoTarget, appName)

		action := "Deployed"
		if exists {
			action = "Updated"
		}

		isVite := false
		for _, f := range entries {
			if strings.HasPrefix(f.Path, "vite.config") || f.Path == "package.json" {
				isVite = true
				break
			}
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "## %s: `%s`\n\n", action, appName)
		fmt.Fprintf(&sb, "**Live URL:** %s\n\n", liveURL)
		fmt.Fprintf(&sb, "**Commit:** [%s](%s)\n\n", result.CommitSHA[:8], result.CommitURL)
		fmt.Fprintf(&sb, "**Files deployed (%d):**\n", len(result.Files))
		for _, f := range result.Files {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
		if repoTarget == "internal" {
			fmt.Fprintf(&sb, "\n_Access requires Variant employee login._\n")
		} else {
			fmt.Fprintf(&sb, "\n_It may take a moment for changes to propagate._\n")
		}
		if isVite {
			fmt.Fprintf(&sb, "_Vite project detected — the platform will build it automatically (takes a minute or two)._\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	}

	return tool, handler
}
