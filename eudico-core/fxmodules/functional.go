package fxmodules

import "go.uber.org/fx"

// Functional idioms for building fx Options.

// Returns option if the predicate is true. Otherwise, it returns an empty option.
func fxOptional(predicate bool, option fx.Option) fx.Option {
	if predicate {
		return option
	} else {
		return fx.Options()
	}
}

// Returns optionOnTrue if the predicate is true. Otherwise, it returns optionOnFalse.
func fxEitherOr(predicate bool, optionOnTrue fx.Option, optionOnFalse fx.Option) fx.Option {
	if predicate {
		return optionOnTrue
	} else {
		return optionOnFalse
	}
}

// Returns the value in map m whose key is expr, if such a key exists. Otherwise,
// it returns an empty option.
func fxCase[T comparable](expr T, m map[T]fx.Option) fx.Option {
	if option, ok := m[expr]; ok {
		return option
	}
	return fx.Options()
}
