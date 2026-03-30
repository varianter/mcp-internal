package tools

// NewFlowcaseCVTool fetches a consultant's CV from FlowCase by name or email.
//
// Required secrets (loaded via secrets.Loader — env var or Key Vault):
//   - FLOWCASE_API_KEY  — FlowCase API token (Token auth)
//   - FLOWCASE_ORG      — FlowCase organisation slug (subdomain of your FlowCase instance)
//
// Local development:
//
//	Set FLOWCASE_API_KEY and FLOWCASE_ORG in your .env file, OR store them in
//	Azure Key Vault (names: "flowcase-api-key", "flowcase-org") and set KEYVAULT_URL.
//
// Kubernetes (AKS):
//
//	Mount as env vars from a k8s Secret. Do NOT put in k8s/configmap.yaml.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/varianter/internal-mcp/internal/flowcase"
	"github.com/varianter/internal-mcp/internal/secrets"
)

func NewFlowcaseCVTool(loader *secrets.Loader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("get-cv-for-consultant",
		mcp.WithDescription("Fetch a consultant's full CV from FlowCase by name. Returns a structured Markdown summary of their profile, skills, work history, projects, education, certifications, and languages."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Consultant's full name (e.g. 'Mikael Brevik')"),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := strings.TrimSpace(req.GetString("query", ""))
		if query == "" {
			return toolError("query parameter is required"), nil
		}

		slog.Info("flowcase-cv: tool called", "query", query)

		apiKey, err := fcSecret(ctx, loader, "FLOWCASE_API_KEY", "flowcase-api-key")
		if err != nil {
			slog.Error("flowcase-cv: failed to load api key", "error", err)
			return toolError(err.Error()), nil
		}
		org, err := fcSecret(ctx, loader, "FLOWCASE_ORG", "flowcase-org")
		if err != nil {
			slog.Error("flowcase-cv: failed to load org", "error", err)
			return toolError(err.Error()), nil
		}

		slog.Info("flowcase-cv: fetching CV", "query", query, "org", org)
		md, err := fetchCV(ctx, apiKey, org, query)
		if err != nil {
			slog.Error("flowcase-cv: fetch failed", "query", query, "error", err)
			return toolError(err.Error()), nil
		}

		return mcp.NewToolResultText(md), nil
	}
	return tool, handler
}

// ── Name search payload ────────────────────────────────────────────────────────

type nameSearchRequest struct {
	Must []nameMust `json:"must"`
	Size int        `json:"size"`
}

type nameMust struct {
	Bool nameBool `json:"bool"`
}

type nameBool struct {
	Should []nameShould `json:"should"`
}

type nameShould struct {
	Query nameQuery `json:"query"`
}

type nameQuery struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────────

func fetchCV(ctx context.Context, apiKey, org, query string) (string, error) {
	return fetchCVByName(ctx, flowcase.BaseURL(org), flowcase.AuthHeader(apiKey), query)
}

func fetchCVByName(ctx context.Context, baseURL, authHeader, name string) (string, error) {
	payload := nameSearchRequest{
		Must: []nameMust{{
			Bool: nameBool{
				Should: []nameShould{{
					Query: nameQuery{Field: "name", Value: name},
				}},
			},
		}},
		Size: 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal search payload: %w", err)
	}

	searchResp, err := flowcase.Do[flowcase.SearchResponse](ctx, http.MethodPost, baseURL+"/v4/search", authHeader, body)
	if err != nil {
		return "", fmt.Errorf("FlowCase search: %w", err)
	}
	if len(searchResp.CVs) == 0 {
		return "", fmt.Errorf("no consultant found matching %q in FlowCase", name)
	}

	// Prefer the CV marked as default; fall back to the first result.
	hit := searchResp.CVs[0].CV
	for _, h := range searchResp.CVs {
		if h.CV.Default {
			hit = h.CV
			break
		}
	}
	if hit.UserID == "" {
		return "", fmt.Errorf("FlowCase search returned a result but user_id is empty")
	}

	cvURL := fmt.Sprintf("%s/v3/cvs/%s/%s", baseURL, hit.UserID, hit.ID)
	cv, err := flowcase.Do[flowcase.CV](ctx, http.MethodGet, cvURL, authHeader, nil)
	if err != nil {
		return "", fmt.Errorf("FlowCase fetch CV: %w", err)
	}

	return formatCV(cv, searchResp.CVs), nil
}

// ── Markdown formatter ─────────────────────────────────────────────────────────

func formatCV(cv flowcase.CV, allHits []flowcase.SearchHit) string {
	var sb strings.Builder

	sb.WriteString("# ")
	sb.WriteString(cv.Name)
	sb.WriteString("\n\n")

	if t := cv.Title.String(); t != "" {
		sb.WriteString("**Title:** ")
		sb.WriteString(t)
		sb.WriteString("  \n")
	}
	if cv.Email != "" {
		sb.WriteString("**Email:** ")
		sb.WriteString(cv.Email)
		sb.WriteString("  \n")
	}
	if cv.BornYear.V != nil {
		fmt.Fprintf(&sb, "**Born:** %d  \n", *cv.BornYear.V)
	}

	if len(allHits) > 1 {
		sb.WriteString("\n> **Note:** ")
		fmt.Fprintf(&sb, "%d matches found; showing first result. Other matches: ", len(allHits))
		others := make([]string, 0, len(allHits)-1)
		for _, h := range allHits[1:] {
			label := h.CV.Name
			if h.CV.Email != "" {
				label += " (" + h.CV.Email + ")"
			}
			others = append(others, label)
		}
		sb.WriteString(strings.Join(others, ", "))
		sb.WriteString("\n")
	}

	// Show the starred key qualification as the main profile/summary text.
	// Fall back to the first non-disabled one if none are starred.
	var profileKQ *flowcase.KeyQualification
	for i := range cv.KeyQualifications {
		kq := &cv.KeyQualifications[i]
		if kq.Disabled {
			continue
		}
		if kq.Starred {
			profileKQ = kq
			break
		}
		if profileKQ == nil {
			profileKQ = kq
		}
	}
	if profileKQ != nil {
		sb.WriteString("\n## Profile\n\n")
		if tagLine := profileKQ.TagLine.String(); tagLine != "" {
			sb.WriteString("_")
			sb.WriteString(tagLine)
			sb.WriteString("_\n\n")
		}
		if desc := profileKQ.LongDescription.String(); desc != "" {
			sb.WriteString(desc)
			sb.WriteString("\n")
		}
	}

	if len(cv.Technologies) > 0 {
		sb.WriteString("## Skills & Technologies\n\n")
		for _, tg := range cv.Technologies {
			cat := tg.Category.String()
			var tags []string
			for _, item := range tg.Technology {
				tags = append(tags, item.Tags...)
			}
			if len(tags) > 0 {
				if cat != "" {
					sb.WriteString("- **")
					sb.WriteString(cat)
					sb.WriteString(":** ")
				} else {
					sb.WriteString("- ")
				}
				sb.WriteString(strings.Join(tags, ", "))
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	if len(cv.WorkExperiences) > 0 {
		sb.WriteString("## Work Experience\n\n")
		for _, w := range cv.WorkExperiences {
			employer := w.Employer.String()
			period := flowcase.FormatPeriod(w.YearFrom, w.MonthFrom, w.YearTo, w.MonthTo, w.CurrentlyWorkingHere)
			sb.WriteString("### ")
			if employer != "" {
				sb.WriteString(employer)
			} else {
				sb.WriteString("(unknown employer)")
			}
			if period != "" {
				sb.WriteString(" (")
				sb.WriteString(period)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
			if title := w.Title.String(); title != "" {
				sb.WriteString("**Role:** ")
				sb.WriteString(title)
				sb.WriteString("  \n")
			}
			if desc := w.Description.String(); desc != "" {
				sb.WriteString(desc)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(cv.ProjectExperiences) > 0 {
		sb.WriteString("## Project Experience\n\n")
		for _, p := range cv.ProjectExperiences {
			customer := p.Customer.String()
			period := flowcase.FormatPeriod(p.YearFrom, p.MonthFrom, p.YearTo, p.MonthTo, false)
			sb.WriteString("### ")
			if customer != "" {
				sb.WriteString(customer)
			} else {
				sb.WriteString("(unnamed project)")
			}
			if period != "" {
				sb.WriteString(" (")
				sb.WriteString(period)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
			if len(p.Roles) > 0 {
				var roleNames []string
				for _, r := range p.Roles {
					if n := r.Name.String(); n != "" {
						roleNames = append(roleNames, n)
					}
				}
				if len(roleNames) > 0 {
					sb.WriteString("**Role:** ")
					sb.WriteString(strings.Join(roleNames, ", "))
					sb.WriteString("  \n")
				}
			}
			if desc := p.Description.String(); desc != "" {
				sb.WriteString("\n")
				sb.WriteString(desc)
				sb.WriteString("\n")
			}
			var skillTags []string
			for _, s := range p.Skills {
				skillTags = append(skillTags, s.Tags...)
			}
			if len(skillTags) > 0 {
				sb.WriteString("\n**Technologies/Competencies:** ")
				sb.WriteString(strings.Join(skillTags, ", "))
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(cv.Educations) > 0 {
		sb.WriteString("## Education\n\n")
		for _, e := range cv.Educations {
			var parts []string
			if d := e.Degree.String(); d != "" {
				parts = append(parts, d)
			}
			if s := e.School.String(); s != "" {
				parts = append(parts, s)
			}
			sb.WriteString("### ")
			if len(parts) > 0 {
				sb.WriteString(strings.Join(parts, ", "))
			} else {
				sb.WriteString("(unknown)")
			}
			if period := flowcase.FormatYearRange(e.YearFrom, e.YearTo); period != "" {
				sb.WriteString(" (")
				sb.WriteString(period)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
			if desc := e.Description.String(); desc != "" {
				sb.WriteString(desc)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(cv.Certifications) > 0 {
		sb.WriteString("## Certifications\n\n")
		for _, c := range cv.Certifications {
			name := c.Name.String()
			if name == "" {
				continue
			}
			sb.WriteString("### ")
			sb.WriteString(name)
			sb.WriteString("\n")
			if org := c.Organizer.String(); org != "" {
				sb.WriteString("**Issuer:** ")
				sb.WriteString(org)
				sb.WriteString("  \n")
			}
			if c.Year.V != nil {
				if c.Month.V != nil && *c.Month.V >= 1 && *c.Month.V <= 12 {
					fmt.Fprintf(&sb, "**Date:** %s %d  \n", flowcase.MonthNames[*c.Month.V], *c.Year.V)
				} else {
					fmt.Fprintf(&sb, "**Year:** %d  \n", *c.Year.V)
				}
			}
			if desc := c.LongDescription.String(); desc != "" {
				sb.WriteString(desc)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(cv.Presentations) > 0 {
		sb.WriteString("## Presentations\n\n")
		for _, p := range cv.Presentations {
			desc := p.Description.String()
			if desc == "" {
				desc = p.LongDesc.String()
			}
			if desc == "" {
				continue
			}
			sb.WriteString("### ")
			sb.WriteString(desc)
			sb.WriteString("\n")
			if p.Year.V != nil {
				if p.Month.V != nil && *p.Month.V >= 1 && *p.Month.V <= 12 {
					fmt.Fprintf(&sb, "**Date:** %s %d\n", flowcase.MonthNames[*p.Month.V], *p.Year.V)
				} else {
					fmt.Fprintf(&sb, "**Year:** %d\n", *p.Year.V)
				}
			}
			if long := p.LongDesc.String(); long != "" && long != desc {
				sb.WriteString(long)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(cv.Courses) > 0 {
		sb.WriteString("## Courses\n\n")
		for _, c := range cv.Courses {
			name := c.Name.String()
			if name == "" {
				continue
			}
			sb.WriteString("- **")
			sb.WriteString(name)
			sb.WriteString("**")
			if prog := c.Program.String(); prog != "" {
				sb.WriteString(" — ")
				sb.WriteString(prog)
			}
			if c.Year.V != nil {
				fmt.Fprintf(&sb, " (%d)", *c.Year.V)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(cv.Languages) > 0 {
		sb.WriteString("## Languages\n\n")
		for _, l := range cv.Languages {
			name := l.Name.String()
			if name == "" {
				continue
			}
			sb.WriteString("- **")
			sb.WriteString(name)
			sb.WriteString("**")
			if level := l.Level.String(); level != "" {
				sb.WriteString(": ")
				sb.WriteString(level)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n") + "\n"
}
