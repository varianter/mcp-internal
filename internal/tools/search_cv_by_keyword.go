package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/varianter/internal-mcp/internal/flowcase"
	"github.com/varianter/internal-mcp/internal/secrets"
)

// NewFlowcaseSearchTool searches FlowCase for consultants matching a list of
// technology/skill keywords and returns them ranked by total years of experience.
func NewFlowcaseSearchTool(loader *secrets.Loader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("search-cv-by-keyword",
		mcp.WithDescription("Search FlowCase for consultants matching a list of technology or skill keywords. Returns consultants ranked by total years of experience with the requested technologies."),
		mcp.WithString("keywords",
			mcp.Required(),
			mcp.Description("Comma-separated list of technologies or skills to search for (e.g. 'React, Kubernetes, PostgreSQL')"),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw := strings.TrimSpace(req.GetString("keywords", ""))
		if raw == "" {
			return mcp.NewToolResultError("Error: keywords parameter is required"), nil
		}

		var keywords []string
		for _, k := range strings.Split(raw, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				keywords = append(keywords, k)
			}
		}
		if len(keywords) == 0 {
			return mcp.NewToolResultError("Error: at least one keyword is required"), nil
		}

		slog.Info("flowcase-search: tool called", "keywords", keywords)

		apiKey, err := flowcase.LoadSecret(ctx, loader, "FLOWCASE_API_KEY", "flowcase-api-key")
		if err != nil {
			slog.Error("flowcase-search: failed to load api key", "error", err)
			return mcp.NewToolResultError("Error: " + err.Error()), nil
		}
		org, err := flowcase.LoadSecret(ctx, loader, "FLOWCASE_ORG", "flowcase-org")
		if err != nil {
			slog.Error("flowcase-search: failed to load org", "error", err)
			return mcp.NewToolResultError("Error: " + err.Error()), nil
		}

		ranked, err := searchByKeywords(ctx, flowcase.BaseURL(org), flowcase.AuthHeader(apiKey), keywords)
		if err != nil {
			slog.Error("flowcase-search: search failed", "error", err)
			return mcp.NewToolResultError("Error: " + err.Error()), nil
		}

		return mcp.NewToolResultText(formatSearchResults(ranked, keywords)), nil
	}
	return tool, handler
}

// ── Search payload types ───────────────────────────────────────────────────────

type tagSearchRequest struct {
	Must []tagMust `json:"must"`
	Size int       `json:"size"`
}

type tagMust struct {
	TechnologySkill tagFilter `json:"technology_skill"`
}

type tagFilter struct {
	Tag string `json:"tag"`
}

type querySearchRequest struct {
	Must []queryMust `json:"must"`
	Size int         `json:"size"`
}

type queryMust struct {
	Query queryValue `json:"query"`
}

type queryValue struct {
	Value string `json:"value"`
}

// ── Result type ────────────────────────────────────────────────────────────────

type searchCandidate struct {
	UserID        string
	CVID          string
	Name          string
	Email         string
	Title         string
	MatchedSkills []string
	TechYears     map[string]int // keyword → total years across matching projects
	TotalYears    int
}

// ── Core search logic ──────────────────────────────────────────────────────────

const resultsPerSkill = 200 // large pool so scoring sees all matched consultants
const maxResults = 30       // final output cap applied after scoring

func searchByKeywords(ctx context.Context, baseURL, authHeader string, keywords []string) ([]searchCandidate, error) {
	type hitEntry struct {
		userID string
		cvID   string
		name   string
		email  string
		title  string
	}
	hitsByUser := map[string]*hitEntry{}
	matchedSkills := map[string]map[string]bool{}

	for _, skill := range keywords {
		slog.Info("flowcase-search: searching skill", "skill", skill)

		hits, err := searchSkill(ctx, baseURL, authHeader, skill)
		if err != nil {
			slog.Warn("flowcase-search: skill search failed, skipping", "skill", skill, "error", err)
			continue
		}

		for _, hit := range hits {
			uid := hit.CV.UserID
			if uid == "" {
				continue
			}
			if hitsByUser[uid] == nil {
				hitsByUser[uid] = &hitEntry{
					userID: uid,
					cvID:   hit.CV.ID,
					name:   hit.CV.Name,
					email:  hit.CV.Email,
					title:  hit.CV.Title.String(),
				}
			}
			if matchedSkills[uid] == nil {
				matchedSkills[uid] = map[string]bool{}
			}
			matchedSkills[uid][skill] = true
		}
	}

	candidates := make([]searchCandidate, 0, len(hitsByUser))
	for uid, entry := range hitsByUser {
		skills := make([]string, 0, len(matchedSkills[uid]))
		for s := range matchedSkills[uid] {
			skills = append(skills, s)
		}
		sort.Strings(skills)
		candidates = append(candidates, searchCandidate{
			UserID:        uid,
			CVID:          entry.cvID,
			Name:          entry.name,
			Email:         entry.email,
			Title:         entry.title,
			MatchedSkills: skills,
		})
	}

	// Fetch full CVs rate-limited to ~5 req/s to stay under FlowCase's HTTP 429 limit.
	// Requests are launched via a ticker so they are spread over time rather than burst.
	currentYear := time.Now().Year()
	throttle := time.NewTicker(200 * time.Millisecond)
	defer throttle.Stop()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range candidates {
		<-throttle.C
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := candidates[i]
			cvURL := fmt.Sprintf("%s/v3/cvs/%s/%s", baseURL, c.UserID, c.CVID)
			cv, err := fetchCVWithRetry(ctx, cvURL, authHeader)
			if err != nil {
				slog.Warn("flowcase-search: CV fetch failed", "user", c.Name, "error", err)
				return
			}
			techYears, total := scoreCV(cv, keywords, currentYear)
			mu.Lock()
			candidates[i].TechYears = techYears
			candidates[i].TotalYears = total
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].TotalYears != candidates[j].TotalYears {
			return candidates[i].TotalYears > candidates[j].TotalYears
		}
		return candidates[i].Name < candidates[j].Name
	})

	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	return candidates, nil
}

