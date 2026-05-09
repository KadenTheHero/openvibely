package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/memory"
)

type MemoryRecallQuery struct {
	Surface       string
	Title         string
	Prompt        string
	RecentContext string
	AgentContext  string
}

type memoryRecallSelection struct {
	SelectedMemories []string `json:"selected_memories"`
}

type memoryRecallSynthesis struct {
	RelevantFacts []string `json:"relevant_facts"`
	CitedMemories []string `json:"cited_memories"`
}

const memoryRecallSelectionPromptTemplate = `You are selecting memories that will be useful to OpenVibely as it processes a user's query. The first section lists the available memory files with their filenames and descriptions; the final section contains the current query.

Return JSON only in this shape:
{"selected_memories":["filename.md"]}

Return a list of filenames for the memories that will clearly be useful to OpenVibely as it processes the user's query (up to 5). Only include memories that you are certain will be helpful based on their name and description.
- If you are unsure if a memory will be useful in processing the user's query, then do not include it in your list. Be selective and discerning.
- If there are no memories in the list that would clearly be useful, return an empty list.
- Be especially conservative with user-profile and project-overview memories ([user], [project]). These describe the user's ongoing focus, not what every question is about. Match on what the query IS ABOUT, not on surface keyword overlap.
- Do not include MEMORY.md in selected_memories; it is the index.

Available memory files:

%s

Memory index:

%s

Select memories relevant to:
%s
`

const memoryRecallSynthesisPromptTemplate = `You read persistent memory files for an AI coding assistant and extract facts to help the coding assistant answer queries. The first section lists every selected memory file with its frontmatter and full body; the final section contains the current query.

Return JSON only in this shape:
{"relevant_facts":["fact"],"cited_memories":["filename.md"]}

For the query, return a JSON object:
- relevant_facts: an array of facts (max 7) that would be useful for processing the query. Each fact is 1-2 sentences and stands on its own.
- cited_memories: array of filenames (matching the selected memory files exactly) for the memories you drew from.

If no memories are relevant, return relevant_facts: [] and cited_memories: [].

A fact is useful when it lets the assistant do one of these things:
- Avoid re-asking: supply something the user would otherwise have to restate (a path, a name, a config value, a decision already made).
- Apply user preferences: surface conventions, styles, or tooling choices the assistant should follow for this query.
- Maintain continuity: surface the state of an ongoing project, goal, or prior thread that this query is continuing.
- Avoid a known pitfall: surface past corrections or mistakes so the assistant pre-empts repeating them.

Style and length:
- Each fact is 1-2 sentences. State the fact directly, then add the context needed to act on it.
- Name a path, flag, or identifier only when it is the thing the assistant must use or avoid. Drop supporting details like timestamps, byte counts, version numbers, and historical asides.
- Do not answer or solve the query yourself. You are a retrieval step, not the assistant: every fact must be lifted from a memory file body, not derived from general knowledge or your own reasoning about the query. If no memory covers it, return relevant_facts: [].
- Do not restate the query.

Selected memory files:

%s

Extract facts relevant to:
%s
`

