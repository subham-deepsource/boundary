package target

import (
	"context"
	"fmt"

	"github.com/hashicorp/boundary/internal/oplog"
)

type RepositoryHooks interface {
	Alloc(publicId string, version uint32, op oplog.OpType) (Target, oplog.Metadata)
	ValidateCreate(context.Context, Target) error
	ValidateUpdate(context.Context, Target) error
	NewTargetId() (string, error)
}

var subtypes = make(map[string]RepositoryHooks)

func Register(t string, r RepositoryHooks) {
	_, ok := subtypes[t]
	if ok {
		panic(fmt.Sprintf("target subtype %s already registered", t))
	}

	subtypes[t] = r
}
