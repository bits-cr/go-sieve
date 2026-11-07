package sieve

import (
	"bufio"
	"context"
	"net/textproto"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foxcpp/go-sieve/interp"
)

// The eml fixture's Date: is "Tue, 1 Apr 1997 09:06:31 -0800 (PST)".

func TestDateTest(t *testing.T) {
	fileinto := []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}}

	for name, script := range map[string]string{
		"originalzone parts": `require ["date", "fileinto"];
if allof (date :originalzone "date" "date" "1997-04-01",
          date :originalzone "date" "time" "09:06:31",
          date :originalzone "date" "zone" "-0800",
          date :originalzone "date" "weekday" "2",
          date :originalzone "date" "julian" "50539",
          date :originalzone "date" "iso8601" "1997-04-01T09:06:31-08:00",
          date :originalzone "date" "std11" "Tue, 01 Apr 1997 09:06:31 -0800") { fileinto "matched"; }`,
		"zone shift": `require ["date", "fileinto"];
if allof (date :zone "+0000" "date" "iso8601" "1997-04-01T17:06:31Z",
          date :zone "+0530" "date" "time" "22:36:31") { fileinto "matched"; }`,
		"count": `require ["date", "relational", "fileinto"];
if allof (date :count "eq" "date" "date" "1",
          date :count "eq" "resent-date" "date" "0") { fileinto "matched"; }`,
		"value": `require ["date", "relational", "fileinto"];
if date :value "lt" :originalzone "date" "date" "1997-04-02" { fileinto "matched"; }`,
	} {
		t.Run(name, func(t *testing.T) { testExecute(t, script, eml, fileinto) })
	}
}

func TestDateJulianFarDates(t *testing.T) {
	// The Modified Julian Day is exact for dates time.Duration can't span.
	for _, tc := range []struct {
		date string
		mjd  string
	}{
		{"Wed, 17 Nov 1858 00:00:00 +0000", "0"},
		{"Tue, 16 Nov 1858 00:00:00 +0000", "-1"},
		{"Wed, 1 Jan 2200 00:00:00 +0000", "124593"},
		{"Fri, 31 Dec 9999 23:59:59 +0000", "2973483"},
	} {
		msg := "Date: " + tc.date + "\nSubject: x\n\nBody.\n"
		testExecute(t, `require ["date", "fileinto"];
if date :originalzone "date" "julian" "`+tc.mjd+`" { fileinto "matched"; }
`, msg, []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}})
	}
}

func TestDateLoadErrors(t *testing.T) {
	for name, script := range map[string]string{
		"missing require":          `if date "date" "year" "1997" { keep; }`,
		"zone and originalzone":    `require "date"; if date :zone "+0100" :originalzone "date" "year" "1997" { keep; }`,
		"bad zone":                 `require "date"; if date :zone "UTC" "date" "year" "1997" { keep; }`,
		"bad date-part":            `require "date"; if date "date" "fortnight" "1" { keep; }`,
		"currentdate originalzone": `require "date"; if currentdate :originalzone "year" "1997" { keep; }`,
		"index without require":    `require "date"; if date :index 1 "date" "year" "1997" { keep; }`,
		"last without index":       `require ["date", "index"]; if header :last "to" "x" { keep; }`,
		"index zero":               `require ["index"]; if header :index 0 "to" "x" { keep; }`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(script), DefaultOptions()); err == nil {
				t.Fatal("expected a load error")
			}
		})
	}
}

// runWithClock executes script against eml with a fixed clock and local zone.
func runWithClock(t *testing.T, script string, now time.Time, loc *time.Location) []interp.AppliedAction {
	t.Helper()
	msgHdr, err := textproto.NewReader(bufio.NewReader(strings.NewReader(eml))).ReadMIMEHeader()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(strings.NewReader(script), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data := interp.NewRuntimeData(loaded, interp.DummyPolicy{},
		interp.EnvelopeStatic{From: "from@test.com", To: "to@test.com"},
		interp.MessageStatic{Size: len(eml), Header: msgHdr})
	calls := 0
	data.Now = func() time.Time {
		calls++
		return now.Add(time.Duration(calls-1) * time.Hour) // a clock that moves
	}
	data.Location = loc
	if err := loaded.Execute(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if calls > 1 {
		t.Errorf("Now called %d times; currentdate must use one instant per execution", calls)
	}
	return data.AppliedActions
}

func TestCurrentDate(t *testing.T) {
	now := time.Date(2026, time.November, 7, 23, 30, 0, 0, time.UTC)
	costaRica := time.FixedZone("-0600", -6*3600)

	got := runWithClock(t, `require ["date", "variables", "fileinto"];
if currentdate :matches "date" "*" { set "d" "${0}"; }
if currentdate :matches "hour" "*" { set "h" "${0}"; }
if currentdate :zone "+0000" :matches "date" "*" { set "utc" "${0}"; }
fileinto "${d} ${h} ${utc}";
`, now, costaRica)
	want := []interp.AppliedAction{interp.ActionFileInto{Mailbox: "2026-11-07 17 2026-11-07"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	// The local zone also applies to the date test without :zone.
	got = runWithClock(t, `require ["date", "fileinto"];
if date "date" "time" "11:06:31" { fileinto "matched"; }
`, now, costaRica)
	want = []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	// A :zone that is only invalid after variable expansion fails the test.
	got = runWithClock(t, `require ["date", "variables", "fileinto"];
set "z" "Mars";
if currentdate :zone "${z}" "year" "2026" { fileinto "matched"; }
`, now, costaRica)
	want = []interp.AppliedAction{interp.ActionKeep{Implicit: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

var indexEml = `Received: from a by b; Tue, 1 Apr 1997 09:06:31 -0800
X-A: first
To: one@example.com
Received: from c by d; Tue, 1 Apr 1997 08:06:31 -0800
X-A: second
To: two@example.com, three@example.com
Subject: index

Body.
`

func TestIndex(t *testing.T) {
	fileinto := []interp.AppliedAction{interp.ActionFileInto{Mailbox: "matched"}}

	for name, script := range map[string]string{
		"header":          `require ["index", "fileinto"]; if header :index 2 "x-a" "second" { fileinto "matched"; }`,
		"header last":     `require ["index", "fileinto"]; if header :index 2 :last "x-a" "first" { fileinto "matched"; }`,
		"across names":    `require ["index", "fileinto"]; if header :index 3 ["x-a", "subject"] "index" { fileinto "matched"; }`,
		"address":         `require ["index", "fileinto"]; if address :localpart :index 2 "to" "three" { fileinto "matched"; }`,
		"date":            `require ["index", "date", "fileinto"]; if date :index 2 :originalzone "received" "hour" "08" { fileinto "matched"; }`,
		"date last":       `require ["index", "date", "fileinto"]; if date :index 1 :last :originalzone "received" "hour" "08" { fileinto "matched"; }`,
		"count is 0 or 1": `require ["index", "relational", "fileinto"]; if allof (header :count "eq" :index 1 "x-a" "1", header :count "eq" :index 9 "x-a" "0") { fileinto "matched"; }`,
		"out of range":    `require ["index", "fileinto"]; if not header :index 3 "x-a" "*" { fileinto "matched"; }`,
	} {
		t.Run(name, func(t *testing.T) { testExecute(t, script, indexEml, fileinto) })
	}
}
