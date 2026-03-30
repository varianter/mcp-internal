package tools

// NewFlowcaseCVTool fetches a consultant's CV from FlowCase by name or email.
//
// Required secrets (loaded via secrets.Loader — env var or Key Vault):
//   - FLOWCASE_API_KEY  — FlowCase API token (Token auth)
//   - FLOWCASE_ORG      — FlowCase organisation slug (subdomain of your FlowCase instance)
//
// Local development:
//   Set FLOWCASE_API_KEY and FLOWCASE_ORG in your .env file, OR store them in
//   Azure Key Vault (names: "flowcase-api-key", "flowcase-org") and set KEYVAULT_URL.
//
// Kubernetes (AKS):
//   Mount as env vars from a k8s Secret. Do NOT put in k8s/configmap.yaml.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
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
			return fcToolError("query parameter is required"), nil
		}

		slog.Info("flowcase-cv: tool called", "query", query)

		apiKey, err := fcSecret(ctx, loader, "FLOWCASE_API_KEY", "flowcase-api-key")
		if err != nil {
			slog.Error("flowcase-cv: failed to load api key", "error", err)
			return fcToolError(err.Error()), nil
		}
		org, err := fcSecret(ctx, loader, "FLOWCASE_ORG", "flowcase-org")
		if err != nil {
			slog.Error("flowcase-cv: failed to load org", "error", err)
			return fcToolError(err.Error()), nil
		}

		slog.Info("flowcase-cv: fetching CV", "query", query, "org", org)
		md, err := fcFetchCV(ctx, apiKey, org, query)
		if err != nil {
			slog.Error("flowcase-cv: fetch failed", "query", query, "error", err)
			return fcToolError(err.Error()), nil
		}

		return mcp.NewToolResultText(md), nil
	}
	return tool, handler
}

func fcToolError(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError("Error: " + msg)
}

// fcSecret checks the underscore env var first (local dev), then the
// hyphenated Key Vault name. Key Vault does not allow underscores in names.
func fcSecret(ctx context.Context, loader *secrets.Loader, envName, kvName string) (string, error) {
	if v := os.Getenv(envName); v != "" {
		return v, nil
	}
	v, err := loader.Get(ctx, kvName)
	if err != nil {
		return "", fmt.Errorf("secret %s / Key Vault %s: %w", envName, kvName, err)
	}
	return v, nil
}

// ── FlowCase API request/response types ───────────────────────────────────────

type fcSearchRequest struct {
	Must []fcMustClause `json:"must"`
	Size int            `json:"size"`
}

type fcMustClause struct {
	Bool fcBool `json:"bool"`
}

type fcBool struct {
	Should []fcShould `json:"should"`
}

type fcShould struct {
	Query fcQuery `json:"query"`
}

type fcQuery struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

type fcSearchResponse struct {
	CVs []fcSearchHit `json:"cvs"`
}

type fcSearchHit struct {
	CV fcSearchCV `json:"cv"`
}

