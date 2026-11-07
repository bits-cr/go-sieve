package interp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/foxcpp/go-sieve/parser"
)

// ActionVacation is the vacation action (RFC 5230), with the :seconds
// argument of vacation-seconds (RFC 6131). It doesn't cancel the implicit
// keep.
//
// The host decides whether to send a response: it tracks the responses sent
// per Handle and sender for Period, and it can build the response with
// BuildVacationResponse, which also applies the RFC 5230 rules about whom
// not to reply to.
type ActionVacation struct {
	// Reason is the response body, or a MIME entity (headers and content)
	// if Mime is set. Variables are expanded.
	Reason  string
	Subject string
	// From is the :from argument, a mailbox-list; "" if absent.
	From string
	// Addresses are the :addresses argument: more addresses of the user.
	Addresses []string
	Mime      bool
	// Handle identifies the response for tracking (RFC 5230, section 4.2):
	// the :handle argument, or else a hash of :subject, :from, :mime and the
	// reason taken before variable expansion.
	Handle string
	// Period is the interval in which the same sender gets no second response
	// with the same Handle, from :days or :seconds. With DefaultPeriod set,
	// neither was given and the host uses its default. The host should apply
	// its minimum and maximum; :seconds 0 is valid (RFC 6131).
	Period        time.Duration
	DefaultPeriod bool
}

func (ActionVacation) testActionName() string    { return "vacation" }
func (ActionVacation) cancelsImplicitKeep() bool { return false }

type CmdVacation struct {
	Reason     string
	Subject    string
	From       string
	Addresses  []string
	Mime       bool
	Handle     string
	Days       int
	DaysSet    bool
	Seconds    int
	SecondsSet bool
}

func (c CmdVacation) Execute(ctx context.Context, d *RuntimeData) error {
	for _, a := range d.AppliedActions {
		switch a.(type) {
		case ActionVacation:
			return errors.New("vacation: can be executed only once per script")
		case ActionReject, ActionEReject:
			return errors.New("vacation: cannot be used together with reject")
		}
	}

	action := ActionVacation{
		Reason:    expandVars(d, c.Reason),
		Subject:   expandVars(d, c.Subject),
		From:      expandVars(d, c.From),
		Addresses: expandVarsList(d, c.Addresses),
		Mime:      c.Mime,
		Handle:    c.Handle,
	}
	switch {
	case c.SecondsSet:
		action.Period = time.Duration(c.Seconds) * time.Second
	case c.DaysSet:
		action.Period = time.Duration(c.Days) * 24 * time.Hour
	default:
		action.DefaultPeriod = true
	}

	return d.OnAction(ctx, action, d)
}

func loadVacation(s *Script, pcmd parser.Cmd) (Cmd, error) {
	// "vacation-seconds" implies "vacation" (RFC 6131, section 2).
	if !s.RequiresExtension("vacation") && !s.RequiresExtension("vacation-seconds") {
		return nil, parser.ErrorAt(pcmd.Position, "missing require 'vacation'")
	}
	cmd := CmdVacation{}
	var handleSet, subjectSet, fromSet bool
	err := LoadSpec(s, &Spec{
		Tags: map[string]SpecTag{
			"days": {
				NeedsValue: true,
				MatchNum: func(val int) {
					cmd.Days = val
					cmd.DaysSet = true
				},
			},
			"seconds": {
				NeedsValue: true,
				MatchNum: func(val int) {
					cmd.Seconds = val
					cmd.SecondsSet = true
				},
			},
			"subject": {
				NeedsValue:  true,
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Subject = val[0]
					subjectSet = true
				},
			},
			"from": {
				NeedsValue:  true,
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.From = val[0]
					fromSet = true
				},
			},
			"addresses": {
				NeedsValue:  true,
				MinStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Addresses = val
				},
			},
			"mime": {
				MatchBool: func() {
					cmd.Mime = true
				},
			},
			"handle": {
				NeedsValue:  true,
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Handle = val[0]
					handleSet = true
				},
			},
		},
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Reason = val[0]
				},
			},
		},
	}, pcmd.Position, pcmd.Args, pcmd.Tests, pcmd.Block)
	if err != nil {
		return nil, err
	}

	if cmd.SecondsSet && !s.RequiresExtension("vacation-seconds") {
		return nil, parser.ErrorAt(pcmd.Position, "missing require 'vacation-seconds'")
	}
	if cmd.SecondsSet && cmd.DaysSet {
		return nil, parser.ErrorAt(pcmd.Position, "vacation: :days and :seconds cannot be used together")
	}
	if cmd.Days < 0 || cmd.Seconds < 0 {
		return nil, parser.ErrorAt(pcmd.Position, "vacation: the period cannot be negative")
	}
	if fromSet && len(usedVars(s, cmd.From)) == 0 {
		if _, err := mail.ParseAddressList(cmd.From); err != nil {
			return nil, parser.ErrorAt(pcmd.Position, "vacation: invalid :from address: %v", err)
		}
	}
	if !handleSet {
		cmd.Handle = vacationHandle(cmd.Subject, subjectSet, cmd.From, fromSet, cmd.Mime, cmd.Reason)
	}

	return cmd, nil
}

