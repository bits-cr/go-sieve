package interp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// MatcherTest contains code shared between tests
// such as 'header', 'address', 'envelope', 'string' -
// all tests that compare some values from message
// with pre-defined "key"
//
// It is exported so that encoding/gob includes its fields when a script is
// saved (Script.Save): gob skips embedded fields of unexported types, which
// made restored tests lose their comparator, match type and keys.
// keyCompiled is not saved; restored tests compile keys when they run.
type MatcherTest struct {
	Comparator Comparator
	Match      Match
	Relational Relational
	Key        []string

	// Used for keys without variables.
	keyCompiled []CompiledMatcher

	matchCnt int
}

func newMatcherTest() MatcherTest {
	return MatcherTest{
		Comparator: "",
		Match:      MatchIs,
	}
}

func (t *MatcherTest) addSpecTags(s *Spec) *Spec {
	if s.Tags == nil {
		s.Tags = make(map[string]SpecTag, 4)
	}
	s.Tags["comparator"] = SpecTag{
		NeedsValue:  true,
		MinStrCount: 1,
		MaxStrCount: 1,
		MatchStr: func(val []string) {
			t.Comparator = Comparator(val[0])
		},
		NoVariables: true,
	}
	s.Tags["is"] = SpecTag{
		MatchBool: func() {
			t.Match = MatchIs
			t.matchCnt++
		},
	}
	s.Tags["contains"] = SpecTag{
		MatchBool: func() {
			t.Match = MatchContains
			t.matchCnt++
		},
	}
	s.Tags["matches"] = SpecTag{
		MatchBool: func() {
			t.Match = MatchMatches
			t.matchCnt++
		},
	}
	// regex extension. Accepted here so every test with a match-type takes it;
	// setKey rejects it unless "regex" is required.
	s.Tags["regex"] = SpecTag{
		MatchBool: func() {
			t.Match = MatchRegex
			t.matchCnt++
		},
	}
	s.Tags["value"] = SpecTag{
		NeedsValue:  true,
		MinStrCount: 1,
		MaxStrCount: 1,
		NoVariables: true,
		MatchStr: func(val []string) {
			t.Match = MatchValue
			t.matchCnt++
			t.Relational = Relational(val[0])
		},
	}
	s.Tags["count"] = SpecTag{
		NeedsValue:  true,
		MinStrCount: 1,
		MaxStrCount: 1,
		NoVariables: true,
		MatchStr: func(val []string) {
			t.Match = MatchCount
			t.matchCnt++
			t.Relational = Relational(val[0])
		},
	}
	return s
}

func (t *MatcherTest) setKey(s *Script, k []string) error {
	t.Key = k

	if t.matchCnt > 1 {
		return fmt.Errorf("multiple match-types are not allowed")
	}

	if t.Match == MatchCount || t.Match == MatchValue {
		if !s.RequiresExtension("relational") {
			return fmt.Errorf("missing require 'relational'")
		}
		switch t.Relational {
		case RelGreaterThan, RelGreaterOrEqual,
			RelLessThan, RelLessOrEqual, RelEqual,
			RelNotEqual:
		default:
			return fmt.Errorf("unknown relational operator: %v", t.Relational)
		}
	}

	if t.Comparator == "" {
		if t.Match == MatchCount {
			t.Comparator = ComparatorASCIINumeric
		} else {
			t.Comparator = DefaultComparator
		}
	}

	caseFold := false
	octet := false
	switch t.Comparator {
	case ComparatorOctet, ComparatorUnicodeCaseMap, ComparatorASCIICaseMap:
		octet = t.Comparator.IsOctet()
		caseFold = t.Comparator.IsCaseMap()
	case ComparatorASCIINumeric:
	case "":
	default:
		return fmt.Errorf("unsupported comparator: %v", t.Comparator)
	}

	if t.Match == MatchRegex {
		if !s.RequiresExtension("regex") {
			return fmt.Errorf("missing require 'regex'")
		}
		if t.Comparator == ComparatorASCIINumeric {
			return fmt.Errorf("comparator %v cannot be used with :regex", t.Comparator)
		}
		t.keyCompiled = make([]CompiledMatcher, len(t.Key))
		for i := range t.Key {
			if len(usedVars(s, t.Key[i])) > 0 {
				continue
			}

			var err error
			t.keyCompiled[i], err = compileMatcherPOSIX(t.Key[i], octet, caseFold)
			if err != nil {
				return fmt.Errorf("malformed regular expression (%v): %v", t.Key[i], err)
			}
		}
	}

	if t.Match == MatchMatches {
		t.keyCompiled = make([]CompiledMatcher, len(t.Key))
		for i := range t.Key {
			if len(usedVars(s, t.Key[i])) > 0 {
				continue
			}

			var err error
			t.keyCompiled[i], err = compileMatcher(t.Key[i], octet, caseFold)
			if err != nil {
				return fmt.Errorf("malformed pattern (%v): %v", t.Key[i], err)
			}
		}
	}

	if t.Match == MatchCount && t.Comparator != ComparatorASCIINumeric {
		return fmt.Errorf("non-numeric comparators cannot be used with :count")
	}

	return nil
}

func (t *MatcherTest) isCount() bool {
	return t.Match == MatchCount
}

func (t *MatcherTest) countMatches(d *RuntimeData, value uint64) bool {
	if !t.isCount() {
		panic("countMatches can be called only with MatchCount matcher")
	}

	for _, k := range t.Key {
		kNum, err := strconv.ParseUint(expandVars(d, k), 10, 64)
		if err != nil {
			continue
		}

		if t.Relational.CompareUint64(value, kNum) {
			return true
		}
	}

	return false
}

func (t *MatcherTest) tryMatchBodyPart(ctx context.Context, d *RuntimeData, part BodyPart, stripHTML bool) (bool, error) {
	for i, key := range t.Key {
		partReader, err := part.Open(ctx)
		if err != nil {
			if errors.Is(err, ErrNoBody) {
				return false, nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, fmt.Errorf("open part: %w", err)
		}

		r := io.Reader(partReader)
		if stripHTML {
			br, ok := r.(io.ByteReader)
			if !ok {
				br = bufio.NewReader(partReader)
			}
			r = &htmlStripper{BR: br}
		}

		var ok bool
		if t.keyCompiled != nil && t.keyCompiled[i].IsLoaded() {
			ok, err = t.keyCompiled[i].MatchReader(r)
		} else {
			key = expandVars(d, key)
			ok, err = testReader(t.Comparator, t.Match, r, expandVars(d, key))
		}
		if err != nil {
			_ = partReader.Close()
			return false, err
		}
		if ok {
			_ = partReader.Close()
			return true, nil
		}
		_ = partReader.Close()
	}
	return false, nil
}

func (t *MatcherTest) tryMatch(d *RuntimeData, source string) (bool, error) {
	for i, key := range t.Key {
		var (
			ok      bool
			matches []string
			err     error
		)
		if t.keyCompiled != nil && t.keyCompiled[i].IsLoaded() {
			ok, matches, err = t.keyCompiled[i].Match(source)
		} else {
			key = expandVars(d, key)
			ok, matches, err = testString(t.Comparator, t.Match, t.Relational, source, expandVars(d, key))
		}
		if err != nil {
			return false, err
		}
		if ok {
			if t.Match == MatchMatches || t.Match == MatchRegex {
				d.MatchVariables = matches
			}
			return true, nil
		}
	}
	return false, nil
}
