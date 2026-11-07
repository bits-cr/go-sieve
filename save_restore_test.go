package sieve

import (
	"testing"

	"github.com/foxcpp/go-sieve/interp"
)

// Each case runs twice through testExecute: as loaded, and after Script.Save
// and RestoreSaved. Before matcher fields were encoded and all command and test
// types were registered with gob, the restored runs lost their keys (falling
// back to implicit keep) or failed to save at all.
func TestSaveRestoreMatchers(t *testing.T) {
	fileinto := []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}}

	t.Run("header", func(t *testing.T) {
		testExecute(t, `require "fileinto";
if header :is "subject" "I have a present for you" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("header :contains with comparator", func(t *testing.T) {
		testExecute(t, `require "fileinto";
if header :contains :comparator "i;octet" "subject" "present" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("header :matches", func(t *testing.T) {
		testExecute(t, `require "fileinto";
if header :matches "subject" "I have*" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("address", func(t *testing.T) {
		testExecute(t, `require "fileinto";
if address :domain "from" "desert.example.org" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("envelope", func(t *testing.T) {
		testExecute(t, `require ["fileinto", "envelope"];
if envelope :localpart "to" "to" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("set and string", func(t *testing.T) {
		testExecute(t, `require ["fileinto", "variables"];
set "box" "matched";
if string :is "${box}" "matched" { fileinto "${box}"; }
`, eml, fileinto)
	})
	t.Run("set modifiers", func(t *testing.T) {
		testExecute(t, `require ["fileinto", "variables"];
set :upperfirst :lower "box" "MATCHED";
set :length "len" "${box}";
if string :is "${box}${len}" "Matched7" { fileinto "matched"; }
`, eml, fileinto)
	})
	t.Run("hasflag", func(t *testing.T) {
		testExecute(t, `require ["fileinto", "imap4flags"];
setflag "\\Seen";
if hasflag :is "\\Seen" { fileinto "matched"; }
`, eml, []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched", Flags: interp.Flags{"\\Seen"}}})
	})
}
