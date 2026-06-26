package repository

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/require"
)

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
