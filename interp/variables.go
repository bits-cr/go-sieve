package interp

import (
	"context"
	"encoding/gob"
	"regexp"
	"strconv"
	"strings"
)

/*
variable-ref        =  "${" [namespace] variable-name "}"
namespace           =  identifier "." *sub-namespace
sub-namespace       =  variable-name "."
variable-name       =  num-variable / identifier
num-variable        =  1*DIGIT
*/
var variableRegexp = regexp.MustCompile(`\${(?:[a-zA-Z_][a-zA-Z0-9_]*\.(?:(?:[a-zA-Z_][a-zA-Z0-9_]*|[0-9]+)\.)*)?(?:[a-zA-Z_][a-zA-Z0-9_]*|[0-9]+)}`)

func usedVars(script *Script, s string) []string {
	if !script.RequiresExtension("variables") {
		return nil
	}

	variables := variableRegexp.FindAllString(s, -1)
	for i := range variables {
		// Cut ${} and case-fold.
		variables[i] = strings.ToLower(variables[i][2 : len(variables[i])-1])
	}

	return variables
}

func usedVarsAreValid(script *Script, s string) bool {
	for _, v := range usedVars(script, s) {
		matchNum, err := strconv.Atoi(v)
		if err == nil && matchNum >= 0 {
			continue
		}

		_, gettable := script.IsVarUsable(v)
		if !gettable {
			return false
		}
	}
	return true
}

func expandVarsList(d *RuntimeData, list []string) []string {
	if !d.Script.RequiresExtension("variables") {
		return list
	}

	listCpy := make([]string, len(list))
	for i, val := range list {
		listCpy[i] = expandVars(d, val)
	}
	return listCpy
}

func expandVars(d *RuntimeData, s string) string {
	if !d.Script.RequiresExtension("variables") {
		return s
	}

	expanded := variableRegexp.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]

		if matchNum, err := strconv.Atoi(name); err == nil && matchNum >= 0 {
			return d.MatchVariable(matchNum)
		}

		value, err := d.Var(name)
		if err != nil {
			panic("attempt to use an unusable variable: " + name)
		}
		return value
	})
	return expanded
}

type CmdSet struct {
	Name  string
	Value string

	// Modifiers are the value modifiers (RFC 5229, section 4.1) in the order
	// they apply: "upper"/"lower", "upperfirst"/"lowerfirst",
	// "quotewildcard", "length". Unlike ModifyValue they survive Script.Save.
	Modifiers []string

	// ModifyValue, if set, replaces Modifiers. It is kept for API
	// compatibility; encoding/gob cannot save a function, so the loader no
	// longer sets it.
	ModifyValue func(string) string
}

func (c CmdSet) Execute(_ context.Context, d *RuntimeData) error {
	value := expandVars(d, c.Value)
	if c.ModifyValue != nil {
		value = c.ModifyValue(value)
	} else {
		value = applySetModifiers(c.Modifiers, value, d.Script.opts.MaxVariableLen)
	}
	return d.SetVar(c.Name, value)
}

func applySetModifiers(modifiers []string, s string, maxVariableLen int) string {
	for _, m := range modifiers {
		switch m {
		case "upper":
			s = strings.ToUpper(s)
		case "lower":
			s = strings.ToLower(s)
		case "upperfirst":
			if len(s) != 0 && s[0] >= 'a' && s[0] <= 'z' {
				s = string(s[0]-('a'-'A')) + s[1:]
			}
		case "lowerfirst":
			if len(s) != 0 && s[0] >= 'A' && s[0] <= 'Z' {
				s = string(s[0]+('a'-'A')) + s[1:]
			}
		case "quotewildcard":
			escaped := strings.Builder{}
			escaped.Grow(len(s))
			for _, chr := range s {
				switch chr {
				case '\\', '*', '?':
					escaped.WriteByte('\\')
				}
				escaped.WriteRune(chr)
			}
			s = escaped.String()
		case "length":
			// RFC mentions `characters' and not octets
			s = strconv.Itoa(len([]rune(s)))
		}
	}

	// If the last modifier was quotewildcard, check whether the value would
	// remain valid if truncated to MaxVariableLen. If not, truncate here and
	// drop a dangling backslash.
	if len(modifiers) != 0 && modifiers[len(modifiers)-1] == "quotewildcard" && len(s) > maxVariableLen {
		until := maxVariableLen

		// (Copy-pasted from RuntimeData.SetVar)
		// If this truncated an otherwise valid Unicode character,
		// remove the character altogether.
		for until > 0 && s[until] >= 128 && s[until] < 192 /* second or further octet of UTF-8 encoding */ {
			until--
		}

		if until > 0 && s[until-1] == '\\' {
			until--
		}

		s = s[:until]
	}

	return s
}

type TestString struct {
	MatcherTest

	Source []string
}

func (t TestString) Check(_ context.Context, d *RuntimeData) (bool, error) {
	entryCount := uint64(0)
	for _, source := range t.Source {
		source = expandVars(d, source)

		if t.isCount() {
			if source != "" {
				entryCount++
			}
			continue
		}

		ok, err := t.MatcherTest.tryMatch(d, source)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	if t.isCount() {
		return t.countMatches(d, entryCount), nil
	}

	return false, nil
}

func init() {
	gob.Register(CmdSet{})
	gob.Register(TestString{})
}