// vacationHandle synthesizes a response handle from the arguments as written
// in the script, before variable expansion (RFC 5230, section 4.2). Each
// argument is length-prefixed so that the same text in different arguments
// gives different handles.
func vacationHandle(subject string, subjectSet bool, from string, fromSet bool, isMime bool, reason string) string {
	h := sha256.New()
	field := func(set bool, value string) {
		var hdr [9]byte
		if set {
			hdr[0] = 1
		}
		binary.BigEndian.PutUint64(hdr[1:], uint64(len(value)))
		h.Write(hdr[:])
		h.Write([]byte(value))
	}
	field(subjectSet, subject)
	field(fromSet, from)
	field(isMime, "")
	field(true, reason)
	return "vacation-" + hex.EncodeToString(h.Sum(nil)[:16])
}

// VacationOptions are the site settings BuildVacationResponse applies.
type VacationOptions struct {
	// UserAddresses are addresses known to belong to the recipient, besides
	// the envelope recipient and the :addresses argument (RFC 5230,
	// section 4.5).
	UserAddresses []string
	// OriginalRecipient, if known, is the original envelope recipient
	// (for example from the ORCPT parameter); it also counts as the user's.
	OriginalRecipient string
	// DontCheckRecipient replies even when none of the user's addresses is
	// in a To, Cc, Bcc, Resent-To, Resent-Cc or Resent-Bcc field.
	DontCheckRecipient bool
	// SendFromRecipient sets the response's envelope sender to the
	// recipient's address instead of the null sender.
	SendFromRecipient bool
	// DefaultSubject is used when there is no :subject and the message has
	// no Subject; "Automated reply" if empty (RFC 5230, section 5.3).
	DefaultSubject string
	// Now is the response's Date; time.Now if zero.
	Now time.Time
	// MessageID is the response's Message-ID, with or without angle
	// brackets; generated if empty.
	MessageID string
}

// VacationResponse is a response message ready for submission.
type VacationResponse struct {
	// EnvelopeFrom is the SMTP MAIL FROM: "" (the null sender) unless
	// VacationOptions.SendFromRecipient is set (RFC 5230, section 5.1).
	EnvelopeFrom string
	// Recipient is the SMTP RCPT TO: the envelope sender of the original
	// message.
	Recipient string
	// Message is the RFC 5322 message, with CRLF line endings.
	Message []byte
}

