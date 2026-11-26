package cli

import (
	"os"
	"testing"
)

func TestTextTableCLI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin string
		file  string
		args  []string
		out   string
		code  int
	}{
		{"rex hosts", "", "testdata/rex-hosts-list.txt", []string{`map(.LABEL) | join(",")`}, "elon-macbook-m4,Herdr,herdrex-report,dev\n", 0},
		{"rex header order", "", "testdata/rex-hosts-list.txt", []string{`.[0] | keys_unsorted | join(",")`}, "LABEL,ENDPOINTS,SOURCE\n", 0},
		{"rex endpoint", "", "testdata/rex-hosts-list.txt", []string{`.[0].ENDPOINTS`}, "https://macbook-m4-rex.ts.net\n", 0},
		{"herdr machines", "", "testdata/herdr-machine-list.txt", []string{`map(.[1]) | join(",")`}, "dev1,DEV2\n", 0},
		{"herdr IDs remain strings", "", "testdata/herdr-machine-list.txt", []string{`.[1][0]`}, "0157419224420049c01b830f42f75b7e\n", 0},
		{"herdr fields", "", "testdata/herdr-machine-list.txt", []string{`map(length) | tojson`}, "[5,5]\n", 0},
		{"JSON stream", "1 2 3\n", "", []string{"-s", "add"}, "6\n", 0},
		{"malformed JSON stream", "1 2 x\n", "", []string{"-s", "."}, "", 5},
		{"force numeric table", "1 2\n3 4\n", "", []string{"--input", "table", `map(.[0]) | join(",")`}, "1,3\n", 0},
		{"force two columns", "NAME  VALUE\nfoo   bar\n", "", []string{"--input=table", `.[0].VALUE`}, "bar\n", 0},
		{"force JSON", "LABEL ENDPOINTS SOURCE\n", "", []string{"--input=json", "."}, "", 5},
		{"raw bypass", "LABEL ENDPOINTS SOURCE\n", "", []string{"-R", "length"}, "22\n", 0},
		{"raw overrides format", "LABEL ENDPOINTS SOURCE\n", "", []string{"--input=table", "-R", "length"}, "22\n", 0},
		{"ragged table", "A B C\none two\n", "", []string{"."}, "", 5},
		{"table no final newline", "A B C\none two three", "", []string{`.[0].C`}, "three\n", 0},
		{"table after blank lines", "\n\nA B C\none two three\n", "", []string{`.[0].C`}, "three\n", 0},
		{"table with ANSI", "\x1b[32mA B C\x1b[0m\none two three\n", "", []string{`.[0].C`}, "three\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdin := tc.stdin
			if tc.file != "" {
				data, err := os.ReadFile(tc.file)
				if err != nil {
					t.Fatal(err)
				}
				stdin = string(data)
			}
			out, errOut, code := tq(t, stdin, tc.args...)
			if out != tc.out || code != tc.code || code == 0 && errOut != "" {
				t.Errorf("got (%d) %q %q; want (%d) %q", code, out, errOut, tc.code, tc.out)
			}
		})
	}
}
