package agentlibrary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeApplier struct {
	applied        []*SkillDeclaration
	archivedAgents []string
	archivedSkills []string
	protected      map[string]string // "<type>:<key>" -> reason
}

func (f *fakeApplier) ApplyDeclaration(_ context.Context, d *SkillDeclaration) ([]string, error) {
	f.applied = append(f.applied, d)
	return []string{"agent:" + d.Agent.Key, "skill:" + d.Handle()}, nil
}
func (f *fakeApplier) ArchiveAgent(_ context.Context, key, _, _ string) error {
	f.archivedAgents = append(f.archivedAgents, key)
	return nil
}
func (f *fakeApplier) ArchiveSkill(_ context.Context, handle, _, _ string) error {
	f.archivedSkills = append(f.archivedSkills, handle)
	return nil
}
func (f *fakeApplier) IsProtected(_ context.Context, targetType, key string) (bool, string, error) {
	r, ok := f.protected[targetType+":"+key]
	return ok, r, nil
}

func newImporter(t *testing.T) (*Importer, *fakeApplier, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "project")
	app := &fakeApplier{protected: map[string]string{}}
	imp := NewImporter(SkillRoots{Project: root}, app)
	return imp, app, root
}

func TestWriteSkill_CreatesStandaloneFileAndIndex(t *testing.T) {
	imp, app, root := newImporter(t)
	decl := &SkillDeclaration{
		Kind: "openvibely.agent_skill", Version: 1,
		Skill: SkillBlock{Key: "verify", Scope: "project", Name: "Verify", Description: "checks changes"},
	}
	res, err := imp.WriteSkill(context.Background(), decl, "# Verify\n")
	if err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	if !res.Applied || len(res.Created) != 1 || res.Created[0] != "verify" {
		t.Fatalf("bad result: %+v", res)
	}
	path := filepath.Join(root, "skills", "verify", "SKILL.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
	if len(app.applied) != 0 {
		t.Fatalf("standalone skill writes must not mutate agent DB, got %#v", app.applied)
	}
	index, err := os.ReadFile(filepath.Join(root, "skills", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read SKILLS.md: %v", err)
	}
	if !strings.Contains(string(index), "## verify") || !strings.Contains(string(index), "[Verify](verify/SKILL.md) — checks changes") {
		t.Fatalf("standalone index missing skill link:\n%s", index)
	}

	res, err = imp.WriteSkill(context.Background(), decl, "# Verify v2\n")
	if err != nil {
		t.Fatalf("WriteSkill update: %v", err)
	}
	if len(res.Created) != 0 || len(res.Updated) != 1 || res.Updated[0] != "verify" {
		t.Fatalf("expected update, got %+v", res)
	}
}

func TestWriteAgentRootDeclaration_CreatesSkillsIndexAndCallsApplier(t *testing.T) {
	imp, app, root := newImporter(t)
	decl := &SkillDeclaration{
		Kind: "openvibely.agent_skill", Version: 1,
		Agent: AgentDeclaration{Key: "backend", Scope: "project"},
		Tools: []string{"skill_view"},
	}
	body := "# Backend Skills\n\n## backend/verify\n\n[Verify](skills/verify/SKILL.md) — checks changes.\n"
	res, err := imp.WriteAgentRootDeclaration(context.Background(), decl, body)
	if err != nil {
		t.Fatalf("WriteAgentRootDeclaration: %v", err)
	}
	if !res.Applied || len(res.Created) != 1 || res.Created[0] != "backend" {
		t.Fatalf("bad result: %+v", res)
	}
	path := filepath.Join(root, "agents", "backend", "SKILLS.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SKILLS.md: %v", err)
	}
	if !strings.Contains(string(got), "## backend/verify") || !strings.Contains(string(got), "skills/verify/SKILL.md") {
		t.Fatalf("SKILLS.md did not preserve linked index body:\n%s", got)
	}
	agentsIndex, err := os.ReadFile(filepath.Join(root, "agents", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsIndex), "## backend") || !strings.Contains(string(agentsIndex), "backend/SKILLS.md") {
		t.Fatalf("AGENTS.md missing backend entry:\n%s", agentsIndex)
	}
	if len(app.applied) != 1 || !app.applied[0].IsAgentRootDeclaration() {
		t.Fatalf("expected root declaration applied, got %#v", app.applied)
	}
}

func TestWriteAgentRootDeclaration_MergesExistingSkillLinks(t *testing.T) {
	imp, _, root := newImporter(t)
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Agent: AgentDeclaration{Key: "backend", Scope: "project"}}
	first := "# Backend Skills\n\n## backend/verify\n\n[Verify](skills/verify/SKILL.md) — checks changes.\n"
	if _, err := imp.WriteAgentRootDeclaration(context.Background(), decl, first); err != nil {
		t.Fatalf("first root declaration: %v", err)
	}
	second := "# Backend Skills\n\n## backend/review\n\n[Review](skills/review/SKILL.md) — reviews changes.\n"
	if _, err := imp.WriteAgentRootDeclaration(context.Background(), decl, second); err != nil {
		t.Fatalf("second root declaration: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "agents", "backend", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read SKILLS.md: %v", err)
	}
	text := string(got)
	for _, want := range []string{"## backend/verify", "skills/verify/SKILL.md", "## backend/review", "skills/review/SKILL.md"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged SKILLS.md missing %q:\n%s", want, text)
		}
	}
}

