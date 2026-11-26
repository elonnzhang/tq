package cli

import (
	"fmt"

	"github.com/itchyny/gojq"
)

// addOrderedBuiltins makes entry arrays follow the keys of their input.
// User definitions come last, so they can still replace to_entries.
func addOrderedBuiltins(q *gojq.Query) {
	builtins, err := gojq.Parse(`def to_entries:
		[keys_unsorted[] as $k | {key: $k, value: .[$k]}]; .`)
	if err != nil {
		panic(err)
	}
	q.FuncDefs = append(builtins.FuncDefs, q.FuncDefs...)
}

// orderedKeysOption provides jq's keys_unsorted using the recorded order.
func orderedKeysOption(order *keyOrder) gojq.CompilerOption {
	return gojq.WithFunction("keys_unsorted", 0, 0, func(v any, _ []any) any {
		switch v := v.(type) {
		case map[string]any:
			keys := order.keys(v)
			result := make([]any, len(keys))
			for i, key := range keys {
				result[i] = key
			}
			return result
		case []any:
			keys := make([]any, len(v))
			for i := range v {
				keys[i] = i
			}
			return keys
		default:
			return fmt.Errorf("%s has no keys", gojq.TypeOf(v))
		}
	})
}
