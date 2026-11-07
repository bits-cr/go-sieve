package interp

import (
	"bufio"
	"bytes"
	"mime"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func vacationData(t *testing.T, from, to, msg string) *RuntimeData {
	t.Helper()
	hdr, err := textproto.NewReader(bufio.NewReader(strings.NewReader(msg))).ReadMIMEHeader()
	if err != nil {
		t.Fatal(err)
	}
	return &RuntimeData{
		Envelope: EnvelopeStatic{From: from, To: to},
		Msg:      MessageStatic{Header: hdr},
	}
}

const vacationMsg = "From: \"Wile E. Coyote\" <coyote@desert.example.org>\r\n" +
	"To: roadrunner@acme.example.com\r\n" +
	"Subject: Anvil\r\n" +
	"Message-ID: <anvil@desert.example.org>\r\n" +
	"References: <older@desert.example.org>\r\n" +
	"\r\nBeep?\r\n"

func TestVacationResponseSkips(t *testing.T) {
	act := ActionVacation{Reason: "away"}
	for name, tc := range map[string]struct {
		from, to, extra string
		opts            VacationOptions
	}{
		"null sender":          {from: "", to: "roadrunner@acme.example.com"},
		"null sender brackets": {from: "<>", to: "roadrunner@acme.example.com"},
		"mailer-daemon":        {from: "MAILER-DAEMON@desert.example.org", to: "roadrunner@acme.example.com"},
		"owner-":               {from: "owner-list@desert.example.org", to: "roadrunner@acme.example.com"},
		"-request":             {from: "list-request@desert.example.org", to: "roadrunner@acme.example.com"},
		"list header":          {from: "coyote@desert.example.org", to: "roadrunner@acme.example.com", extra: "List-Id: <acme.example.com>\r\n"},
		"auto-submitted":       {from: "coyote@desert.example.org", to: "roadrunner@acme.example.com", extra: "Auto-Submitted: auto-generated\r\n"},
		"precedence bulk":      {from: "coyote@desert.example.org", to: "roadrunner@acme.example.com", extra: "Precedence: bulk\r\n"},
		"suppress oof":         {from: "coyote@desert.example.org", to: "roadrunner@acme.example.com", extra: "X-Auto-Response-Suppress: DR, OOF\r\n"},
		"own address":          {from: "roadrunner@acme.example.com", to: "roadrunner@acme.example.com"},
		"not a recipient":      {from: "coyote@desert.example.org", to: "someone@acme.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			d := vacationData(t, tc.from, tc.to, tc.extra+vacationMsg)
			resp, why, err := BuildVacationResponse(d, act, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if resp != nil || why == "" {
				t.Fatalf("expected no response with a reason, got %v, %q", resp != nil, why)
			}
		})
	}

	// Auto-Submitted: no is a person's message.
	d := vacationData(t, "coyote@desert.example.org", "roadrunner@acme.example.com", "Auto-Submitted: no\r\n"+vacationMsg)
	if resp, why, _ := BuildVacationResponse(d, act, VacationOptions{}); resp == nil {
		t.Errorf("Auto-Submitted: no suppressed the response: %s", why)
	}
	// DontCheckRecipient replies even when the user isn't a recipient.
	d = vacationData(t, "coyote@desert.example.org", "someone@acme.example.com", vacationMsg)
	if resp, why, _ := BuildVacationResponse(d, act, VacationOptions{DontCheckRecipient: true}); resp == nil {
		t.Errorf("DontCheckRecipient: %s", why)
	}
}

func TestVacationResponse(t *testing.T) {
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	d := vacationData(t, "coyote@desert.example.org", "rr@acme.example.com",
		"Cc: Road Runner <ROADRUNNER@acme.example.com>\r\n"+vacationMsg)

	resp, why, err := BuildVacationResponse(d,
		ActionVacation{Reason: "Meep meep.\nBack soon.", Addresses: []string{"roadrunner@acme.example.com"}},
		VacationOptions{Now: now, MessageID: "reply@acme.example.com"})
	if err != nil || resp == nil {
		t.Fatalf("no response: %q, %v", why, err)
	}
	if resp.EnvelopeFrom != "" || resp.Recipient != "coyote@desert.example.org" {
		t.Errorf("envelope: from %q to %q", resp.EnvelopeFrom, resp.Recipient)
	}

	hdr, body := parseResponse(t, resp.Message)
	for name, want := range map[string]string{
		// From is the matched recipient address, as written in the message.
		"From":                      "roadrunner@acme.example.com",
		"To":                        `"Wile E. Coyote" <coyote@desert.example.org>`,
		"Subject":                   "Auto: Anvil",
		"Date":                      "Sat, 07 Nov 2026 12:00:00 +0000",
		"Message-Id":                "<reply@acme.example.com>",
		"In-Reply-To":               "<anvil@desert.example.org>",
		"References":                "<older@desert.example.org> <anvil@desert.example.org>",
		"Auto-Submitted":            "auto-replied",
		"Content-Type":              "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "7bit",
	} {
		if got := hdr.Get(name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	if body != "Meep meep.\r\nBack soon.\r\n" {
		t.Errorf("body: %q", body)
	}
	if bytes.Contains(bytes.ReplaceAll(resp.Message, []byte("\r\n"), nil), []byte("\n")) {
		t.Error("response has bare LF line endings")
	}
}

func TestVacationResponseEncoding(t *testing.T) {
	d := vacationData(t, "coyote@desert.example.org", "roadrunner@acme.example.com",
		"From: coyote@desert.example.org\r\nTo: roadrunner@acme.example.com\r\nSubject: =?utf-8?q?=C3=84pfel?=\r\n\r\nx\r\n")

	resp, _, err := BuildVacationResponse(d,
		ActionVacation{Reason: "Auswärts", From: "Günther <g@acme.example.com>"}, VacationOptions{})
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	hdr, _ := parseResponse(t, resp.Message)
	dec := mime.WordDecoder{}
	subject, err := dec.DecodeHeader(hdr.Get("Subject"))
	if err != nil || subject != "Auto: Äpfel" || hdr.Get("Subject") == subject {
		t.Errorf("subject %q decodes to %q (%v); want an encoded 'Auto: Äpfel'", hdr.Get("Subject"), subject, err)
	}
	from, _ := dec.DecodeHeader(hdr.Get("From"))
	if from != "Günther <g@acme.example.com>" || hdr.Get("From") == from {
		t.Errorf("from %q decodes to %q; want an encoded name", hdr.Get("From"), from)
	}
	if hdr.Get("Content-Transfer-Encoding") != "8bit" {
		t.Errorf("non-ASCII body: %q", hdr.Get("Content-Transfer-Encoding"))
	}

	// No subject at all: the default, or the configured one.
	d = vacationData(t, "coyote@desert.example.org", "roadrunner@acme.example.com",
		"To: roadrunner@acme.example.com\r\n\r\nx\r\n")
	resp, _, _ = BuildVacationResponse(d, ActionVacation{Reason: "x"}, VacationOptions{})
	if hdr, _ := parseResponse(t, resp.Message); hdr.Get("Subject") != "Automated reply" {
		t.Errorf("default subject: %q", hdr.Get("Subject"))
	}
	resp, _, _ = BuildVacationResponse(d, ActionVacation{Reason: "x"}, VacationOptions{DefaultSubject: "Out of office"})
	if hdr, _ := parseResponse(t, resp.Message); hdr.Get("Subject") != "Out of office" {
		t.Errorf("configured subject: %q", hdr.Get("Subject"))
	}
}

func TestVacationResponseMIME(t *testing.T) {
	d := vacationData(t, "coyote@desert.example.org", "roadrunner@acme.example.com", vacationMsg)
	reason := "Content-Type: text/html; charset=us-ascii\n\n<p>Away</p>\n"
	resp, _, err := BuildVacationResponse(d, ActionVacation{Reason: reason, Mime: true}, VacationOptions{SendFromRecipient: true})
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	hdr, body := parseResponse(t, resp.Message)
	if hdr.Get("Content-Type") != "text/html; charset=us-ascii" || body != "<p>Away</p>\r\n" {
		t.Errorf("MIME reason not used as the entity: %q, %q", hdr.Get("Content-Type"), body)
	}
	if resp.EnvelopeFrom != "roadrunner@acme.example.com" {
		t.Errorf("SendFromRecipient: envelope from %q", resp.EnvelopeFrom)
	}
}

func parseResponse(t *testing.T, msg []byte) (textproto.MIMEHeader, string) {
	t.Helper()
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(msg)))
	hdr, err := r.ReadMIMEHeader()
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for {
		line, err := r.R.ReadString('\n')
		body.WriteString(line)
		if err != nil {
			break
		}
	}
	return hdr, body.String()
}
