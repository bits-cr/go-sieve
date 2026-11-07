package extensions

import (
	"path/filepath"
	"testing"

	"github.com/foxcpp/go-sieve/tests"
)

func TestRegexExtension(t *testing.T) {
	base := filepath.Join("..", "pigeonhole", "tests", "extensions", "regex")

	t.Run("basic", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "basic.svtest"))
	})
	t.Run("match-values", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "match-values.svtest"))
	})
	t.Run("errors", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "errors.svtest"))
	})
}
