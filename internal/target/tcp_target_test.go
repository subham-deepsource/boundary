package target_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/boundary/internal/db"
	dbassert "github.com/hashicorp/boundary/internal/db/assert"
	"github.com/hashicorp/boundary/internal/errors"
	"github.com/hashicorp/boundary/internal/iam"
	"github.com/hashicorp/boundary/internal/oplog"
	"github.com/hashicorp/boundary/internal/target"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTcpTarget_Create(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	_, prj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	type args struct {
		scopeId string
		opt     []target.Option
	}
	tests := []struct {
		name          string
		args          args
		want          *target.TcpTarget
		wantErr       bool
		wantIsErr     errors.Code
		create        bool
		wantCreateErr bool
	}{
		{
			name:      "empty-scopeId",
			args:      args{},
			wantErr:   true,
			wantIsErr: errors.InvalidParameter,
		},
		{
			name: "valid-proj-scope",
			args: args{
				scopeId: prj.PublicId,
				opt:     []target.Option{target.WithName("valid-proj-scope")},
			},
			want: func() *target.TcpTarget {
				t := target.AllocTcpTarget()
				t.ScopeId = prj.PublicId
				t.Name = "valid-proj-scope"
				t.SessionMaxSeconds = uint32((8 * time.Hour).Seconds())
				t.SessionConnectionLimit = 1
				return &t
			}(),
			create: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			got, err := target.NewTcpTarget(tt.args.scopeId, tt.args.opt...)
			if tt.wantErr {
				require.Error(err)
				assert.True(errors.Match(errors.T(tt.wantIsErr), err))
				return
			}
			require.NoError(err)
			assert.Equal(tt.want, got)
			if tt.create {
				id, err := target.NewTcpTargetId()
				require.NoError(err)
				got.PublicId = id
				err = db.New(conn).Create(context.Background(), got)
				if tt.wantCreateErr {
					assert.Error(err)
					return
				} else {
					assert.NoError(err)
				}
			}
		})
	}
}

func TestTcpTarget_Delete(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))

	tests := []struct {
		name            string
		target          *target.TcpTarget
		wantRowsDeleted int
		wantErr         bool
		wantErrMsg      string
	}{
		{
			name:            "valid",
			target:          target.TestTcpTarget(t, conn, proj.PublicId, target.TestTargetName(t, proj.PublicId)),
			wantErr:         false,
			wantRowsDeleted: 1,
		},
		{
			name: "bad-id",
			target: func() *target.TcpTarget {
				tar := target.AllocTcpTarget()
				id, err := target.NewTcpTargetId()
				require.NoError(t, err)
				tar.PublicId = id
				tar.ScopeId = proj.PublicId
				tar.Name = target.TestTargetName(t, proj.PublicId)
				return &tar
			}(),
			wantErr:         false,
			wantRowsDeleted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			deleteTarget := target.AllocTcpTarget()
			deleteTarget.PublicId = tt.target.PublicId
			deletedRows, err := rw.Delete(context.Background(), &deleteTarget)
			if tt.wantErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			if tt.wantRowsDeleted == 0 {
				assert.Equal(tt.wantRowsDeleted, deletedRows)
				return
			}
			assert.Equal(tt.wantRowsDeleted, deletedRows)
			foundTarget := target.AllocTcpTarget()
			foundTarget.PublicId = tt.target.PublicId
			err = rw.LookupById(context.Background(), &foundTarget)
			require.Error(err)
			assert.True(errors.IsNotFoundError(err))
		})
	}
}

