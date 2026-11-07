package extensions

import (
	"path/filepath"
	"testing"

	"github.com/foxcpp/go-sieve/tests"
)

func TestDateExtension(t *testing.T) {
	base := filepath.Join("..", "pigeonhole", "tests", "extensions", "date")

	t.Run("basic", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "basic.svtest"))
	})
	t.Run("date-parts", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "date-parts.svtest"))
	})
	t.Run("zones", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "zones.svtest"))
	})
}

func TestIndexExtension(t *testing.T) {
	base := filepath.Join("..", "pigeonhole", "tests", "extensions", "index")

	t.Run("basic", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "basic.svtest"))
	})
	t.Run("errors", func(t *testing.T) {
		tests.RunDovecotTest(t, filepath.Join(base, "errors.svtest"))
	})
}
