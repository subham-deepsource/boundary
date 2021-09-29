package target

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/boundary/internal/db"
	dbcommon "github.com/hashicorp/boundary/internal/db/common"
	"github.com/hashicorp/boundary/internal/errors"
	"github.com/hashicorp/boundary/internal/kms"
	"github.com/hashicorp/boundary/internal/oplog"
)

// CreateTarget inserts into the repository and returns the new Target With
// its list of host sets and credential libraries.
// WithHostSources and WithCredentialSources are the only supported option.
func (r *Repository) CreateTarget(ctx context.Context, target Target, opt ...Option) (Target, []HostSource, []CredentialSource, error) {
	const op = "target.(Repository).CreateTarget"
	opts := GetOpts(opt...)
	if target == nil {
		return nil, nil, nil, errors.New(ctx, errors.InvalidParameter, op, "missing target")
	}

	rh, ok := subtypes[target.GetType()]
	if !ok {
		return nil, nil, nil, errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("unsupported target type %s", target.GetType()))
	}

	if err := rh.ValidateCreate(ctx, target); err != nil {
		return nil, nil, nil, err
	}

	t := target.Clone()

	if opts.WithPublicId != "" {
		if err := t.SetPublicId(ctx, opts.WithPublicId); err != nil {
			return nil, nil, nil, err
		}
	} else {
		id, err := rh.NewTargetId()
		if err != nil {
			return nil, nil, nil, errors.Wrap(ctx, err, op)
		}
		t.SetPublicId(ctx, id)
	}

	oplogWrapper, err := r.kms.GetWrapper(ctx, target.GetScopeId(), kms.KeyPurposeOplog)
	if err != nil {
		return nil, nil, nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get oplog wrapper"))
	}

	newHostSets := make([]interface{}, 0, len(opts.WithHostSources))
	for _, hsId := range opts.WithHostSources {
		hostSet, err := NewTargetHostSet(t.GetPublicId(), hsId)
		if err != nil {
			return nil, nil, nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to create in memory target host set"))
		}
		newHostSets = append(newHostSets, hostSet)
	}

	newCredLibs := make([]interface{}, 0, len(opts.WithCredentialSources))
	for _, clId := range opts.WithCredentialSources {
		credLib, err := NewCredentialLibrary(t.GetPublicId(), clId)
		if err != nil {
			return nil, nil, nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to create in memory target credential library"))
		}
		newCredLibs = append(newCredLibs, credLib)
	}

	metadata := t.Oplog(oplog.OpType_OP_TYPE_CREATE)
	var returnedTarget interface{}
	var returnedHostSources []HostSource
	var returnedCredSources []CredentialSource
	_, err = r.writer.DoTx(
		ctx,
		db.StdRetryCnt,
		db.ExpBackoff{},
		func(read db.Reader, w db.Writer) error {
			targetTicket, err := w.GetTicket(t)
			if err != nil {
				return errors.Wrap(ctx, err, op, errors.WithMsg("unable to get ticket"))
			}
			msgs := make([]*oplog.Message, 0, 2)
			var targetOplogMsg oplog.Message
			returnedTarget = t.Clone()
			if err := w.Create(ctx, returnedTarget, db.NewOplogMsg(&targetOplogMsg)); err != nil {
				return errors.Wrap(ctx, err, op, errors.WithMsg("unable to create target"))
			}
			msgs = append(msgs, &targetOplogMsg)
			if len(newHostSets) > 0 {
				hostSetOplogMsgs := make([]*oplog.Message, 0, len(newHostSets))
				if err := w.CreateItems(ctx, newHostSets, db.NewOplogMsgs(&hostSetOplogMsgs)); err != nil {
					return errors.Wrap(ctx, err, op, errors.WithMsg("unable to add host sets"))
				}
				if returnedHostSources, err = fetchHostSources(ctx, read, t.GetPublicId()); err != nil {
					return errors.Wrap(ctx, err, op, errors.WithMsg("unable to read host sources"))
				}
				msgs = append(msgs, hostSetOplogMsgs...)
			}
			if len(newCredLibs) > 0 {
				credLibOplogMsgs := make([]*oplog.Message, 0, len(newCredLibs))
				if err := w.CreateItems(ctx, newCredLibs, db.NewOplogMsgs(&credLibOplogMsgs)); err != nil {
					return errors.Wrap(ctx, err, op, errors.WithMsg("unable to add credential sources"))
				}
				if returnedCredSources, err = fetchCredentialSources(ctx, read, t.GetPublicId()); err != nil {
					return errors.Wrap(ctx, err, op, errors.WithMsg("unable to read credential sources"))
				}
				msgs = append(msgs, credLibOplogMsgs...)
			}
			if err := w.WriteOplogEntryWith(ctx, oplogWrapper, targetTicket, metadata, msgs); err != nil {
				return errors.Wrap(ctx, err, op, errors.WithMsg("unable to write oplog"))
			}

			return nil
		},
	)
	if err != nil {
		return nil, nil, nil, errors.Wrap(ctx, err, op, errors.WithMsg(fmt.Sprintf("failed for %s target id", t.GetPublicId())))
	}
	return returnedTarget.(Target), returnedHostSources, returnedCredSources, nil
}

