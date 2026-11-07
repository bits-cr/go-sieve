package sieve

import (
	"bufio"
	"context"
	"net/textproto"
	"reflect"
	"strings"
	"testing"

	"github.com/foxcpp/go-sieve/interp"
)

func TestFileintoCreateAndSpecialUse(t *testing.T) {
	testExecute(t, `require ["fileinto", "mailbox"];
fileinto :create "new";
`, eml, []interp.AppliedAction{interp.ActionFileInto{Mailbox: "new", Create: true}})

	testExecute(t, `require ["fileinto", "special-use", "variables"];
set "use" "\\Junk";
fileinto :specialuse "${use}" "Spam";
`, eml, []interp.AppliedAction{interp.ActionFileInto{Mailbox: "Spam", SpecialUse: "\\Junk"}})

	testExecute(t, `require ["fileinto", "special-use", "mailbox"];
fileinto :specialuse "\\Archive" :create "Archive";
`, eml, []interp.AppliedAction{interp.ActionFileInto{Mailbox: "Archive", Create: true, SpecialUse: "\\Archive"}})

	// Without a MailboxInfo, the existence tests are false.
	testExecute(t, `require ["fileinto", "mailbox", "special-use"];
if anyof (mailboxexists "INBOX", specialuse_exists "\\Sent") { fileinto "wrong"; }
`, eml, []interp.AppliedAction{interp.ActionKeep{Implicit: true}})
}

func TestMailboxLoadErrors(t *testing.T) {
	for name, script := range map[string]string{
		"create without require":         `require "fileinto"; fileinto :create "x";`,
		"specialuse without require":     `require "fileinto"; fileinto :specialuse "\\Junk" "x";`,
		"specialuse flag without slash":  `require ["fileinto", "special-use"]; fileinto :specialuse "Junk" "x";`,
		"specialuse missing mailbox":     `require ["fileinto", "special-use"]; fileinto :specialuse "\\Junk";`,
		"mailboxexists no args":          `require "mailbox"; if mailboxexists { keep; }`,
		"mailboxexists number":           `require "mailbox"; if mailboxexists 3423 { keep; }`,
		"mailboxexists two args":         `require "mailbox"; if mailboxexists "a" ["b"] { keep; }`,
		"mailboxexists without require":  `if mailboxexists "a" { keep; }`,
		"specialuse_exists bad flag":     `require "special-use"; if specialuse_exists "frop" { keep; }`,
		"specialuse_exists list mailbox": `require "special-use"; if specialuse_exists ["a", "b"] "\\Sent" { keep; }`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(script), DefaultOptions()); err == nil {
				t.Fatal("expected a load error")
			}
		})
	}
}

type mailboxStore struct {
	mailboxes  map[string]bool
	specialUse map[string]string // flag -> mailbox
}

func (m mailboxStore) MailboxExists(_ context.Context, name string) (bool, error) {
	return m.mailboxes[name], nil
}

func (m mailboxStore) SpecialUseExists(_ context.Context, mailbox, flag string) (bool, error) {
	owner, ok := m.specialUse[flag]
	return ok && (mailbox == "" || mailbox == owner), nil
}

func TestMailboxExistence(t *testing.T) {
	store := mailboxStore{
		mailboxes:  map[string]bool{"INBOX": true, "Sent": true, "Junk": true},
		specialUse: map[string]string{"\\Sent": "Sent", "\\Junk": "Junk"},
	}
	for name, tc := range map[string]struct {
		script string
		want   bool
	}{
		"all exist":            {`mailboxexists ["INBOX", "Sent"]`, true},
		"one missing":          {`mailboxexists ["INBOX", "Lists"]`, false},
		"flags on any mailbox": {`specialuse_exists ["\\Sent", "\\Junk"]`, true},
		"flag missing":         {`specialuse_exists ["\\Sent", "\\Archive"]`, false},
		"flag on mailbox":      {`specialuse_exists "Junk" "\\Junk"`, true},
		"flag elsewhere":       {`specialuse_exists "INBOX" "\\Junk"`, false},
	} {
		t.Run(name, func(t *testing.T) {
			script := `require ["fileinto", "mailbox", "special-use"]; if ` + tc.script + ` { fileinto "yes"; }`
			got := runWithMailboxInfo(t, script, store)
			want := []interp.AppliedAction{interp.ActionKeep{Implicit: true}}
			if tc.want {
				want = []interp.AppliedAction{interp.ActionFileInto{Mailbox: "yes"}}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestSpecialUseInvalidAtRuntime(t *testing.T) {
	loaded, err := Load(strings.NewReader(`require ["fileinto", "special-use", "variables"];
set "use" "Junk";
fileinto :specialuse "${use}" "Spam";
`), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data := interp.NewRuntimeData(loaded, interp.DummyPolicy{}, interp.EnvelopeStatic{}, interp.MessageStatic{})
	if err := loaded.Execute(context.Background(), data); err == nil {
		t.Fatal("expected a run-time error for an invalid special-use flag")
	}
}

func runWithMailboxInfo(t *testing.T, script string, info interp.MailboxInfo) []interp.AppliedAction {
	t.Helper()
	msgHdr, err := textproto.NewReader(bufio.NewReader(strings.NewReader(eml))).ReadMIMEHeader()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(strings.NewReader(script), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data := interp.NewRuntimeData(loaded, interp.DummyPolicy{}, interp.EnvelopeStatic{},
		interp.MessageStatic{Size: len(eml), Header: msgHdr})
	data.MailboxInfo = info
	if err := loaded.Execute(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	return data.AppliedActions
}
