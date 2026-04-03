package secretscanner

import (
	"fmt"
	"regexp"
	"strings"
)

// pattern describes a recognizable secret type and its detection regex.
type pattern struct {
	name string
	re   *regexp.Regexp
}

// patterns is the list of patterns checked against every deployed file.
var patterns = []pattern{
	// GitHub personal access tokens (classic and fine-grained)
	{name: "GitHub PAT (classic)", re: regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{name: "GitHub PAT (fine-grained)", re: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`)},
	{name: "GitHub OAuth token", re: regexp.MustCompile(`gho_[A-Za-z0-9]{36}`)},
	{name: "GitHub Actions token", re: regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`)},

	// HubSpot tokens
	{name: "HubSpot Private App token", re: regexp.MustCompile(`pat-[a-z]{2,3}-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)},
	{name: "HubSpot API key", re: regexp.MustCompile(`(?i)hubspot[_-]?(?:api[_-]?)?key\s*[:=]\s*["']?([A-Za-z0-9\-]{20,})["']?`)},

	// AWS
	{name: "AWS Access Key ID", re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{name: "AWS Secret Access Key", re: regexp.MustCompile(`(?i)aws[_-]?secret[_-]?(?:access[_-]?)?key\s*[:=]\s*["']?([A-Za-z0-9/+]{40})["']?`)},

	// Generic high-value secret assignments in code/config
	{name: "bearer token assignment", re: regexp.MustCompile(`(?i)(?:authorization|auth)\s*[:=]\s*["']?bearer\s+([A-Za-z0-9\-._~+/]{20,})["']?`)},
	{name: "API key assignment", re: regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|access[_-]?key)\s*[:=]\s*["']([A-Za-z0-9\-._]{20,})["']`)},
	{name: "secret/token assignment", re: regexp.MustCompile(`(?i)(?:secret|private[_-]?key|access[_-]?token|auth[_-]?token)\s*[:=]\s*["']([A-Za-z0-9\-._+/]{20,})["']`)},
	{name: "password assignment", re: regexp.MustCompile(`(?i)password\s*[:=]\s*["']([^"']{8,})["']`)},
}

// Finding records a detected secret within a file.
type Finding struct {
	File    string
	Kind    string
	Excerpt string
}

// File is a file to be scanned with its path and plain-text content.
type File struct {
	Path    string
	Content string
}

// Scan checks every file for hardcoded secrets and returns all findings.
func Scan(files []File) []Finding {
	var findings []Finding
	for _, f := range files {
		for _, p := range patterns {
			match := p.re.FindString(f.Content)
			if match == "" {
				continue
			}
			findings = append(findings, Finding{
				File:    f.Path,
				Kind:    p.name,
				Excerpt: redact(match),
			})
		}
	}
	return findings
}

// ErrorMessage builds a human-readable error string from a list of findings.
func ErrorMessage(findings []Finding) string {
	var sb strings.Builder
	sb.WriteString("Deployment aborted: potential secrets detected in files to be deployed.\n\n")
	sb.WriteString("Remove all credentials, tokens, and API keys from the source files before deploying.\n\n")
	sb.WriteString("Findings:\n")
	for _, f := range findings {
		fmt.Fprintf(&sb, "  - %s: %s (in `%s`)\n", f.Kind, f.Excerpt, f.File)
	}
	sb.WriteString("\nNever commit secrets to a repository. Use environment variables or a secrets manager instead.")
	return sb.String()
}

// redact replaces the middle portion of a matched secret with asterisks so
// the error message is useful without exposing the actual credential value.
func redact(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}
