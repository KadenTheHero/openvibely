package repository

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailAuthRepo_CRUDAuthorizationAndNormalization(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewEmailAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Email Auth Project"}
	require.NoError(t, projectRepo.Create(ctx, project))

	hasAny, err := repo.HasAnyAuthorizedUsers(ctx, project.ID)
	require.NoError(t, err)
	assert.False(t, hasAny)

	sender := &models.EmailAuthorizedSender{ProjectID: project.ID, EmailAddress: " Alice@Example.COM ", DisplayName: "Alice", AddedBy: "test"}
	require.NoError(t, repo.Create(ctx, sender))
	require.NotEmpty(t, sender.ID)
	require.Equal(t, "alice@example.com", sender.EmailAddress)

	loaded, err := repo.GetByID(ctx, sender.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "alice@example.com", loaded.EmailAddress)

	ok, err := repo.IsAuthorized(ctx, project.ID, "ALICE@example.com")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = repo.IsAuthorizedAnywhere(ctx, "alice@EXAMPLE.com")
	require.NoError(t, err)
	assert.True(t, ok)

	hasAny, err = repo.HasAnyAuthorizedUsers(ctx, project.ID)
	require.NoError(t, err)
	assert.True(t, hasAny)

	hasAnyAnywhere, err := repo.HasAnyAuthorizedUsersAnywhere(ctx)
	require.NoError(t, err)
	assert.True(t, hasAnyAnywhere)

	senders, err := repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, senders, 1)

	require.NoError(t, repo.Delete(ctx, sender.ID))
	senders, err = repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	assert.Empty(t, senders)
}

func TestEmailAuthRepo_DuplicateAddressRejectedCaseInsensitive(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewEmailAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Email Unique"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, repo.Create(ctx, &models.EmailAuthorizedSender{ProjectID: project.ID, EmailAddress: "a@example.com", AddedBy: "test"}))
	err := repo.Create(ctx, &models.EmailAuthorizedSender{ProjectID: project.ID, EmailAddress: "A@EXAMPLE.com", AddedBy: "test"})
	require.Error(t, err)
}
