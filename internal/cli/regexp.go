package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dlclark/regexp2/v2"
	"github.com/itchyny/gojq"
)

const regexpTimeout = 250 * time.Millisecond

// regexpOption installs jq's regex functions with a bounded backtracking engine.
func regexpOption() gojq.CompilerOption {
	// gojq resolves internal functions before custom functions, and compiles
	// builtins in a separate scope. Rebind the regex builtins in an init module
	// so their calls reach the custom hook rather than gojq's internal _match.
	return combineRegexpOptions(
		gojq.WithFunction("_match", 3, 3, regexpMatch),
		gojq.WithFunction("_tq_match", 3, 3, regexpMatch),
		gojq.WithModuleLoader(regexpModule{}),
	)
}

// The compiler argument of CompilerOption is private to gojq. Type inference
// lets us compose its public options without accessing compiler internals.
func combineRegexpOptions[T any](options ...func(T)) func(T) {
	return func(c T) {
		for _, option := range options {
			option(c)
		}
	}
}

type regexpModule struct{}

func (regexpModule) LoadInitModules() ([]*gojq.Query, error) {
	// These are gojq v0.12.19's definitions, with the full arities first so
	// shorthand definitions bind to this module rather than builtin scope.
	q, err := gojq.Parse(`
def _match($re; $flags; $testing): _tq_match($re; $flags; $testing);
def match($re; $flags): _match($re; $flags; false)[];
def match($re): match($re; null);
def test($re; $flags): _match($re; $flags; true);
def test($re): test($re; null);
def capture($re; $flags): match($re; $flags) | .captures | _captures;
def capture($re): capture($re; null);
def scan($re; $flags):
  match($re; $flags + "g") |
  if .captures == [] then .string else [.captures[].string] end;
def scan($re): scan($re; null);
def splits($re; $flags):
  .[foreach (match($re; $flags + "g"), null) as {$offset, $length}
      (null; {start: .next, end: $offset, next: $offset + $length})];
def splits($re): splits($re; null);
def split($re; $flags): [splits($re; $flags)];
def sub($re; str; $flags):
  reduce match($re; $flags) as {$offset, $length, $captures}
    ({s: ., r: []};
      reduce ($captures | _captures | str) as $s
        (.i = 0; .r[.i] += .s[.next:$offset] + $s | .i += 1) |
      .next = $offset + $length) | .r[] + .s[.next:] // .s;
def sub($re; str): sub($re; str; null);
def gsub($re; str; $flags): sub($re; str; $flags + "g");
def gsub($re; str): sub($re; str; "g");
`)
	if err != nil {
		return nil, err
	}
	return []*gojq.Query{q}, nil
}

func regexpMatch(v any, args []any) any {
	name := "match"
	testing := args[2] == true
	if testing {
		name = "test"
	}
	s, inputOK := v.(string)
	pattern, patternOK := args[0].(string)
	flags, flagsOK := args[1].(string)
	if !inputOK || !patternOK || (args[1] != nil && !flagsOK) {
		return fmt.Errorf("%s requires string input, a string pattern, and string or null flags", name)
	}
	options := regexp2.None
	global, nonempty := false, false
	for _, flag := range flags {
		switch flag {
		case 'g':
			global = true
		case 'i':
			options |= regexp2.IgnoreCase
		case 'm', 'p':
			options |= regexp2.Singleline
		case 's':
			// jq's default anchors already apply to the whole string.
		case 'x':
			options |= regexp2.IgnorePatternWhitespace
		case 'n':
			nonempty = true
		case 'l':
			return fmt.Errorf("unsupported regular expression flag %q: longest matching is not supported", string(flag))
		default:
			return fmt.Errorf("unsupported regular expression flag: %q", flags)
		}
	}
	prepared, captureNames := prepareTQRegexp(pattern, options)
	r, err := compileTQRegexp(prepared, options)
	if err != nil {
		return err
	}
	var consuming *regexp2.Regexp
	if nonempty {
		// At a fixed search origin, \G at the end succeeds only for an empty
		// match. Reject it inside the engine so lazy quantifiers and alternate
		// branches can backtrack to a consuming match at the same position.
		consuming, err = compileTQRegexp(`\G(?:`+prepared+`)(?!\G)`, options)
		if err != nil {
			return err
		}
	}
	text := []rune(s)
	groups := r.GetGroupNumbers()[1:]
	results := make([]any, 0)
	deadline := time.Now().Add(regexpTimeout)
	for start := 0; start <= len(text); {
		if time.Now().After(deadline) {
			return fmt.Errorf("regular expression match exceeded %s", regexpTimeout)
		}
		m, err := r.FindRunesMatchStartingAt(text, start)
		if err != nil {
			return regexpMatchError(err)
		}
		if m == nil {
			break
		}
		if nonempty && m.RuneLength == 0 {
			emptyAt := m.RuneIndex
			m, err = consuming.FindRunesMatchStartingAt(text, m.RuneIndex)
			if err != nil {
				return regexpMatchError(err)
			}
			if m == nil {
				// The initial match was empty at this position. Retry the next
				// rune, including an empty match at the end of the input.
				start = emptyAt + 1
				continue
			}
		}
		if testing {
			return true
		}
		captures := make([]any, 0, m.GroupCount()-1)
		for _, number := range groups {
			group := m.GroupByNumber(number)
			var groupName any
			if n := r.GroupNameFromNumber(number); n != strconv.Itoa(number) {
				if original, ok := captureNames[n]; ok {
					groupName = original
				} else {
					groupName = n
				}
			}
			var value any
			offset, length := -1, 0
			if len(group.Captures) != 0 {
				offset, length, value = group.RuneIndex, group.RuneLength, group.String()
			}
			captures = append(captures, map[string]any{
				"name": groupName, "offset": offset, "length": length, "string": value,
			})
		}
		results = append(results, map[string]any{
			"offset": m.RuneIndex, "length": m.RuneLength,
			"string": m.String(), "captures": captures,
		})
		if !global {
			break
		}
		start = m.RuneIndex + m.RuneLength
		if m.RuneLength == 0 {
			start++
		}
	}
	if testing {
		return false
	}
	return results
}

