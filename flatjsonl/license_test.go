package flatjsonl

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileLicense(t *testing.T, cfg Config, caseSensitive bool) *keyLicense {
	t.Helper()

	var includeRegex, excludeRegex []*regexp.Regexp

	for _, r := range cfg.IncludeKeysRegex {
		rc, err := regex(r)
		require.NoError(t, err)

		includeRegex = append(includeRegex, rc)
	}

	for _, r := range cfg.ExcludeKeysRegex {
		rc, err := regex(r)
		require.NoError(t, err)

		excludeRegex = append(excludeRegex, rc)
	}

	return newKeyLicense(cfg, includeRegex, excludeRegex, caseSensitive)
}

func TestKeyLicense_openByDefault(t *testing.T) {
	l := compileLicense(t, Config{}, false)

	assert.True(t, l.allowed(".anything"))
	assert.True(t, l.allowed(".a.b.[0]"))
}

func TestKeyLicense_excludeLiteralIsFullPathScoped(t *testing.T) {
	l := compileLicense(t, Config{ExcludeKeys: []string{".name"}}, false)

	// Raw top-level key is denied, nested path with the same tail text survives.
	assert.False(t, l.allowed(".name"))
	assert.True(t, l.allowed(".user.name"))
	assert.True(t, l.allowed(".name.id"))
}

func TestKeyLicense_excludeAlwaysWins(t *testing.T) {
	l := compileLicense(t, Config{
		IncludeKeys: []string{".a", ".b"},
		ExcludeKeys: []string{".b"},
	}, false)

	assert.True(t, l.allowed(".a"))
	assert.False(t, l.allowed(".b"))
	// Anything not on the allowlist is denied.
	assert.False(t, l.allowed(".c"))
}

func TestKeyLicense_regexFoldingMatchesLiterals(t *testing.T) {
	cfg := Config{ExcludeKeysRegex: []string{".secret"}}

	// Insensitive: lower-case rule denies upper-case source path.
	insensitive := compileLicense(t, cfg, false)
	assert.False(t, insensitive.allowed(".SECRET"))
	assert.True(t, insensitive.allowed(".a"))

	// Sensitive: same rule does not match the upper-case path.
	sensitive := compileLicense(t, cfg, true)
	assert.True(t, sensitive.allowed(".SECRET"))
	assert.False(t, sensitive.allowed(".secret"))
}

func TestKeyLicense_includeRegexWithExclude(t *testing.T) {
	l := compileLicense(t, Config{
		IncludeKeysRegex: []string{".a.*"},
		ExcludeKeys:      []string{".a.x"},
	}, false)

	assert.True(t, l.allowed(".a.y"))
	assert.False(t, l.allowed(".a.x"))
	assert.False(t, l.allowed(".b"))
}

func TestKeyLicense_constColumnsAreNotSourcePaths(t *testing.T) {
	l := compileLicense(t, Config{
		IncludeKeys: []string{"const:v", ".a"},
		ExcludeKeys: []string{".b"},
	}, false)

	// Only real source paths enter the literal allowlist.
	assert.True(t, l.allowed(".a"))
	assert.False(t, l.allowed(".b"))
	assert.NotContains(t, l.includeLiterals, l.canonical("const:v"))
}

// Case folding must not rewrite pattern text, otherwise uppercase escapes like
// \D (non-digit) would be corrupted into \d (digit).
func TestKeyLicense_caseFoldingKeepsUppercaseEscapes(t *testing.T) {
	// \D.* excludes every non-digit-leading key, while keeping digit-leading ones.
	l := compileLicense(t, Config{ExcludeKeysRegex: []string{`^\.\D`}}, false)

	assert.False(t, l.allowed(".SECRET"), "non-digit leading path must be excluded")
	assert.False(t, l.allowed(".secret"))
	assert.True(t, l.allowed(".123abc"), "digit-leading path must survive an intact \\D pattern")
}
