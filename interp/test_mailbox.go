package interp

import (
	"context"
	"encoding/gob"
	"fmt"
	"unicode/utf8"

	"github.com/foxcpp/go-sieve/parser"
)

// MailboxExistsTest implements mailboxexists (RFC 5490, section 3.1): true if
// every named mailbox exists and the message could be delivered into it.
type MailboxExistsTest struct {
	Mailboxes []string
}

func (t MailboxExistsTest) Check(ctx context.Context, d *RuntimeData) (bool, error) {
	if d.MailboxInfo == nil {
		return false, nil
	}
	for _, name := range expandVarsList(d, t.Mailboxes) {
		if !utf8.ValidString(name) {
			return false, nil
		}
		ok, err := d.MailboxInfo.MailboxExists(ctx, name)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// SpecialUseExistsTest implements specialuse_exists (RFC 8579, section 4):
// true if every flag is assigned to Mailbox or, without Mailbox, to some
// mailbox.
type SpecialUseExistsTest struct {
	Mailbox string
	Flags   []string
}

func (t SpecialUseExistsTest) Check(ctx context.Context, d *RuntimeData) (bool, error) {
	if d.MailboxInfo == nil {
		return false, nil
	}
	mailbox := expandVars(d, t.Mailbox)
	if !utf8.ValidString(mailbox) {
		return false, nil
	}
	for _, flag := range expandVarsList(d, t.Flags) {
		if !validSpecialUseFlag(flag) {
			return false, fmt.Errorf("specialuse_exists: invalid special-use flag: %q", flag)
		}
		ok, err := d.MailboxInfo.SpecialUseExists(ctx, mailbox, flag)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// validSpecialUseFlag reports whether flag is a special-use attribute:
// a backslash followed by an IMAP atom (RFC 6154, RFC 3501).
func validSpecialUseFlag(flag string) bool {
	if len(flag) < 2 || flag[0] != '\\' {
		return false
	}
	for i := 1; i < len(flag); i++ {
		c := flag[i]
		if c <= 0x20 || c >= 0x7f {
			return false // SP, CTL and non-ASCII
		}
		switch c {
		case '(', ')', '{', '%', '*', '"', '\\', ']':
			return false // atom-specials
		}
	}
	return true
}

func loadMailboxExistsTest(s *Script, test parser.Test) (Test, error) {
	if !s.RequiresExtension("mailbox") {
		return nil, fmt.Errorf("missing require 'mailbox'")
	}
	loaded := MailboxExistsTest{}
	err := LoadSpec(s, &Spec{
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MatchStr: func(val []string) {
					loaded.Mailboxes = val
				},
			},
		},
	}, test.Position, test.Args, test.Tests, nil)
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

func loadSpecialUseExistsTest(s *Script, test parser.Test) (Test, error) {
	if !s.RequiresExtension("special-use") {
		return nil, fmt.Errorf("missing require 'special-use'")
	}
	var arg1, arg2 []string
	err := LoadSpec(s, &Spec{
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MatchStr: func(val []string) {
					arg1 = val
				},
			},
			{
				Optional:    true,
				MinStrCount: 1,
				MatchStr: func(val []string) {
					arg2 = val
				},
			},
		},
	}, test.Position, test.Args, test.Tests, nil)
	if err != nil {
		return nil, err
	}

	// specialuse_exists [<mailbox: string>] <special-use-flags: string-list>
	loaded := SpecialUseExistsTest{Flags: arg1}
	if arg2 != nil {
		if len(arg1) != 1 {
			return nil, fmt.Errorf("specialuse_exists: the mailbox must be a single string")
		}
		loaded.Mailbox = arg1[0]
		loaded.Flags = arg2
	}
	for _, flag := range loaded.Flags {
		if len(usedVars(s, flag)) == 0 && !validSpecialUseFlag(flag) {
			return nil, fmt.Errorf("specialuse_exists: invalid special-use flag: %q", flag)
		}
	}
	return loaded, nil
}

func init() {
	gob.Register(MailboxExistsTest{})
	gob.Register(SpecialUseExistsTest{})
}
