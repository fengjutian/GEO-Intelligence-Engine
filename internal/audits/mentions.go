package audits

import (
	"sort"
	"strings"
)

// countTermMentions counts non-overlapping, longest-first term occurrences.
func countTermMentions(answer string, terms []string) int {
	text := strings.ToLower(answer)
	normalized := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		normalized = append(normalized, term)
	}
	sort.Slice(normalized, func(i, j int) bool { return len(normalized[i]) > len(normalized[j]) })
	count := 0
	for index := 0; index < len(text); {
		matched := false
		for _, term := range normalized {
			if strings.HasPrefix(text[index:], term) {
				count++
				index += len(term)
				matched = true
				break
			}
		}
		if !matched {
			index++
		}
	}
	return count
}
