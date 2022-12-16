package fxmodules

import "go.uber.org/fx"

// An optional provider. Returns `provider` is the predicate is true; Otherwise,
// it returns an empty option.
func optionalProvide(provider any, predicate bool) fx.Option {
	if predicate {
		return fx.Provide(provider)
	} else {
		return fx.Options()
	}
}

// An optional invoke. Returns `provider` is the predicate is true; Otherwise,
// it returns an empty option.
func optionalInvoke(invoke any, predicate bool) fx.Option {
	if predicate {
		return fx.Invoke(invoke)
	} else {
		return fx.Options()
	}
}

// An optional supply. Returns `provider` is the predicate is true; Otherwise,
// it returns an empty option.
func optionalSupply(value any, predicate bool) fx.Option {
	if predicate {
		return fx.Supply(value)
	} else {
		return fx.Options()
	}
}