type regexpToken struct {
	text, name string
	kind       byte
}

// prepareTQRegexp leaves the pattern safe to embed in a noncapturing group and
// gives duplicate named captures distinct slots without changing their order.
func prepareTQRegexp(pattern string, options regexp2.RegexOptions) (string, map[string]string) {
	tokens := tokenizeTQRegexp(pattern, options&regexp2.IgnorePatternWhitespace != 0)
	counts := make(map[string]int)
	for _, token := range tokens {
		if token.kind == 'c' {
			counts[token.name]++
		}
	}
	prefix := "tqCapture"
	for strings.Contains(pattern, prefix) {
		prefix += "X"
	}
	aliases := make(map[string][]string)
	names := make(map[string]string)
	for _, token := range tokens {
		if token.kind == 'c' && counts[token.name] > 1 {
			alias := prefix + strconv.Itoa(len(names))
			aliases[token.name] = append(aliases[token.name], alias)
			names[alias] = token.name
		}
	}
	var result strings.Builder
	next := make(map[string]int)
	for _, token := range tokens {
		groups := aliases[token.name]
		if len(groups) == 0 {
			if token.kind == 't' && counts[token.name] != 0 {
				result.WriteString("(?(" + token.name + ")")
			} else {
				result.WriteString(token.text)
			}
			continue
		}
		switch token.kind {
		case 'c':
			result.WriteString("(?<" + groups[next[token.name]] + ">")
			next[token.name]++
		case 'r':
			// Oniguruma tries the latest declaration first, including
			// backtracking to earlier declarations if its text does not match.
			result.WriteString("(?:")
			for i := len(groups) - 1; i >= 0; i-- {
				if i != len(groups)-1 {
					result.WriteByte('|')
				}
				result.WriteString(`\k<` + groups[i] + ">")
			}
			result.WriteByte(')')
		case 't':
			// A named condition is true when any declaration participated.
			result.WriteString("(?(?=(?:")
			for i, group := range groups {
				if i != 0 {
					result.WriteByte('|')
				}
				result.WriteString("(?(" + group + ")|(?!))")
			}
			result.WriteString("))")
		default:
			result.WriteString(token.text)
		}
	}
	return result.String(), names
}

// tokenizeTQRegexp tracks only the syntax needed for safe embedding and named
// capture rewriting. The regex engine still validates the complete pattern.
func tokenizeTQRegexp(pattern string, extended bool) []regexpToken {
	var tokens []regexpToken
	var scopes []bool
	plain := func(text string) { tokens = append(tokens, regexpToken{text: text}) }
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '#':
			if extended {
				end := strings.IndexByte(pattern[i:], '\n')
				if end < 0 {
					i = len(pattern)
				} else {
					i += end
				}
				continue
			}
		case '\\':
			if i+1 < len(pattern) {
				if pattern[i+1] == 'Q' {
					end := strings.Index(pattern[i+2:], `\E`)
					if end < 0 {
						plain(pattern[i:] + `\E`)
						i = len(pattern)
					} else {
						end += i + 4
						plain(pattern[i:end])
						i = end
					}
					continue
				}
				if pattern[i+1] == 'k' && i+2 < len(pattern) {
					if name, end, ok := tqRegexpName(pattern, i+2); ok {
						tokens = append(tokens, regexpToken{pattern[i:end], name, 'r'})
						i = end
						continue
					}
				}
				plain(pattern[i : i+2])
				i += 2
				continue
			}
		case '[':
			end := tqRegexpClassEnd(pattern, i)
			plain(pattern[i:end])
			i = end
			continue
		case '(':
			if strings.HasPrefix(pattern[i:], "(?#") {
				if end := strings.IndexByte(pattern[i+3:], ')'); end >= 0 {
					end += i + 4
					plain(pattern[i:end])
					i = end
					continue
				}
			}
			if changed, scoped, end, ok := tqRegexpInlineFlags(pattern, i, extended); ok {
				if scoped {
					scopes = append(scopes, extended)
				}
				extended = changed
				plain(pattern[i:end])
				i = end
				continue
			}
			scopes = append(scopes, extended)
			if strings.HasPrefix(pattern[i:], "(?(") {
				// Consume a reference condition's header, whose parentheses
				// do not introduce an additional option scope.
				start := i + 3
				if start < len(pattern) && pattern[start] != '?' {
					if end := strings.IndexByte(pattern[start:], ')'); end >= 0 {
						end += start
						name := ""
						if n, e, ok := tqRegexpName(pattern, start); ok && e == end {
							name = n
						}
						tokens = append(tokens, regexpToken{pattern[i : end+1], name, 't'})
						i = end + 1
						continue
					}
				}
			}
			start := i + 2
			python := strings.HasPrefix(pattern[i:], "(?P<")
			if python {
				start++
			}
			if strings.HasPrefix(pattern[i:], "(?") && start < len(pattern) {
				if name, end, ok := tqRegexpName(pattern, start); ok {
					text := pattern[i:end]
					if python {
						text = "(?<" + name + ">"
					}
					tokens = append(tokens, regexpToken{text, name, 'c'})
					i = end
					continue
				}
			}
		case ')':
			if len(scopes) != 0 {
				extended = scopes[len(scopes)-1]
				scopes = scopes[:len(scopes)-1]
			}
		}
		end := len(pattern)
		if next := strings.IndexAny(pattern[i+1:], "#\\[()"); next >= 0 {
			end = i + 1 + next
		}
		plain(pattern[i:end])
		i = end
	}
	return tokens
}

