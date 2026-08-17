package factcheck

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	maxCommentRunes = 3500
	commentHeader   = "FACT CHECK STATUS: FALSE ❌"
)

var (
	urlPattern          = regexp.MustCompile(`https?://[^\s<>()]+`)
	spaceCommentPattern = regexp.MustCompile(`[\p{Z}\s]+`)
)

func formatComment(result Result) string {
	body := sanitizeGenerated(result.Summary)

	footerParts := []string{}
	sources := commentSources(result)
	if len(sources) > 0 {
		lines := make([]string, 0, len(sources))
		for index, source := range sources {
			lines = append(lines, fmt.Sprintf("%d. %s — %s", index+1, source.title, source.url))
		}
		footerParts = append(footerParts, "Пруфы, с которыми хрен поспоришь:\n"+strings.Join(lines, "\n"))
	}
	footer := strings.Join(footerParts, "\n\n")

	fixedLength := len([]rune(commentHeader)) + len([]rune(footer)) + 2
	if footer != "" {
		fixedLength += 2
	}
	budget := maxCommentRunes - fixedLength
	body = truncate(body, budget)
	parts := []string{commentHeader}
	if body != "" {
		parts = append(parts, body)
	}
	if footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n\n")
}

type commentSource struct {
	title string
	url   string
}

func commentSources(result Result) []commentSource {
	candidates := make([]commentSource, 0, len(result.Sources))
	for _, source := range result.Sources {
		if len([]rune(source.URL)) > 350 {
			continue
		}
		candidates = append(candidates, commentSource{
			title: truncate(source.Title, 100), url: source.URL,
		})
	}
	return candidates
}

func sanitizeGenerated(value string) string {
	value = urlPattern.ReplaceAllString(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(spaceCommentPattern.ReplaceAllString(value, " "))
	return value
}
