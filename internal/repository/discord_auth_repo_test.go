package repository

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscordAuthRepo_CRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewDiscordAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Discord Auth Project"}
	require.NoError(t, projectRepo.Create(ctx, project))

	users, err := repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, users, 0)

	user := &models.DiscordAuthorizedUser{
		ProjectID:     project.ID,
		DiscordUserID: "12345",
		DisplayName:   "Alice",
		AddedBy:       "web",
	}
	require.NoError(t, repo.Create(ctx, user))
	require.NotEmpty(t, user.ID)
	require.False(t, user.AddedAt.IsZero())

	loaded, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "12345", loaded.DiscordUserID)
	assert.Equal(t, "Alice", loaded.DisplayName)

	users, err = repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, users, 1)

	require.NoError(t, repo.Delete(ctx, user.ID))
	users, err = repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, users, 0)

	require.Error(t, repo.Delete(ctx, "missing-id"))
}

func TestDiscordAuthRepo_DeleteByProject(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewDiscordAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Discord Delete Project"}
	otherProject := &models.Project{Name: "Discord Keep Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, projectRepo.Create(ctx, otherProject))

	require.NoError(t, repo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "111", DisplayName: "A", AddedBy: "test"}))
	require.NoError(t, repo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "222", DisplayName: "B", AddedBy: "test"}))
	require.NoError(t, repo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: otherProject.ID, DiscordUserID: "333", DisplayName: "C", AddedBy: "test"}))

	require.NoError(t, repo.DeleteByProject(ctx, project.ID))

	users, err := repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	assert.Len(t, users, 0)
	otherUsers, err := repo.ListByProject(ctx, otherProject.ID)
	require.NoError(t, err)
	assert.Len(t, otherUsers, 1)
}

func TestDiscordAuthRepo_AuthorizationChecks(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewDiscordAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Discord Auth Check"}
	require.NoError(t, projectRepo.Create(ctx, project))

	hasAny, err := repo.HasAnyAuthorizedUsers(ctx, project.ID)
	require.NoError(t, err)
	assert.False(t, hasAny)

	hasAnyAnywhere, err := repo.HasAnyAuthorizedUsersAnywhere(ctx)
	require.NoError(t, err)
	assert.False(t, hasAnyAnywhere)

	authorized, err := repo.IsAuthorized(ctx, project.ID, "999")
	require.NoError(t, err)
	assert.False(t, authorized)

	authorizedAnywhere, err := repo.IsAuthorizedAnywhere(ctx, "999")
	require.NoError(t, err)
	assert.False(t, authorizedAnywhere)

	require.NoError(t, repo.Create(ctx, &models.DiscordAuthorizedUser{
		ProjectID:     project.ID,
		DiscordUserID: "111",
		DisplayName:   "Allowed",
		AddedBy:       "test",
	}))

	hasAny, err = repo.HasAnyAuthorizedUsers(ctx, project.ID)
	require.NoError(t, err)
	assert.True(t, hasAny)

	hasAnyAnywhere, err = repo.HasAnyAuthorizedUsersAnywhere(ctx)
	require.NoError(t, err)
	assert.True(t, hasAnyAnywhere)

	authorized, err = repo.IsAuthorized(ctx, project.ID, "111")
	require.NoError(t, err)
	assert.True(t, authorized)

	authorized, err = repo.IsAuthorized(ctx, project.ID, "222")
	require.NoError(t, err)
	assert.False(t, authorized)

	authorizedAnywhere, err = repo.IsAuthorizedAnywhere(ctx, "111")
	require.NoError(t, err)
	assert.True(t, authorizedAnywhere)
}

func TestDiscordAuthRepo_UniqueConstraint(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewDiscordAuthRepo(db)
	projectRepo := NewProjectRepo(db)
	ctx := context.Background()

	project := &models.Project{Name: "Discord Unique"}
	require.NoError(t, projectRepo.Create(ctx, project))

	require.NoError(t, repo.Create(ctx, &models.DiscordAuthorizedUser{
		ProjectID:     project.ID,
		DiscordUserID: "dup",
		DisplayName:   "Original",
		AddedBy:       "test",
	}))

	err := repo.Create(ctx, &models.DiscordAuthorizedUser{
		ProjectID:     project.ID,
		DiscordUserID: "dup",
		DisplayName:   "Duplicate",
		AddedBy:       "test",
	})
	require.Error(t, err)
}
