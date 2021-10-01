package target

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/boundary/internal/db"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
)

func testTargetName(t *testing.T, scopeId string) string {
	t.Helper()
	return fmt.Sprintf("%s-%s", scopeId, testId(t))
}

func testId(t *testing.T) string {
	t.Helper()
	id, err := uuid.GenerateUUID()
	require.NoError(t, err)
	return id
}

// TestCredentialLibrary creates a CredentialLibrary for targetId and
// libraryId.
func TestCredentialLibrary(t *testing.T, conn *db.DB, targetId, libraryId string) *CredentialLibrary {
	t.Helper()
	require := require.New(t)
	rw := db.New(conn)
	lib, err := NewCredentialLibrary(targetId, libraryId)
	require.NoError(err)
	err = rw.Create(context.Background(), lib)
	require.NoError(err)
	return lib
}