// fetchCVWithRetry fetches a CV, retrying up to 3 times on HTTP 429 responses.
func fetchCVWithRetry(ctx context.Context, cvURL, authHeader string) (flowcase.CV, error) {
	var cv flowcase.CV
	var err error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return cv, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
		cv, err = flowcase.Do[flowcase.CV](ctx, http.MethodGet, cvURL, authHeader, nil)
		if err == nil || !strings.Contains(err.Error(), "429") {
			break
		}
	}
	return cv, err
}

// scoreCV computes years of experience per keyword by collecting project date
// intervals, merging overlaps, then summing. This prevents double-counting when
// two projects with the same technology overlap in time.
func scoreCV(cv flowcase.CV, keywords []string, currentYear int) (techYears map[string]int, total int) {
	techYears = make(map[string]int, len(keywords))
	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		var intervals [][2]int
		for _, proj := range cv.ProjectExperiences {
			if !projectHasSkill(proj, kwLower) || proj.YearFrom.V == nil {
				continue
			}
			from := *proj.YearFrom.V
			to := currentYear
			if proj.YearTo.V != nil && *proj.YearTo.V > 0 {
				to = *proj.YearTo.V
			}
			if to > from {
				intervals = append(intervals, [2]int{from, to})
			}
		}
		years := mergedYears(intervals)
		techYears[kw] = years
		total += years
	}
	return
}

// mergedYears sorts intervals, merges overlaps, and returns the total span in years.
// e.g. [(2010,2015),(2012,2016)] → merged [(2010,2016)] → 6 years.
func mergedYears(intervals [][2]int) int {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i][0] < intervals[j][0]
	})
	merged := [][2]int{intervals[0]}
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if iv[0] <= last[1] {
			if iv[1] > last[1] {
				last[1] = iv[1]
			}
		} else {
			merged = append(merged, iv)
		}
	}
	total := 0
	for _, iv := range merged {
		total += iv[1] - iv[0]
	}
	return total
}

// projectHasSkill reports whether any skill tag in the project matches kw
// (case-insensitive substring in either direction).
func projectHasSkill(proj flowcase.ProjectExp, kwLower string) bool {
	for _, skill := range proj.Skills {
		for _, tag := range skill.Tags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, kwLower) {
				return true
			}
		}
	}
	return false
}

func searchSkill(ctx context.Context, baseURL, authHeader, skill string) ([]flowcase.SearchHit, error) {
	// FlowCase tag search is case-sensitive, so try both the original input and a
	// title-cased variant (e.g. "react" → also try "React").
	variants := []string{skill}
	if titled := titleCase(skill); titled != skill {
		variants = append(variants, titled)
	}

	seen := map[string]bool{}
	var tagHits []flowcase.SearchHit
	for _, v := range variants {
		tagPayload := tagSearchRequest{
			Must: []tagMust{{TechnologySkill: tagFilter{Tag: v}}},
			Size: resultsPerSkill,
		}
		body, err := json.Marshal(tagPayload)
		if err != nil {
			return nil, err
		}
		resp, err := flowcase.Do[flowcase.SearchResponse](ctx, http.MethodPost, baseURL+"/v4/search", authHeader, body)
		if err != nil {
			return nil, fmt.Errorf("technology_skill search for %q: %w", v, err)
		}
		for _, h := range resp.CVs {
			if !seen[h.CV.UserID] {
				seen[h.CV.UserID] = true
				tagHits = append(tagHits, h)
			}
		}
	}
	if len(tagHits) > 0 {
		return tagHits, nil
	}

	// Fallback: free-text query search.
	queryPayload := querySearchRequest{
		Must: []queryMust{{Query: queryValue{Value: skill}}},
		Size: resultsPerSkill,
	}
	body, err := json.Marshal(queryPayload)
	if err != nil {
		return nil, err
	}
	resp, err := flowcase.Do[flowcase.SearchResponse](ctx, http.MethodPost, baseURL+"/v4/search", authHeader, body)
	if err != nil {
		return nil, fmt.Errorf("query search for %q: %w", skill, err)
	}
	return resp.CVs, nil
}

// titleCase capitalises the first letter of each word, e.g. "react native" → "React Native".
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// ── Formatter ──────────────────────────────────────────────────────────────────

func formatSearchResults(candidates []searchCandidate, keywords []string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Consultant search results\n\n")
	fmt.Fprintf(&sb, "**Keywords searched:** %s\n\n", strings.Join(keywords, ", "))

	if len(candidates) == 0 {
		sb.WriteString("No consultants found matching the given keywords.\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "Found **%d** consultants (ranked by total years of experience):\n\n", len(candidates))

	for i, c := range candidates {
		fmt.Fprintf(&sb, "## %d. %s\n", i+1, c.Name)

		// Build "React (10 years), Kubernetes (4 years)" line using original keyword order.
		techParts := make([]string, 0, len(keywords))
		for _, kw := range keywords {
			years := c.TechYears[kw]
			techParts = append(techParts, fmt.Sprintf("%s (%d years)", kw, years))
		}
		fmt.Fprintf(&sb, "**Experience:** %s  \n", strings.Join(techParts, ", "))
		fmt.Fprintf(&sb, "**Total:** %d years\n\n", c.TotalYears)
	}

	return strings.TrimRight(sb.String(), "\n") + "\n"
}
