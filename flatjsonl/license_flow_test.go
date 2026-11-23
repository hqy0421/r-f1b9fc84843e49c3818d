package flatjsonl_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vearutop/flatjsonl/flatjsonl"
)

// memInput is an in-memory Input source.
type memInput struct {
	*bytes.Reader
}

func (m memInput) Size() int64         { return int64(m.Len()) }
func (m memInput) Reset()              { _, _ = m.Seek(0, 0) }
func (m memInput) Compression() string { return "" }

func newMemInput(data string) flatjsonl.Input {
	return flatjsonl.Input{Reader: memInput{bytes.NewReader([]byte(data))}}
}

func mustCfg(t *testing.T, raw string) flatjsonl.Config {
	t.Helper()

	var cfg flatjsonl.Config
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))

	return cfg
}

// runLicenseCase processes input into a CSV in a temp dir and returns its content.
func runLicenseCase(t *testing.T, cfg flatjsonl.Config, input string, flags func(*flatjsonl.Flags)) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "out.csv")

	f := flatjsonl.Flags{}
	f.CSV = out
	f.Concurrency = 1
	if flags != nil {
		flags(&f)
	}

	proc, err := flatjsonl.NewProcessor(f, cfg, newMemInput(input))
	require.NoError(t, err)
	require.NoError(t, proc.Process())

	b, err := os.ReadFile(out)
	require.NoError(t, err)

	return string(b)
}

// A literal text can appear both as a nested object path and as a raw key name.
// Excluding the raw key must not remove the nested path, and excluding the
// nested path must not remove the raw key: rules are matched on full source
// paths, not on tail segments.
func TestLicense_fullPathStageScoping(t *testing.T) {
	input := `{"name":"top","user":{"name":"nested"}}` + "\n"

	excludedRaw := runLicenseCase(t, mustCfg(t, `{"excludeKeys":[".name"]}`), input, nil)
	assert.Contains(t, excludedRaw, ".user.name")
	assert.NotContains(t, strings.SplitN(excludedRaw, "\n", 2)[0], ".name,")

	excludedNested := runLicenseCase(t, mustCfg(t, `{"excludeKeys":[".user.name"]}`), input, nil)
	header := strings.SplitN(excludedNested, "\n", 2)[0]
	assert.Contains(t, header, ".name")
	assert.NotContains(t, header, ".user.name")
}

// An excluded source path can never be published as an auto-inferred column,
// including when it is also present in the explicit include list.
func TestLicense_excludeBeatsIncludeAndAutoInfer(t *testing.T) {
	input := `{"a":{"x":1},"secret":2,"keep":3}` + "\n"

	out := runLicenseCase(t, mustCfg(t, `{
		"includeKeys":[".a.x",".secret",".keep"],
		"excludeKeys":[".secret"]
	}`), input, nil)

	assert.NotContains(t, out, "secret")
	assert.Contains(t, out, ".a.x")
	assert.Contains(t, out, ".keep")
}

// The flattener and every writer share the same permitted set: run CSV and RAW
// writers together and assert neither emits the excluded column/value.
func TestLicense_flattenerAndWritersShareLicense(t *testing.T) {
	dir := t.TempDir()
	csvOut := filepath.Join(dir, "out.csv")
	rawOut := filepath.Join(dir, "out.raw")

	f := flatjsonl.Flags{}
	f.CSV = csvOut
	f.Raw = rawOut
	f.Concurrency = 1
	f.PrepareOutput()

	cfg := mustCfg(t, `{
		"includeKeys":[".a.x",".secret",".keep"],
		"excludeKeys":[".secret"]
	}`)

	proc, err := flatjsonl.NewProcessor(f, cfg,
		newMemInput(`{"a":{"x":1},"secret":2,"keep":3}`+"\n"))
	require.NoError(t, err)
	require.NoError(t, proc.Process())

	csv, err := os.ReadFile(csvOut)
	require.NoError(t, err)
	raw, err := os.ReadFile(rawOut)
	require.NoError(t, err)

	assert.NotContains(t, string(csv), "secret")
	assert.NotContains(t, string(raw), "secret")

	// RAW has no header; the excluded value 2 must be gone from the row too.
	assert.Equal(t, "13\n", string(raw))
}

// Regex and literal rules use the same case folding: in default
// case-insensitive mode a lower-case regex denies an upper-case source path.
func TestLicense_regexFoldingConsistent(t *testing.T) {
	input := `{"a":{"x":1},"SECRET":2}` + "\n"

	insensitive := runLicenseCase(t, mustCfg(t, `{"excludeKeysRegex":[".secret"]}`), input, nil)
	assert.NotContains(t, insensitive, ".SECRET")

	sensitive := runLicenseCase(t, mustCfg(t, `{"excludeKeysRegex":[".secret"]}`), input,
		func(fl *flatjsonl.Flags) { fl.CaseSensitiveKeys = true })
	assert.Contains(t, sensitive, ".SECRET")
}

// Excluded columns must not leak through diagnostic outputs (flat key list,
// hierarchy tree, JSON schema).
func TestLicense_excludedKeysAbsentFromDiagnosticOutputs(t *testing.T) {
	f := flatjsonl.Flags{}
	f.CSV = filepath.Join(t.TempDir(), "out.csv")
	f.Concurrency = 1
	f.ShowKeysFlat = true
	f.ShowKeysHier = true
	f.ShowJSONSchema = true

	cfg := mustCfg(t, `{"excludeKeys":[".secret"]}`)

	proc, err := flatjsonl.NewProcessor(f, cfg,
		newMemInput(`{"a":{"x":1},"secret":2,"keep":3}`+"\n"))
	require.NoError(t, err)

	stdout := bytes.NewBuffer(nil)
	proc.Stdout = stdout
	require.NoError(t, proc.Process())

	out := stdout.String()
	assert.NotContains(t, out, "secret")
	assert.Contains(t, out, ".a.x")
	assert.Contains(t, out, ".keep")
}
