package keepsorted

import (
	"cmp"
)

type cmpFunc[T any] func(T, T) int

func (f cmpFunc[T]) andThen(next cmpFunc[T]) cmpFunc[T] {
	return func(a, b T) int {
		if c := f(a, b); c != 0 {
			return c
		}
		return next(a, b)
	}
}

func (f cmpFunc[T]) reversed() cmpFunc[T] {
	return func(a, b T) int {
		return f(b, a)
	}
}

func comparing[T any, R cmp.Ordered](f func(T) R) cmpFunc[T] {
	return comparingFunc(f, cmp.Compare)
}

func comparingFunc[T, R any](f func(T) R, cmp cmpFunc[R]) cmpFunc[T] {
	return func(a, b T) int {
		r1, r2 := f(a), f(b)
		return cmp(r1, r2)
	}
}
