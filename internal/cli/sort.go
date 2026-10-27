package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

// A sortKey is a --sort option: a path like ".data.id" to the array to
// sort and, after it, to the value to sort its elements by.
type sortKey struct {
	spec string // as written, for messages
	path []step
	desc bool
}

// A step is a step of a sort path: an object key, an array index, or "[]"
// for the elements of an array.
type step struct {
	key   string
	index int
	kind  stepKind
}

type stepKind int

const (
	keyStep stepKind = iota
	indexStep
	eachStep
)

// parseSortKey parses the value of a --sort option: a path such as ".id",
// ".data.id", ".data[].id", "."owned by"" or ".items[0].size", where the
// leading dot may be left out, after a "-" to sort in reverse.
func parseSortKey(spec string) (sortKey, error) {
	k := sortKey{spec: spec}
	s := spec
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		k.desc, s = true, rest
	}
	switch {
	case s == "":
		return k, errors.New("empty path")
	case s[0] == '-':
		return k, errors.New("one - is enough to sort in reverse")
	}
	if s[0] != '.' && s[0] != '[' {
		s = "." + s
	}
	if s == "." {
		return k, nil // the elements themselves
	}
	for s != "" {
		var st step
		var err error
		switch {
		case strings.HasPrefix(s, ".["):
			s = s[1:]
			continue
		case s[0] == '[':
			st, s, err = parseBracket(s)
		case strings.HasPrefix(s, `."`):
			st.key, s, err = parseQuoted(s[1:])
		case s[0] == '.':
			end := strings.IndexAny(s[1:], ".[")
			if end < 0 {
				end = len(s) - 1
			}
			st.key, s = s[1:1+end], s[1+end:]
			if st.key == "" {
				err = errors.New("empty key")
			}
		default:
			err = fmt.Errorf("unexpected %q", s)
		}
		if err != nil {
			return k, err
		}
		if st.kind == eachStep && slices.ContainsFunc(k.path, func(p step) bool { return p.kind == eachStep }) {
			return k, errors.New("more than one []")
		}
		k.path = append(k.path, st)
	}
	return k, nil
}

// parseBracket parses a step in brackets at the start of s: "[]", an
// index like "[0]" or a key like `["a b"]`.
func parseBracket(s string) (step, string, error) {
	switch {
	case strings.HasPrefix(s, "[]"):
		return step{kind: eachStep}, s[2:], nil
	case strings.HasPrefix(s, `["`):
		key, rest, err := parseQuoted(s[1:])
		if err == nil && !strings.HasPrefix(rest, "]") {
			err = errors.New("missing ]")
		}
		return step{key: key}, strings.TrimPrefix(rest, "]"), err
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return step{}, "", errors.New("missing ]")
	}
	n, err := strconv.Atoi(s[1:end])
	if err != nil || n < 0 {
		return step{}, "", fmt.Errorf("bad index %s", s[:end+1])
	}
	return step{index: n, kind: indexStep}, s[end+1:], nil
}

// parseQuoted parses the JSON string at the start of s.
func parseQuoted(s string) (string, string, error) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			var key string
			if err := json.Unmarshal([]byte(s[:i+1]), &key); err != nil {
				return "", "", fmt.Errorf("bad key %s", s[:i+1])
			}
			return key, s[i+1:], nil
		}
	}
	return "", "", errors.New(`missing closing "`)
}

// sortBy sorts arrays in v in place: the array k leads to, by the values
// the rest of k leads to in its elements, or if k leads to no such array,
// every array whose elements have the values k leads to, by those.
func sortBy(v any, k sortKey) error {
	arr, at, rest, err := k.target(v)
	if err == nil {
		if have(arr, rest) {
			sortArray(arr, rest, k.desc)
			return nil
		}
		where := pathString(at)
		if len(at) == 0 {
			where = "the output"
		}
		err = fmt.Errorf("no element of %s has %s", where, pathString(rest))
	}
	if len(k.path) == 0 || k.explicit() {
		return err
	}
	sorted := false
	eachArray(v, func(arr []any) {
		if have(arr, k.path) {
			sortArray(arr, k.path, k.desc)
			sorted = true
		}
	})
	switch {
	case sorted:
		return nil
	case arr == nil:
		return fmt.Errorf("%v, and no array in the output has elements with %s", err, pathString(k.path))
	}
	return err
}

// explicit reports whether k marks the array to sort with "[]".
func (k sortKey) explicit() bool {
	return slices.ContainsFunc(k.path, func(s step) bool { return s.kind == eachStep })
}

// have reports whether some element of arr has a value at path, or arr is
// empty.
func have(arr []any, path []step) bool {
	return len(arr) == 0 || slices.ContainsFunc(arr, func(e any) bool {
		_, ok := lookupPath(e, path)
		return ok
	})
}

// clone returns a copy of v that shares no arrays or objects with it, so
// that sorting an array in it sorts no other: the results of a filter like
// "{top: .a[:2], all: .a}" share their elements' storage.
func clone(v any) any {
	switch x := v.(type) {
	case []any:
		c := make([]any, len(x))
		for i, e := range x {
			c[i] = clone(e)
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(x))
		for k, e := range x {
			c[k] = clone(e)
		}
		return c
	}
	return v
}

// eachArray calls f for each array in v, outer ones first.
func eachArray(v any, f func([]any)) {
	switch x := v.(type) {
	case []any:
		f(x)
		for _, e := range x {
			eachArray(e, f)
		}
	case map[string]any:
		for _, e := range x {
			eachArray(e, f)
		}
	}
}