func (s *MemoryService) RecallContext(ctx context.Context, projectID string, q MemoryRecallQuery) string {
	enabled, err := s.IsEnabled(ctx, projectID)
	if err != nil {
		log.Printf("memory: recall: settings lookup failed for %s: %v", projectID, err)
		return ""
	}
	if !enabled {
		return ""
	}
	if _, err := s.ensureProjectMemoryDir(ctx, projectID); err != nil {
		log.Printf("memory: recall: ensure project failed for %s: %v", projectID, err)
		return ""
	}
	agent, ok, err := s.resolveMemoryAgent(ctx, projectID)
	if err != nil {
		log.Printf("memory: recall: resolve model failed for %s: %v", projectID, err)
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	if !ok || agent == nil {
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	index, files, err := s.memoryManifest(projectID)
	if err != nil {
		log.Printf("memory: recall: manifest failed for %s: %v", projectID, err)
		return ""
	}
	if strings.TrimSpace(index) == "" && len(files) == 0 {
		return ""
	}
	projectDir, err := s.ProjectDir(projectID)
	if err != nil {
		log.Printf("memory: recall: project dir failed for %s: %v", projectID, err)
		return ""
	}
	selectionPrompt := buildMemoryRecallSelectionPrompt(q, index, files)
	selectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	selectionOut, _, err := s.modelCaller.CallAgentDirect(selectCtx, selectionPrompt, nil, *agent, projectDir)
	cancel()
	if err != nil {
		log.Printf("memory: recall: selection model failed for %s: %v", projectID, err)
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	var selection memoryRecallSelection
	if err := unmarshalModelJSON(selectionOut, &selection); err != nil {
		log.Printf("memory: recall: selection parse failed for %s: %v", projectID, err)
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	selected := filterSelectedMemoryFiles(selection.SelectedMemories, files)
	if len(selected) == 0 {
		return ""
	}
	synthesisPrompt := buildMemoryRecallSynthesisPrompt(q, selected)
	synthCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	synthesisOut, _, err := s.modelCaller.CallAgentDirect(synthCtx, synthesisPrompt, nil, *agent, projectDir)
	cancel()
	if err != nil {
		log.Printf("memory: recall: synthesis model failed for %s: %v", projectID, err)
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	var synthesis memoryRecallSynthesis
	if err := unmarshalModelJSON(synthesisOut, &synthesis); err != nil {
		log.Printf("memory: recall: synthesis parse failed for %s: %v", projectID, err)
		return s.BuildContext(ctx, projectID, memory.ContextOptions{IncludeIndex: true, Keywords: []string{q.Title, q.Prompt, q.RecentContext}})
	}
	return renderSynthesizedMemoryContext(synthesis)
}

func (s *MemoryService) memoryManifest(projectID string) (string, []memory.MemoryFile, error) {
	index, err := s.store.ReadIndex(projectID)
	if err != nil {
		return "", nil, err
	}
	files, err := s.store.ListFiles(projectID)
	if err != nil {
		return "", nil, err
	}
	return index, files, nil
}

func buildMemoryRecallSelectionPrompt(q MemoryRecallQuery, index string, files []memory.MemoryFile) string {
	return fmt.Sprintf(memoryRecallSelectionPromptTemplate,
		renderMemoryManifest(files), strings.TrimSpace(index), renderMemoryRecallQuery(q))
}

func buildMemoryRecallSynthesisPrompt(q MemoryRecallQuery, files []memory.MemoryFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "### %s\n\n", f.RelPath)
		if f.Meta.Title != "" || f.Meta.Type != "" || f.Meta.Confidence != "" {
			fmt.Fprintf(&b, "frontmatter: title=%q type=%q confidence=%q updated=%q\n\n", f.Meta.Title, f.Meta.Type, f.Meta.Confidence, f.Meta.Updated)
		}
		body := strings.TrimSpace(f.Body)
		if body != "" {
			b.WriteString(truncateForPrompt(memory.Redact(body), 5000))
			b.WriteString("\n\n")
		}
	}
	return fmt.Sprintf(memoryRecallSynthesisPromptTemplate, b.String(), renderMemoryRecallQuery(q))
}

func renderMemoryRecallQuery(q MemoryRecallQuery) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Surface: %s\n", cleanRecallField(q.Surface))
	fmt.Fprintf(&b, "Title: %s\n\n", cleanRecallField(q.Title))
	fmt.Fprintf(&b, "Prompt/context:\n%s\n", truncateForPrompt(memory.Redact(q.Prompt+"\n\n"+q.RecentContext), 6000))
	if strings.TrimSpace(q.AgentContext) != "" {
		fmt.Fprintf(&b, "\nAgent/project context:\n%s\n", truncateForPrompt(memory.Redact(q.AgentContext), 2000))
	}
	return strings.TrimSpace(b.String())
}

func renderMemoryManifest(files []memory.MemoryFile) string {
	if len(files) == 0 {
		return "(no topic files)"
	}
	sorted := append([]memory.MemoryFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RelPath < sorted[j].RelPath })
	var b strings.Builder
	for _, f := range sorted {
		title := strings.TrimSpace(f.Meta.Title)
		if title == "" {
			title = strings.TrimSpace(f.Meta.Name)
		}
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(f.RelPath), ".md")
		}
		fmt.Fprintf(&b, "- %s: title=%q type=%q confidence=%q updated=%q size=%d\n", f.RelPath, title, f.Meta.Type, f.Meta.Confidence, f.Meta.Updated, f.SizeBytes)
	}
	return b.String()
}

func filterSelectedMemoryFiles(names []string, files []memory.MemoryFile) []memory.MemoryFile {
	byName := map[string]memory.MemoryFile{}
	for _, f := range files {
		byName[f.RelPath] = f
	}
	seen := map[string]bool{}
	out := make([]memory.MemoryFile, 0, len(names))
	for _, name := range names {
		name = filepath.ToSlash(strings.TrimSpace(name))
		if name == "" || name == memory.IndexFileName || seen[name] {
			continue
		}
		if f, ok := byName[name]; ok {
			out = append(out, f)
			seen[name] = true
		}
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func renderSynthesizedMemoryContext(s memoryRecallSynthesis) string {
	facts := make([]string, 0, len(s.RelevantFacts))
	for _, fact := range s.RelevantFacts {
		fact = strings.TrimSpace(fact)
		if fact != "" {
			facts = append(facts, fact)
		}
		if len(facts) >= 7 {
			break
		}
	}
	if len(facts) == 0 {
		return ""
	}
	citations := make([]string, 0, len(s.CitedMemories))
	seen := map[string]bool{}
	for _, c := range s.CitedMemories {
		c = filepath.ToSlash(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		citations = append(citations, c)
	}
	var b strings.Builder
	b.WriteString("Recalled from your persistent memory system:\n\n")
	for _, fact := range facts {
		fmt.Fprintf(&b, "- %s\n", fact)
	}
	if len(citations) > 0 {
		b.WriteString("\nSources: ")
		b.WriteString(strings.Join(citations, ", "))
		b.WriteString("\n")
	}
	return b.String()
}

func cleanRecallField(s string) string {
	s = strings.TrimSpace(memory.Redact(s))
	if s == "" {
		return "(none)"
	}
	return s
}

func unmarshalModelJSON(raw string, v any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty response")
	}
	if err := json.Unmarshal([]byte(raw), v); err == nil {
		return nil
	}
	startObj := strings.Index(raw, "{")
	endObj := strings.LastIndex(raw, "}")
	if startObj >= 0 && endObj > startObj {
		if err := json.Unmarshal([]byte(raw[startObj:endObj+1]), v); err == nil {
			return nil
		}
	}
	return json.Unmarshal([]byte(raw), v)
}
