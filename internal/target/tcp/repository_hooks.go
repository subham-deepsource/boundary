package tcp

import (
	"context"

	"github.com/hashicorp/boundary/internal/db"
	"github.com/hashicorp/boundary/internal/errors"
	"github.com/hashicorp/boundary/internal/oplog"
	"github.com/hashicorp/boundary/internal/target"
)

const (
	targetPrefix = "ttcp"
)

type repository struct{}

func (r *repository) Alloc(publicId string, version uint32, op oplog.OpType) (target.Target, oplog.Metadata) {
	t := allocTarget()
	t.PublicId = publicId
	t.Version = version
	metadata := t.Oplog(op)

	return &t, metadata
}

func (r *repository) ValidateCreate(ctx context.Context, t target.Target) error {
	const op = "tcp.(repository).ValidateCreate"

	tt, ok := t.(*Target)
	if !ok {
		return errors.New(ctx, errors.InvalidParameter, op, "target is not a tcp.Target")
	}

	if tt.Target == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing target store")
	}
	if tt.ScopeId == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "missing scope id")
	}
	if tt.Name == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "missing name")
	}
	if tt.PublicId != "" {
		return errors.New(ctx, errors.InvalidParameter, op, "public id not empty")
	}
	return nil
}

func (r *repository) ValidateUpdate(ctx context.Context, t target.Target) error {
	const op = "tcp.(repository).ValidateUpdate"

	tt, ok := t.(*Target)
	if !ok {
		return errors.New(ctx, errors.InvalidParameter, op, "target is not a tcp.Target")
	}

	if tt.Target == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing target store")
	}
	if tt.PublicId == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "missing target public id")
	}
	return nil
}

func (r *repository) NewTargetId() (string, error) {
	const op = "tcp.(repository).NewTargetId"
	id, err := db.NewPublicId(targetPrefix)
	if err != nil {
		return "", errors.WrapDeprecated(err, op)
	}
	return id, nil
}

var rh target.RepositoryHooks = &repository{}
