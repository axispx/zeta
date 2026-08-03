package search

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

type stringSource []string

func (s stringSource) String(i int) string { return s[i] }
func (s stringSource) Len() int            { return len(s) }

// Indices returns indexes into haystacks matching query, best match first.
// An empty query returns every index in order.
// Matching is fuzzy (subsequence), suited to file paths and free-form names.
func Indices(query string, haystacks []string) []int {
	return IndicesN(query, haystacks, 0)
}

// IndicesN is Indices capped at limit results (best first).
// limit <= 0 means no cap.
func IndicesN(query string, haystacks []string, limit int) []int {
	if len(haystacks) == 0 {
		return nil
	}
	if query == "" {
		n := len(haystacks)
		if limit > 0 && n > limit {
			n = limit
		}
		idx := make([]int, n)
		for i := range idx {
			idx[i] = i
		}
		return idx
	}
	results := fuzzy.FindFrom(query, stringSource(haystacks))
	n := len(results)
	if limit > 0 && n > limit {
		n = limit
	}
	idx := make([]int, n)
	for i := 0; i < n; i++ {
		idx[i] = results[i].Index
	}
	return idx
}

// Filter returns items whose haystack matches query, best match first (fuzzy).
func Filter[T any](query string, items []T, haystack func(T) string) []T {
	return FilterN(query, items, 0, haystack)
}

// FilterN is Filter capped at limit results (best first). limit <= 0 means no cap.
// Prefer this for UI lists that only render a few rows (e.g. @ file picker).
func FilterN[T any](query string, items []T, limit int, haystack func(T) string) []T {
	if len(items) == 0 {
		return nil
	}
	// Empty query + cap: avoid building haystacks for the whole inventory.
	if query == "" && limit > 0 {
		n := limit
		if n > len(items) {
			n = len(items)
		}
		out := make([]T, n)
		copy(out, items[:n])
		return out
	}
	stacks := make([]string, len(items))
	for i, item := range items {
		stacks[i] = haystack(item)
	}
	idx := IndicesN(query, stacks, limit)
	out := make([]T, len(idx))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}

// Prefix returns items whose key has query as a case-insensitive prefix,
// preserving input order. Empty query returns every item (optionally capped).
//
// Use for small, typed vocabularies (slash commands) where subsequence ranking
// is surprising and stable prefix completion is what users expect.
// limit <= 0 means no cap.
func Prefix[T any](query string, items []T, limit int, key func(T) string) []T {
	if len(items) == 0 {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		n := len(items)
		if limit > 0 && n > limit {
			n = limit
		}
		out := make([]T, n)
		copy(out, items[:n])
		return out
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(key(item)), q) {
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
