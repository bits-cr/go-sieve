package sieve

import (
	"strings"
	"testing"

	"github.com/foxcpp/go-sieve/interp"
)

func TestRegexMatch(t *testing.T) {
	// Unanchored: the expression matches anywhere in the value.
	testExecute(t, `require ["regex", "fileinto"];
if header :regex "subject" "present" { fileinto "matched"; }
`, eml,
		[]interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}})

	// The default comparator (i;ascii-casemap) folds case.
	testExecute(t, `require ["regex", "fileinto"];
if header :regex "subject" "^i HAVE a" { fileinto "matched"; }
`, eml,
		[]interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}})

	// i;octet does not.
	testExecute(t, `require ["regex", "fileinto"];
if header :regex :comparator "i;octet" "subject" "^i HAVE a" { fileinto "matched"; }
`, eml,
		[]interp.AppliedAction{interp.ActionKeep{Implicit: true}})

	// address and envelope take :regex too.
	testExecute(t, `require ["regex", "envelope", "fileinto"];
if allof (address :regex :domain "from" "^desert\\.example\\.(org|net)$",
          envelope :regex "from" "@test\\.com$") { fileinto "matched"; }
`, eml,
		[]interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}})
}

func TestRegexLeftmostLongest(t *testing.T) {
	// POSIX semantics: of the alternatives, the longest match wins, so ${1} is
	// "ab", not "a" as leftmost-first matching would give.
	testExecute(t, `require ["regex", "variables", "fileinto"];
if string :regex "ab" "(a|ab)" { fileinto "${1}"; }
`, eml,
		[]interp.AppliedAction{interp.ActionFileInto{Mailbox: "ab"}})
}

func TestRegexMatchVariables(t *testing.T) {
	testExecute(t, `require ["regex", "variables", "fileinto"];
if header :regex "from" "^([a-z]+)@([a-z]+)\\." { fileinto "${2}/${1}/${0}"; }
`, eml,
		[]interp.AppliedAction{interp.ActionFileInto{Mailbox: "desert/coyote/coyote@desert."}})
}

func TestRegexInvalidAtRuntime(t *testing.T) {
	// A pattern that is only invalid after variable expansion fails the test
	// instead of aborting the script.
	testExecute(t, `require ["regex", "variables", "fileinto"];
set "re" "[";
if header :regex "subject" "${re}" { fileinto "matched"; }
`, eml,
		[]interp.AppliedAction{interp.ActionKeep{Implicit: true}})
}

func TestRegexLoadErrors(t *testing.T) {
	for name, script := range map[string]string{
		"missing require": `if header :regex "subject" "a" { keep; }`,
		"invalid pattern": `require "regex"; if header :regex "subject" "a[" { keep; }`,
		"not POSIX ERE":   `require "regex"; if header :regex "subject" "\\d+" { keep; }`,
		"numeric comparator": `require ["regex", "comparator-i;ascii-numeric"];
if header :regex :comparator "i;ascii-numeric" "subject" "1" { keep; }`,
		"two match types": `require "regex"; if header :regex :is "subject" "a" { keep; }`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(script), DefaultOptions()); err == nil {
				t.Fatal("expected a load error")
			}
		})
	}
}
