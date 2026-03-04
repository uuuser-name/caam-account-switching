package project

import (
	"path/filepath"
	"sort"
	"strings"
)

func isGlob(pattern string) bool {
	// filepath.Match treats *, ?, and [] as special.
	return strings.ContainsAny(pattern, "*?[")
}

type globMatch struct {
	pattern string
}

func matchingGlobs(associations map[string]map[string]string, target string) []globMatch {
	if len(associations) == 0 {
		return nil
	}

	matches := make([]globMatch, 0, 4)
	for key := range associations {
		if !isGlob(key) {
			continue
		}
		ok, err := filepath.Match(key, target)
		if err != nil || !ok {
			continue
		}
		matches = append(matches, globMatch{pattern: key})
	}

	sort.Slice(matches, func(i, j int) bool {
		pi := matches[i].pattern
		pj := matches[j].pattern

		wi := wildcardCount(pi)
		wj := wildcardCount(pj)
		if wi != wj {
			return wi < wj // fewer wildcards = more specific
		}
		if len(pi) != len(pj) {
			return len(pi) > len(pj) // longer pattern = more specific
		}
		return pi < pj // deterministic
	})

	return matches
}

func wildcardCount(pattern string) int {
	count := 0
	for _, r := range pattern {
		switch r {
		case '*', '?', '[':
			count++
		}
	}
	return count
}