func TestWriteAgentRootDeclaration_UpdatesExistingAgentIndexEntry(t *testing.T) {
	imp, _, root := newImporter(t)
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Agent: AgentDeclaration{Key: "backend", Scope: "project", Name: "Backend", Description: "old"}}
	if _, err := imp.WriteAgentRootDeclaration(context.Background(), decl, "# Backend\n"); err != nil {
		t.Fatalf("first root declaration: %v", err)
	}
	decl.Agent.Name = "Backend Updated"
	decl.Agent.Description = "new"
	if _, err := imp.WriteAgentRootDeclaration(context.Background(), decl, "# Backend Updated\n"); err != nil {
		t.Fatalf("second root declaration: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "agents", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	text := string(got)
	if !strings.Contains(text, "[Backend Updated](backend/SKILLS.md) — new") || strings.Contains(text, "[Backend](backend/SKILLS.md) — old") {
		t.Fatalf("AGENTS.md did not refresh metadata:\n%s", text)
	}
}

func TestWriteSkill_RejectsAgentRootDeclaration(t *testing.T) {
	imp, _, _ := newImporter(t)
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Agent: AgentDeclaration{Key: "backend"}}
	if _, err := imp.WriteSkill(context.Background(), decl, "# Backend\n"); err == nil {
		t.Fatalf("expected WriteSkill to reject agent root declaration")
	}
}

func TestWriteSkill_BlockedByProtection(t *testing.T) {
	imp, app, _ := newImporter(t)
	app.protected["skill:verify"] = "bundled"
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "verify", Scope: "project"}}
	res, err := imp.WriteSkill(context.Background(), decl, "")
	if err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	if res.Applied {
		t.Fatalf("protection must block")
	}
	if len(res.Blocked) != 1 || res.Blocked[0] != "verify" {
		t.Fatalf("blocked list: %+v", res.Blocked)
	}
	if len(app.applied) != 0 {
		t.Fatalf("applier must not be called on block")
	}
}

func TestWriteSkill_RejectsBadScope(t *testing.T) {
	imp, _, _ := newImporter(t)
	imp.roots = SkillRoots{Global: ""}
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "b", Scope: "global"}}
	if _, err := imp.WriteSkill(context.Background(), decl, ""); err == nil {
		t.Fatalf("expected error for unconfigured scope")
	}
}

