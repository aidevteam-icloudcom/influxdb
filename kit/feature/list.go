// Code generated by the feature package; DO NOT EDIT.

package feature

var backendExample = MakeBoolFlag(
	"Backend Example",
	"backendExample",
	"Gavin Cabbage",
	false,
	Permanent,
	false,
)

// BackendExample - A permanent backend example boolean flag
func BackendExample() BoolFlag {
	return backendExample
}

var frontendExample = MakeIntFlag(
	"Frontend Example",
	"frontendExample",
	"Gavin Cabbage",
	42,
	Temporary,
	true,
)

// FrontendExample - A temporary frontend example integer flag
func FrontendExample() IntFlag {
	return frontendExample
}

var pushDownWindowAggregateCount = MakeBoolFlag(
	"Push Down Window Aggregate Count",
	"pushDownWindowAggregateCount",
	"Query Team",
	false,
	Temporary,
	false,
)

// PushDownWindowAggregateCount - Enable Count variant of PushDownWindowAggregateRule and PushDownBareAggregateRule
func PushDownWindowAggregateCount() BoolFlag {
	return pushDownWindowAggregateCount
}

var pushDownWindowAggregateRest = MakeBoolFlag(
	"Push Down Window Aggregate Rest",
	"pushDownWindowAggregateRest",
	"Query Team",
	false,
	Temporary,
	false,
)

// PushDownWindowAggregateRest - Enable non-Count variants of PushDownWindowAggregateRule and PushDownWindowAggregateRule (stage 2)
func PushDownWindowAggregateRest() BoolFlag {
	return pushDownWindowAggregateRest
}

var newAuth = MakeBoolFlag(
	"New Auth Package",
	"newAuth",
	"Alirie Gray",
	false,
	Temporary,
	false,
)

// NewAuthPackage - Enables the refactored authorization api
func NewAuthPackage() BoolFlag {
	return newAuth
}

var sessionService = MakeBoolFlag(
	"Session Service",
	"sessionService",
	"Lyon Hill",
	false,
	Temporary,
	true,
)

// SessionService - A temporary switching system for the new session system
func SessionService() BoolFlag {
	return sessionService
}

var pushDownGroupAggregateCount = MakeBoolFlag(
	"Push Down Group Aggregate Count",
	"pushDownGroupAggregateCount",
	"Query Team",
	false,
	Temporary,
	false,
)

// PushDownGroupAggregateCount - Enable the count variant of PushDownGroupAggregate planner rule
func PushDownGroupAggregateCount() BoolFlag {
	return pushDownGroupAggregateCount
}

var newLabels = MakeBoolFlag(
	"New Label Package",
	"newLabels",
	"Alirie Gray",
	false,
	Temporary,
	false,
)

// NewLabelPackage - Enables the refactored labels api
func NewLabelPackage() BoolFlag {
	return newLabels
}

var all = []Flag{
	backendExample,
	frontendExample,
	pushDownWindowAggregateCount,
	pushDownWindowAggregateRest,
	newAuth,
	sessionService,
	pushDownGroupAggregateCount,
	newLabels,
}

var byKey = map[string]Flag{
	"backendExample":               backendExample,
	"frontendExample":              frontendExample,
	"pushDownWindowAggregateCount": pushDownWindowAggregateCount,
	"pushDownWindowAggregateRest":  pushDownWindowAggregateRest,
	"newAuth":                      newAuth,
	"sessionService":               sessionService,
	"pushDownGroupAggregateCount":  pushDownGroupAggregateCount,
	"newLabels":                    newLabels,
}
