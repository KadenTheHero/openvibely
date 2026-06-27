package repository

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestChannelTargetRepo_ReplaceProjectTargetsDeletesRemovedRowsBeforeInsert(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := NewProjectRepo(db)
	project := &models.Project{Name: "Replace Targets Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	repo := NewChannelTargetRepo(db)

	first := models.ChannelTarget{ID: "target-keep", ProjectID: project.ID, Platform: "email", Name: "keep", TargetID: "keep@example.com"}
	removed := models.ChannelTarget{ID: "target-removed", ProjectID: project.ID, Platform: "email", TargetID: "restore@example.com"}
	require.NoError(t, repo.ReplaceProjectTargets(ctx, project.ID, []models.ChannelTarget{first, removed}))
	require.NoError(t, repo.ReplaceProjectTargets(ctx, project.ID, []models.ChannelTarget{first}))

	readded := models.ChannelTarget{ID: "target-readded", ProjectID: project.ID, Platform: "email", TargetID: "restore@example.com"}
	require.NoError(t, repo.ReplaceProjectTargets(ctx, project.ID, []models.ChannelTarget{first, readded}))

	targets, err := repo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, targets, 2)
	foundReadded, err := repo.GetByID(ctx, "target-readded")
	require.NoError(t, err)
	require.NotNil(t, foundReadded)
	require.Equal(t, "restore@example.com", foundReadded.TargetID)
}

func TestChannelTargetRepo_CRUDAndAudit(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := NewProjectRepo(db)
	project := &models.Project{Name: "Targets Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	repo := NewChannelTargetRepo(db)

	require.NoError(t, repo.Upsert(ctx, models.ChannelTarget{ID: "target-1", ProjectID: project.ID, Platform: "Slack", Name: "Ops", TargetID: "C123", ThreadID: "169.1", Home: true}))
	home, err := repo.FindHome(ctx, project.ID, "slack")
	require.NoError(t, err)
	require.NotNil(t, home)
	require.Equal(t, "ops", home.Name)
	named, err := repo.FindByName(ctx, project.ID, "slack", "ops")
	require.NoError(t, err)
	require.Equal(t, "C123", named.TargetID)
	byTarget, err := repo.FindByTarget(ctx, project.ID, "slack", "C123", "169.1")
	require.NoError(t, err)
	require.NotNil(t, byTarget)

	require.NoError(t, repo.RecordSend(ctx, models.ChannelMessageSend{ID: "send-1", ProjectID: project.ID, Platform: "slack", TargetID: "C123", ThreadID: "169.1", RequestedBySurface: "web", MessagePreview: "hello", Success: true}))
	sends, err := repo.ListSendsByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, sends, 1)
	require.Equal(t, "web", sends[0].RequestedBySurface)

	require.NoError(t, repo.Delete(ctx, "target-1"))
	missing, err := repo.FindByName(ctx, project.ID, "slack", "ops")
	require.NoError(t, err)
	require.Nil(t, missing)
}
