package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/foxcpp/go-sieve/interp"
)

type simpleExecuteRuntime struct {
	smtp      []*interp.ExecuteTestMessage
	mailboxes map[string][]*interp.ExecuteTestMessage
}

func (s *simpleExecuteRuntime) CreateMailbox(name string) error {
	if s.mailboxes == nil {
		s.mailboxes = make(map[string][]*interp.ExecuteTestMessage)
	}
	s.mailboxes[name] = []*interp.ExecuteTestMessage{}
	return nil
}

// MailboxExists implements interp.MailboxInfo.
func (s *simpleExecuteRuntime) MailboxExists(_ context.Context, name string) (bool, error) {
	_, ok := s.mailboxes[name]
	return ok, nil
}

// SpecialUseExists implements interp.MailboxInfo. This store has no
// special-use attributes.
func (s *simpleExecuteRuntime) SpecialUseExists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (s *simpleExecuteRuntime) GetDefaultMailbox() string {
	return "INBOX"
}

func (s *simpleExecuteRuntime) ExecuteActions(d *interp.RuntimeData, actions []interp.AppliedAction) error {
	if s.mailboxes == nil {
		s.mailboxes = make(map[string][]*interp.ExecuteTestMessage)
	}
	// As in Pigeonhole, test_message :smtp <n> refers to the messages sent by
	// the latest execution; the mail store persists.
	s.smtp = nil
	for _, act := range actions {
		switch act := act.(type) {
		case interp.ActionFileInto:
			// No mailbox has a special-use attribute here, so :specialuse
			// falls back to the named mailbox (RFC 8579, section 5).
			s.mailboxes[act.Mailbox] = append(s.mailboxes[act.Mailbox], &interp.ExecuteTestMessage{
				Envelope: d.Envelope,
				Message:  d.Msg,
			})
		case interp.ActionRedirect:
			s.smtp = append(s.smtp, &interp.ExecuteTestMessage{
				Envelope: d.Envelope,
				Message:  d.Msg,
			})
		case interp.ActionDiscard:
			continue
		case interp.ActionKeep:
			s.mailboxes[s.GetDefaultMailbox()] = append(s.mailboxes[s.GetDefaultMailbox()], &interp.ExecuteTestMessage{
				Envelope: d.Envelope,
				Message:  d.Msg,
			})
		case interp.ActionReject, interp.ActionEReject:
			// Reject/ereject: no message delivery.
			// TODO: Build MDN and add SMTP to enable SMTP reject tests.
			continue
		default:
			return fmt.Errorf("unknown action type: %T", act)
		}
	}

	return nil
}

func (s *simpleExecuteRuntime) GetSMTPMessage(index int) (*interp.ExecuteTestMessage, error) {
	if index >= len(s.smtp) {
		return nil, fmt.Errorf("index out of range")
	}

	return s.smtp[index], nil
}

func (s *simpleExecuteRuntime) HasSMTPMessage(index int) (bool, error) {
	return index < len(s.smtp), nil
}

func (s *simpleExecuteRuntime) GetMailboxMessage(mailboxName string, index int) (*interp.ExecuteTestMessage, error) {
	mailbox, ok := s.mailboxes[mailboxName]
	if !ok {
		return nil, fmt.Errorf("mailbox %s not found", mailboxName)
	}
	if index >= len(mailbox) {
		return nil, fmt.Errorf("index out of range")
	}

	return mailbox[index], nil
}

func (s *simpleExecuteRuntime) HasMailboxMessage(mailboxName string, index int) (bool, error) {
	mailbox, ok := s.mailboxes[mailboxName]
	if !ok {
		return false, nil
	}
	return index < len(mailbox), nil
}

func TestExecute(t *testing.T) {
	RunExecuteTests(t, &simpleExecuteRuntime{})
}
