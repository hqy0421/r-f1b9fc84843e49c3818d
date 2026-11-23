package flatjsonl

import (
	"regexp"
	"strings"
)

// keyLicense is the single source of truth for include/exclude rules.
//
// Rules are compiled once against source path stage, i.e. the original
// dot-separated flat paths (".a.[0].b") discovered by the flattener, before
// any replacement or transpose transformation. The same compiled instance is
// then used by the flattener, key preparation and every writer-facing
// projection, so that an excluded source path can never be reintroduced as an
// auto-inferred column.
type keyLicense struct {
	caseSensitive bool

	// Literal rules are stored in canonical form.
	excludeLiterals map[string]bool
	includeLiterals map[string]bool

	// Regex rules are compiled against canonical source paths, so that in
	// case-insensitive mode they share the same folding as literal rules.
	excludeRegex []*regexp.Regexp
	includeRegex []*regexp.Regexp

	hasIncludeLiterals bool
	hasIncludeRegex    bool
}

// newKeyLicense compiles include/exclude configuration into a reusable license.
func newKeyLicense(cfg Config, includeRegex, excludeRegex []*regexp.Regexp, caseSensitive bool) *keyLicense {
	l := &keyLicense{
		caseSensitive:      caseSensitive,
		excludeLiterals:    make(map[string]bool, len(cfg.ExcludeKeys)),
		includeLiterals:    make(map[string]bool, len(cfg.IncludeKeys)),
		hasIncludeLiterals: len(cfg.IncludeKeys) > 0,
		hasIncludeRegex:    len(includeRegex) > 0,
	}

	for _, k := range cfg.ExcludeKeys {
		l.excludeLiterals[l.canonical(k)] = true
	}

	for _, k := range cfg.IncludeKeys {
		if strings.HasPrefix(k, "const:") {
			// Constant columns are declared explicitly and are not source paths.
			continue
		}

		l.includeLiterals[l.canonical(k)] = true
	}

	l.excludeRegex = l.compileRegexes(excludeRegex)
	l.includeRegex = l.compileRegexes(includeRegex)

	return l
}

// canonical folds a source path the same way Processor.ck does.
func (l *keyLicense) canonical(k string) string {
	if l.caseSensitive {
		return k
	}

	return strings.ToLower(k)
}

// compileRegexes adapts regex rules to canonical source paths.
//
// User-provided expressions are already anchored/compiled by regex(). For
// case-insensitive processing the source paths are lower-cased, so the
// patterns are made case-insensitive via the inline (?i) flag instead of
// lower-casing the pattern text itself. Rewriting the text would corrupt
// uppercase escapes and syntax such as \D, \S, \W, \B and (?P<Name>...).
func (l *keyLicense) compileRegexes(rr []*regexp.Regexp) []*regexp.Regexp {
	if l.caseSensitive {
		return rr
	}

	out := make([]*regexp.Regexp, 0, len(rr))

	for _, r := range rr {
		out = append(out, regexp.MustCompile("(?i)"+r.String()))
	}

	return out
}

// isExcluded reports whether a canonical source path is denied by an exclude
// rule. Exclusion always wins over inclusion.
func (l *keyLicense) isExcluded(canonicalKey string) bool {
	if l.excludeLiterals[canonicalKey] {
		return true
	}

	for _, r := range l.excludeRegex {
		if r.MatchString(canonicalKey) {
			return true
		}
	}

	return false
}

func (l *keyLicense) matchesIncludeRegex(canonicalKey string) bool {
	for _, r := range l.includeRegex {
		if r.MatchString(canonicalKey) {
			return true
		}
	}

	return false
}

// allowed reports whether a source path is permitted to become a column.
//
// originalKey is the raw dot-separated source path as discovered by the
// flattener. Excluded paths are rejected regardless of any include rule.
// When no include rules are configured every non-excluded path is allowed.
func (l *keyLicense) allowed(originalKey string) bool {
	ck := l.canonical(originalKey)

	if l.isExcluded(ck) {
		return false
	}

	if !l.hasIncludeLiterals && !l.hasIncludeRegex {
		return true
	}

	if l.includeLiterals[ck] {
		return true
	}

	return l.matchesIncludeRegex(ck)
}