type fcSearchCV struct {
	UserID  string `json:"user_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Default bool   `json:"default"`
}

// fcInt unmarshals FlowCase integer fields that are sometimes returned as JSON
// strings (e.g. month_from, year_from).
type fcInt struct{ v *int }

func (f *fcInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	// try number first
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		f.v = &n
		return nil
	}
	// fall back to quoted string
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	f.v = &n
	return nil
}

// fcTags unmarshals FlowCase tag fields that can be either a []string or a
// map[string]string (id → label), depending on the endpoint/version.
type fcTags []string

func (t *fcTags) UnmarshalJSON(b []byte) error {
	// Try array first
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*t = arr
		return nil
	}
	// Fall back to object — take the values
	var obj map[string]string
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	out := make([]string, 0, len(obj))
	for _, v := range obj {
		if v != "" {
			out = append(out, v)
		}
	}
	*t = out
	return nil
}

// fcText holds localised strings — FlowCase stores them as {no: "...", en: "..."}
type fcText struct {
	NO  string `json:"no"`
	EN  string `json:"en"`
	INT string `json:"int"`
}

func (t fcText) String() string {
	if t.EN != "" {
		return t.EN
	}
	if t.INT != "" {
		return t.INT
	}
	return t.NO
}

// fcCVResponse is the v3 CV object returned directly (not wrapped).
type fcCVResponse = fcCV

type fcCV struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Title    fcText `json:"title"`
	BornYear fcInt  `json:"born_year"`

	KeyQualifications  []fcKeyQualification `json:"key_qualifications"`
	Technologies       []fcTechnologyGroup  `json:"technologies"`
	WorkExperiences    []fcWorkExperience   `json:"work_experiences"`
	ProjectExperiences []fcProjectExp       `json:"project_experiences"`
	Educations         []fcEducation        `json:"educations"`
	Certifications     []fcCertification    `json:"certifications"`
	Presentations      []fcPresentation     `json:"presentations"`
	Languages          []fcLanguage         `json:"languages"`
	Courses            []fcCourse           `json:"courses"`
}

type fcKeyQualification struct {
	Label           fcText `json:"label"`
	LongDescription fcText `json:"long_description"`
	TagLine         fcText `json:"tag_line"`
	Starred         bool   `json:"starred"`
	Disabled        bool   `json:"disabled"`
}

type fcTechnologyGroup struct {
	Category   fcText       `json:"category"`
	Technology []fcTechItem `json:"technology"`
}

type fcTechItem struct {
	Tags fcTags `json:"tags"`
}

type fcWorkExperience struct {
	Employer             fcText `json:"employer"`
	Title                fcText `json:"title"`
	Description          fcText `json:"description"`
	YearFrom             fcInt  `json:"year_from"`
	MonthFrom            fcInt  `json:"month_from"`
	YearTo               fcInt  `json:"year_to"`
	MonthTo              fcInt  `json:"month_to"`
	CurrentlyWorkingHere bool   `json:"currently_working_here"`
}

type fcProjectExp struct {
	Customer    fcText        `json:"customer"`
	Description fcText        `json:"description"`
	Roles       []fcRole      `json:"roles"`
	YearFrom    fcInt         `json:"year_from"`
	MonthFrom   fcInt         `json:"month_from"`
	YearTo      fcInt         `json:"year_to"`
	MonthTo     fcInt         `json:"month_to"`
	Skills      []fcProjSkill `json:"project_experience_skills"`
}

type fcRole struct {
	Name fcText `json:"name"`
}

type fcProjSkill struct {
	Tags fcTags `json:"tags"`
}

type fcEducation struct {
	School      fcText `json:"school"`
	Degree      fcText `json:"degree"`
	Description fcText `json:"description"`
	YearFrom    fcInt  `json:"year_from"`
	YearTo      fcInt  `json:"year_to"`
}

type fcCertification struct {
	Name            fcText `json:"name"`
	Organizer       fcText `json:"organizer"`
	LongDescription fcText `json:"long_description"`
	Year            fcInt  `json:"year"`
	Month           fcInt  `json:"month"`
}

type fcPresentation struct {
	Description fcText `json:"description"`
	LongDesc    fcText `json:"long_description"`
	Year        fcInt  `json:"year"`
	Month       fcInt  `json:"month"`
}

type fcLanguage struct {
	Name  fcText `json:"name"`
	Level fcText `json:"level"`
}

type fcCourse struct {
	Name    fcText `json:"name"`
	Program fcText `json:"program"`
	Year    fcInt  `json:"year"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────────

func fcFetchCV(ctx context.Context, apiKey, org, query string) (string, error) {
	baseURL := fmt.Sprintf("https://%s.flowcase.com/api", org)
	authHeader := fmt.Sprintf("Token token=%q", apiKey)
	return fcFetchCVByName(ctx, baseURL, authHeader, query)
}

func fcFetchCVByName(ctx context.Context, baseURL, authHeader, name string) (string, error) {
	payload := fcSearchRequest{
		Must: []fcMustClause{{
			Bool: fcBool{
				Should: []fcShould{{
					Query: fcQuery{Field: "name", Value: name},
				}},
			},
		}},
		Size: 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal search payload: %w", err)
	}

	searchResp, err := fcDo[fcSearchResponse](ctx, http.MethodPost, baseURL+"/v4/search", authHeader, body)
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
	cv, err := fcDo[fcCVResponse](ctx, http.MethodGet, cvURL, authHeader, nil)
	if err != nil {
		return "", fmt.Errorf("FlowCase fetch CV: %w", err)
	}

	return fcFormatCV(cv, searchResp.CVs), nil
}

func fcDo[T any](ctx context.Context, method, url, authHeader string, body []byte) (T, error) {
	var zero T
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("HTTP %d: %s", resp.StatusCode, fcTruncate(string(data), 200))
	}

	slog.Debug("flowcase-cv: raw response", "url", url, "body", fcTruncate(string(data), 1000))

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// ── Markdown formatter ─────────────────────────────────────────────────────────

func fcFormatCV(cv fcCV, allHits []fcSearchHit) string {
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
	if cv.BornYear.v != nil {
		fmt.Fprintf(&sb, "**Born:** %d  \n", *cv.BornYear.v)
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
	var profileKQ *fcKeyQualification
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
			period := fcFormatPeriod(w.YearFrom, w.MonthFrom, w.YearTo, w.MonthTo, w.CurrentlyWorkingHere)
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
			period := fcFormatPeriod(p.YearFrom, p.MonthFrom, p.YearTo, p.MonthTo, false)
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
			if period := fcFormatYearRange(e.YearFrom, e.YearTo); period != "" {
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
			if c.Year.v != nil {
				if c.Month.v != nil && *c.Month.v >= 1 && *c.Month.v <= 12 {
					fmt.Fprintf(&sb, "**Date:** %s %d  \n", fcMonthNames[*c.Month.v], *c.Year.v)
				} else {
					fmt.Fprintf(&sb, "**Year:** %d  \n", *c.Year.v)
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
			if p.Year.v != nil {
				if p.Month.v != nil && *p.Month.v >= 1 && *p.Month.v <= 12 {
					fmt.Fprintf(&sb, "**Date:** %s %d\n", fcMonthNames[*p.Month.v], *p.Year.v)
				} else {
					fmt.Fprintf(&sb, "**Year:** %d\n", *p.Year.v)
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
			if c.Year.v != nil {
				fmt.Fprintf(&sb, " (%d)", *c.Year.v)
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

var fcMonthNames = [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func fcFormatPeriod(yearFrom, monthFrom, yearTo, monthTo fcInt, current bool) string {
	from := ""
	if yearFrom.v != nil {
		if monthFrom.v != nil && *monthFrom.v >= 1 && *monthFrom.v <= 12 {
			from = fmt.Sprintf("%s %d", fcMonthNames[*monthFrom.v], *yearFrom.v)
		} else {
			from = fmt.Sprintf("%d", *yearFrom.v)
		}
	}
	to := ""
	if current {
		to = "Present"
	} else if yearTo.v != nil {
		if monthTo.v != nil && *monthTo.v >= 1 && *monthTo.v <= 12 {
			to = fmt.Sprintf("%s %d", fcMonthNames[*monthTo.v], *yearTo.v)
		} else {
			to = fmt.Sprintf("%d", *yearTo.v)
		}
	}
	switch {
	case from != "" && to != "":
		return from + " – " + to
	case from != "":
		return from
	case to != "":
		return "– " + to
	default:
		return ""
	}
}

func fcFormatYearRange(from, to fcInt) string {
	switch {
	case from.v != nil && to.v != nil:
		return fmt.Sprintf("%d – %d", *from.v, *to.v)
	case from.v != nil:
		return fmt.Sprintf("%d", *from.v)
	case to.v != nil:
		return fmt.Sprintf("– %d", *to.v)
	default:
		return ""
	}
}

func fcTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