// BuildVacationResponse decides whether the vacation action should send a
// response to the message in d and builds it (RFC 5230, sections 4.5, 4.6 and
// 5). If no response should be sent, it returns a nil response and the reason
// why. Response tracking (:days, :seconds and the handle) is left to the
// caller.
func BuildVacationResponse(d *RuntimeData, a ActionVacation, opts VacationOptions) (*VacationResponse, string, error) {
	sender := strings.TrimSpace(d.Envelope.EnvelopeFrom())
	sender = strings.TrimSuffix(strings.TrimPrefix(sender, "<"), ">")
	if sender == "" {
		return nil, "null envelope sender", nil
	}
	if isSystemAddress(sender) {
		return nil, "envelope sender is a system address", nil
	}

	if skip, err := automatedMessage(d.Msg); err != nil || skip != "" {
		return nil, skip, err
	}

	own := map[string]bool{}
	addOwn := func(addr string) {
		if a := normalizeAddress(addr); a != "" {
			own[a] = true
		}
	}
	addOwn(d.Envelope.EnvelopeTo())
	addOwn(opts.OriginalRecipient)
	for _, addr := range opts.UserAddresses {
		addOwn(addr)
	}
	for _, addr := range a.Addresses {
		addOwn(addr)
	}
	if own[normalizeAddress(sender)] {
		return nil, "envelope sender is the user's own address", nil
	}

	matched, err := addressedTo(d.Msg, own)
	if err != nil {
		return nil, "", err
	}
	if matched == "" && !opts.DontCheckRecipient {
		return nil, "none of the user's addresses is a recipient", nil
	}

	recipientAddr := matched
	if recipientAddr == "" {
		recipientAddr = strings.TrimSpace(d.Envelope.EnvelopeTo())
	}

	// :from after variable expansion. An invalid value is ignored rather
	// than failing the action (RFC 5230, section 4.3); a valid one is written
	// from its parsed form, so no raw text reaches the header.
	from := ""
	if a.From != "" {
		if addrs, err := mail.ParseAddressList(a.From); err == nil && len(addrs) != 0 {
			parts := make([]string, len(addrs))
			for i, addr := range addrs {
				parts[i] = addr.String()
			}
			from = strings.Join(parts, ", ")
		}
	}
	if from == "" {
		from = recipientAddr
		if from == "" {
			return nil, "no address to send the response from", nil
		}
	}

	subject := singleLine(a.Subject)
	if subject == "" {
		original, err := firstHeader(d.Msg, "Subject")
		if err != nil {
			return nil, "", err
		}
		original = singleLine(decodeHeader(original))
		switch {
		case strings.TrimSpace(original) != "":
			subject = "Auto: " + original
		case opts.DefaultSubject != "":
			subject = opts.DefaultSubject
		default:
			subject = "Automated reply"
		}
	}

	to, err := replyTo(d.Msg, sender)
	if err != nil {
		return nil, "", err
	}

	now := opts.Now
	if now.IsZero() {
		now = d.currentTime()
	}
	messageID := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(opts.MessageID), "<"), ">")
	if messageID == "" {
		messageID, err = generateMessageID(recipientAddr)
		if err != nil {
			return nil, "", err
		}
	}

	var b strings.Builder
	header := func(name, value string) {
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
	}
	header("Date", now.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	header("From", encodeAddressList(from))
	header("To", to)
	header("Subject", encodeHeaderText(subject))
	header("Message-ID", "<"+messageID+">")

	originalID, err := firstHeader(d.Msg, "Message-ID")
	if err != nil {
		return nil, "", err
	}
	if originalID = strings.TrimSpace(originalID); originalID != "" {
		header("In-Reply-To", originalID)
		references, err := firstHeader(d.Msg, "References")
		if err != nil {
			return nil, "", err
		}
		header("References", foldIDs(strings.Fields(references+" "+originalID)))
	}
	header("Auto-Submitted", "auto-replied")
	header("MIME-Version", "1.0")

	if a.Mime {
		// The reason is a MIME entity: its headers follow ours.
		b.WriteString(crlf(a.Reason))
	} else {
		header("Content-Type", "text/plain; charset=utf-8")
		if isASCII(a.Reason) {
			header("Content-Transfer-Encoding", "7bit")
		} else {
			header("Content-Transfer-Encoding", "8bit")
		}
		b.WriteString("\r\n")
		b.WriteString(crlf(a.Reason))
	}
	if !strings.HasSuffix(b.String(), "\r\n") {
		b.WriteString("\r\n")
	}

	resp := &VacationResponse{Recipient: sender, Message: []byte(b.String())}
	if opts.SendFromRecipient {
		resp.EnvelopeFrom = recipientAddr
	}
	return resp, "", nil
}

// isSystemAddress reports addresses vacation must not reply to (RFC 5230,
// section 4.6): mailer daemons and list software.
func isSystemAddress(addr string) bool {
	local := strings.ToLower(addr)
	if i := strings.LastIndexByte(local, '@'); i != -1 {
		local = local[:i]
	}
	switch local {
	case "mailer-daemon", "listserv", "majordomo":
		return true
	}
	return strings.HasPrefix(local, "owner-") || strings.HasSuffix(local, "-request")
}

// automatedMessage returns why the message looks automated or sent to a list,
// or "" (RFC 5230, section 4.6; RFC 3834).
func automatedMessage(msg Message) (string, error) {
	for _, h := range []string{"List-Id", "List-Help", "List-Subscribe", "List-Unsubscribe", "List-Post", "List-Owner", "List-Archive"} {
		values, err := msg.HeaderGet(h)
		if err != nil {
			return "", err
		}
		if len(values) != 0 {
			return "mailing list message (" + h + ")", nil
		}
	}

	values, err := msg.HeaderGet("Auto-Submitted")
	if err != nil {
		return "", err
	}
	for _, v := range values {
		keyword, _, _ := strings.Cut(v, ";")
		if !strings.EqualFold(strings.TrimSpace(keyword), "no") {
			return "auto-submitted message", nil
		}
	}

	values, err = msg.HeaderGet("Precedence")
	if err != nil {
		return "", err
	}
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "bulk", "list", "junk":
			return "bulk message (Precedence: " + strings.TrimSpace(v) + ")", nil
		}
	}

	// Microsoft's X-Auto-Response-Suppress: All or OOF asks for no
	// out-of-office replies.
	values, err = msg.HeaderGet("X-Auto-Response-Suppress")
	if err != nil {
		return "", err
	}
	for _, v := range values {
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			if strings.EqualFold(item, "All") || strings.EqualFold(item, "OOF") {
				return "X-Auto-Response-Suppress: " + item, nil
			}
		}
	}
	return "", nil
}