func TestWriteAgentOwnedSkill_ProjectScopeRequiresProjectRoot(t *testing.T) {
	globalRoot := t.TempDir()
	imp := NewImporter(SkillRoots{Global: globalRoot}, &fakeApplier{protected: map[string]string{}})
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "review", Name: "Review"}}
	if _, err := imp.WriteAgentOwnedSkill(context.Background(), "project", "reviewer", decl, "body"); err == nil {
		t.Fatalf("expected project-scoped agent skill write to require project root")
	}
	if _, err := os.Stat(filepath.Join(globalRoot, "agents", "reviewer", "skills", "review", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("project write must not fall back to global root, stat err=%v", err)
	}
}

func TestWriteAgentOwnedSkill_EmptyScopeUsesDeclarationScope(t *testing.T) {
	globalRoot := t.TempDir()
	projectRoot := t.TempDir()
	imp := NewImporter(SkillRoots{Global: globalRoot, Project: projectRoot}, &fakeApplier{protected: map[string]string{}})
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "review", Scope: "global", Name: "Review"}}
	if _, err := imp.WriteAgentOwnedSkill(context.Background(), "", "reviewer", decl, "body"); err != nil {
		t.Fatalf("WriteAgentOwnedSkill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalRoot, "agents", "reviewer", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("expected global agent skill file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "agents", "reviewer", "skills", "review", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("empty scope with global declaration must not write project file, stat err=%v", err)
	}
}

func TestWriteAgentOwnedSkill_EmptyScopeWithoutDeclarationIsRejected(t *testing.T) {
	imp := NewImporter(SkillRoots{Global: t.TempDir(), Project: t.TempDir()}, &fakeApplier{protected: map[string]string{}})
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "review", Name: "Review"}}
	if _, err := imp.WriteAgentOwnedSkill(context.Background(), "", "reviewer", decl, "body"); err == nil {
		t.Fatal("expected empty agent-owned skill scope without declaration scope to be rejected")
	}
}

func TestWriteAgentOwnedSupportFile_ProjectScopeRequiresProjectRoot(t *testing.T) {
	globalRoot := t.TempDir()
	imp := NewImporter(SkillRoots{Global: globalRoot}, &fakeApplier{protected: map[string]string{}})
	if _, err := imp.WriteAgentOwnedSupportFile(context.Background(), "project", "reviewer", "review", SupportReferences, "note.md", []byte("x")); err == nil {
		t.Fatalf("expected project-scoped agent support write to require project root")
	}
	if _, err := os.Stat(filepath.Join(globalRoot, "agents", "reviewer", "skills", "review", "references", "note.md")); !os.IsNotExist(err) {
		t.Fatalf("project support write must not fall back to global root, stat err=%v", err)
	}
}

func TestWriteAgentOwnedSupportFile_EmptyScopeIsRejected(t *testing.T) {
	imp := NewImporter(SkillRoots{Global: t.TempDir(), Project: t.TempDir()}, &fakeApplier{protected: map[string]string{}})
	if _, err := imp.WriteAgentOwnedSupportFile(context.Background(), "", "reviewer", "review", SupportReferences, "note.md", []byte("x")); err == nil {
		t.Fatal("expected empty agent-owned support scope to be rejected")
	}
}

func TestWriteSupportFile_AcceptsAllowedKinds(t *testing.T) {
	imp, _, root := newImporter(t)
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "verify", Scope: "project"}}
	if _, err := imp.WriteSkill(context.Background(), decl, ""); err != nil {
		t.Fatal(err)
	}
	res, err := imp.WriteSupportFile(context.Background(), "project", "verify", SupportAssets, "example.json", []byte("hello"))
	if err != nil {
		t.Fatalf("WriteSupportFile: %v", err)
	}
	if !res.Applied || len(res.Created) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(root, "skills", "verify", "assets", "example.json"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file mismatch: %v / %q", err, got)
	}
}

