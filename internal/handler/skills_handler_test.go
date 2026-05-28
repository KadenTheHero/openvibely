package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/agentlibrary"
)

func TestSkillsPageListsGlobalAndProjectStandaloneSkillCards(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	globalRoot := t.TempDir()
	projectRepoPath := t.TempDir()
	h.SetAgentSkillRoot(globalRoot)
	project := createProject(t, h, "Skills Project")
	project.RepoPath = projectRepoPath
	if err := h.projectRepo.Update(context.Background(), project); err != nil {
		t.Fatalf("update project repo path: %v", err)
	}

	writeStandaloneSkill(t, globalRoot, "global_review", "Global Review", "global description", "global")
	writeStandaloneSkill(t, filepath.Join(projectRepoPath, ".openvibely"), "project_review", "Project Review", "project description", "project")

	req := httptest.NewRequest(http.MethodGet, "/skills?project_id="+project.ID, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Skills", "Search skills...", "global_review", "project_review", `data-search-card`, `data-skill-scope="global"`, `data-skill-scope="project"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
	for _, unwanted := range []string{"Standalone project and global skills available for skill routing.", "project skill · project_review", "global skill · global_review"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("expected body not to contain %q", unwanted)
		}
	}
}

func TestSkillsPageHeaderUsesAddSkillDropdownMenu(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	h.SetAgentSkillRoot(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`class="btn btn-primary btn-sm"`,
		`+ Add Skill`,
		`Create Skill`,
		`openNewSkillModal()`,
		`Import Skill Package`,
		`openImportSkillModal()`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
	if strings.Contains(body, `class="btn btn-ghost btn-sm"`) {
		t.Fatalf("expected skills header not to use a separate kebab button")
	}
}

func TestSkillsPageCardsIncludeKebabEditAndDeleteActions(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)
	writeStandaloneSkill(t, root, "global_review", "Global Review", "global description", "global")

	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`class="dropdown dropdown-end"`,
		`editSkillFromData(this.closest('[data-skill-handle]'))`,
		`deleteSkill(this)`,
		`data-skill-scope="global"`,
		`Delete`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestSkillsPageNewSkillModalIncludesFrontmatterTemplate(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	h.SetAgentSkillRoot(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`kind: openvibely.agent_skill`,
		`version: 1`,
		`skill:`,
		`key: openvibely_database_migration_workflow`,
		`scope: project`,
		`description: Manage OpenVibely goose schema migrations`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestSkillsModalDisablesScopeWhenEditing(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	h.SetAgentSkillRoot(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`document.getElementById('skill_scope').disabled = false`,
		`document.getElementById('skill_scope').disabled = true`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestSkillsPageEditModalIncludesPackageFileList(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)
	writeStandaloneSkill(t, root, "global_review", "Global Review", "global description", "global")
	refPath := filepath.Join(root, "skills", "global_review", "references", "notes.md")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("mkdir support dir: %v", err)
	}
	if err := os.WriteFile(refPath, []byte("notes"), 0o644); err != nil {
		t.Fatalf("write support file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-skill-files="references/notes.md"`, `id="skill_files"`, `Package files`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestDeleteSkillRemovesStandaloneSkillAndReturnsCards(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)
	writeStandaloneSkill(t, root, "debug_tests", "Debug Tests", "Find and fix tests", "global")

	req := httptest.NewRequest(http.MethodDelete, "/skills/debug_tests?scope=global", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "debug_tests")); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory to be removed, stat err=%v", err)
	}
	if strings.Contains(rec.Body.String(), "Debug Tests") {
		t.Fatalf("expected response to omit deleted skill card")
	}
	index, err := os.ReadFile(filepath.Join(root, "skills", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read skill index: %v", err)
	}
	if strings.Contains(string(index), "debug_tests") {
		t.Fatalf("expected skill index to omit deleted skill, got:\n%s", index)
	}
}

func TestImportSkillPackageWritesSkillAndSupportFiles(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("scope", "global"); err != nil {
		t.Fatalf("write scope: %v", err)
	}
	addMultipartFile(t, writer, "files", "SKILL.md", `---
kind: openvibely.agent_skill
version: 1
skill:
    key: imported_skill
    name: Imported Skill
    scope: project
    description: Imported from disk.
---
Use this imported skill.
`)
	if err := writer.WriteField("paths", "imported_skill/SKILL.md"); err != nil {
		t.Fatalf("write skill path: %v", err)
	}
	addMultipartFile(t, writer, "files", "guide.md", "# Guide\n")
	if err := writer.WriteField("paths", "imported_skill/references/guide.md"); err != nil {
		t.Fatalf("write support path: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/skills/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	skillPath := filepath.Join(root, "skills", "imported_skill", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read imported skill: %v", err)
	}
	content := string(data)
	for _, want := range []string{"key: imported_skill", "scope: global", "Use this imported skill."} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected imported skill to contain %q; got:\n%s", want, content)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", "imported_skill", "references", "guide.md")); err != nil || string(got) != "# Guide\n" {
		t.Fatalf("expected support file to be imported, got %q err=%v", got, err)
	}
	index, err := os.ReadFile(filepath.Join(root, "skills", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read skills index: %v", err)
	}
	if !strings.Contains(string(index), "imported_skill") {
		t.Fatalf("expected index to include imported skill, got:\n%s", index)
	}
	if !strings.Contains(rec.Body.String(), "Imported Skill") {
		t.Fatalf("expected response to include imported skill card")
	}
}

func TestImportSkillPackageAcceptsStandardSkillFormat(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("scope", "global"); err != nil {
		t.Fatalf("write scope: %v", err)
	}
	addMultipartFile(t, writer, "files", "SKILL.md", `---
name: skill-creator
description: Create new skills, modify and improve existing skills, and measure skill performance.
---

# Skill Creator

A skill for creating new skills and iteratively improving them.
`)
	if err := writer.WriteField("paths", "skill-creator/SKILL.md"); err != nil {
		t.Fatalf("write skill path: %v", err)
	}
	addMultipartFile(t, writer, "files", "guide.md", "# Guide\n")
	if err := writer.WriteField("paths", "skill-creator/references/guide.md"); err != nil {
		t.Fatalf("write support path: %v", err)
	}
	addMultipartFile(t, writer, "files", "generate_review.py", "print('review')\n")
	if err := writer.WriteField("paths", "skill-creator/eval-viewer/generate_review.py"); err != nil {
		t.Fatalf("write eval viewer path: %v", err)
	}
	addMultipartFile(t, writer, "files", "LICENSE.txt", "MIT\n")
	if err := writer.WriteField("paths", "skill-creator/LICENSE.txt"); err != nil {
		t.Fatalf("write license path: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/skills/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(root, "skills", "skill-creator", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read imported skill: %v", err)
	}
	content := string(data)
	for _, want := range []string{"kind: openvibely.agent_skill", "key: skill-creator", "name: skill-creator", "scope: global", "# Skill Creator"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected imported standard skill to contain %q; got:\n%s", want, content)
		}
	}
	for path, want := range map[string]string{
		"references/guide.md":            "# Guide\n",
		"eval-viewer/generate_review.py": "print('review')\n",
		"LICENSE.txt":                    "MIT\n",
	} {
		got, err := os.ReadFile(filepath.Join(root, "skills", "skill-creator", filepath.FromSlash(path)))
		if err != nil || string(got) != want {
			t.Fatalf("expected package file %s to be imported, got %q err=%v", path, got, err)
		}
	}
	if !strings.Contains(rec.Body.String(), "skill-creator") {
		t.Fatalf("expected response to include imported standard skill card")
	}
}

func TestCreateSkillWritesStandaloneSkillAndReturnsCards(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)

	payload := skillSaveRequest{
		Handle:      "debug_tests",
		Name:        "Debug Tests",
		Description: "Find and fix test failures",
		Scope:       "global",
		Body:        "Run the focused failure first, then the package test.",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/skills?project_id=default", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(root, "skills", "debug_tests", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	content := string(data)
	for _, want := range []string{"key: debug_tests", "name: Debug Tests", "scope: global", "Run the focused failure"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected skill file to contain %q; got %s", want, content)
		}
	}
	if !strings.Contains(rec.Body.String(), "debug_tests") {
		t.Fatalf("expected response to include new skill card")
	}
}

func addMultipartFile(t *testing.T, writer *multipart.Writer, fieldName, filename, content string) {
	t.Helper()
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create multipart file %s: %v", filename, err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart file %s: %v", filename, err)
	}
}

func writeStandaloneSkill(t *testing.T, root, handle, name, description, scope string) {
	t.Helper()
	enabled := true
	decl := &agentlibrary.SkillDeclaration{
		Kind:    "openvibely.agent_skill",
		Version: 1,
		Skill: agentlibrary.SkillBlock{
			Key:         handle,
			Name:        name,
			Scope:       scope,
			Description: description,
			Enabled:     &enabled,
		},
	}
	importer := agentlibrary.NewImporter(agentlibrary.SkillRoots{Global: root, Project: root}, nil)
	if _, err := importer.WriteSkill(context.Background(), decl, "Use this skill when appropriate."); err != nil {
		t.Fatalf("write skill %s: %v", handle, err)
	}
}
