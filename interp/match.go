package interp

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"io"
	"regexp"
	"regexp/syntax"
	"strings"

	"rsc.io/binaryregexp"
)

func foldASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

func patternToRegex(pattern string, caseFold bool) string {
	result := strings.Builder{}
	if caseFold {
		result.WriteString(`(?i)`)
	}
	result.WriteRune('^')
	escaped := false
	for _, chr := range pattern {
		if !escaped {
			switch chr {
			case '\\':
				escaped = true
			case '?':
				result.WriteString(`(.)`)
			case '*':
				result.WriteString(`((?s:.*?))`)
			case '.', '+', '(', ')', '|', '[', ']', '{', '}', '^', '$':
				result.WriteRune('\\')
				fallthrough
			default:
				result.WriteRune(chr)
			}
		} else {
			switch chr {
			case '\\', '?', '*', '.', '+', '(', ')', '|', '[', ']', '{', '}', '^', '$':
				result.WriteRune('\\')
				fallthrough
			default:
				result.WriteRune(chr)
			}

			escaped = false
		}
	}

	// Such regex won't compile.
	if escaped {
		return result.String()
	}

	result.WriteRune('$')

	return result.String()
}

type compiledMatcherData struct {
	Regexp string
	Octet  bool
	// Longest selects leftmost-longest (POSIX) match semantics instead of
	// the default leftmost-first. Set for :regex match type.
	Longest bool
}

type CompiledMatcher struct {
	compiledMatcherData
	loaded bool
	binary *binaryregexp.Regexp
	string *regexp.Regexp
}

func (cm *CompiledMatcher) IsLoaded() bool {
	return cm.loaded
}

func (cm *CompiledMatcher) GobDecode(i []byte) error {
	err := gob.NewDecoder(bytes.NewBuffer(i)).Decode(&cm.compiledMatcherData)
	if err != nil {
		return err
	}

	return cm.restore()
}

func (cm *CompiledMatcher) restore() error {
	var err error
	if cm.Octet {
		cm.binary, err = binaryregexp.Compile(cm.Regexp)
		if err == nil && cm.Longest {
			cm.binary.Longest()
		}
	} else {
		cm.string, err = regexp.Compile(cm.Regexp)
		if err == nil && cm.Longest {
			cm.string.Longest()
		}
	}
	if err != nil {
		return err
	}

	cm.loaded = true

	return nil
}

func (cm *CompiledMatcher) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(cm.compiledMatcherData)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (cm *CompiledMatcher) Match(value string) (bool, []string, error) {
	if cm.binary != nil {
		matches := cm.binary.FindStringSubmatch(value)
		return len(matches) != 0, matches, nil
	}

	matches := cm.string.FindStringSubmatch(value)
	return len(matches) != 0, matches, nil
}

func (cm *CompiledMatcher) MatchReader(r io.Reader) (bool, error) {
	if cm.binary != nil {
		br, ok := r.(io.ByteReader)
		if !ok {
			br = bufio.NewReader(r)
		}

		return cm.binary.MatchReader(br), nil
	}

	rr, ok := r.(io.RuneReader)
	if !ok {
		rr = bufio.NewReader(r)
	}

	return cm.string.MatchReader(rr), nil
}

// compileMatcher returns a function that will check whether pre-defined pattern matches the passed
// value. It is preferable to use compileMatcher over matchOctet, matchUnicode if
// pattern does not change often (e.g. does not depend on any variables).
func compileMatcher(pattern string, octet bool, caseFold bool) (CompiledMatcher, error) {
	res := CompiledMatcher{}
	res.Regexp = patternToRegex(pattern, caseFold)
	res.Octet = octet

	if err := res.restore(); err != nil {
		return CompiledMatcher{}, err
	}
	return res, nil
}

// comppileMatcherRegex is compileMatcher that accepts regular expression as input
// instead of Sieve patterns.
func compileMatcherRegex(regex string, octet bool) (CompiledMatcher, error) {
	res := CompiledMatcher{}
	res.Regexp = regex
	res.Octet = octet

	if err := res.restore(); err != nil {
		return CompiledMatcher{}, err
	}
	return res, nil
}

// posixRegexFlags are the regexp/syntax flags matching POSIX regcomp with
// REG_EXTENDED and without REG_NEWLINE, as used by the regex extension
// (draft-ietf-sieve-regex-01): extended syntax, '^' and '$' anchor only at
// the ends of the value and '.' also matches a newline.
const posixRegexFlags = syntax.POSIX | syntax.OneLine | syntax.DotNL

// translatePOSIXRegex validates regex as a POSIX extended regular expression
// and returns an equivalent expression in the syntax accepted by
// regexp.Compile, so that it can be stored and restored like any other
// compiled matcher.
func translatePOSIXRegex(regex string, caseFold bool) (string, error) {
	flags := posixRegexFlags
	if caseFold {
		flags |= syntax.FoldCase
	}
	parsed, err := syntax.Parse(regex, flags)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// compileMatcherPOSIX compiles a POSIX extended regular expression (the
// :regex match type) with leftmost-longest semantics. The match is not
// anchored: the expression may match anywhere in the value.
func compileMatcherPOSIX(regex string, octet bool, caseFold bool) (CompiledMatcher, error) {
	translated, err := translatePOSIXRegex(regex, caseFold)
	if err != nil {
		return CompiledMatcher{}, err
	}

	res := CompiledMatcher{}
	res.Regexp = translated
	res.Octet = octet
	res.Longest = true

	if err := res.restore(); err != nil {
		return CompiledMatcher{}, err
	}
	return res, nil
}

func matchRegex(regex, value string, octet, caseFold bool) (bool, []string, error) {
	cm, err := compileMatcherPOSIX(regex, octet, caseFold)
	if err != nil {
		// An expression that is invalid only after variable expansion makes
		// the test fail instead of aborting the script, as Pigeonhole does
		// (tests/extensions/regex/errors/runtime.sieve).
		return false, nil, nil
	}
	return cm.Match(value)
}

func matchOctet(pattern, value string, caseFold bool) (bool, []string, error) {
	regex, err := binaryregexp.Compile(patternToRegex(pattern, caseFold))
	if err != nil {
		return false, nil, err
	}

	matches := regex.FindStringSubmatch(value)
	return len(matches) != 0, matches, nil
}

func matchUnicode(pattern, value string, caseFold bool) (bool, []string, error) {
	regex, err := regexp.Compile(patternToRegex(pattern, caseFold))
	if err != nil {
		return false, nil, err
	}

	matches := regex.FindStringSubmatch(value)
	return len(matches) != 0, matches, nil
}
