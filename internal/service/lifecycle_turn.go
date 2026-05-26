package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"github.com/openvibely/openvibely/internal/agentskills"
	"github.com/openvibely/openvibely/internal/lifecycle"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

// LifecycleTurn is the result of preparing a model turn for lifecycle hooks.
// It carries the prepared context (with runtime tools and prompt context
// attached) and an AfterComplete closure the caller must invoke once the
// underlying LLM call returns.
//
// The same struct is used by initial task runs (WorkerService.executeTask)
// and by task-thread followups (handler.processStreamingResponse). The
// runbook (§Lifecycle Slots line 296 + §Task-Thread Followup lines 94-99)
// requires lifecycle hooks to run on every model turn, including followups.
type LifecycleTurn struct {
	// Ctx is the prepared context. Caller passes it to LLMService.
	Ctx context.Context

	// Task is the task that will be executed. Lifecycle routing no longer
	// changes AgentDefinitionID; agents are selected manually/defaulted, while
	// route_task only selects relevant skills for this turn.
	Task models.Task

	// AfterComplete must be invoked once the LLM call returns so the
	// after_complete hook slot fires. Safe to call with err == nil. The
	// runbook (§Execution And Queueing line 1806) says after_complete runs
	// asynchronously; this closure handles that for you.
	AfterComplete func(err error, chatContext llmcontracts.ChatContext)
}

