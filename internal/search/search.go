package search

import "github.com/sahilm/fuzzy"

type stringSource []string

func (s stringSource) String(i int) string { return s[i] }
func (s stringSource) Len() int            { return len(s) }

// Indices returns indexes into haystacks matching query, best match first.
// An empty query returns every index in order.
func Indices(query string, haystacks []string) []int {
	if len(haystacks) == 0 {
		return nil
	}
	if query == "" {
		idx := make([]int, len(haystacks))
		for i := range haystacks {
			idx[i] = i
		}
		return idx
	}
	results := fuzzy.FindFrom(query, stringSource(haystacks))
	idx := make([]int, len(results))
	for i, r := range results {
		idx[i] = r.Index
	}
	return idx
}

// Filter returns items whose haystack matches query, best match first.
func Filter[T any](query string, items []T, haystack func(T) string) []T {
	if len(items) == 0 {
		return nil
	}
	stacks := make([]string, len(items))
	for i, item := range items {
		stacks[i] = haystack(item)
	}
	idx := Indices(query, stacks)
	out := make([]T, len(idx))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}
