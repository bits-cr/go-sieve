package interp

import (
	"fmt"

	"github.com/foxcpp/go-sieve/parser"
)

// indexArgs collects the :index and :last arguments of the index extension
// (RFC 5260, section 6) while a test is loaded.
type indexArgs struct {
	index int
	set   bool
	last  bool
}

// addIndexTags adds :index and :last to spec if the script requires "index".
func addIndexTags(s *Script, spec *Spec, a *indexArgs) {
	if !s.RequiresExtension("index") {
		return
	}
	spec.Tags["index"] = SpecTag{
		NeedsValue: true,
		MatchNum: func(val int) {
			a.index = val
			a.set = true
		},
	}
	spec.Tags["last"] = SpecTag{
		MatchBool: func() {
			a.last = true
		},
	}
}

func (a indexArgs) validate() error {
	if a.last && !a.set {
		return fmt.Errorf(":last requires :index")
	}
	if a.set && a.index < 1 {
		return fmt.Errorf(":index must be 1 or more, got %d", a.index)
	}
	return nil
}

func loadDateTest(s *Script, test parser.Test) (Test, error) {
	if !s.RequiresExtension("date") {
		return nil, fmt.Errorf("missing require 'date'")
	}

	loaded := DateTest{MatcherTest: newMatcherTest()}
	var (
		key     []string
		zoneSet bool
		index   indexArgs
	)
	spec := loaded.addSpecTags(&Spec{
		Tags: map[string]SpecTag{
			"zone": {
				NeedsValue:  true,
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					loaded.Zone = val[0]
					zoneSet = true
				},
			},
			"originalzone": {
				MatchBool: func() {
					loaded.OriginalZone = true
				},
			},
		},
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					loaded.Header = val[0]
				},
			},
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					loaded.DatePart = val[0]
				},
			},
			{
				MinStrCount: 1,
				MatchStr: func(val []string) {
					key = val
				},
			},
		},
	})
	addIndexTags(s, spec, &index)

	if err := LoadSpec(s, spec, test.Position, test.Args, test.Tests, nil); err != nil {
		return nil, err
	}

	if zoneSet && loaded.OriginalZone {
		return nil, fmt.Errorf("date: :zone and :originalzone cannot be used together")
	}
	if err := validateDateArgs(s, loaded.Zone, loaded.DatePart); err != nil {
		return nil, err
	}
	if err := index.validate(); err != nil {
		return nil, err
	}
	loaded.Index = index.index
	loaded.Last = index.last

	if err := loaded.setKey(s, key); err != nil {
		return nil, err
	}
	return loaded, nil
}

func loadCurrentDateTest(s *Script, test parser.Test) (Test, error) {
	if !s.RequiresExtension("date") {
		return nil, fmt.Errorf("missing require 'date'")
	}

	loaded := CurrentDateTest{MatcherTest: newMatcherTest()}
	var key []string
	spec := loaded.addSpecTags(&Spec{
		Tags: map[string]SpecTag{
			"zone": {
				NeedsValue:  true,
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					loaded.Zone = val[0]
				},
			},
		},
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					loaded.DatePart = val[0]
				},
			},
			{
				MinStrCount: 1,
				MatchStr: func(val []string) {
					key = val
				},
			},
		},
	})

	if err := LoadSpec(s, spec, test.Position, test.Args, test.Tests, nil); err != nil {
		return nil, err
	}
	if err := validateDateArgs(s, loaded.Zone, loaded.DatePart); err != nil {
		return nil, err
	}
	if err := loaded.setKey(s, key); err != nil {
		return nil, err
	}
	return loaded, nil
}

// validateDateArgs checks a constant :zone and date-part at load time. Values
// that contain variables are checked when the test runs, where an invalid
// value makes the test fail.
func validateDateArgs(s *Script, zone, datePart string) error {
	if zone != "" && len(usedVars(s, zone)) == 0 {
		if _, err := parseZone(zone); err != nil {
			return err
		}
	}
	if len(usedVars(s, datePart)) == 0 && !validDatePart(datePart) {
		return fmt.Errorf("unknown date-part: %v", datePart)
	}
	return nil
}
