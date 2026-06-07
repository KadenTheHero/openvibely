package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)


func TestUploadAttachment_TaskNotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/tasks/nonexistent-task/attachments").Execute()
	tc.Assert(rec).StatusCode(http.StatusNotFound)
}

func TestUploadAttachment_NoFiles(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/tasks/"+task.ID+"/attachments", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	tc.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAttachment_Success(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	tmpDir := t.TempDir()
	origDir := uploadsDir
	SetUploadsDir(tmpDir)
	t.Cleanup(func() { SetUploadsDir(origDir) })

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write([]byte("hello world"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/tasks/"+task.ID+"/attachments", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	tc.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteAttachment_NotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Delete("/attachments/nonexistent-attachment").Execute()
	tc.Assert(rec).StatusCode(http.StatusNotFound)
}

func TestDeleteAttachment_Success(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	tmpDir := t.TempDir()
	origDir := uploadsDir
	SetUploadsDir(tmpDir)
	t.Cleanup(func() { SetUploadsDir(origDir) })

	attachment := &models.Attachment{
		TaskID:    task.ID,
		FileName:  "test.txt",
		FilePath:  filepath.Join(tmpDir, "test.txt"),
		MediaType: "text/plain",
		FileSize:  4,
	}
	if err := tc.attachmentRepo.Create(context.Background(), attachment); err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	rec := tc.HTTP().Delete("/attachments/" + attachment.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}
