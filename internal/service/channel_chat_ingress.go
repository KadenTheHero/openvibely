package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

type channelChatAttachmentLinkOptions struct {
	Platform     string
	UploadsDir   string
	Repo         *repository.ChatAttachmentRepo
	FallbackName string
}

func channelChatAttachmentContextAndImages(chatAttachments []models.ChatAttachment, maxTextFileSize int64) (string, []models.Attachment) {
	var imageAttachments []models.Attachment
	var attachmentContents []string
	for _, att := range chatAttachments {
		if isSlackImageFile(att.MediaType) {
			imageAttachments = append(imageAttachments, models.Attachment{
				FileName:  att.FileName,
				FilePath:  att.FilePath,
				MediaType: att.MediaType,
				FileSize:  att.FileSize,
			})
			continue
		}
		if att.FileSize <= maxTextFileSize {
			content, readErr := os.ReadFile(att.FilePath)
			if readErr == nil {
				attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s\n```\n%s\n```\n", att.FileName, string(content)))
				continue
			}
		}
		attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s (attached, %d bytes - too large to include inline)\n", att.FileName, att.FileSize))
	}
	attachmentContext := ""
	if len(attachmentContents) > 0 {
		attachmentContext = "\n\n--- Attached Files ---\n" + strings.Join(attachmentContents, "")
	}
	return attachmentContext, imageAttachments
}

func saveChannelChatAttachmentsToPendingSession(uploadsDir, fallbackName string, attachments []models.ChatAttachment) (string, error) {
	if len(attachments) == 0 {
		return "", nil
	}
	sessionID := generateSlackPendingSessionID()
	sessionDir := filepath.Join(uploadsDir, "chat", "pending", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return "", fmt.Errorf("creating pending upload directory: %w", err)
	}
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		fileName := safeChannelChatAttachmentFileName(att.FileName, fallbackName)
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(sessionDir, fmt.Sprintf("%s_%s", generateSlackPendingSessionID()[:8], fileName))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			_ = os.RemoveAll(sessionDir)
			cleanupChannelChatAttachmentDirs(cleanupDirs)
			return "", fmt.Errorf("staging %s: %w", fileName, err)
		}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
	return sessionID, nil
}

func linkChannelChatAttachmentsToExecution(ctx context.Context, execID string, attachments []models.ChatAttachment, opts channelChatAttachmentLinkOptions) ([]models.ChatAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = "channel"
	}
	if opts.Repo == nil {
		cleanupChannelChatAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("chat attachment repository is unavailable")
	}
	displayPlatform := channelChatAttachmentDisplayPlatform(platform)
	execDir := filepath.Join(opts.UploadsDir, "chat", execID)
	if err := os.MkdirAll(execDir, 0755); err != nil {
		applog.Infof("[%s] error creating exec dir %s: %v", platform, execDir, err)
		cleanupChannelChatAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("storing %s attachment: %w", displayPlatform, err)
	}
	cleanupDirs := make(map[string]struct{})
	linked := make([]models.ChatAttachment, 0, len(attachments))
	var linkErrs []string
	for i := range attachments {
		att := &attachments[i]
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(execDir, uniqueSlackTempFilename(execDir, safeChannelChatAttachmentFileName(att.FileName, opts.FallbackName)))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			applog.Infof("[%s] error moving attachment file=%s: %v", platform, att.FileName, err)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
			continue
		}
		absPath, err := filepath.Abs(destPath)
		if err != nil {
			absPath = destPath
		}
		att.FilePath = absPath
		att.ExecutionID = execID
		if err := opts.Repo.Create(ctx, att); err != nil {
			applog.Infof("[%s] error creating chat attachment record: %v", platform, err)
			_ = os.Remove(destPath)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
		} else {
			linked = append(linked, *att)
			applog.Infof("[%s] linked attachment id=%s file=%s to exec=%s", platform, att.ID, att.FileName, execID)
		}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
	if len(linkErrs) > 0 {
		cleanupLinkedChannelChatAttachments(ctx, opts.Repo, platform, linked)
		return nil, fmt.Errorf("storing %s attachment failed for %d of %d file(s): %s", displayPlatform, len(linkErrs), len(attachments), strings.Join(linkErrs, "; "))
	}
	return linked, nil
}

func cleanupLinkedChannelChatAttachments(ctx context.Context, repo *repository.ChatAttachmentRepo, platform string, attachments []models.ChatAttachment) {
	for _, att := range attachments {
		if strings.TrimSpace(att.ID) != "" && repo != nil {
			if err := repo.Delete(ctx, att.ID); err != nil {
				applog.Infof("[%s] error deleting partial chat attachment record id=%s: %v", platform, att.ID, err)
			}
		}
		if strings.TrimSpace(att.FilePath) != "" {
			if err := os.Remove(att.FilePath); err != nil && !os.IsNotExist(err) {
				applog.Infof("[%s] error deleting partial chat attachment file=%s: %v", platform, att.FilePath, err)
			}
		}
	}
}

func cleanupChannelChatAttachmentSourceDirs(attachments []models.ChatAttachment) {
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		if strings.TrimSpace(att.FilePath) == "" {
			continue
		}
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
}

func cleanupChannelChatAttachmentDirs(dirs map[string]struct{}) {
	for dir := range dirs {
		_ = os.RemoveAll(dir)
	}
}

func safeChannelChatAttachmentFileName(name, fallbackName string) string {
	fileName := filepath.Base(name)
	if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
		fileName = strings.TrimSpace(fallbackName)
		if fileName == "" {
			fileName = "channel-attachment"
		}
	}
	return fileName
}

func channelChatAttachmentDisplayPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "Channel"
	}
	return strings.ToUpper(platform[:1]) + platform[1:]
}
