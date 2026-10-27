package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// inputs reads JSON values from a list of files in turn, "-" being stdin.
// It implements gojq.Iter so the input and inputs builtins can share it.
// A read or parse error is returned as a value, after which the rest of
// that file is skipped since the stream can't be resynchronized.
type inputs struct {
	files  []string
	stdin  io.Reader
	order  *keyOrder // ranks object keys as they are read
	failed bool      // whether any error was returned

	name string
	file io.Closer
	dec  *json.Decoder
}

func newInputs(files []string, stdin io.Reader, order *keyOrder) *inputs {
	if len(files) == 0 {
		files = []string{"-"}
	}
	return &inputs{files: files, stdin: stdin, order: order}
}

func (in *inputs) Next() (any, bool) {
	for {
		if in.dec == nil {
			if len(in.files) == 0 {
				return nil, false
			}
			in.name, in.files = in.files[0], in.files[1:]
			if in.name == "-" {
				in.dec = json.NewDecoder(in.stdin)
			} else {
				f, err := os.Open(in.name)
				if err != nil {
					in.failed = true
					return err, true
				}
				in.file, in.dec = f, json.NewDecoder(f)
			}
			in.dec.UseNumber()
		}
		tok, err := in.dec.Token()
		if err == nil {
			var v any
			if v, err = in.value(tok); err == nil {
				return v, true
			}
		}
		in.close()
		if err != io.EOF {
			in.failed = true
			return in.wrap(err), true
		}
	}
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
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return tok, err
}

func (in *inputs) close() {
	if in.file != nil {
		in.file.Close()
	}
	in.file, in.dec = nil, nil
}

func (in *inputs) wrap(err error) error {
	name := in.name
	if name == "-" {
		name = "<stdin>"
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Errorf("%s: invalid JSON at byte %d: %v", name, syntax.Offset, err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%s: invalid JSON: unexpected end of input", name)
	}
	return fmt.Errorf("%s: %v", name, err)
}