// UpdateTarget will update a target in the repository and return the written
// target. fieldMaskPaths provides field_mask.proto paths for fields that should
// be updated.  Fields will be set to NULL if the field is a zero value and
// included in fieldMask. Name, Description, and WorkerFilter are the only
// updatable fields. If no updatable fields are included in the fieldMaskPaths,
// then an error is returned.
func (r *Repository) UpdateTarget(ctx context.Context, target Target, version uint32, fieldMaskPaths []string, _ ...Option) (Target, []HostSource, []CredentialSource, int, error) {
	const op = "target.(Repository).UpdateTarget"
	if target == nil {
		return nil, nil, nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing target")
	}

	rh, ok := subtypes[target.GetType()]
	if !ok {
		return nil, nil, nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("unsupported target type %s", target.GetType()))
	}

	if err := rh.ValidateCreate(ctx, target); err != nil {
		return nil, nil, nil, db.NoRowsAffected, err
	}

	for _, f := range fieldMaskPaths {
		switch {
		case strings.EqualFold("name", f):
		case strings.EqualFold("description", f):
		case strings.EqualFold("defaultport", f):
		case strings.EqualFold("sessionmaxseconds", f):
		case strings.EqualFold("sessionconnectionlimit", f):
		case strings.EqualFold("workerfilter", f):
		default:
			return nil, nil, nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidFieldMask, op, fmt.Sprintf("invalid field mask: %s", f))
		}
	}
	var dbMask, nullFields []string
	dbMask, nullFields = dbcommon.BuildUpdatePaths(
		map[string]interface{}{
			"Name":                   target.GetName(),
			"Description":            target.GetDescription(),
			"DefaultPort":            target.GetDefaultPort(),
			"SessionMaxSeconds":      target.GetSessionMaxSeconds(),
			"SessionConnectionLimit": target.GetSessionConnectionLimit(),
			"WorkerFilter":           target.GetWorkerFilter(),
		},
		fieldMaskPaths,
		[]string{"SessionMaxSeconds", "SessionConnectionLimit"},
	)
	if len(dbMask) == 0 && len(nullFields) == 0 {
		return nil, nil, nil, db.NoRowsAffected, errors.New(ctx, errors.EmptyFieldMask, op, "empty field mask")
	}
	var returnedTarget Target
	var rowsUpdated int
	var hostSources []HostSource
	var credSources []CredentialSource
	_, err := r.writer.DoTx(
		ctx,
		db.StdRetryCnt,
		db.ExpBackoff{},
		func(read db.Reader, w db.Writer) error {
			var err error
			t := target.Clone()
			returnedTarget, hostSources, credSources, rowsUpdated, err = r.update(ctx, t, version, dbMask, nullFields)
			if err != nil {
				return errors.Wrap(ctx, err, op)
			}
			return nil
		},
	)
	if err != nil {
		if errors.IsUniqueError(err) {
			return nil, nil, nil, db.NoRowsAffected, errors.New(ctx, errors.NotUnique, op, fmt.Sprintf("target %s already exists in scope %s", target.GetName(), target.GetScopeId()))
		}
		return nil, nil, nil, db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg(fmt.Sprintf("failed for %s", target.GetPublicId())))
	}
	return returnedTarget, hostSources, credSources, rowsUpdated, nil
}