func TestWriteSupportFile_RejectsTraversal(t *testing.T) {
	imp, _, _ := newImporter(t)
	if _, err := imp.WriteSupportFile(context.Background(), "project", "verify", SupportReferences, "../outside.md", []byte("x")); err == nil {
		t.Fatalf("expected traversal rejection")
	}
}

func TestWriteSupportFile_RejectsUnknownKind(t *testing.T) {
	imp, _, _ := newImporter(t)
	if _, err := imp.WriteSupportFile(context.Background(), "project", "verify", SupportFileKind("docs"), "x.md", []byte("y")); err == nil {
		t.Fatalf("expected unknown kind rejection")
	}
}

func TestRemoveSupportFile_RemovesAllowedKind(t *testing.T) {
	imp, _, root := newImporter(t)
	decl := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "verify", Scope: "project"}}
	if _, err := imp.WriteSkill(context.Background(), decl, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := imp.WriteSupportFile(context.Background(), "project", "verify", SupportTemplates, "plan.md", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	res, err := imp.RemoveSupportFile(context.Background(), "project", "verify", SupportTemplates, "plan.md")
	if err != nil {
		t.Fatalf("RemoveSupportFile: %v", err)
	}
	if !res.Applied || len(res.Archived) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "verify", "templates", "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("support file should be removed, stat err=%v", err)
	}
}

func TestArchiveSkill_BlockedWhenProtected(t *testing.T) {
	imp, app, _ := newImporter(t)
	app.protected["skill:verify"] = "bundled"
	res, err := imp.ArchiveSkill(context.Background(), "verify", "", "")
	if err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}
	if res.Applied || len(res.Blocked) != 1 {
		t.Fatalf("must be blocked, got %+v", res)
	}
}

func TestArchiveSkill_MarksSkillFileAndRemovesStandaloneIndexLink(t *testing.T) {
	imp, app, root := newImporter(t)
	verify := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "verify", Scope: "project", Name: "Verify", Description: "checks changes"}}
	if _, err := imp.WriteSkill(context.Background(), verify, "# Verify\n"); err != nil {
		t.Fatalf("write verify: %v", err)
	}
	review := &SkillDeclaration{Kind: "openvibely.agent_skill", Version: 1, Skill: SkillBlock{Key: "review", Scope: "project", Name: "Review", Description: "reviews changes"}}
	if _, err := imp.WriteSkill(context.Background(), review, "# Review\n"); err != nil {
		t.Fatalf("write review: %v", err)
	}
	res, err := imp.ArchiveSkill(context.Background(), "verify", "review", "consolidated")
	if err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}
	if !res.Applied || len(res.Archived) != 1 || len(app.archivedSkills) != 1 {
		t.Fatalf("bad archive result/apply: %+v app=%+v", res, app.archivedSkills)
	}
	rootIndex, err := os.ReadFile(filepath.Join(root, "skills", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read root index: %v", err)
	}
	if strings.Contains(string(rootIndex), "## verify") || !strings.Contains(string(rootIndex), "## review") {
		t.Fatalf("root index not consolidated:\n%s", rootIndex)
	}
	skillFile, err := os.ReadFile(filepath.Join(root, "skills", "verify", "SKILL.md"))
	if err != nil {
		t.Fatalf("read archived skill: %v", err)
	}
	for _, want := range []string{"enabled: false", "archived: true", "absorbed_into: review", "archive_reason: consolidated"} {
		if !strings.Contains(string(skillFile), want) {
			t.Fatalf("archived skill missing %q:\n%s", want, skillFile)
		}
	}
}

func TestArchiveAgent_PassesThrough(t *testing.T) {
	imp, app, _ := newImporter(t)
	res, err := imp.ArchiveAgent(context.Background(), "backend", "platform", "merged")
	if err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}
	if !res.Applied || len(res.Archived) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	if len(app.archivedAgents) != 1 || app.archivedAgents[0] != "backend" {
		t.Fatalf("applier not called: %v", app.archivedAgents)
	}
}
