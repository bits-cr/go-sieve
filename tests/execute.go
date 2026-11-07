package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foxcpp/go-sieve/interp"
)

// vacationDisabledTests are Pigeonhole vacation tests that go-sieve can't
// pass for reasons outside the vacation extension, or that check behaviour
// specific to Pigeonhole.
var vacationDisabledTests = []string{
	// test_error is a no-op in this harness (it always matches), so the
	// "too many runtime errors" check always fails. The conflict itself is
	// tested by TestVacationConflicts.
	"Action conflicts: reject <-> vacation",
	// Pigeonhole's sieve_vacation_default_subject_template setting.
	"Subject - configured",
	"Subject - configured, full variable",
	// Pigeonhole truncates long generated subjects with an ellipsis;
	// go-sieve keeps the whole subject.
	"Extremely long subject",
	"Extremely long japanese subject",
	// Pigeonhole's sieve_vacation_to_header_ignore_envelope setting.
	"Reply to (ignored envelope)",
	// Header tests compare raw values: MessageStatic does not decode RFC 2047
	// encoded words, so the correctly encoded response can't be compared
	// with the decoded text. TestVacationResponse checks the encoding.
	"UTF-8 Subject",
	"MIME Encoded Subject",
	"MIME Encoded From",
	"MIME Encoded From - UTF-8 in phrase",
}

func RunExecuteTests(t *testing.T, env interp.ExecuteTestEnvironment) {
	t.Run("examples", func(t *testing.T) {
		t.Skip("needs reject extension")

		svTest, err := os.ReadFile(filepath.Join("pigeonhole", "tests", "execute", "examples.svtest"))
		if err != nil {
			t.Fatal(err)
		}
		svTestPatched := strings.ReplaceAll(string(svTest), "../../examples/", "examples/")

		RunDovecotTestInline(t, "pigeonhole", svTestPatched,
			ExecuteTestRuntime(env),
		)
	})
	t.Run("extensions/imap4flags", func(t *testing.T) {
		t.Skip("fileinto deduplication is not implemented in go-sieve")

		RunDovecotTest(t, filepath.Join("pigeonhole", "tests", "extensions", "imap4flags", "execute.svtest"),
			ExecuteTestRuntime(env),
		)
	})
	t.Run("extensions/mailbox", func(t *testing.T) {
		RunDovecotTest(t,
			filepath.Join("pigeonhole", "tests", "extensions", "mailbox", "execute.svtest"),
			ExecuteTestRuntime(env),
		)
	})
	t.Run("extensions/special-use", func(t *testing.T) {
		RunDovecotTest(t,
			filepath.Join("pigeonhole", "tests", "extensions", "special-use", "execute.svtest"),
			ExecuteTestRuntime(env),
		)
	})
	t.Run("extensions/vacation", func(t *testing.T) {
		base := filepath.Join("pigeonhole", "tests", "extensions", "vacation")
		for _, suite := range []string{"execute", "errors", "reply", "smtp", "message", "utf-8"} {
			suite := suite
			t.Run(suite, func(t *testing.T) {
				RunDovecotTestWithout(t, filepath.Join(base, suite+".svtest"), vacationDisabledTests,
					ExecuteTestRuntime(env),
				)
			})
		}
	})
	t.Run("extensions/reject", func(t *testing.T) {
		RunDovecotTest(t,
			filepath.Join("pigeonhole", "tests", "extensions", "reject", "execute.svtest"),
			ExecuteTestRuntime(env),
		)
	})
}
