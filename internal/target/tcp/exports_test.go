package tcp

import "github.com/hashicorp/boundary/internal/target"

var (
	RH               = rh
	TestId           = testId
	TestTargetName   = testTargetName
	DefaultTableName = defaultTableName
	// AllocTargetView         = allocTargetView
	// TargetsViewDefaultTable = targetsViewDefaultTable
)

// NewTestTarget is a test helper that bypasses the scopeId checks
// performed by NewTarget, allowing tests to create Targets with
// nil scopeIds for more robust testing.
func NewTestTarget(scopeId string, opt ...target.Option) *Target {
	t, _ := NewTarget("testScope", opt...)
	t.ScopeId = scopeId
	return t
}
