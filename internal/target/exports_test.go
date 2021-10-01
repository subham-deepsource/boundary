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
	return t.Oplog(op)
}
