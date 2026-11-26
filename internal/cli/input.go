package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// inputs reads JSON values from a list of files in turn, "-" being stdin.
// It implements gojq.Iter so the input and inputs builtins can share it.
// A file open error skips that file. A read or parse error stops the input.
// Plain text tables are read as one array; raw input bypasses detection.
type inputs struct {
	files    []string
	stdin    io.Reader
	order    *keyOrder // ranks object keys as they are read
	failure  int       // exit code of the last input error
	format   string    // auto, json or table
	raw      bool
	rawSlurp bool

	name   string
	file   io.Closer
	dec    *json.Decoder
	reader *bufio.Reader
}

func newInputs(files []string, stdin io.Reader, order *keyOrder) *inputs {
	if len(files) == 0 {
		files = []string{"-"}
	}
	return &inputs{files: files, stdin: stdin, order: order}
}

func (in *inputs) Next() (any, bool) {
	for {
		if in.dec == nil && in.reader == nil {
			if len(in.files) == 0 {
				return nil, false
			}
			in.name, in.files = in.files[0], in.files[1:]
			var r io.Reader = in.stdin
			if in.name != "-" {
				f, err := os.Open(in.name)
				if err != nil {
					in.failure = exitInput
					return err, true
				}
				in.file, r = f, f
			}
			in.reader = bufio.NewReader(r)
			if in.raw {
				continue
			}
			r = in.reader
			table := in.format == "table"
			if in.format != "json" && !table {
				var err error
				if r, table, err = detectTable(in.reader); err != nil {
					return in.fail(err)
				}
			}
			if table {
				v, err := readTextTable(r, in.order)
				if err != nil {
					return in.fail(err)
				}
				in.close()
				return v, true
			}
			in.dec = json.NewDecoder(r)
			in.dec.UseNumber()
		}
		if in.raw {
			if in.rawSlurp {
				data, err := io.ReadAll(in.reader)
				if err != nil {
					return in.fail(err)
				}
				in.close()
				return string(data), true
			}
			line, err := in.reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return in.fail(err)
			}
			if errors.Is(err, io.EOF) {
				in.close()
			}
			if line != "" {
				return strings.TrimSuffix(line, "\n"), true
			}
			continue
		}
		tok, err := in.dec.Token()
		if err == nil {
			var v any
			if v, err = in.value(tok); err == nil {
				return v, true
			}
		}
		if !errors.Is(err, io.EOF) {
			return in.fail(err)
		}
		in.close()
	}
}

// detectTable examines a text line, but leaves JSON values readable as soon
// as they arrive without waiting for the end of a line.
func detectTable(reader *bufio.Reader) (io.Reader, bool, error) {
	var prefix strings.Builder
	table := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, false, err
			}
			break
		}
		prefix.WriteByte(b)
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		if b != '{' && b != '[' && b != '"' {
			for {
				b, err = reader.ReadByte()
				if err != nil {
					if !errors.Is(err, io.EOF) {
						return nil, false, err
					}
					break
				}
				prefix.WriteByte(b)
				if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
					break
				}
			}
			if !json.Valid([]byte(strings.TrimSpace(prefix.String()))) {
				if !errors.Is(err, io.EOF) && b != '\n' {
					line, err := reader.ReadString('\n')
					if err != nil && !errors.Is(err, io.EOF) {
						return nil, false, err
					}
					prefix.WriteString(line)
				}
				table = isTextTable(prefix.String())
			}
		}
		break
	}
	return io.MultiReader(strings.NewReader(prefix.String()), reader), table, nil
}

// fail stops reading after an error, since the stream cannot be resumed.
func (in *inputs) fail(err error) (any, bool) {
	in.close()
	in.files = nil
	in.failure = exitRuntime
	return in.wrap(err), true
}

// value reads the rest of the value starting with tok.
func (in *inputs) value(tok json.Token) (any, error) {
	switch tok {
	case json.Delim('{'):
		m := map[string]any{}
		var keys []string
		for in.dec.More() {
			k, err := in.token()
			if err != nil {
				return nil, err
			}
			key := k.(string) // the decoder only allows strings here
			if _, dup := m[key]; !dup {
				keys = append(keys, key)
			}
			in.order.add(key)
			if m[key], err = in.next(); err != nil {
				return nil, err
			}
		}
		in.order.record(m, keys)
		_, err := in.token() // '}'
		return m, err
	case json.Delim('['):
		a := []any{}
		for in.dec.More() {
			v, err := in.next()
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err := in.token() // ']'
		return a, err
	}
	return tok, nil // string, json.Number, bool or nil
}

func (in *inputs) next() (any, error) {
	tok, err := in.token()
	if err != nil {
		return nil, err
	}
	return in.value(tok)
}

// token reads a token inside a value, where the input must not end.
func (in *inputs) token() (json.Token, error) {
	tok, err := in.dec.Token()
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return tok, err
}

func (in *inputs) close() {
	if in.file != nil {
		in.file.Close()
	}
	in.file, in.dec, in.reader = nil, nil, nil
}

func (in *inputs) wrap(err error) error {
	name := in.name
	if name == "-" {
		name = "<stdin>"
	}
	if syntax, ok := errors.AsType[*json.SyntaxError](err); ok {
		return fmt.Errorf("%s: invalid JSON at byte %d: %v", name, syntax.Offset, err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%s: invalid JSON: unexpected end of input", name)
	}
	return fmt.Errorf("%s: %v", name, err)
}
