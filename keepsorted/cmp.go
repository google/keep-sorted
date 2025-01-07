package keepsorted

import (
	"cmp"
)

type cmpFunc[T any] func(T, T) int

// andThen returns a cmpFunc based on the current one that first checks the
// current cmpFunc and only checks the next cmpFunc if the current cmpFunc
// thinks the two elements are equal.
func (f cmpFunc[T]) andThen(next cmpFunc[T]) cmpFunc[T] {
	return func(a, b T) int {
		if c := f(a, b); c != 0 {
			return c
		}
		return next(a, b)
	}
}

// reverse returns a cmpFunc based on the current one that yields the opposite
// order.
func (f cmpFunc[T]) reversed() cmpFunc[T] {
	return func(a, b T) int {
		return f(b, a)
	}
}

// comparing creates a cmpFunc that orders T based on one of its properties, R.
func comparing[T any, R cmp.Ordered](f func(T) R) cmpFunc[T] {
	return comparingFunc(f, cmp.Compare)
}

// comparingFunc creates a cmpFunc that orders T based on one of its properties,
// R and R has its own explicit ordering.
func comparingFunc[T, R any](f func(T) R, cmp cmpFunc[R]) cmpFunc[T] {
	return func(a, b T) int {
		r1, r2 := f(a), f(b)
		return cmp(r1, r2)
	}
}
