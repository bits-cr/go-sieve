package interp

import (
	"fmt"
	"strings"

	"github.com/foxcpp/go-sieve/parser"
)

func loadSet(script *Script, pcmd parser.Cmd) (Cmd, error) {
	if !script.RequiresExtension("variables") {
		return nil, parser.ErrorAt(pcmd.Position, "missing require 'variables'")
	}
	cmd := CmdSet{}

	// Modifier name by precedence (RFC 5229, section 4.1).
	var modifiers = map[int]string{}
	var conflictingMods bool
	setModifier := func(prec int, name string) func() {
		return func() {
			if modifiers[prec] != "" {
				conflictingMods = true
			}
			modifiers[prec] = name
		}
	}

	err := LoadSpec(script, &Spec{
		Tags: map[string]SpecTag{
			"length":        {MatchBool: setModifier(10, "length")},
			"quotewildcard": {MatchBool: setModifier(20, "quotewildcard")},
			"upper":         {MatchBool: setModifier(40, "upper")},
			"lower":         {MatchBool: setModifier(40, "lower")},
			"upperfirst":    {MatchBool: setModifier(30, "upperfirst")},
			"lowerfirst":    {MatchBool: setModifier(30, "lowerfirst")},
		},
		Pos: []SpecPosArg{
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Name = strings.ToLower(val[0])
				},
			},
			{
				MinStrCount: 1,
				MaxStrCount: 1,
				MatchStr: func(val []string) {
					cmd.Value = val[0]
				},
			},
		},
	}, pcmd.Position, pcmd.Args, pcmd.Tests, pcmd.Block)

	if conflictingMods {
		return nil, parser.ErrorAt(pcmd.Position, "conflicting value modifiers")
	}

	settable, _ := script.IsVarUsable(cmd.Name)
	if !settable {
		return nil, parser.ErrorAt(pcmd.Position, "cannot set this variable")
	}

	// Applied from the highest precedence to the lowest.
	for _, prec := range [4]int{40, 30, 20, 10} {
		if name := modifiers[prec]; name != "" {
			cmd.Modifiers = append(cmd.Modifiers, name)
		}
	}

	return cmd, err
}

func loadStringTest(s *Script, test parser.Test) (Test, error) {
	if !s.RequiresExtension("variables") {
		return nil, fmt.Errorf("missing require 'variables'")
	}

	loaded := TestString{MatcherTest: newMatcherTest()}
	var key []string
	err := LoadSpec(s, loaded.addSpecTags(&Spec{
		Pos: []SpecPosArg{
			{
				MatchStr: func(val []string) {
					loaded.Source = val
				},
				MinStrCount: 1,
			},
			{
				MatchStr: func(val []string) {
					key = val
				},
				MinStrCount: 1,
			},
		},
	}), test.Position, test.Args, test.Tests, nil)
	if err != nil {
		return nil, err
	}

	if err := loaded.setKey(s, key); err != nil {
		return nil, err
	}

	return loaded, nil
}
