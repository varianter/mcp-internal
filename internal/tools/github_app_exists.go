package tools

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/varianter/internal-mcp/internal/flowcase"
	"github.com/varianter/internal-mcp/internal/github"
	"github.com/varianter/internal-mcp/internal/secrets"
)

// NewGithubAppExistsTool checks whether an app directory already exists in one
// of the Variant artifact repos. Read-only — no side effects.
func NewGithubAppExistsTool(loader *secrets.Loader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("github-app-exists",
		mcp.WithDescription(`Check whether apps/<app_name>/ already exists in a Variant artifact repo.
Returns "exists" if the app directory is found, or "not_found" if it is not.
Use this before deploying to decide whether to use commit prefix "deploy:" (new) or "update:" (replacing).`),
		mcp.WithString("app_name",
			mcp.Required(),
			mcp.Description(`The app identifier. Becomes the path prefix apps/<app_name>/ in the repo. Use kebab-case (e.g. "budget-tracker"). Must not contain "/" or "..".`),
		),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description(`Deployment target. "public" → varianter/external-artifacts (share.variant.dev). "internal" → varianter/vibe-artifacts (artifacts.variant.dev, Variant employees only).`),
		),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appName := strings.TrimSpace(req.GetString("app_name", ""))
		repoTarget := strings.TrimSpace(req.GetString("repo", ""))

		if appName == "" {
			return mcp.NewToolResultError("app_name is required"), nil
		}
		if strings.Contains(appName, "..") || strings.Contains(appName, "/") {
			return mcp.NewToolResultError(`app_name must not contain ".." or "/"`), nil
		}
		if repoTarget != "public" && repoTarget != "internal" {
			return mcp.NewToolResultError(`repo must be "public" or "internal"`), nil
		}

		token, err := flowcase.LoadSecret(ctx, loader, "GITHUB_TOKEN", "mcp-github-token")
		if err != nil {
			slog.Error("github-app-exists: failed to load token", "error", err)
			return mcp.NewToolResultError("GitHub token not configured"), nil
		}

		owner, repoName, err := github.RepoForTarget(repoTarget)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		slog.Info("github-app-exists: checking", "app", appName, "repo", repoTarget)

		exists, err := github.NewClient(token).AppExists(ctx, owner, repoName, appName)
		if err != nil {
			slog.Error("github-app-exists: API error", "error", err)
			return mcp.NewToolResultError("GitHub API error: " + err.Error()), nil
		}

		if exists {
			return mcp.NewToolResultText("exists"), nil
		}
		return mcp.NewToolResultText("not_found"), nil
	}

	return tool, handler
}