func tqRegexpName(pattern string, start int) (string, int, bool) {
	close := byte('>')
	if pattern[start] == '\'' {
		close = '\''
	} else if pattern[start] != '<' {
		return "", 0, false
	}
	if start+1 == len(pattern) || pattern[start+1] == '=' || pattern[start+1] == '!' {
		return "", 0, false
	}
	if end := strings.IndexByte(pattern[start+1:], close); end >= 0 {
		end += start + 1
		name := pattern[start+1 : end]
		if name == "" {
			return "", 0, false
		}
		for _, r := range name {
			if !unicode.In(r, unicode.L, unicode.Mn, unicode.Nd, unicode.Pc) {
				return "", 0, false
			}
		}
		return name, end + 1, true
	}
	return "", 0, false
}

func tqRegexpInlineFlags(pattern string, start int, extended bool) (bool, bool, int, bool) {
	if !strings.HasPrefix(pattern[start:], "(?") {
		return false, false, 0, false
	}
	enable, seen := true, false
	for i := start + 2; i < len(pattern); i++ {
		switch pattern[i] {
		case 'i', 'm', 'n', 's', 'x':
			seen = true
			if pattern[i] == 'x' {
				extended = enable
			}
		case '-':
			enable = false
		case ')', ':':
			return extended, pattern[i] == ':', i + 1, seen
		default:
			return false, false, 0, false
		}
	}
	return false, false, 0, false
}

func tqRegexpClassEnd(pattern string, start int) int {
	i := start + 1
	if i < len(pattern) && pattern[i] == '^' {
		i++
	}
	first := true
	for i < len(pattern) {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) && pattern[i+1] == 'Q' {
				if end := strings.Index(pattern[i+2:], `\E`); end >= 0 {
					i += end + 4
					if end > 0 {
						first = false
					}
					continue
				}
				return len(pattern)
			}
			i += 2
		case ']':
			if !first {
				return i + 1
			}
			i++
		case '[':
			if i+1 < len(pattern) && strings.ContainsRune(":.=", rune(pattern[i+1])) {
				// The closing bracket of a POSIX class belongs to the inner
				// construct, leaving the enclosing character class open.
				end := strings.Index(pattern[i+2:], string(pattern[i+1])+"]")
				if end < 0 {
					return len(pattern)
				}
				i += end + 4
			} else if i > start+1 && pattern[i-1] == '-' {
				i = tqRegexpClassEnd(pattern, i)
			} else {
				i++
			}
		default:
			i++
		}
		first = false
	}
	return len(pattern)
}

func compileTQRegexp(pattern string, options regexp2.RegexOptions) (*regexp2.Regexp, error) {
	r, err := regexp2.Compile(pattern, options, regexp2.OptionMaintainCaptureOrder())
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression %q: %w", pattern, err)
	}
	r.MatchTimeout = regexpTimeout
	return r, nil
}

func regexpMatchError(err error) error {
	// regexp2 timeout diagnostics include the entire input. Do not put that
	// input into an error message, which may be logged separately by callers.
	if errors.Is(err, regexp2.ErrMatchTimeout) {
		return fmt.Errorf("regular expression match exceeded %s", regexpTimeout)
	}
	if errors.Is(err, regexp2.ErrBacktrackingStackLimit) {
		return errors.New("regular expression exceeded the backtracking stack limit")
	}
	return errors.New("regular expression matching failed")
}
