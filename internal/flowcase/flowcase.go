// Package flowcase provides a client for the FlowCase CV API.
// It handles JSON unmarshaling of FlowCase's inconsistent field types,
// HTTP communication, and shared formatting helpers.
package flowcase

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

	"github.com/varianter/internal-mcp/internal/secrets"
)

// ── HTTP client ────────────────────────────────────────────────────────────────

// LoadSecret resolves a secret by checking the env var first (local dev), then
// falling back to Key Vault. Key Vault does not allow underscores in names,
// hence the two separate name parameters.
func LoadSecret(ctx context.Context, loader *secrets.Loader, envName, kvName string) (string, error) {
	if v := os.Getenv(envName); v != "" {
		return v, nil
	}
	v, err := loader.Get(ctx, kvName)
	if err != nil {
		return "", fmt.Errorf("secret %s / Key Vault %s: %w", envName, kvName, err)
	}
	return v, nil
}

// BaseURL returns the FlowCase API base URL for the given organisation slug.
func BaseURL(org string) string {
	return fmt.Sprintf("https://%s.flowcase.com/api", org)
}

// AuthHeader returns the Authorization header value for the given API key.
func AuthHeader(apiKey string) string {
	return fmt.Sprintf("Token token=%q", apiKey)
}

// Do executes an HTTP request against the FlowCase API and decodes the JSON
// response into T.
func Do[T any](ctx context.Context, method, url, authHeader string, body []byte) (T, error) {
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
		return zero, fmt.Errorf("HTTP %d: %s", resp.StatusCode, Truncate(string(data), 200))
	}

	slog.Debug("flowcase: raw response", "url", url, "body", Truncate(string(data), 1000))

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// Truncate shortens s to at most n bytes, appending … if cut.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── Custom JSON unmarshalers ───────────────────────────────────────────────────

// Int unmarshals FlowCase integer fields that are sometimes returned as JSON
// strings (e.g. month_from, year_from).
type Int struct{ V *int }

func (f *Int) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		f.V = &n
		return nil
	}
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
	f.V = &n
	return nil
}

// Tags unmarshals FlowCase tag fields that can be either a []string or a
// map[string]string (id → label), depending on the endpoint/version.
type Tags []string

func (t *Tags) UnmarshalJSON(b []byte) error {
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*t = arr
		return nil
	}
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

// Text holds localised strings — FlowCase stores them as {no: "...", en: "..."}
// but some endpoints return a plain string instead of an object.
type Text struct {
	NO  string `json:"no"`
	EN  string `json:"en"`
	INT string `json:"int"`
}

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t.EN = s
		return nil
	}
	type textRaw Text
	var raw textRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*t = Text(raw)
	return nil
}

func (t Text) String() string {
	if t.EN != "" {
		return t.EN
	}
	if t.INT != "" {
		return t.INT
	}
	return t.NO
}

// ── Search API types ───────────────────────────────────────────────────────────

type SearchResponse struct {
	CVs []SearchHit `json:"cvs"`
}

type SearchHit struct {
	CV SearchCV `json:"cv"`
}

type SearchCV struct {
	UserID  string `json:"user_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Default bool   `json:"default"`
	Title   Text   `json:"title"`
}

// ── CV detail types ────────────────────────────────────────────────────────────

type CV struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Title    Text   `json:"title"`
	BornYear Int    `json:"born_year"`

	KeyQualifications  []KeyQualification `json:"key_qualifications"`
	Technologies       []TechnologyGroup  `json:"technologies"`
	WorkExperiences    []WorkExperience   `json:"work_experiences"`
	ProjectExperiences []ProjectExp       `json:"project_experiences"`
	Educations         []Education        `json:"educations"`
	Certifications     []Certification    `json:"certifications"`
	Presentations      []Presentation     `json:"presentations"`
	Languages          []Language         `json:"languages"`
	Courses            []Course           `json:"courses"`
}

type KeyQualification struct {
	Label           Text `json:"label"`
	LongDescription Text `json:"long_description"`
	TagLine         Text `json:"tag_line"`
	Starred         bool `json:"starred"`
	Disabled        bool `json:"disabled"`
}

type TechnologyGroup struct {
	Category   Text       `json:"category"`
	Technology []TechItem `json:"technology"`
}

type TechItem struct {
	Tags Tags `json:"tags"`
}

type WorkExperience struct {
	Employer             Text `json:"employer"`
	Title                Text `json:"title"`
	Description          Text `json:"description"`
	YearFrom             Int  `json:"year_from"`
	MonthFrom            Int  `json:"month_from"`
	YearTo               Int  `json:"year_to"`
	MonthTo              Int  `json:"month_to"`
	CurrentlyWorkingHere bool `json:"currently_working_here"`
}

type ProjectExp struct {
	Customer    Text        `json:"customer"`
	Description Text        `json:"description"`
	Roles       []Role      `json:"roles"`
	YearFrom    Int         `json:"year_from"`
	MonthFrom   Int         `json:"month_from"`
	YearTo      Int         `json:"year_to"`
	MonthTo     Int         `json:"month_to"`
	Skills      []ProjSkill `json:"project_experience_skills"`
}

type Role struct {
	Name Text `json:"name"`
}

type ProjSkill struct {
	Tags Tags `json:"tags"`
}

type Education struct {
	School      Text `json:"school"`
	Degree      Text `json:"degree"`
	Description Text `json:"description"`
	YearFrom    Int  `json:"year_from"`
	YearTo      Int  `json:"year_to"`
}

type Certification struct {
	Name            Text `json:"name"`
	Organizer       Text `json:"organizer"`
	LongDescription Text `json:"long_description"`
	Year            Int  `json:"year"`
	Month           Int  `json:"month"`
}

type Presentation struct {
	Description Text `json:"description"`
	LongDesc    Text `json:"long_description"`
	Year        Int  `json:"year"`
	Month       Int  `json:"month"`
}

type Language struct {
	Name  Text `json:"name"`
	Level Text `json:"level"`
}

type Course struct {
	Name    Text `json:"name"`
	Program Text `json:"program"`
	Year    Int  `json:"year"`
}

// ── Formatting helpers ─────────────────────────────────────────────────────────

var MonthNames = [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func FormatPeriod(yearFrom, monthFrom, yearTo, monthTo Int, current bool) string {
	from := ""
	if yearFrom.V != nil {
		if monthFrom.V != nil && *monthFrom.V >= 1 && *monthFrom.V <= 12 {
			from = fmt.Sprintf("%s %d", MonthNames[*monthFrom.V], *yearFrom.V)
		} else {
			from = fmt.Sprintf("%d", *yearFrom.V)
		}
	}
	to := ""
	if current {
		to = "Present"
	} else if yearTo.V != nil {
		if monthTo.V != nil && *monthTo.V >= 1 && *monthTo.V <= 12 {
			to = fmt.Sprintf("%s %d", MonthNames[*monthTo.V], *yearTo.V)
		} else {
			to = fmt.Sprintf("%d", *yearTo.V)
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

func FormatYearRange(from, to Int) string {
	switch {
	case from.V != nil && to.V != nil:
		return fmt.Sprintf("%d – %d", *from.V, *to.V)
	case from.V != nil:
		return fmt.Sprintf("%d", *from.V)
	case to.V != nil:
		return fmt.Sprintf("– %d", *to.V)
	default:
		return ""
	}
}
