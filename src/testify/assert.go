package assert

// https://antonz.org/do-not-testify/#asserting-equality

import (
	"reflect"
	"testing"
)

//run: go test -v %

// AssertEqual asserts that got is equal to want.
func AssertEqual[T any](tb testing.TB, want T, got T) {
	tb.Helper()

	// Check if both are nil.
	if isNil(got) && isNil(want) {
		return
	}

	// Fallback to reflective comparison.
	if reflect.DeepEqual(got, want) {
		return
	}

	// No match, report the failure.
	tb.Errorf("got: %#v; want: %#v", got, want)
}

// isNil checks if v is nil.
func isNil(v any) bool {
	if v == nil {
		return true
	}

	// A non-nil interface can still hold a nil value,
	// so we must check the underlying value.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
