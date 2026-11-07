package sieve

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/foxcpp/go-sieve/interp"
)

func TestVacationAction(t *testing.T) {
	// Vacation keeps the implicit keep.
	got := runVacation(t, `require "vacation";
vacation :days 3 :subject "Away" :from "me@example.com" :addresses ["alias@example.com"] :handle "trip" "I'm away";
`)
	if len(got) != 2 {
		t.Fatalf("got %d actions, want vacation and implicit keep: %#v", len(got), got)
	}
	v := got[0].(interp.ActionVacation)
	if v.Reason != "I'm away" || v.Subject != "Away" || v.From != "me@example.com" ||
		len(v.Addresses) != 1 || v.Handle != "trip" || v.Period != 72*time.Hour || v.DefaultPeriod {
		t.Errorf("unexpected action: %#v", v)
	}
	if k, ok := got[1].(interp.ActionKeep); !ok || !k.Implicit {
		t.Errorf("second action is %#v, want implicit keep", got[1])
	}

	// No period: the host's default applies.
	v = runVacation(t, `require "vacation"; vacation "x";`)[0].(interp.ActionVacation)
	if !v.DefaultPeriod || v.Period != 0 {
		t.Errorf("want DefaultPeriod, got %#v", v)
	}

	// vacation-seconds implies vacation, and :seconds 0 is valid.
	v = runVacation(t, `require "vacation-seconds"; vacation :seconds 0 "x";`)[0].(interp.ActionVacation)
	if v.DefaultPeriod || v.Period != 0 {
		t.Errorf("want an explicit zero period, got %#v", v)
	}
}

func TestVacationHandle(t *testing.T) {
	handle := func(script string) string {
		return runVacation(t, script)[0].(interp.ActionVacation).Handle
	}

	// The synthesized handle is computed before variable expansion, so a
	// different subject value gives the same handle (RFC 5230, section 4.2).
	a := handle(`require ["vacation", "variables"]; set "s" "one"; vacation :subject "Re: ${s}" "away";`)
	b := handle(`require ["vacation", "variables"]; set "s" "two"; vacation :subject "Re: ${s}" "away";`)
	if a != b {
		t.Errorf("handles differ for the same arguments: %q, %q", a, b)
	}
	// The same text in a different argument gives a different handle.
	c := handle(`require "vacation"; vacation :subject "me@example.com" "away";`)
	d := handle(`require "vacation"; vacation :from "me@example.com" "away";`)
	if c == d {
		t.Errorf("handles collide across arguments: %q", c)
	}
	if e := handle(`require "vacation"; vacation "away";`); e == c {
		t.Errorf("handle ignores :subject: %q", e)
	}
}

func TestVacationConflicts(t *testing.T) {
	for name, script := range map[string]string{
		"twice":         `require "vacation"; vacation "a"; vacation "b";`,
		"then reject":   `require ["vacation", "reject"]; vacation "a"; reject "no";`,
		"after reject":  `require ["vacation", "reject"]; reject "no"; vacation "a";`,
		"after ereject": `require ["vacation", "ereject"]; ereject "no"; vacation "a";`,
	} {
		t.Run(name, func(t *testing.T) {
			loaded, err := Load(strings.NewReader(script), DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			data := interp.NewRuntimeData(loaded, interp.DummyPolicy{}, interp.EnvelopeStatic{}, interp.MessageStatic{})
			if err := loaded.Execute(context.Background(), data); err == nil {
				t.Fatal("expected a run-time error")
			}
		})
	}
}

func TestVacationLoadErrors(t *testing.T) {
	for name, script := range map[string]string{
		"missing require":         `vacation "x";`,
		"seconds without require": `require "vacation"; vacation :seconds 60 "x";`,
		"days and seconds":        `require "vacation-seconds"; vacation :days 1 :seconds 60 "x";`,
		"invalid from":            `require "vacation"; vacation :from "not an address <" "x";`,
		"missing reason":          `require "vacation"; vacation :days 1;`,
		"reason list":             `require "vacation"; vacation ["a", "b"];`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(script), DefaultOptions()); err == nil {
				t.Fatal("expected a load error")
			}
		})
	}
}

func TestVacationSaveRestore(t *testing.T) {
	testExecute(t, `require "vacation";
vacation :days 2 :handle "h" "away";
`, eml, []interp.AppliedAction{
		interp.ActionVacation{Reason: "away", Handle: "h", Period: 48 * time.Hour},
		interp.ActionKeep{Implicit: true},
	})
}

func runVacation(t *testing.T, script string) []interp.AppliedAction {
	t.Helper()
	loaded, err := Load(strings.NewReader(script), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data := interp.NewRuntimeData(loaded, interp.DummyPolicy{}, interp.EnvelopeStatic{}, interp.MessageStatic{})
	if err := loaded.Execute(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	return data.AppliedActions
}