func TestTcpTarget_Update(t *testing.T) {
	t.Parallel()
	id := target.TestId(t)
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))

	type args struct {
		name           string
		description    string
		fieldMaskPaths []string
		nullPaths      []string
		ScopeId        string
	}
	tests := []struct {
		name           string
		args           args
		wantRowsUpdate int
		wantErr        bool
		wantErrMsg     string
		wantDup        bool
	}{
		{
			name: "valid",
			args: args{
				name:           "valid" + id,
				fieldMaskPaths: []string{"Name"},
				ScopeId:        proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "proj-scope-id-not-in-mask",
			args: args{
				name:           "proj-scope-id" + id,
				fieldMaskPaths: []string{"Name"},
				ScopeId:        proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "empty-scope-id",
			args: args{
				name:           "empty-scope-id" + id,
				fieldMaskPaths: []string{"Name"},
				ScopeId:        "",
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "dup-name",
			args: args{
				name:           "dup-name" + id,
				fieldMaskPaths: []string{"Name"},
				ScopeId:        proj.PublicId,
			},
			wantErr:    true,
			wantDup:    true,
			wantErrMsg: `db.Update: duplicate key value violates unique constraint "target_tcp_scope_id_name_key": unique constraint violation: integrity violation: error #1002`,
		},
		{
			name: "set description null",
			args: args{
				name:           "set description null" + id,
				fieldMaskPaths: []string{"Name"},
				nullPaths:      []string{"Description"},
				ScopeId:        proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "set name null",
			args: args{
				description:    "set description null" + id,
				fieldMaskPaths: []string{"Description"},
				nullPaths:      []string{"Name"},
				ScopeId:        proj.PublicId,
			},
			wantErr:    true,
			wantErrMsg: `db.Update: name must not be empty: not null constraint violated: integrity violation: error #1001`,
		},
		{
			name: "set description null",
			args: args{
				name:           "set name null" + id,
				fieldMaskPaths: []string{"Name"},
				nullPaths:      []string{"Description"},
				ScopeId:        proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			if tt.wantDup {
				target := target.TestTcpTarget(t, conn, proj.PublicId, target.TestTargetName(t, proj.PublicId))
				target.Name = tt.args.name
				_, err := rw.Update(context.Background(), target, tt.args.fieldMaskPaths, tt.args.nullPaths)
				require.NoError(err)
			}

			id := target.TestId(t)
			tar := target.TestTcpTarget(t, conn, proj.PublicId, id, target.WithDescription(id))

			updateTarget := target.AllocTcpTarget()
			updateTarget.PublicId = tar.PublicId
			updateTarget.ScopeId = tt.args.ScopeId
			updateTarget.Name = tt.args.name
			updateTarget.Description = tt.args.description

			updatedRows, err := rw.Update(context.Background(), &updateTarget, tt.args.fieldMaskPaths, tt.args.nullPaths)
			if tt.wantErr {
				require.Error(err)
				assert.Equal(0, updatedRows)
				assert.Equal(tt.wantErrMsg, err.Error())
				err = db.TestVerifyOplog(t, rw, tar.PublicId, db.WithOperation(oplog.OpType_OP_TYPE_UPDATE), db.WithCreateNotBefore(10*time.Second))
				require.Error(err)
				assert.Contains(err.Error(), "record not found")
				return
			}
			require.NoError(err)
			assert.Equal(tt.wantRowsUpdate, updatedRows)
			assert.NotEqual(tar.UpdateTime, updateTarget.UpdateTime)
			foundTarget := target.AllocTcpTarget()
			foundTarget.PublicId = tar.GetPublicId()
			err = rw.LookupByPublicId(context.Background(), &foundTarget)
			require.NoError(err)
			assert.True(proto.Equal(updateTarget, foundTarget))
			if len(tt.args.nullPaths) != 0 {
				underlyingDB, err := conn.SqlDB(ctx)
				require.NoError(err)
				dbassert := dbassert.New(t, underlyingDB)
				for _, f := range tt.args.nullPaths {
					dbassert.IsNull(&foundTarget, f)
				}
			}
		})
	}
	t.Run("update dup names in diff scopes", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		id := target.TestId(t)
		_, proj2 := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		_ = target.TestTcpTarget(t, conn, proj2.PublicId, id, target.WithDescription(id))
		projTarget := target.TestTcpTarget(t, conn, proj.PublicId, id)
		projTarget.Name = id
		updatedRows, err := rw.Update(context.Background(), projTarget, []string{"Name"}, nil)
		require.NoError(err)
		assert.Equal(1, updatedRows)

		foundTarget := target.AllocTcpTarget()
		foundTarget.PublicId = projTarget.GetPublicId()
		err = rw.LookupByPublicId(context.Background(), &foundTarget)
		require.NoError(err)
		assert.Equal(id, projTarget.Name)
	})
}

func TestTcpTarget_Clone(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	t.Run("valid", func(t *testing.T) {
		assert := assert.New(t)
		_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		tar := target.TestTcpTarget(t, conn, proj.PublicId, target.TestTargetName(t, proj.PublicId))
		cp := tar.Clone()
		assert.True(proto.Equal(cp.(*target.TcpTarget).TcpTarget, tar.TcpTarget))
	})
	t.Run("not-equal", func(t *testing.T) {
		assert := assert.New(t)
		_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		_, proj2 := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		target1 := target.TestTcpTarget(t, conn, proj.PublicId, target.TestTargetName(t, proj.PublicId))
		target2 := target.TestTcpTarget(t, conn, proj2.PublicId, target.TestTargetName(t, proj2.PublicId))

		cp := target1.Clone()
		assert.True(!proto.Equal(cp.(*target.TcpTarget).TcpTarget, target2.TcpTarget))
	})
}

func TestTcpTable_SetTableName(t *testing.T) {
	t.Parallel()
	defaultTableName := target.DefaultTcpTableName
	tests := []struct {
		name      string
		setNameTo string
		want      string
	}{
		{
			name:      "new-name",
			setNameTo: "new-name",
			want:      "new-name",
		},
		{
			name:      "reset to default",
			setNameTo: "",
			want:      defaultTableName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			def := target.AllocTcpTarget()
			require.Equal(defaultTableName, def.TableName())
			s := target.AllocTcpTarget()
			s.SetTableName(tt.setNameTo)
			assert.Equal(tt.want, s.TableName())
		})
	}
}

func TestTcpTarget_oplog(t *testing.T) {
	id := target.TestId(t)
	tests := []struct {
		name   string
		target *target.TcpTarget
		op     oplog.OpType
		want   oplog.Metadata
	}{
		{
			name: "simple",
			target: func() *target.TcpTarget {
				t := target.AllocTcpTarget()
				t.PublicId = id
				t.ScopeId = id
				return &t
			}(),
			op: oplog.OpType_OP_TYPE_CREATE,
			want: oplog.Metadata{
				"resource-public-id": []string{id},
				"resource-type":      []string{"tcp target"},
				"op-type":            []string{oplog.OpType_OP_TYPE_CREATE.String()},
				"scope-id":           []string{id},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			got := target.Oplog(tt.target, tt.op)
			assert.Equal(got, tt.want)
		})
	}
}
