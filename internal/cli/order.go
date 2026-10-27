package cli

import (
	"cmp"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/itchyny/gojq"
)

// keyOrder puts object keys in display order. gojq keeps objects as Go
// maps, which forget the order of their keys, so tq remembers the order
// of the keys of each object it reads. gojq never changes an object it
// didn't make, so these objects keep their keys as read. Objects the
// filter makes get their keys ranked the way a reader expects to see
// them: keys the filter builds objects with come first, in the order
// written, so that '{name, age}' shows name before age; then keys in the
// order they first appear in the input; then any other keys, sorted.
type keyOrder struct {
	rank map[string]int
	// objects holds the keys of each object read, by its address. The
	// addresses also keep the objects alive, so no object the filter
	// makes can take the address of one read.
	objects map[unsafe.Pointer][]string
}

func newKeyOrder() *keyOrder {
	return &keyOrder{rank: map[string]int{}, objects: map[unsafe.Pointer][]string{}}
}

// add ranks k after all keys added before it, unless it is already ranked.
func (o *keyOrder) add(k string) {
	if _, ok := o.rank[k]; !ok {
		o.rank[k] = len(o.rank)
	}
}

// record remembers that the keys of m, an object read, are keys in order.
func (o *keyOrder) record(m map[string]any, keys []string) {
	o.objects[reflect.ValueOf(m).UnsafePointer()] = keys
}

// keys returns the keys of m in display order. The result must not be
// modified.
func (o *keyOrder) keys(m map[string]any) []string {
	if keys, ok := o.objects[reflect.ValueOf(m).UnsafePointer()]; ok && len(keys) == len(m) {
		return keys
	}
	return o.sort(slices.Collect(maps.Keys(m)))
}

// sort sorts keys in display order.
func (o *keyOrder) sort(keys []string) []string {
	slices.SortFunc(keys, func(a, b string) int {
		ra, okA := o.rank[a]
		rb, okB := o.rank[b]
		switch {
		case okA && okB:
			return cmp.Compare(ra, rb)
		case okA:
			return -1
		case okB:
			return 1
		}
		return strings.Compare(a, b)
	})
	return keys
}

// addQueryKeys ranks the constant keys of the objects q constructs, in the
// order they are written.
func (o *keyOrder) addQueryKeys(q *gojq.Query) {
	walk(reflect.ValueOf(q), func(kv *gojq.ObjectKeyVal) {
		switch {
		case kv.Key != "":
			o.add(strings.TrimPrefix(kv.Key, "$")) // {$name} makes the key "name"
		case kv.KeyString != nil && len(kv.KeyString.Queries) == 0:
			o.add(kv.KeyString.Str)
		}
	})
}

// walk calls f for each object key/value pair in the syntax tree below v,
// depth first in source order.
func walk(v reflect.Value, f func(*gojq.ObjectKeyVal)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		if kv, ok := reflect.TypeAssert[*gojq.ObjectKeyVal](v); ok {
			f(kv)
		}
		walk(v.Elem(), f)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				walk(v.Field(i), f)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			walk(v.Index(i), f)
		}
	}
}