// addressedTo returns the first of the user's addresses found in a recipient
// field of the message, or "".
func addressedTo(msg Message, own map[string]bool) (string, error) {
	for _, h := range []string{"To", "Cc", "Bcc", "Resent-To", "Resent-Cc", "Resent-Bcc"} {
		values, err := msg.HeaderGet(h)
		if err != nil {
			return "", err
		}
		for _, v := range values {
			addrs, err := mail.ParseAddressList(v)
			if err != nil {
				continue // an unparseable field contains none of the user's addresses
			}
			for _, addr := range addrs {
				if own[normalizeAddress(addr.Address)] {
					return addr.Address, nil
				}
			}
		}
	}
	return "", nil
}

// replyTo returns the To field of the response: the envelope sender, with the
// display name it has in the Sender or From field if it appears there.
func replyTo(msg Message, sender string) (string, error) {
	for _, h := range []string{"Sender", "From"} {
		values, err := msg.HeaderGet(h)
		if err != nil {
			return "", err
		}
		for _, v := range values {
			addrs, err := mail.ParseAddressList(v)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if normalizeAddress(addr.Address) == normalizeAddress(sender) && addr.Name != "" {
					return (&mail.Address{Name: addr.Name, Address: addr.Address}).String(), nil
				}
			}
		}
	}
	return "<" + sender + ">", nil
}

func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimSuffix(strings.TrimPrefix(addr, "<"), ">")
	if parsed, err := mail.ParseAddress(addr); err == nil {
		addr = parsed.Address
	}
	return strings.ToLower(addr)
}

func firstHeader(msg Message, name string) (string, error) {
	values, err := msg.HeaderGet(name)
	if err != nil || len(values) == 0 {
		return "", err
	}
	return values[0], nil
}

var wordDecoder = mime.WordDecoder{}

// decodeHeader decodes RFC 2047 encoded words, leaving the text as is if it
// can't be decoded.
func decodeHeader(value string) string {
	decoded, err := wordDecoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

// encodeHeaderText encodes text as RFC 2047 encoded words if and only if it
// contains non-ASCII characters (RFC 5230, section 4.3).
func encodeHeaderText(text string) string {
	if isASCII(text) {
		return text
	}
	return mime.QEncoding.Encode("utf-8", text)
}

// encodeAddressList encodes the display names of a mailbox-list that contain
// non-ASCII characters.
func encodeAddressList(list string) string {
	if isASCII(list) {
		return list
	}
	addrs, err := mail.ParseAddressList(list)
	if err != nil {
		return list
	}
	parts := make([]string, len(addrs))
	for i, addr := range addrs {
		parts[i] = addr.String() // String encodes a non-ASCII name
	}
	return strings.Join(parts, ", ")
}

// foldIDs joins message ids into a References value folded at 78 columns.
func foldIDs(ids []string) string {
	var b strings.Builder
	lineLen := len("References: ")
	for i, id := range ids {
		if i > 0 {
			if lineLen+1+len(id) > 78 {
				b.WriteString("\r\n ")
				lineLen = 1
			} else {
				b.WriteByte(' ')
				lineLen++
			}
		}
		b.WriteString(id)
		lineLen += len(id)
	}
	return b.String()
}

func generateMessageID(from string) (string, error) {
	domain := "localhost"
	if i := strings.LastIndexByte(from, '@'); i != -1 && i+1 < len(from) {
		domain = strings.TrimSuffix(from[i+1:], ">")
	}
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("vacation: message-id: %w", err)
	}
	return hex.EncodeToString(buf[:]) + ".vacation@" + domain, nil
}

// singleLine replaces line breaks with spaces, so that text from a script
// (a text: string can span lines) or a decoded header can't add header
// fields.
func singleLine(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s))
}

func crlf(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func init() {
	gob.Register(CmdVacation{})
}
