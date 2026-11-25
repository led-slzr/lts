package cli

import (
	"sort"
	"strings"
)

// suggest returns candidates that plausibly match a mistyped input:
// case-insensitive prefix / substring matches first, then small edit
// distances. Suggest-only by contract — callers never act on the result.
func suggest(input string, candidates []string, max int) []string {
	in := strings.ToLower(strings.TrimSpace(input))
	if in == "" {
		return nil
	}
	type scored struct {
		s     string
		score int
	}
	var out []scored
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		lc := strings.ToLower(c)
		score := -1
		switch {
		case lc == in:
			score = 0
		case strings.HasPrefix(lc, in) || strings.HasPrefix(in, lc):
			score = 1
		case strings.Contains(lc, in) || (len(in) >= 3 && strings.Contains(in, lc)):
			score = 2
		default:
			// allow ~1 edit per 4 chars, at least 2
			limit := len(in) / 4
			if limit < 2 {
				limit = 2
			}
			if d := levenshtein(in, lc); d <= limit {
				score = 2 + d
			} else if d := levenshtein(in, lastSegment(lc)); d <= limit {
				score = 3 + d
			}
		}
		if score >= 0 {
			out = append(out, scored{c, score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		return out[i].s < out[j].s
	})
	var res []string
	for i, o := range out {
		if max > 0 && i >= max {
			break
		}
		res = append(res, o.s)
	}
	return res
}

// lastSegment returns the part after the final "/" (feat/login → login).
func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