// sortArray sorts arr by the values path leads to in its elements, in
// reverse if desc is set. Elements without one, or with null, go last.
func sortArray(arr []any, path []step, desc bool) {
	keys := make([]any, len(arr))
	for i, e := range arr {
		keys[i], _ = lookupPath(e, path)
	}
	order := make([]int, len(arr))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		a, b := keys[i], keys[j]
		switch {
		case a == nil || b == nil: // nulls and missing values go last
			return cmp.Compare(isNil(a), isNil(b))
		case desc:
			return compareValues(b, a)
		}
		return compareValues(a, b)
	})
	sorted := make([]any, len(arr))
	for i, j := range order {
		sorted[i] = arr[j]
	}
	copy(arr, sorted)
}

func isNil(v any) int {
	if v == nil {
		return 1
	}
	return 0
}

// target returns the array to sort in v, the path to it, and the path on
// to the values to sort it by: the array at "[]" in k, or else the first
// one k meets, or the one it ends at. An index after an array is taken to
// be in the path to sort by, as in ".rows[0]" for rows of arrays, if the
// elements have what the path leads to, and else to pick the element.
func (k sortKey) target(v any) (arr []any, at, rest []step, err error) {
	explicit := k.explicit()
	for i, s := range k.path {
		if arr, ok := v.([]any); ok {
			switch {
			case s.kind == eachStep:
				return arr, k.path[:i], k.path[i+1:], nil
			case explicit:
			case s.kind != indexStep || have(arr, k.path[i:]):
				return arr, k.path[:i], k.path[i:], nil
			}
		}
		next, ok := stepInto(v, s)
		switch {
		case ok:
			v = next
		case s.kind == eachStep:
			return nil, nil, nil, fmt.Errorf("%s is %s, not an array", pathString(k.path[:i]), kindOf(v))
		default:
			return nil, nil, nil, fmt.Errorf("the output has no %s", pathString(k.path[:i+1]))
		}
	}
	if arr, ok := v.([]any); ok {
		return arr, k.path, nil, nil
	}
	return nil, nil, nil, fmt.Errorf("%s is %s, not an array", pathString(k.path), kindOf(v))
}

// kindOf names the type of v for messages, as in "an object".
func kindOf(v any) string {
	switch typ := gojq.TypeOf(v); typ {
	case "null":
		return typ
	case "object", "array":
		return "an " + typ
	default:
		return "a " + typ
	}
}

// stepInto takes step s from v.
func stepInto(v any, s step) (any, bool) {
	switch s.kind {
	case keyStep:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[s.key]
		return v, ok
	case indexStep:
		a, ok := v.([]any)
		if !ok || s.index >= len(a) {
			return nil, false
		}
		return a[s.index], true
	}
	return nil, false
}

// lookupPath returns the value at path in v.
func lookupPath(v any, path []step) (any, bool) {
	for _, s := range path {
		var ok bool
		if v, ok = stepInto(v, s); !ok {
			return nil, false
		}
	}
	return v, true
}

// pathString writes path the way jq would.
func pathString(path []step) string {
	if len(path) == 0 {
		return "."
	}
	var b strings.Builder
	for _, s := range path {
		switch {
		case s.kind == eachStep:
			b.WriteString("[]")
		case s.kind == indexStep:
			fmt.Fprintf(&b, "[%d]", s.index)
		case identifier.MatchString(s.key):
			b.WriteString(".")
			b.WriteString(s.key)
		default:
			q, _ := json.Marshal(s.key)
			b.WriteString(".")
			b.WriteString(string(q))
		}
	}
	return b.String()
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// decimal matches strings that are numbers, like "42", "-1.5" or "2e10".
var decimal = regexp.MustCompile(`^[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?$`)

// compareValues orders values to sort by: numbers by value, counting
// strings that are numbers as numbers; other strings in natural order, with
// runs of digits compared as numbers, so that "v2" comes before "v10", and
// case ignored but for ties; and anything else the way jq orders values.
func compareValues(a, b any) int {
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			return gojq.Compare(x, y) // exact, even for big integers
		}
	}
	if x, ok := a.(string); ok {
		if y, ok := b.(string); ok {
			return natural(x, y)
		}
	}
	return gojq.Compare(a, b)
}

// number returns v as a number for gojq to compare, if it is a number or a
// string of one.
func number(v any) (any, bool) {
	switch x := v.(type) {
	case int, float64, *big.Int, json.Number:
		return v, true
	case string:
		if s := strings.TrimSpace(x); decimal.MatchString(s) {
			return json.Number(strings.TrimPrefix(s, "+")), true
		}
	}
	return nil, false
}

// natural compares strings in natural order.
func natural(a, b string) int {
	if c := naturalFold(a, b, true); c != 0 {
		return c
	}
	if c := naturalFold(a, b, false); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// naturalFold compares a and b with runs of digits taken as numbers, and
// letters in either case taken as the same if fold is set.
func naturalFold(a, b string, fold bool) int {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			da, db := digitRun(a), digitRun(b)
			na, nb := strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")
			if c := cmp.Compare(len(na), len(nb)); c != 0 {
				return c
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if fold {
			ra, rb = unicode.ToLower(ra), unicode.ToLower(rb)
		}
		if ra != rb {
			return cmp.Compare(ra, rb)
		}
		a, b = a[na:], b[nb:]
	}
	return cmp.Compare(len(a), len(b))
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func digitRun(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i]
}
