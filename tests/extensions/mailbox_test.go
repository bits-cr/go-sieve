package extensions

import (
	"path/filepath"
	"testing"

	"github.com/foxcpp/go-sieve/tests"
)

// mailbox/errors.svtest requires vnd.dovecot.debug, which go-sieve does not
// implement; its cases are covered by TestMailboxLoadErrors in the root
// package. mailbox/execute.svtest and special-use/execute.svtest run in
// tests.RunExecuteTests, which provides a mail store.

func TestSpecialUseErrors(t *testing.T) {
	tests.RunDovecotTest(t, filepath.Join("..", "pigeonhole", "tests", "extensions", "special-use", "errors.svtest"))
}
