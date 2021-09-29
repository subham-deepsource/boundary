package target

import "github.com/hashicorp/boundary/internal/oplog"

var (
	NewTcpTargetId          = newTcpTargetId
	TestId                  = testId
	TestTargetName          = testTargetName
	FetchCredentialSources  = fetchCredentialSources
	FetchHostSources        = fetchHostSources
	AllocTargetHostSet      = allocTargetHostSet // only used by a test, so can be moved out of non-test code
	AllocTargetView         = allocTargetView
	TargetsViewDefaultTable = targetsViewDefaultTable
)

func Oplog(t Target, op oplog.OpType) oplog.Metadata {
	return t.oplog(op)
}

// NewTestTcpTarget is a test helper that bypasses the scopeId checks
// performed by NewTcpTarget, allowing tests to create TcpTargets with
// nil scopeIds for more robust testing.
func NewTestTcpTarget(scopeId string, opt ...Option) *TcpTarget {
	t, _ := NewTcpTarget("testScope", opt...)
	t.ScopeId = scopeId
	return t
}