// PrepareLifecycleTurn runs the route_task + before_run lifecycle slots for
// a model turn and returns the prepared context. The caller passes the
// resulting Ctx to the LLM and then invokes AfterComplete(err, chatContext)
// with the model-facing chat context from that LLM turn once the LLM returns.
//
// This method is the single integration point for lifecycle behavior. Both
// initial task runs and task-thread followups must call it so selected skills,
// skill runtime tools, and any recall outputs are consistently delivered to the
// model per runbook §Lifecycle Slots line 296.
func (w *WorkerService) PrepareLifecycleTurn(ctx context.Context, task models.Task) LifecycleTurn {
	if w == nil {
		return LifecycleTurn{Ctx: ctx, Task: task, AfterComplete: func(error, llmcontracts.ChatContext) {}}
	}

	runID := newLifecycleTaskRunID(task.ID)
	projectRoot := projectSkillRoot(ctx, w.projectRepo, task.ProjectID)
	assignedAgent := w.taskAgentDefinition(ctx, task)
	if w.agentRootSyncService != nil {
		if err := w.agentRootSyncService.SyncRootDeclarations(ctx, projectRoot); err != nil {
			log.Printf("[lifecycle-turn] sync agent root declarations failed task=%s: %v", task.ID, err)
		}
	}
	catalog := w.buildSkillCatalog(ctx, task)
	w.currentCatalog.Store(catalog)
	fullSkillIndex := w.renderAvailableSkillsForTask(ctx, task, projectRoot)
	ctx = withLifecycleTurnContext(ctx, lifecycleTurnContext{Catalog: catalog, SkillIndex: fullSkillIndex, AssignedAgent: assignedAgent})
	hookReadTools := w.buildLifecycleReadRuntimeTools(task, catalog)
	if hookReadTools != nil {
		ctx = llmcontracts.WithRuntimeTools(ctx, hookReadTools)
	}
	hookMutationTools := w.buildLifecycleRuntimeTools(task, catalog)
	log.Printf("[lifecycle-turn] prepared task=%s catalog_skills=%d runtime_tools=%t", task.ID, len(catalog.Entries()), hookReadTools != nil)

	// route_task selects relevant skill handles for this turn. It never changes
	// Task.AgentDefinitionID. No-agent tasks route standalone skills; assigned-agent
	// tasks route skills owned by the assigned agent.
	selectedSkillHandles := []string{}
	if w.lifecycleRunner != nil {
		if route := w.runLifecycleSlot(ctx, models.LifecycleRouteTask, task, runID, nil, llmcontracts.ChatContext{}); len(route.Outputs) > 0 {
			var best lifecycle.SelectedSkills
			haveBest := false
			for _, out := range route.Outputs {
				if out.OutputContract != models.OutputContractSelectedSkills || len(out.Payload) == 0 || out.Error != "" {
					continue
				}
				selected, err := lifecycle.ValidateSelectedSkills(out.Payload)
				if err != nil {
					log.Printf("[lifecycle-turn] route_task invalid selected_skills task=%s: %v", task.ID, err)
					continue
				}
				if selected.NeedsClarification {
					log.Printf("[lifecycle-turn] route_task clarification requested task=%s question=%q", task.ID, selected.ClarifyingQuestion)
					continue
				}
				if !haveBest || selected.Confidence > best.Confidence {
					best = selected
					haveBest = true
				}
			}
			if haveBest {
				selectedSkillHandles = filterCatalogHandles(catalog, best.Skills)
				log.Printf("[lifecycle-turn] route_task selected_skills task=%s handles=%d confidence=%.2f", task.ID, len(selectedSkillHandles), best.Confidence)
			}
		}
	}
	taskCatalog := catalog.Filter(runID+":selected", selectedSkillHandles)
	ctx = withLifecycleTurnContext(ctx, lifecycleTurnContext{Catalog: catalog, SelectedSkillHandles: selectedSkillHandles, AssignedAgent: assignedAgent})

	// before_run: produce context_blocks the model should see. The runbook
	// (§Auto-Routing line 130) says these blocks are merged into the system
	// prompt before the active agent runs.
	preparedContext := ""
	if w.lifecycleRunner != nil {
		before := w.runLifecycleSlot(ctx, models.LifecycleBeforeRun, task, runID, nil, llmcontracts.ChatContext{})
		preparedContext = lifecycle.MergeContextBlocks(before.Outputs)
		if preparedContext != "" {
			log.Printf("[lifecycle-turn] before_run prepared_context task=%s bytes=%d outputs=%d", task.ID, len(preparedContext), len(before.Outputs))
		}
	}

	skillIndex := agentskills.RenderSelectedSkillsMarkdown(taskCatalog, selectedSkillHandles)
	taskRuntimeTools := w.buildTaskSkillRuntimeTools(ctx, task, taskCatalog)
	if taskRuntimeTools != nil {
		ctx = llmcontracts.WithRuntimeTools(ctx, taskRuntimeTools)
	}
	ctx = withLifecycleTurnContext(ctx, lifecycleTurnContext{
		Catalog:              taskCatalog,
		SkillIndex:           skillIndex,
		PreparedBlocks:       preparedContext,
		SelectedSkillHandles: selectedSkillHandles,
		AssignedAgent:        assignedAgent,
	})
	promptContext := buildLifecyclePromptContext(skillIndex, preparedContext)
	if promptContext != "" {
		ctx = withAdditionalProjectInstructions(ctx, promptContext)
	}

	after := func(err error, chatContext llmcontracts.ChatContext) {
		if w.lifecycleRunner == nil {
			return
		}
		// Run after_complete in a detached goroutine so it never blocks
		// caller dispatch. Runbook §Execution And Queueing line 1806.
		go func(t models.Task, taskRunID string, runErr error, taskChatContext llmcontracts.ChatContext, rt *llmcontracts.RuntimeTools, turn lifecycleTurnContext) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[lifecycle-turn] after_complete panic for task=%s: %v", t.ID, rec)
				}
			}()
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			bgCtx = withLifecycleTurnContext(bgCtx, turn)
			if rt != nil {
				bgCtx = llmcontracts.WithRuntimeTools(bgCtx, rt)
			}
			w.runLifecycleSlot(bgCtx, models.LifecycleAfterComplete, t, taskRunID, runErr, taskChatContext)
		}(task, runID, err, chatContext, hookMutationTools, lifecycleTurnContext{Catalog: taskCatalog, SelectedSkillHandles: selectedSkillHandles, AssignedAgent: assignedAgent})
	}
	return LifecycleTurn{Ctx: ctx, Task: task, AfterComplete: after}
}

func filterCatalogHandles(catalog *agentskills.Catalog, handles []string) []string {
	out := make([]string, 0, len(handles))
	seen := map[string]struct{}{}
	for _, handle := range handles {
		if _, ok := seen[handle]; ok {
			continue
		}
		if _, ok := catalog.Lookup(handle); !ok {
			log.Printf("[lifecycle-turn] route_task selected unknown skill handle=%s", handle)
			continue
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	return out
}

func newLifecycleTaskRunID(taskID string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return taskID + ":" + hex.EncodeToString(b[:])
	}
	return taskID + ":" + time.Now().UTC().Format("20060102T150405.000000000")
}
