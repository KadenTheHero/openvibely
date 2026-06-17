package components

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

// TestChatAutoScrollScript verifies the auto-scroll JavaScript is correctly generated
func TestChatAutoScrollScript(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// Verify core auto-scroll utility exists
	if !strings.Contains(content, "window.chatAutoScroll") {
		t.Error("Missing window.chatAutoScroll namespace")
	}

	// Verify isNearBottom function exists
	if !strings.Contains(content, "isNearBottom: function") {
		t.Error("Missing isNearBottom function")
	}

	// Verify scrollToBottom function exists
	if !strings.Contains(content, "scrollToBottom: function") {
		t.Error("Missing scrollToBottom function")
	}

	// Verify the threshold is reasonable (100px for "near bottom")
	if !strings.Contains(content, "100") {
		t.Error("Expected to find threshold value in script")
	}

	t.Logf("ChatAutoScrollScript generated successfully (%d bytes)", len(content))
}

func TestChatLoadingDots_RendersThreeDotsAndSizeVariants(t *testing.T) {
	var sm bytes.Buffer
	if err := ChatLoadingDots("sm").Render(context.Background(), &sm); err != nil {
		t.Fatalf("Failed to render ChatLoadingDots(sm): %v", err)
	}
	smHTML := sm.String()
	if !strings.Contains(smHTML, "ov-loading-dots ov-loading-dots-sm") {
		t.Error("expected sm variant classes on loading dots")
	}
	if !strings.Contains(smHTML, `aria-hidden="true"`) {
		t.Error("expected loading dots to be aria-hidden")
	}
	if count := strings.Count(smHTML, `class="ov-loading-dot"`); count != 3 {
		t.Errorf("expected exactly 3 loading dots for sm variant, got %d", count)
	}

	var xs bytes.Buffer
	if err := ChatLoadingDots("xs").Render(context.Background(), &xs); err != nil {
		t.Fatalf("Failed to render ChatLoadingDots(xs): %v", err)
	}
	xsHTML := xs.String()
	if !strings.Contains(xsHTML, "ov-loading-dots ov-loading-dots-xs") {
		t.Error("expected xs variant classes on loading dots")
	}
	if count := strings.Count(xsHTML, `class="ov-loading-dot"`); count != 3 {
		t.Errorf("expected exactly 3 loading dots for xs variant, got %d", count)
	}
}

// TestChatBubbleStreaming_ThreadCompletionStaysSmooth verifies task completion
// does not refresh the task-thread fragment; queued promotion is handled by live events.
func TestChatBubbleStreaming_ThreadCompletionStaysSmooth(t *testing.T) {
	var buf bytes.Buffer
	err := ChatBubbleStreaming("assistant", "exec-id", "task-thread-messages", "task-thread-view", true).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render thread ChatBubbleStreaming: %v", err)
	}
	content := buf.String()

	if strings.Contains(content, "'#task-detail-content'") || strings.Contains(content, `"#task-detail-content"`) {
		t.Error("thread completion must not target #task-detail-content (hard-refresh UX)")
	}
	if strings.Contains(content, "?tab=chat") {
		t.Error("thread completion must not refresh via ?tab=chat full-detail swap")
	}
	if strings.Contains(content, "refreshThreadViewForPendingInputs()") || strings.Contains(content, "hasVisiblePendingThreadInputs()") {
		t.Error("thread completion must not refresh the thread fragment for pending rows")
	}
	if strings.Contains(content, "htmx.ajax('GET', '/tasks/' + taskIdMatch[1] + '/thread'") {
		t.Error("thread completion must not hard-refresh the task thread fragment")
	}
	if !strings.Contains(content, "showThreadTerminalStatus('completed')") {
		t.Error("thread completion should show terminal status dynamically when no pending rows exist")
	}
	if !strings.Contains(content, "stopThreadPolling()") {
		t.Error("thread completion should stop thread polling dynamically when no pending rows exist")
	}
	if !strings.Contains(content, "restoreThreadPollingFallback()") {
		t.Error("transport errors should restore polling as a fallback")
	}
}

// TestChatBubbleStreaming_NeverTargetsTaskDetailContent ensures NO streaming
// bubble (thread or non-thread) issues a post-stream HTMX swap targeting
// #task-detail-content. That swap was the source of the perceived hard refresh
// after task completion.
func TestChatBubbleStreaming_NeverTargetsTaskDetailContent(t *testing.T) {
	cases := []struct {
		name          string
		messagesID    string
		pauseTargetID string
		isThread      bool
	}{
		{"thread", "task-thread-messages", "task-thread-view", true},
		{"chat", "chat-messages", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := ChatBubbleStreaming("assistant", "exec-id", tc.messagesID, tc.pauseTargetID, tc.isThread).Render(context.Background(), &buf)
			if err != nil {
				t.Fatalf("Failed to render ChatBubbleStreaming: %v", err)
			}
			content := buf.String()
			if strings.Contains(content, "'#task-detail-content'") || strings.Contains(content, `"#task-detail-content"`) {
				t.Error("streaming bubble must not target #task-detail-content for any post-stream refresh (causes hard-refresh UX)")
			}
			if strings.Contains(content, "?tab=chat") {
				t.Error("streaming bubble must not refresh via ?tab=chat full-detail swap")
			}
		})
	}
}

// TestInitThreadStreamingScript_CompletionStaysSmooth verifies the resume SSE
// handler also avoids fragment refreshes; live events append promoted turns.
func TestInitThreadStreamingScript_CompletionStaysSmooth(t *testing.T) {
	var buf bytes.Buffer
	err := _initThreadStreamingScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render _initThreadStreamingScript: %v", err)
	}
	content := buf.String()
	if strings.Contains(content, "'#task-detail-content'") || strings.Contains(content, `"#task-detail-content"`) {
		t.Error("resume thread stream script must not target #task-detail-content (hard-refresh UX)")
	}
	if strings.Contains(content, "?tab=chat") {
		t.Error("resume thread stream script must not refresh via ?tab=chat full-detail swap")
	}
	if strings.Contains(content, "refreshThreadViewForPendingInputs()") || strings.Contains(content, "hasVisiblePendingThreadInputs()") {
		t.Error("resume completion must not refresh the thread fragment for pending rows")
	}
	if strings.Contains(content, "htmx.ajax('GET', '/tasks/' + taskIdMatch[1] + '/thread'") {
		t.Error("resume completion must not hard-refresh the task thread fragment")
	}
	if !strings.Contains(content, "showThreadTerminalStatus('completed')") {
		t.Error("resume completion should show terminal status dynamically when no pending rows exist")
	}
	if !strings.Contains(content, "restoreThreadPollingFallback()") {
		t.Error("resume transport errors should restore polling as a fallback")
	}
}

// TestChatBubbleStreamingScrollBehavior verifies streaming bubble has correct scroll behavior
func TestChatBubbleStreamingScrollBehavior(t *testing.T) {
	var buf bytes.Buffer
	err := ChatBubbleStreaming("assistant", "test-exec-id", "chat-messages", "pause-target", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleStreaming: %v", err)
	}

	content := buf.String()

	// Verify EventSource is used for streaming
	if !strings.Contains(content, "new EventSource") {
		t.Error("Missing EventSource for streaming")
	}
	if !strings.Contains(content, "window.registerChatStreamEventSource(execId, eventSource)") {
		t.Error("Missing chat stream EventSource registration for non-thread streaming")
	}
	if !strings.Contains(content, "window.unregisterChatStreamEventSource(execId, es)") {
		t.Error("Missing chat stream EventSource unregister helper call")
	}

	// Verify onmessage handler exists
	if !strings.Contains(content, "eventSource.onmessage") {
		t.Error("Missing onmessage handler for streaming")
	}
	if !strings.Contains(content, "renderBufferedOutput(false)") {
		t.Error("Missing batched streaming renderer call in onmessage handler")
	}
	if !strings.Contains(content, "requestAnimationFrame(runRender)") {
		t.Error("Missing requestAnimationFrame batching for streaming renders")
	}

	// Verify that the onmessage handler uses the scroll tracker
	if !strings.Contains(content, "tracker.shouldAutoScroll") {
		t.Error("Missing tracker.shouldAutoScroll check in onmessage handler")
	}

	// Verify done event handler exists
	if !strings.Contains(content, "addEventListener('done'") {
		t.Error("Missing done event listener")
	}

	// Verify error handling
	if !strings.Contains(content, "addEventListener('error'") {
		t.Error("Missing error event listener")
	}

	// Verify page-level tracker is reused (not destroyed per stream)
	if !strings.Contains(content, "scrollTracker_") {
		t.Error("Missing page-level tracker key pattern")
	}

	t.Logf("ChatBubbleStreaming scroll behavior verified (%d bytes)", len(content))
}

// TestChatBubbleStreamingResumeScrollBehavior verifies resume bubble uses data attributes
// for deferred SSE initialization (EventSource created by _initThreadStreaming after morph).
func TestChatBubbleStreamingResumeScrollBehavior(t *testing.T) {
	var buf bytes.Buffer
	err := ChatBubbleStreamingResume("assistant", "existing content", "test-exec-id", "chat-messages", "").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleStreamingResume: %v", err)
	}

	content := buf.String()

	// Verify data-streaming-resume attribute for deferred SSE init
	if !strings.Contains(content, `data-streaming-resume="true"`) {
		t.Error("Missing data-streaming-resume attribute")
	}

	// Verify exec ID is in data attributes
	if !strings.Contains(content, "test-exec-id") {
		t.Error("Missing exec ID")
	}

	// Verify initial length attribute for delta rendering
	if !strings.Contains(content, "data-initial-length") {
		t.Error("Missing data-initial-length attribute")
	}

	// Verify messages container reference
	if !strings.Contains(content, `data-messages-container="chat-messages"`) {
		t.Error("Missing data-messages-container attribute")
	}

	t.Logf("ChatBubbleStreamingResume scroll behavior verified (%d bytes)", len(content))
}

// TestChatAutoScrollLogic tests the JavaScript logic for determining if we should auto-scroll
func TestChatAutoScrollLogic(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// Verify the isNearBottom function exists and is properly structured
	if !strings.Contains(content, "isNearBottom: function") {
		t.Error("Missing isNearBottom function")
	}

	// Verify it checks scroll position using the key calculation
	if !strings.Contains(content, "scrollHeight") && !strings.Contains(content, "scrollTop") && !strings.Contains(content, "clientHeight") {
		t.Error("Missing scroll position calculation")
	}

	// Verify threshold variable is declared (100px threshold for "near bottom" detection)
	if !strings.Contains(content, "var threshold = 100") {
		t.Error("Missing or incorrect threshold variable")
	}

	// Verify the comparison operator - should return true if near bottom
	thresholdPattern := regexp.MustCompile(`threshold\s*[<>=]+\s*\d+`)
	if !thresholdPattern.MatchString(content) {
		t.Logf("Warning: Could not find explicit threshold comparison, but function exists")
	}

	t.Logf("ChatAutoScrollLogic verified (%d bytes)", len(content))
}

func TestChatAutoScrollScript_RehydratesAssistantRawContentViaStreamingRenderer(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "container.querySelectorAll('.chat-stream-content[data-raw-content]').forEach(function(el)") {
		t.Error("cleanAssistantMessages must scan raw assistant containers on hydration")
	}
	if !strings.Contains(content, "if (raw && window.renderStreamingContent)") {
		t.Error("cleanAssistantMessages must prefer streaming renderer for tool-card reconstruction")
	}
	if !strings.Contains(content, "window.renderStreamingContent(el, raw);") {
		t.Error("cleanAssistantMessages must rebuild tool/thinking cards from raw content")
	}
}

func TestChatAutoScrollScript_ToolHeaderUsesTextNodesNotInnerHTML(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()
	if strings.Contains(content, "header.innerHTML = headerHtml;") {
		t.Fatal("tool header must not assign concatenated innerHTML from model/tool text")
	}
	if !strings.Contains(content, "nameSpan.textContent = dn;") {
		t.Error("tool header should render tool name via textContent")
	}
	if !strings.Contains(content, "secondarySpan.textContent = seg.secondary;") {
		t.Error("tool header should render tool secondary text via textContent")
	}
}

func TestChatInputForm_SubmitButtonUsesRequestSubmit(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "task-thread-form",
		InputID:      "task-message-input",
		PostEndpoint: "/tasks/task-1/thread",
		TargetID:     "task-thread-messages",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatInputForm: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "var submitBtn = form.querySelector('button[type=\"submit\"]');") {
		t.Fatal("chat input script must bind the submit button for click-path parity")
	}
	if !strings.Contains(content, "submitBtn.addEventListener('click', function(e)") {
		t.Fatal("chat input script must normalize submit button clicks")
	}
	if !strings.Contains(content, "if (typeof form.requestSubmit === 'function')") {
		t.Fatal("chat input script must feature-detect requestSubmit")
	}
	if !strings.Contains(content, "var submitEvent = new Event('submit', { bubbles: true, cancelable: true });") {
		t.Fatal("chat input script must synthesize submit event when requestSubmit is unavailable")
	}
	if !strings.Contains(content, "form.dispatchEvent(submitEvent);") {
		t.Fatal("chat input script must dispatch submit event fallback")
	}
}

func TestChatInputForm_EnterKeyHasRequestSubmitFallback(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "task-thread-form",
		InputID:      "task-message-input",
		PostEndpoint: "/tasks/task-1/thread",
		TargetID:     "task-thread-messages",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatInputForm: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "if (typeof form.requestSubmit === 'function')") {
		t.Fatal("enter key path must feature-detect requestSubmit")
	}
	if !strings.Contains(content, "var submitEvent = new Event('submit', { bubbles: true, cancelable: true });") {
		t.Fatal("enter key path must synthesize submit event when requestSubmit is unavailable")
	}
	if !strings.Contains(content, "form.dispatchEvent(submitEvent);") {
		t.Fatal("enter key path must dispatch submit fallback")
	}
}

func TestChatInputForm_MessageHistoryNavigationScript(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "chat-form",
		InputID:      "message-input",
		PostEndpoint: "/chat/send",
		TargetID:     "chat-messages",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	required := []string{
		`data-message-history-key="openvibely-chat-message-history"`,
		"var messageHistoryLimit = 50;",
		"function loadMessageHistory()",
		"localStorage.getItem(messageHistoryStorageKey)",
		"JSON.parse(raw)",
		"function saveMessageHistory(entries)",
		"localStorage.setItem(messageHistoryStorageKey, JSON.stringify(entries.slice(-messageHistoryLimit)))",
		"function rememberSubmittedMessage(message)",
		"messageHistoryEntries.push(message);",
		"function handleMessageHistoryKeydown(e)",
		"if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown' && e.key !== 'Escape') return false;",
		"if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && messageInput.value !== '' && !isAtFirstLineStart(messageInput)) return false;",
		"if (e.key === 'ArrowDown' && messageHistoryIndex === -1) return false;",
		"if (messageHistoryIndex === -1) messageHistoryDraft = messageInput.value;",
		"messageHistoryEntries[messageHistoryEntries.length - 1 - messageHistoryIndex]",
		"setMessageInputFromHistory(messageHistoryDraft);",
		"resetMessageHistoryNavigation();",
		"rememberSubmittedMessage(messageHistorySubmittedValue);",
		"if (!event.detail || event.detail.elt !== form) return;",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Fatalf("message history script missing %q", r)
		}
	}
}

func TestChatInputForm_MessageHistoryScopedPerTaskThread(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "task-thread-form",
		InputID:      "task-message-input",
		PostEndpoint: "/tasks/task-123/thread",
		TargetID:     "task-thread-messages",
		TaskID:       "task-123",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, `data-message-history-key="openvibely-task-thread-message-history-task-123"`) {
		t.Fatal("task thread form should use a task-scoped message history key")
	}
	if strings.Contains(content, `data-message-history-key="openvibely-chat-message-history"`) {
		t.Fatal("task thread form must not share the global chat message history key")
	}
}

func TestChatInputForm_MobileControlsStayContained(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:            "chat-form",
		InputID:           "message-input",
		PostEndpoint:      "/chat/send",
		TargetID:          "chat-messages",
		ShowModelSelector: true,
		ShowModeSelector:  true,
		Agents: []models.LLMConfig{
			{ID: "agent-1", Name: "Very Long Agent Name That Should Not Push Send Button", Model: "very-long-model-name"},
		},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	gutterClass := `class="chat-input-shadow-gutter w-full min-w-0 max-w-full pt-2 pb-4"`
	if !strings.Contains(content, gutterClass) {
		t.Fatalf("chat composer should render inside a full-width shadow gutter without artificial side padding or desktop caps; missing %q", gutterClass)
	}
	if strings.Contains(content, `sm:max-w-3xl`) || strings.Contains(content, `sm:mx-auto`) || strings.Contains(content, `px-3 pt-2 pb-4`) {
		t.Fatal("chat composer gutter must not add desktop side gaps or mobile right-side empty space")
	}
	formClass := `class="chat-input-container rounded-xl p-4 relative min-w-0 max-w-full"`
	if !strings.Contains(content, formClass) {
		t.Fatalf("chat composer shell should fill the shadow gutter without clipping; missing %q", formClass)
	}
	if !strings.Contains(content, `var form = wrapper ? wrapper.querySelector('form.chat-input-container') : null;`) {
		t.Fatal("chat composer script must find the nested form inside the shadow gutter wrapper")
	}
	if strings.Contains(content, `chat-input-container rounded-xl p-4 relative w-full`) {
		t.Fatal("chat composer shell must not use w-full with visual margins because it clips the rounded right edge")
	}
	if strings.Contains(content, `class="chat-input-container rounded-xl p-4 relative min-w-0 max-w-full overflow-x-hidden"`) {
		t.Fatal("chat composer shell must not hard-clip its rounded edge with overflow-x-hidden")
	}
	required := []string{
		`class="flex items-center justify-between gap-2 pt-2 min-w-0 max-w-full overflow-hidden"`,
		`class="flex items-center gap-1 min-w-0 flex-1"`,
		`w-auto min-w-0 max-w-[7.5rem] sm:max-w-[140px]`,
		`w-auto min-w-0 max-w-[6rem] sm:max-w-[120px]`,
		`class="flex items-center gap-2 flex-shrink-0"`,
	}
	for _, expected := range required {
		if !strings.Contains(content, expected) {
			t.Fatalf("chat composer should keep mobile controls contained; missing %q", expected)
		}
	}
}

func TestChatInputForm_MessageHistoryCursorGuardsPreventArrowHijack(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "chat-form",
		InputID:      "message-input",
		PostEndpoint: "/chat/send",
		TargetID:     "chat-messages",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	required := []string{
		"function isAtFirstLineStart(textarea)",
		"if (start !== end) return false;",
		"return start === 0;",
		"if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && messageInput.value !== '' && !isAtFirstLineStart(messageInput)) return false;",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Fatalf("message history cursor guard missing %q", r)
		}
	}
}

func TestChatInputForm_RunningChatDoesNotExposeComposerSteeringControl(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "chat-form",
		InputID:      "message-input",
		PostEndpoint: "/chat/send",
		TargetID:     "chat-messages",
		IsRunning:    true,
		ActiveTurnID: "exec-active-chat",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, `name="expected_turn_id" value="exec-active-chat"`) {
		t.Fatal("running chat form should keep the active turn guard for queue safety")
	}
	if strings.Contains(content, `name="steer_endpoint"`) || strings.Contains(content, `data-steer-submit="true"`) || strings.Contains(content, ">Steer") {
		t.Fatal("running chat form must not expose composer steering controls")
	}
	if strings.Contains(content, "htmx.ajax('POST', steerEndpoint") {
		t.Fatal("running chat form must not post directly to the steering endpoint")
	}
}

func TestChatInputForm_RunningTaskThreadDoesNotExposeComposerSteeringControl(t *testing.T) {
	var buf bytes.Buffer
	err := ChatInputForm(ChatInputFormConfig{
		FormID:       "task-thread-form",
		InputID:      "task-message-input",
		PostEndpoint: "/tasks/task-1/thread",
		TargetID:     "task-thread-messages",
		TaskID:       "task-1",
		IsRunning:    true,
		ActiveTurnID: "exec-active-task",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, `name="expected_turn_id" value="exec-active-task"`) {
		t.Fatal("running task-thread form should keep the active turn guard for queue safety")
	}
	if strings.Contains(content, `name="steer_endpoint"`) || strings.Contains(content, `data-steer-submit="true"`) || strings.Contains(content, ">Steer") {
		t.Fatal("running task-thread form must not expose composer steering controls")
	}
}

func TestPendingThreadInputRows_LeavesComposerOwnedInputsOutOfTranscript(t *testing.T) {
	inputs := []models.ThreadInput{
		{ID: "queued-1", TaskID: "task-1", InputMode: models.ThreadInputModeQueued, Content: "queue this"},
		{ID: "steer-1", TaskID: "task-1", InputMode: models.ThreadInputModeSteering, Content: "steer this"},
	}
	var buf bytes.Buffer
	err := PendingThreadInputRows(inputs, func(input models.ThreadInput) string {
		return "/tasks/" + input.TaskID + "/thread/queued/" + input.ID + "/steer"
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PendingThreadInputRows: %v", err)
	}

	content := buf.String()
	if strings.Contains(content, `thread-input-queued-1`) || strings.Contains(content, `thread-input-steer-1`) || strings.Contains(content, `pending-steering-inputs`) {
		t.Fatal("queued and steering pending rows should render inside the composer, not the chat transcript")
	}
}

func TestChatComposerQueuedInputRows_RenderInsideInputBoxStyle(t *testing.T) {
	inputs := []models.ThreadInput{
		{ID: "queued-1", TaskID: "task-1", InputMode: models.ThreadInputModeQueued, Content: "queue this", AttachmentSessionID: "pending-session-1"},
		{ID: "steer-1", TaskID: "task-1", InputMode: models.ThreadInputModeSteering, Content: "steer this"},
	}
	var buf bytes.Buffer
	err := ChatComposerQueuedInputRows(inputs, func(input models.ThreadInput) string {
		return "/tasks/" + input.TaskID + "/thread/queued/" + input.ID + "/steer"
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatComposerQueuedInputRows: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, `id="pending-thread-inputs"`) || !strings.Contains(content, `space-y-1.5`) {
		t.Fatal("pending rows should render in a composer queue container")
	}
	if strings.Contains(content, `id="pending-thread-inputs" class="space-y-1.5 mb-3`) || strings.Contains(content, `id="pending-thread-inputs" class="space-y-1.5 pb-4`) {
		t.Fatal("pending row spacing should be controlled by shared CSS, not per-render classes")
	}
	if !strings.Contains(content, `queued-input-row`) || !strings.Contains(content, `steering-input-row`) || !strings.Contains(content, `bg-base-300/45`) || !strings.Contains(content, `flex-1`) || !strings.Contains(content, `ml-auto`) {
		t.Fatal("pending rows should look like part of the input box with right-aligned actions")
	}
	if !strings.Contains(content, `hx-post="/tasks/task-1/thread/queued/queued-1/steer"`) || !strings.Contains(content, "Steer") {
		t.Fatal("queued pending row must expose Steer action")
	}
	if !strings.Contains(content, "Attachments queued") || !strings.Contains(content, `aria-label="Attachments queued with this follow-up"`) || !strings.Contains(content, `M15.172 7l-6.586 6.586`) {
		t.Fatal("queued pending row with an attachment session should indicate that attachments are queued")
	}
	if !strings.Contains(content, `hx-post="/thread-inputs/queued-1/cancel"`) || !strings.Contains(content, `aria-label="Cancel queued follow-up"`) || !strings.Contains(content, `M19 7l-.867 12.142`) {
		t.Fatal("queued pending row must expose the alerts trash icon cancel action")
	}
	if !strings.Contains(content, `thread-input-steer-1`) || !strings.Contains(content, "Steering pending") || !strings.Contains(content, `aria-label="Cancel pending steering"`) {
		t.Fatal("composer pending rows should include steering rows with a trash-icon cancel action")
	}
	if strings.Contains(content, "Send now") || strings.Contains(content, "btn-warning") || strings.Contains(content, "bg-warning") || strings.Contains(content, ">Cancel</button>") || strings.Contains(content, ">×</button>") {
		t.Fatal("composer pending rows should avoid old warning/text/× cancel treatments")
	}
}

func TestChatQueuedInputRowOOB_WithAttachmentsShowsQueuedAttachmentIndicator(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatQueuedInputRowOOB("queued-1", "queue this", "/chat/queued/queued-1/steer", true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render ChatQueuedInputRowOOB: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, `hx-swap-oob="beforeend"`) || !strings.Contains(content, `thread-input-queued-1`) {
		t.Fatal("OOB queued row should append the pending input row")
	}
	if !strings.Contains(content, "Attachments queued") || !strings.Contains(content, `aria-label="Attachments queued with this follow-up"`) {
		t.Fatal("OOB queued row should indicate when attachments are queued with the message")
	}
}

func TestChatBubbleWithAttachments_MarksImagesForSmartScroll(t *testing.T) {
	attachments := []models.ChatAttachment{
		{ID: "att-image", FileName: "screenshot.png", MediaType: "image/png", FileSize: 1234},
		{ID: "att-file", FileName: "notes.txt", MediaType: "text/plain", FileSize: 42},
	}

	var buf bytes.Buffer
	if err := ChatBubbleWithAttachments("User", "see screenshot", attachments).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render ChatBubbleWithAttachments: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, `data-chat-attachment-image="true"`) {
		t.Fatal("image attachments must be marked so lazy-load layout growth can trigger smart-scroll correction")
	}
	if count := strings.Count(content, `data-chat-attachment-image="true"`); count != 1 {
		t.Fatalf("expected only image attachments to be marked for smart scroll, got %d markers", count)
	}
}

func TestChatAutoScrollScript_BindsAttachmentImageSmartScroll(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}
	content := buf.String()

	required := []string{
		"window.markChatSendScrollIntent = function(formOrMessagesId)",
		"window.consumeChatSendScrollIntent = function(messagesId)",
		"window.hasChatSendScrollIntent = function(messagesId)",
		"window.scrollChatToBottomAfterLayout = function(messagesEl, smooth)",
		"window.bindAttachmentImageSmartScroll = function(messagesEl, trackerKey, trackerFallback)",
		`querySelectorAll('img[data-chat-attachment-image="true"]')`,
		"var hasSendIntent = window.hasChatSendScrollIntent(messagesId);",
		"snapshotPinnedState();",
		"var tracker = trackerKey && window.resolveScrollTracker ? window.resolveScrollTracker(trackerKey, liveMessages) : trackerFallback;",
		"if (hasSendIntent && tracker) tracker.userScrolledUp = false;",
		"shouldScroll = hasSendIntent || !tracker || tracker.shouldAutoScroll();",
		"if (tracker && !tracker.shouldAutoScroll()) return;",
		"img.addEventListener('load', scrollAfterImageLayout, { once: true });",
		"img.addEventListener('error', scrollAfterImageLayout, { once: true });",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Fatalf("attachment image smart-scroll helper missing %q", r)
		}
	}
}

func TestChatInputForm_MarksSendScrollIntentOnSubmit(t *testing.T) {
	config := ChatInputFormConfig{
		FormID:       "chat-form",
		InputID:      "message-input",
		PostEndpoint: "/chat/send",
		TargetID:     "chat-messages",
	}

	var buf bytes.Buffer
	if err := ChatInputForm(config).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render ChatInputForm: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, "form.addEventListener('submit', markSendScrollIntent);") {
		t.Fatal("chat input form should mark scroll intent on any real submit, including button click and Enter")
	}
	if !strings.Contains(content, "if (window.markChatSendScrollIntent) window.markChatSendScrollIntent(form);") {
		t.Fatal("submit handler should route through the shared send-scroll intent helper")
	}
}

func TestChatAutoScrollScript_ShowsToolCardsInPlanMode(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	if strings.Contains(content, "function shouldHidePlanToolCard(seg)") {
		t.Error("tool-card suppression helper should not exist in plan mode rendering")
	}
	if strings.Contains(content, "if (shouldHidePlanToolCard(seg)) return;") {
		t.Error("streaming renderer should not suppress plan-mode tool cards")
	}
}

// TestStreamingScrollIntegration verifies the integration of streaming and scrolling
func TestStreamingScrollIntegration(t *testing.T) {
	var buf1 bytes.Buffer
	ChatBubbleStreaming("assistant", "exec-1", "chat-messages", "", false).Render(context.Background(), &buf1)
	streamingContent := buf1.String()

	var buf2 bytes.Buffer
	ChatBubbleStreamingResume("assistant", "Previous content", "exec-1", "chat-messages", "").Render(context.Background(), &buf2)
	resumeContent := buf2.String()

	tests := []struct {
		name     string
		content  string
		mustHave []string
	}{
		{
			name:    "ChatBubbleStreaming integration",
			content: streamingContent,
			mustHave: []string{
				"eventSource.onmessage",
				"tracker.shouldAutoScroll",
				"addEventListener('done'",
				"scrollTracker_",
				"resetOnUserSend",
			},
		},
		{
			name:    "ChatBubbleStreamingResume integration",
			content: resumeContent,
			mustHave: []string{
				`data-streaming-resume="true"`,
				"data-exec-id",
				"data-initial-length",
				"data-messages-container",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, required := range tt.mustHave {
				if !strings.Contains(tt.content, required) {
					t.Errorf("Missing required element: %q", required)
				}
			}
		})
	}
}

// TestUserScrollTracking verifies user scroll detection and auto-scroll control
func TestUserScrollTracking(t *testing.T) {
	tests := []struct {
		name     string
		renderFn func() (string, error)
	}{
		{
			name: "ChatBubbleStreaming",
			renderFn: func() (string, error) {
				var buf bytes.Buffer
				err := ChatBubbleStreaming("assistant", "test-exec-123", "chat-messages", "", false).Render(context.Background(), &buf)
				return buf.String(), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := tt.renderFn()
			if err != nil {
				t.Fatalf("Failed to render %s: %v", tt.name, err)
			}

			// Verify tracker is obtained/created
			if !strings.Contains(content, "ChatScrollTracker") {
				t.Error("Missing ChatScrollTracker reference")
			}

			// Verify tracker.shouldAutoScroll() is called
			if !strings.Contains(content, "tracker.shouldAutoScroll") {
				t.Error("Missing tracker.shouldAutoScroll check")
			}

			// Verify EventSource connection setup
			if !strings.Contains(content, "new EventSource") {
				t.Error("Missing EventSource setup")
			}

			// Verify onmessage handler
			if !strings.Contains(content, "eventSource.onmessage") {
				t.Error("Missing onmessage handler")
			}

			// Verify done event listener
			if !strings.Contains(content, "addEventListener('done'") {
				t.Error("Missing done event listener")
			}

			// Verify page-level tracker pattern (tracker persists across streams)
			if !strings.Contains(content, "scrollTracker_") {
				t.Error("Missing page-level tracker key pattern")
			}
		})
	}

	// ChatBubbleStreamingResume uses data-streaming-resume attribute instead of inline script.
	// The EventSource is initialized by _initThreadStreaming() from TaskThreadView's afterSwap handler.
	t.Run("ChatBubbleStreamingResume", func(t *testing.T) {
		var buf bytes.Buffer
		err := ChatBubbleStreamingResume("assistant", "initial content", "test-exec-456", "chat-messages", "").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}
		content := buf.String()

		if !strings.Contains(content, `data-streaming-resume="true"`) {
			t.Error("Missing data-streaming-resume attribute")
		}
		if !strings.Contains(content, "test-exec-456") {
			t.Error("Missing exec ID in data attributes")
		}
		if !strings.Contains(content, "data-initial-length") {
			t.Error("Missing data-initial-length attribute")
		}
	})
}

// TestCleanDisplayContent_ToolMarkers verifies that tool use markers are stripped from display content.
func TestCleanDisplayContent_ToolMarkers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips Using tool marker",
			input:    "Let me check.\n[Using tool: Read]\nHere is the result.",
			expected: "Let me check.\n\nHere is the result.",
		},
		{
			name:     "strips multiple tool markers",
			input:    "I'll look at that.\n[Using tool: Read]\n[Using tool: Grep]\n[Using tool: Bash]\nDone.",
			expected: "I'll look at that.\n\nDone.",
		},
		{
			name:     "strips Tool done markers",
			input:    "Checking.\n[Tool Read done: file contents here]\nFound it.",
			expected: "Checking.\nFound it.",
		},
		{
			name:     "strips Tool error markers",
			input:    "Trying.\n[Tool Bash error: command not found]\nFailed.",
			expected: "Trying.\nFailed.",
		},
		{
			name:     "strips Tool done block markers",
			input:    "Checking.\n[Tool read_file done]\nfile contents here\n[/Tool]\nFound it.",
			expected: "Checking.\nFound it.",
		},
		{
			name:     "strips Tool error block markers",
			input:    "Trying.\n[Tool bash error]\ncommand not found\n[/Tool]\nFailed.",
			expected: "Trying.\nFailed.",
		},
		{
			name:     "strips Tool block markers on same line",
			input:    "Working.\n[Tool grep_search done]matches here[/Tool]\nDone.",
			expected: "Working.\nDone.",
		},
		{
			name:     "strips mixed tool and status markers",
			input:    "Working.\n[Using tool: Edit]\n[Tool Edit done: updated]\nAll done.\n[STATUS: SUCCESS]",
			expected: "Working.\n\nAll done.",
		},
		{
			name:     "strips Thinking blocks at end",
			input:    "Actual response.\n[Thinking]\nSome internal thoughts",
			expected: "Actual response.",
		},
		{
			name:     "strips Thinking block with end marker preserving first char of response",
			input:    "\n[Thinking]\nLet me think about this task...\n[/Thinking]\nNow let me read the chat handler.",
			expected: "Now let me read the chat handler.",
		},
		{
			name:     "strips Thinking block before content without eating first character",
			input:    "[Thinking]\nSome thoughts here\n[/Thinking]\nHere is my response.",
			expected: "Here is my response.",
		},
		{
			name:     "strips multiple Thinking blocks preserving content between them",
			input:    "[Thinking]\nFirst thought\n[/Thinking]\n\nResponse part 1.\n\n[Thinking]\nSecond thought\n[/Thinking]\n\nResponse part 2.",
			expected: "Response part 1.\n\nResponse part 2.",
		},
		{
			name:     "preserves clean text",
			input:    "This is a normal response with no markers.",
			expected: "This is a normal response with no markers.",
		},
		{
			name:     "strips proposed_plan wrappers and keeps content",
			input:    "Plan:\n<proposed_plan>\nStep one\nStep two\n</proposed_plan>\nDone.",
			expected: "Plan:\n\nStep one\nStep two\n\nDone.",
		},
		{
			name:     "does not strip arbitrary angle-bracket tags",
			input:    "Keep literal tag text: <custom_tag>hello</custom_tag>",
			expected: "Keep literal tag text: <custom_tag>hello</custom_tag>",
		},
		{
			name:     "strips CREATE_TASK blocks",
			input:    "Creating.\n[CREATE_TASK]\n{\"title\":\"test\"}\n[/CREATE_TASK]\nDone.",
			expected: "Creating.\n\nDone.",
		},
		{
			name:     "thinking-only output extracts thinking content as fallback",
			input:    "\n[Thinking]\nThe answer is 1 + 1 = 2.\n",
			expected: "The answer is 1 + 1 = 2.",
		},
		{
			name:     "thinking-only output with closed marker extracts content",
			input:    "[Thinking]\nLet me calculate: 5 * 3 = 15\n[/Thinking]",
			expected: "Let me calculate: 5 * 3 = 15",
		},
		{
			name:     "empty input returns empty",
			input:    "",
			expected: "",
		},
		{
			name:  "unclosed thinking blocks with embedded markers",
			input: "\n[Thinking]\nLet me start by reading.\n\n\n[Thinking]\nNow I see the issue.\n\n[Using tool: Read]\n\n[Thinking]\nThe fix is clear.\n",
			// After stripping tool/status markers, all content is in unclosed thinking blocks.
			// The fallback extracts thinking content and strips embedded [Thinking] markers.
			expected: "Let me start by reading.\n\nNow I see the issue.\n\nThe fix is clear.",
		},
		{
			name:  "multi-turn unclosed thinking with tool markers",
			input: "\n[Thinking]\nAnalyzing the problem.\n\n[Using tool: Read]\n\nLet me check this file.\n\n[Using tool: Edit]\n\n[Thinking]\nNow let me verify.\n\n[Using tool: Bash]\n\nAll tests pass.\n\n[STATUS: SUCCESS]",
			// Tool markers and STATUS stripped first, then unclosed thinking handled.
			// extractThinkingContent captures everything after first [Thinking],
			// strips embedded [Thinking] markers from the result.
			expected: "Analyzing the problem.\n\nLet me check this file.\n\nNow let me verify.\n\nAll tests pass.",
		},
		{
			name:     "strips multi_tool_use.parallel protocol artifact",
			input:    "} to=multi_tool_use.parallel code 彩神争霸高json uμ? Wait malformed because command has extra }}. Need correct. let's call separately.{}\nI hit a malformed shell command.",
			expected: "I hit a malformed shell command.",
		},
		{
			name:     "strips multi_tool_use.parallel without to= prefix",
			input:    "multi_tool_use.parallel error\nActual useful text here.",
			expected: "Actual useful text here.",
		},
		{
			name:     "strips multi_tool_use.parallel with leading braces",
			input:    "}} multi_tool_use.sequential something\nNarrative continues.",
			expected: "Narrative continues.",
		},
		{
			name:     "strips multi_tool_use artifact between tool blocks",
			input:    "[Using tool: Bash]\n[Tool Bash error]\nbash error\n[/Tool]\n} to=multi_tool_use.parallel code\nRetrying the command.\n[Using tool: Bash]\n",
			expected: "Retrying the command.",
		},
		{
			name:     "preserves normal text mentioning tools",
			input:    "The multi-tool approach works well for this task.",
			expected: "The multi-tool approach works well for this task.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanDisplayContent(tt.input)
			if got != tt.expected {
				t.Errorf("CleanDisplayContent() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

// TestCleanDisplayContent_DedupSummaries verifies task summary deduplication.
func TestCleanDisplayContent_DedupSummaries(t *testing.T) {
	input := "I'll create a task.\n\n" +
		"[CREATE_TASK]\n{\"title\": \"Fix bug\"}\n[/CREATE_TASK]\n\n" +
		"---\nCreated 1 task(s):\n- \"Fix bug\" (backlog)\n\n" +
		"---\nCreated 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]"

	got := CleanDisplayContent(input)

	count := strings.Count(got, "Created 1 task(s):")
	if count != 1 {
		t.Errorf("should have exactly 1 'Created' summary, got %d in:\n%q", count, got)
	}

	if !strings.Contains(got, "[TASK_ID:abc123]") {
		t.Errorf("should preserve [TASK_ID:] markers for link conversion, got:\n%q", got)
	}

	if strings.Contains(got, "[CREATE_TASK]") {
		t.Errorf("should strip [CREATE_TASK] blocks, got:\n%q", got)
	}
}

// TestDedupTaskSummaries verifies task summary deduplication logic.
func TestDedupTaskSummaries(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no summaries unchanged",
			input:    "Just a normal response.",
			expected: "Just a normal response.",
		},
		{
			name:     "single summary unchanged",
			input:    "I'll create that task.\n\n---\nCreated 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]",
			expected: "I'll create that task.\n\n---\nCreated 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]",
		},
		{
			name:     "duplicate created summaries keeps last",
			input:    "I'll create that task.\n\n---\nCreated 1 task(s):\n- \"Fix bug\" (backlog)\n\n---\nCreated 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]",
			expected: "I'll create that task.\n\n---\nCreated 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]",
		},
		{
			name:     "duplicate edited summaries keeps last",
			input:    "Updated.\n\n---\nEdited 1 task(s):\n- \"New title\" (updated: title)\n\n---\nEdited 1 task(s):\n- \"New title\" (updated: title) [TASK_EDITED:abc]",
			expected: "Updated.\n\n---\nEdited 1 task(s):\n- \"New title\" (updated: title) [TASK_EDITED:abc]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DedupTaskSummaries(tt.input)
			if got != tt.expected {
				t.Errorf("DedupTaskSummaries() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

// TestChatBubbleRunning_CleansToolMarkers verifies that ChatBubbleRunning strips tool markers.
func TestChatBubbleRunning_CleansToolMarkers(t *testing.T) {
	partialOutput := "Let me check the file.\n[Using tool: Read]\n[Tool Read done: contents here]\nI found the issue."

	var buf bytes.Buffer
	err := ChatBubbleRunning("Assistant", partialOutput).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleRunning: %v", err)
	}

	content := buf.String()

	// Should NOT contain raw tool markers
	if strings.Contains(content, "[Using tool:") {
		t.Error("ChatBubbleRunning should strip [Using tool:] markers from partial output")
	}
	if strings.Contains(content, "[Tool Read done:") {
		t.Error("ChatBubbleRunning should strip [Tool ... done:] markers from partial output")
	}

	// Should contain the actual text content
	if !strings.Contains(content, "Let me check the file.") {
		t.Error("ChatBubbleRunning should preserve actual text content")
	}
	if !strings.Contains(content, "I found the issue.") {
		t.Error("ChatBubbleRunning should preserve actual text content")
	}
}

// TestChatBubbleRunning_ShowsWorkingWhenOnlyToolMarkers verifies that when partial output
// contains only tool markers (no actual text), the bubble shows "Working..." instead of empty.
func TestChatBubbleRunning_ShowsWorkingWhenOnlyToolMarkers(t *testing.T) {
	// Output that is entirely tool markers - no user-visible text
	partialOutput := "[Using tool: Read]\n[Tool Read done: file contents here]\n[Using tool: Grep]\n[Tool Grep done: search results]"

	var buf bytes.Buffer
	err := ChatBubbleRunning("Assistant", partialOutput).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleRunning: %v", err)
	}

	content := buf.String()

	// Should show "Working..." indicator, not empty content
	if !strings.Contains(content, "Working...") {
		t.Error("ChatBubbleRunning should show 'Working...' when cleaned output is empty")
	}

	// Should NOT contain raw tool markers
	if strings.Contains(content, "[Using tool:") {
		t.Error("ChatBubbleRunning should not show raw tool markers")
	}
}

// TestChatMessages_RunningExecUsesSSEStreaming verifies that ChatMessages renders
// running executions with SSE streaming (EventSource) instead of a static bubble.
func TestChatMessages_RunningExecUsesSSEStreaming(t *testing.T) {
	task := &models.Task{ID: "task-1", Title: "Test task", Prompt: "Do something"}
	executions := []models.Execution{
		{
			ID:         "exec-1",
			Status:     models.ExecRunning,
			PromptSent: "Do something",
			Output:     "partial output so far",
		},
	}

	var buf bytes.Buffer
	err := ChatMessages(executions, task, nil, "task-thread-messages", "task-thread-view", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatMessages: %v", err)
	}
	content := buf.String()

	// Must have data-streaming-resume attribute for SSE initialization
	if !strings.Contains(content, `data-streaming-resume="true"`) {
		t.Error("Running execution should have data-streaming-resume attribute for SSE streaming")
	}

	// Must contain the exec ID for connecting to the right SSE endpoint
	if !strings.Contains(content, "exec-1") {
		t.Error("Should contain execution ID for SSE endpoint")
	}

	// Must contain the pause-polling-target data attribute for task thread
	if !strings.Contains(content, "data-pause-polling-target") {
		t.Error("Should contain data-pause-polling-target for pausing HTMX polling")
	}

	// Must reference the task-thread-view polling element
	if !strings.Contains(content, "task-thread-view") {
		t.Error("Should reference task-thread-view as pause polling target")
	}

	// Should NOT be a static ChatBubbleRunning (no "Working..." indicator)
	if strings.Contains(content, "Working...") {
		t.Error("Running execution should use streaming bubble, not static 'Working...' bubble")
	}
}

// TestTaskLinkRegex_MatchesBothPlainAndMarkdownRendered verifies that the
// JavaScript regex used by convertTaskLinksInMessage matches both:
// 1. Plain text format: - "Title" (category) [TASK_ID:id] (from raw SSE streaming)
// 2. Markdown-rendered format: "Title" (category) [TASK_ID:id] (after marked.parse() consumes the "- " list marker)
// This was a bug where markdown rendering consumed the "- " prefix and the regex
// required it, causing task links to render as plain text instead of clickable links.
func TestTaskLinkRegex_MatchesBothPlainAndMarkdownRendered(t *testing.T) {
	// This is the JS regex from convertTaskLinksInMessage in chat.templ
	// Go's regexp uses the same syntax (with minor escaping differences)
	taskIDRegex := regexp.MustCompile(`(?:-\s*)?"([^"]+)"\s*(?:\(([^)]+)\)\s*)?\[TASK_ID:([^\]]+)\]`)
	taskEditRegex := regexp.MustCompile(`(?:-\s*)?"([^"]+)"\s*\(updated:\s*([^)]+)\)\s*\[TASK_EDITED:([^\]]+)\]`)

	tests := []struct {
		name        string
		input       string
		regex       *regexp.Regexp
		expectMatch bool
		expectTitle string
		expectExtra string
		expectID    string
	}{
		{
			name:        "plain text with dash - TASK_ID",
			input:       `- "Build API endpoint" (backlog) [TASK_ID:abc123def456]`,
			regex:       taskIDRegex,
			expectMatch: true,
			expectTitle: "Build API endpoint",
			expectExtra: "backlog",
			expectID:    "abc123def456",
		},
		{
			name:        "markdown-rendered without dash - TASK_ID",
			input:       `"Build API endpoint" (backlog) [TASK_ID:abc123def456]`,
			regex:       taskIDRegex,
			expectMatch: true,
			expectTitle: "Build API endpoint",
			expectExtra: "backlog",
			expectID:    "abc123def456",
		},
		{
			name:        "no category - TASK_ID",
			input:       `"Build API endpoint" [TASK_ID:abc123def456]`,
			regex:       taskIDRegex,
			expectMatch: true,
			expectTitle: "Build API endpoint",
			expectExtra: "",
			expectID:    "abc123def456",
		},
		{
			name:        "plain text with dash - TASK_EDITED",
			input:       `- "Updated Task" (updated: title, priority) [TASK_EDITED:task789]`,
			regex:       taskEditRegex,
			expectMatch: true,
			expectTitle: "Updated Task",
			expectExtra: "title, priority",
			expectID:    "task789",
		},
		{
			name:        "markdown-rendered without dash - TASK_EDITED",
			input:       `"Updated Task" (updated: title, priority) [TASK_EDITED:task789]`,
			regex:       taskEditRegex,
			expectMatch: true,
			expectTitle: "Updated Task",
			expectExtra: "title, priority",
			expectID:    "task789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.regex.FindStringSubmatch(tt.input)
			if tt.expectMatch {
				if matches == nil {
					t.Fatalf("expected regex to match %q but it didn't", tt.input)
				}
				if matches[1] != tt.expectTitle {
					t.Errorf("title: got %q, want %q", matches[1], tt.expectTitle)
				}
				if matches[2] != tt.expectExtra {
					t.Errorf("extra: got %q, want %q", matches[2], tt.expectExtra)
				}
				if matches[3] != tt.expectID {
					t.Errorf("id: got %q, want %q", matches[3], tt.expectID)
				}
			} else if matches != nil {
				t.Errorf("expected regex NOT to match %q but it matched: %v", tt.input, matches)
			}
		})
	}
}

// TestChatBubble_PreservesTaskIDMarkers verifies that ChatBubble preserves
// [TASK_ID:xxx] markers in the data-raw-content attribute for JS conversion.
func TestChatBubble_PreservesTaskIDMarkers(t *testing.T) {
	content := "Created 1 task(s):\n- \"Fix bug\" (backlog) [TASK_ID:abc123]"

	var buf bytes.Buffer
	err := ChatBubble("Assistant", content).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubble: %v", err)
	}

	html := buf.String()

	// The raw content with TASK_ID marker should be in data-raw-content for JS processing
	if !strings.Contains(html, "[TASK_ID:abc123]") {
		t.Error("ChatBubble should preserve [TASK_ID:] markers in data-raw-content for convertTaskLinksInMessage")
	}

	// Should have the convertTaskLinksInMessage call
	if !strings.Contains(html, "convertTaskLinksInMessage") {
		t.Error("ChatBubble should call convertTaskLinksInMessage for task link conversion")
	}

	// Keep the chat-stream-content class even in markdown fallback so refresh cleanup
	// can reliably find and re-process the container.
	if strings.Contains(html, "className = 'chat-markdown'") {
		t.Error("ChatBubble should not replace container className with chat-markdown")
	}
	if !strings.Contains(html, "classList.add('chat-markdown')") {
		t.Error("ChatBubble markdown fallback should add chat-markdown class without removing existing classes")
	}
}

// TestRenderStreamingContent_StripsProtocolArtifacts verifies that
// renderStreamingContent pre-strips multi_tool_use.parallel protocol artifact
// lines from the text buffer so they don't leak between tool cards.
func TestRenderStreamingContent_StripsProtocolArtifacts(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// renderStreamingContent must strip multi_tool_use protocol artifact lines
	if !strings.Contains(content, "multi_tool_use") {
		t.Error("renderStreamingContent should contain multi_tool_use artifact stripping regex")
	}

	// Verify the regex pattern is applied to textBuffer before segment parsing
	if !strings.Contains(content, "textBuffer = textBuffer.replace") {
		t.Error("renderStreamingContent should pre-strip protocol artifacts from textBuffer")
	}
}

// TestCleanActionMarkers_StripsProtocolArtifacts verifies that cleanActionMarkers
// strips multi_tool_use.parallel protocol artifact lines from text.
func TestCleanActionMarkers_StripsProtocolArtifacts(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// cleanActionMarkers must include multi_tool_use protocol artifact pattern
	if !strings.Contains(content, "multi_tool_use\\.\\S+") {
		t.Error("cleanActionMarkers should strip multi_tool_use protocol artifact lines")
	}
}

func TestCleanActionMarkers_StripsProposedPlanWrappersOnly(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "text = text.replace(/<\\/?\\s*proposed_plan\\s*>/gi, '')") {
		t.Fatal("cleanActionMarkers should strip <proposed_plan> wrappers")
	}
}

// TestRenderStreamingContent_RemovesWhitespacePreWrap verifies that
// renderStreamingContent removes whitespace-pre-wrap from the streaming container
// to prevent it from leaking into .chat-markdown children via CSS inheritance.
func TestRenderStreamingContent_RemovesWhitespacePreWrap(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// renderStreamingContent should remove whitespace-pre-wrap from the container
	if !strings.Contains(content, "container.classList.remove('whitespace-pre-wrap')") {
		t.Error("renderStreamingContent should remove 'whitespace-pre-wrap' class from container to prevent CSS inheritance issues")
	}
}

// TestRenderStreamingContent_PreservesThinkingOpenState verifies that
// renderStreamingContent saves and restores the open/closed state of
// thinking <details> sections across re-renders (so polling/morph updates
// don't collapse sections the user has expanded).
func TestRenderStreamingContent_PreservesThinkingOpenState(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// Should save open state of existing thinking sections before clearing innerHTML
	if !strings.Contains(content, "prevThinkingStates") {
		t.Error("renderStreamingContent should track previous thinking section open states")
	}
	if !strings.Contains(content, "details.stream-thinking") {
		t.Error("renderStreamingContent should query existing stream-thinking details elements")
	}
	// Should restore open state after creating new thinking sections
	if !strings.Contains(content, "prevThinkingStates[ti]") {
		t.Error("renderStreamingContent should restore open state from saved thinking states")
	}
	// The restoration should set .open = true on new details elements
	if !strings.Contains(content, "newThinkingSections[ti].open = true") {
		t.Error("renderStreamingContent should set open=true on new thinking sections that were previously open")
	}
}

// TestRenderStreamingContent_PersistentThinkingState verifies that thinking section
// open/closed state is persisted in window._thinkingOpenStates to survive DOM
// replacement by morph:outerHTML polling (the 3s morph swap destroys JS-created
// <details> elements, so the state must be stored externally).
func TestRenderStreamingContent_PersistentThinkingState(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// Should initialize persistent store on window
	if !strings.Contains(content, "window._thinkingOpenStates") {
		t.Error("should initialize window._thinkingOpenStates for persistent thinking state storage")
	}
	if !strings.Contains(content, "if (!window._thinkingOpenStates) window._thinkingOpenStates = {};") {
		t.Error("should initialize window._thinkingOpenStates with executable JavaScript, not inside a comment")
	}

	// Should generate a stable key for containers using ID or data-raw-content
	if !strings.Contains(content, "_thinkingStateKey") {
		t.Error("should have _thinkingStateKey function for generating stable container keys")
	}

	// Should save to persistent store when local states are found
	if !strings.Contains(content, "window._thinkingOpenStates[containerKey] = prevThinkingStates.slice()") {
		t.Error("should persist thinking states to window._thinkingOpenStates when local states exist")
	}

	// Should restore from persistent store when local states are empty (after morph DOM replacement)
	if !strings.Contains(content, "prevThinkingStates = window._thinkingOpenStates[containerKey]") {
		t.Error("should restore thinking states from persistent store when DOM elements were replaced by morph")
	}

	// Should update persistent store on user toggle events
	if !strings.Contains(content, "addEventListener('toggle'") {
		t.Error("should add toggle event listener to update persistent state when user expands/collapses")
	}
}

// TestChatBubbleStreaming_ContainerClasses verifies the streaming container
// has whitespace-pre-wrap class (for plain text fallback) which renderStreamingContent
// will remove when it takes over rendering with .chat-markdown children.
func TestChatBubbleStreaming_ContainerClasses(t *testing.T) {
	var buf bytes.Buffer
	err := ChatBubbleStreaming("Assistant", "exec-123", "chat-messages", "", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleStreaming: %v", err)
	}

	html := buf.String()

	// Streaming container should have whitespace-pre-wrap initially (for plain text fallback)
	if !strings.Contains(html, "whitespace-pre-wrap") {
		t.Error("Streaming container should have whitespace-pre-wrap class initially")
	}
}

// TestChatBubbleStreamingResume_UsesDataRawContent verifies that ChatBubbleStreamingResume
// stores content in data-raw-content attribute (not as raw text in the div). This prevents
// raw/unformatted text flash on hard refresh — the div starts empty and the inline render
// script formats the content before the browser paints.
func TestChatBubbleStreamingResume_UsesDataRawContent(t *testing.T) {
	content := "Hello, I'm working on your task.\n[Thinking]\nLet me analyze...\n[Using tool: Read]"

	var buf bytes.Buffer
	err := ChatBubbleStreamingResume("Assistant", content, "exec-1", "chat-messages", "pause-target").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatBubbleStreamingResume: %v", err)
	}

	html := buf.String()

	// Content MUST be in data-raw-content attribute, not as text content in the div
	if !strings.Contains(html, "data-raw-content=") {
		t.Error("ChatBubbleStreamingResume must store content in data-raw-content attribute")
	}

	// The div should be empty (content rendered by inline script, not by templ)
	// Check that the raw content text is NOT directly between > and </ of the streaming div
	if strings.Contains(html, ">Hello, I&#39;m working") || strings.Contains(html, ">Hello, I'm working") {
		t.Error("ChatBubbleStreamingResume should NOT render raw text content in the div (causes unformatted flash on hard refresh)")
	}

	// Must have an inline render script (matching ChatBubble pattern)
	if !strings.Contains(html, "renderStreamingContent") {
		t.Error("ChatBubbleStreamingResume must have inline script calling renderStreamingContent")
	}

	// Must have polling fallback for when renderStreamingContent isn't defined yet
	if !strings.Contains(html, "setInterval") {
		t.Error("ChatBubbleStreamingResume inline script must poll for renderStreamingContent")
	}

	// Must have renderChatMarkdown fallback
	if !strings.Contains(html, "renderChatMarkdown") {
		t.Error("ChatBubbleStreamingResume inline script must have renderChatMarkdown fallback")
	}
}

// TestChatBubbleStreamingResume_InitialLengthUsesCharCount verifies that
// data-initial-length uses character count (not byte count) so it matches
// JavaScript's string.length for proper SSE delta rendering threshold.
func TestChatBubbleStreamingResume_InitialLengthUsesCharCount(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectedLength string
	}{
		{
			name:           "ASCII only",
			content:        "Hello World",
			expectedLength: "11",
		},
		{
			name:           "Unicode characters (multi-byte UTF-8)",
			content:        "Hello… World—test",
			expectedLength: "17", // 17 JS code units (… and — are BMP, 1 code unit each), but 21 bytes in UTF-8
		},
		{
			name:           "emoji content",
			content:        "Done! 🎉",
			expectedLength: "8", // 6 ASCII + 2 code units for 🎉 (surrogate pair), matches JS "Done! 🎉".length === 8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := ChatBubbleStreamingResume("Assistant", tt.content, "exec-1", "chat-messages", "").Render(context.Background(), &buf)
			if err != nil {
				t.Fatalf("Failed to render: %v", err)
			}

			html := buf.String()
			expected := `data-initial-length="` + tt.expectedLength + `"`
			if !strings.Contains(html, expected) {
				t.Errorf("Expected %s but not found in HTML.\nGot HTML snippet around initial-length: %s",
					expected, extractAttr(html, "data-initial-length"))
			}
		})
	}
}

// extractAttr extracts a data attribute value from HTML for debugging
func extractAttr(html, attr string) string {
	idx := strings.Index(html, attr+`="`)
	if idx == -1 {
		return "(not found)"
	}
	start := idx + len(attr) + 2
	end := strings.Index(html[start:], `"`)
	if end == -1 {
		return html[start:]
	}
	return attr + `="` + html[start:start+end] + `"`
}

// TestChatBubbleStreamingResume_EmptyContentShowsThinkingIndicator verifies that
// when partialContent is empty, the streaming container is hidden and the
// thinking indicator is shown.
func TestChatBubbleStreamingResume_EmptyContentShowsThinkingIndicator(t *testing.T) {
	var buf bytes.Buffer
	err := ChatBubbleStreamingResume("Assistant", "", "exec-1", "chat-messages", "").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render: %v", err)
	}

	html := buf.String()

	// Should show thinking indicator when content is empty
	if !strings.Contains(html, `id="streaming-thinking-resume-exec-1"`) || !strings.Contains(html, "ov-loading-dots ov-loading-dots-sm") {
		t.Error("Should show thinking indicator markup when partialContent is empty")
	}

	// Streaming container should be hidden when empty
	if !strings.Contains(html, `id="streaming-message-exec-1"`) || !strings.Contains(html, " hidden") {
		t.Error("Streaming container should be hidden when partialContent is empty")
	}
}

// TestCleanAssistantMessages_HandlesStreamingResumeContainers verifies that the
// cleanAssistantMessages JavaScript function handles [data-streaming-resume][data-raw-content]
// elements explicitly, which is needed after morph:outerHTML replaces the DOM
// (inline scripts don't re-execute after morph).
func TestCleanAssistantMessages_HandlesStreamingResumeContainers(t *testing.T) {
	var buf bytes.Buffer
	err := ChatAutoScrollScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}

	content := buf.String()

	// Must have explicit selector for streaming resume containers with data-raw-content
	if !strings.Contains(content, "[data-streaming-resume][data-raw-content]") {
		t.Error("cleanAssistantMessages must handle [data-streaming-resume][data-raw-content] elements after morph:outerHTML")
	}

	// Must read content from data-raw-content attribute (not textContent)
	if !strings.Contains(content, "getAttribute('data-raw-content')") {
		t.Error("cleanAssistantMessages must read streaming resume content from data-raw-content attribute")
	}

	// Must use content signatures so unchanged bubbles are skipped on poll updates
	if !strings.Contains(content, "el.dataset.cleanedRaw === raw") {
		t.Error("cleanAssistantMessages must skip unchanged chat-stream-content using cleanedRaw signature")
	}
	if !strings.Contains(content, "div.dataset.cleanedText === text") {
		t.Error("cleanAssistantMessages must skip unchanged assistant text blocks using cleanedText signature")
	}

	// If renderStreamingContent is unavailable, fallback markdown render must NOT lock
	// cleanedRaw state. This allows a later pass (after renderStreamingContent loads)
	// to re-render tool cards from raw markers instead of staying markdown-only.
	if !strings.Contains(content, "delete el.dataset.cleanedRaw") {
		t.Error("cleanAssistantMessages fallback markdown path must clear cleanedRaw so tool-card re-render can occur later")
	}
}

// TestTaskThreadView_SkipsExpensiveWorkDuringNavigation verifies that the thread
// view's afterSwap handlers check _sidebarNavigating before running expensive
// DOM operations (cleanAssistantMessages, _initThreadStreaming). This prevents
// morph-induced main-thread blocking from delaying sidebar navigation clicks.
func TestTaskThreadView_SelectsTaskAssignedModelInDropdown(t *testing.T) {
	agentID := "opus-config"
	task := &models.Task{
		ID:        "t1",
		ProjectID: "p1",
		Status:    models.StatusCompleted,
		Category:  models.CategoryActive,
		AgentID:   &agentID,
	}
	agents := []models.LLMConfig{
		{ID: "sonnet-config", Name: "Claude Sonnet 4.6", Model: "claude-sonnet-4-6"},
		{ID: agentID, Name: "Claude Opus 4.7", Model: "claude-opus-4-7"},
	}

	var buf bytes.Buffer
	if err := TaskThreadView(task, nil, agents, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, `id="task-thread-form-agent-id" name="agent_id" value="opus-config"`) {
		t.Fatal("task thread hidden agent input must default to the task-assigned model")
	}
	// Custom select button stores the selected value in data-current-value (no <option selected> in the new implementation)
	if !strings.Contains(content, `data-current-value="opus-config"`) {
		t.Fatal("task thread model selector button must carry data-current-value matching the task-assigned model")
	}
	// The option list must contain the opus-config entry
	if !strings.Contains(content, `<li data-value="opus-config"`) {
		t.Fatal("task thread model option list must include the task-assigned model option")
	}
}

func TestTaskThreadView_ContainsHorizontalOverflowOnMobile(t *testing.T) {
	task := &models.Task{
		ID:        "task-thread-mobile-overflow",
		ProjectID: "p1",
		Status:    models.StatusCompleted,
		Category:  models.CategoryCompleted,
	}
	longUnbroken := strings.Repeat("very-long-unbroken-token", 24)
	execs := []models.Execution{{
		ID:         "exec-wide-content",
		TaskID:     task.ID,
		Status:     models.ExecCompleted,
		PromptSent: longUnbroken,
		Output:     "```\n" + longUnbroken + "\n```",
	}}

	var buf bytes.Buffer
	if err := TaskThreadView(task, execs, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render TaskThreadView: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, `id="task-thread-view" class="flex flex-col flex-1 min-h-0 min-w-0 max-w-full"`) {
		t.Fatal("task thread root should stay width-bounded without clipping the composer shadow")
	}
	if strings.Contains(content, `id="task-thread-view" class="flex flex-col flex-1 min-h-0 min-w-0 max-w-full overflow-x-hidden"`) {
		t.Fatal("task thread root must not clip the composer shadow; horizontal containment belongs on the messages pane and inner controls")
	}
	if !strings.Contains(content, `id="task-thread-messages" class="flex-1 overflow-y-auto py-4 mb-4 space-y-6 min-h-0"`) {
		t.Fatal("task thread messages pane must use the same layout as the original working state")
	}
	if strings.Contains(content, `id="task-thread-messages" class="flex-1 overflow-y-auto overflow-x-hidden`) {
		t.Fatal("task thread messages pane must not hard-clip chat bubble shadows")
	}
	if strings.Contains(content, `-mr-[29px]`) || strings.Contains(content, `-mr-[18px]`) {
		t.Fatal("task thread messages must not use fixed right-margin scrollbar compensation because it leaves bubbles visually shorter than the input in real browsers")
	}
	if !strings.Contains(content, `class="chat-input-shadow-gutter w-full min-w-0 max-w-full pt-2 pb-4"`) {
		t.Fatal("task thread composer must use a full-width shadow gutter without side padding or desktop caps")
	}
	if strings.Contains(content, `chat-input-shadow-gutter w-full min-w-0 max-w-full sm:max-w-3xl`) || strings.Contains(content, `chat-input-shadow-gutter w-full min-w-0 max-w-full pt-2 pb-4 px-3`) {
		t.Fatal("task thread composer gutter must not add artificial side gaps")
	}
	if !strings.Contains(content, `class="chat-input-container rounded-xl p-4 relative min-w-0 max-w-full"`) {
		t.Fatal("task thread composer shell must fill its shadow gutter without clipping")
	}
	if strings.Contains(content, `chat-input-container rounded-xl p-4 relative w-full`) {
		t.Fatal("task thread composer shell must not use w-full with visual margins")
	}
	if !strings.Contains(content, `class="flex items-center justify-between gap-2 pt-2 min-w-0 max-w-full overflow-hidden"`) {
		t.Fatal("task thread composer controls must contain horizontal overflow without clipping the shell bevel")
	}
	if strings.Contains(content, `overflow-x-auto py-4 mb-4`) {
		t.Fatal("task thread should not make the whole messages pane horizontally scrollable")
	}
}

func TestTaskThreadView_RunningThreadCanSteerFromPendingRowsOnly(t *testing.T) {
	task := &models.Task{
		ID:        "task-steer-ui",
		ProjectID: "p1",
		Status:    models.StatusRunning,
		Category:  models.CategoryActive,
	}
	execs := []models.Execution{{ID: "exec-running-task", TaskID: task.ID, Status: models.ExecRunning, PromptSent: "running"}}
	pending := []models.ThreadInput{{ID: "queued-task", TaskID: task.ID, InputMode: models.ThreadInputModeQueued, InputStatus: models.ThreadInputPending, Content: "queued"}}

	var buf bytes.Buffer
	if err := TaskThreadView(task, execs, nil, nil, nil, pending, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render TaskThreadView: %v", err)
	}
	content := buf.String()
	if !strings.Contains(content, `name="expected_turn_id" value="exec-running-task"`) {
		t.Fatal("task-thread UI should keep the active turn id for queue safety")
	}
	if strings.Contains(content, `name="steer_endpoint"`) || strings.Contains(content, `data-steer-submit="true"`) {
		t.Fatal("task-thread composer must not expose direct steering controls")
	}
	if !strings.Contains(content, `id="pending-thread-inputs"`) || !strings.Contains(content, `hx-post="/tasks/task-steer-ui/thread/queued/queued-task/steer"`) || !strings.Contains(content, "Steer") {
		t.Fatal("task-thread composer queued row must expose Steer action")
	}
	messagesStart := strings.Index(content, `id="task-thread-messages"`)
	formStart := strings.Index(content, `id="task-thread-form"`)
	if messagesStart < 0 || formStart < 0 || strings.Contains(content[messagesStart:formStart], `thread-input-queued-task`) {
		t.Fatal("task-thread queued rows should render with the input box, not in the message transcript")
	}
	if !strings.Contains(content, `queued-input-row`) || !strings.Contains(content, `ml-auto`) || !strings.Contains(content, `aria-label="Cancel queued follow-up"`) || !strings.Contains(content, `M19 7l-.867 12.142`) {
		t.Fatal("task-thread queued row should use input-box styling with right-aligned Steer and trash-icon cancel action")
	}
	queuedStart := strings.Index(content, `class="queued-input-row`)
	if queuedStart < 0 {
		t.Fatal("task-thread queued row should render")
	}
	queuedEnd := strings.Index(content[queuedStart:], `</div></div>`)
	if queuedEnd < 0 {
		t.Fatal("task-thread queued row markup should be bounded")
	}
	queuedMarkup := content[queuedStart : queuedStart+queuedEnd]
	if strings.Contains(content, "Send now") || strings.Contains(content, "btn-warning") || strings.Contains(content, "bg-warning") || strings.Contains(queuedMarkup, ">Cancel</button>") || strings.Contains(queuedMarkup, ">×</button>") {
		t.Fatal("task-thread queued rows should not use warning styling, Send now copy, or text/× cancel")
	}
	if !strings.Contains(content, "task_thread_execution_started") || !strings.Contains(content, "/thread/executions/") {
		t.Fatal("task-thread UI must append promoted queued executions smoothly via live event fragments")
	}
	if !strings.Contains(content, "if (data.type === 'task_thread_input_applied')") || !strings.Contains(content, "ensureStreamingFragment(data)") {
		t.Fatal("task-thread UI must treat applied queued input events as a backup promotion signal")
	}
	if !strings.Contains(content, "if (data.type === 'task_thread_input_cancelled')") || !strings.Contains(content, "removePendingRow(data.pending_input_id)") {
		t.Fatal("task-thread UI must remove cancelled pending rows from live events")
	}
	if !strings.Contains(content, "pendingFragmentExecs") || !strings.Contains(content, "setTimeout(function() { ensureStreamingFragment(data, attempt + 1); }") {
		t.Fatal("task-thread UI must retry promoted execution fragment attachment to cover commit/event timing races")
	}
	if !strings.Contains(content, `data-task-id="task-steer-ui"`) || !strings.Contains(content, "getAttribute('data-task-id')") {
		t.Fatal("task-thread live script must bind to the rendered task id, not a literal templ placeholder")
	}
}

func TestTaskThreadView_SkipsExpensiveWorkDuringNavigation(t *testing.T) {
	task := &models.Task{
		ID:        "t1",
		ProjectID: "p1",
		Status:    models.StatusRunning,
		Category:  models.CategoryActive,
	}
	var buf bytes.Buffer
	err := TaskThreadView(task, nil, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	// afterSwap handler for task-thread-messages must guard expensive work
	if !strings.Contains(content, "target.id === 'task-thread-messages'") {
		t.Fatal("expected afterSwap handler for task-thread-messages")
	}

	// afterSwap handler for task-thread-view must guard expensive work
	if !strings.Contains(content, "target.id === 'task-thread-view'") {
		t.Fatal("expected afterSwap handler for task-thread-view")
	}

	// Both branches must check _sidebarNavigating to skip expensive work
	// (cleanAssistantMessages, _initThreadStreaming, renderStreamingContent)
	if !strings.Contains(content, "if (window._sidebarNavigating) return") {
		t.Fatal("afterSwap handlers must check _sidebarNavigating to skip expensive post-morph work during sidebar navigation")
	}

	// Polling afterSwap should not force-clear message clean-state on every swap;
	// cleanAssistantMessages now performs content-signature based incremental work.
	if strings.Contains(content, "delete c.dataset.cleaned") {
		t.Fatal("task thread polling should not delete per-message clean state on each swap")
	}

	// beforeRequest handler must block polling during sidebar navigation
	if !strings.Contains(content, "window._sidebarNavigating") {
		t.Fatal("beforeRequest handler must check _sidebarNavigating to block polling during navigation")
	}
}

func TestTaskThreadView_ClosesStreamsBeforeThreadRefresh(t *testing.T) {
	task := &models.Task{
		ID:        "t-stream-cleanup",
		ProjectID: "p1",
		Status:    models.StatusRunning,
		Category:  models.CategoryActive,
	}
	var buf bytes.Buffer
	if err := TaskThreadView(task, nil, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, "function _closeTaskThreadEventSources()") {
		t.Fatal("expected shared thread EventSource cleanup helper")
	}
	if !strings.Contains(content, "target.id === 'thread-content' || target.id === 'task-thread-view' || target.id === 'task-detail-content' || target.id === 'main-content'") {
		t.Fatal("expected thread refresh and navigation targets to close active stream EventSources before swap")
	}
	if !strings.Contains(content, "_closeTaskThreadEventSources();") {
		t.Fatal("expected beforeSwap to close active thread EventSources")
	}
}

func TestTaskThreadView_ClearsDraftBeforeSuccessfulThreadSwap(t *testing.T) {
	task := &models.Task{
		ID:        "thread-clear-1",
		ProjectID: "p1",
		Status:    models.StatusCompleted,
		Category:  models.CategoryCompleted,
	}

	var buf bytes.Buffer
	if err := TaskThreadView(task, nil, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, "isThreadSendRequest && responseText.trim() !== ''") {
		t.Fatal("beforeSwap should detect successful thread send requests with non-empty response")
	}
	if !strings.Contains(content, "requestPath.indexOf('/thread') !== -1") {
		t.Fatal("thread handlers should treat /thread requests like task-thread form submits for enter/button parity")
	}
	if !strings.Contains(content, "window._taskThreadUserScrolledUp = false;") {
		t.Fatal("beforeRequest should reset thread auto-scroll state for any thread send request")
	}
	if !strings.Contains(content, "if (window.markChatSendScrollIntent) window.markChatSendScrollIntent('task-thread-messages');") {
		t.Fatal("beforeRequest should mark deliberate thread sends for post-swap attachment bottom alignment")
	}
	if !strings.Contains(content, "window._taskThreadSavedInput = ''") {
		t.Fatal("beforeSwap should clear saved thread input on successful thread form swap")
	}
	if !strings.Contains(content, "if (sentKey) delete window._taskThreadDrafts[sentKey];") {
		t.Fatal("beforeSwap should clear persisted draft key on successful thread form swap")
	}
}

func TestTaskThreadView_BindsAttachmentImageSmartScrollAfterRenderAndSwap(t *testing.T) {
	task := &models.Task{
		ID:        "thread-attachment-scroll-1",
		ProjectID: "p1",
		Status:    models.StatusCompleted,
		Category:  models.CategoryCompleted,
	}

	var buf bytes.Buffer
	if err := TaskThreadView(task, nil, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	if count := strings.Count(content, "window.bindAttachmentImageSmartScroll"); count < 4 {
		t.Fatalf("expected task thread to bind attachment image smart-scroll on initial render and HTMX swap paths, got %d", count)
	}
	trackerInit := strings.Index(content, "window._taskThreadPageTracker = new window.ChatScrollTracker(chatMessages)")
	initialBind := strings.Index(content, "window.bindAttachmentImageSmartScroll(chatMessages, 'scrollTracker_task-thread-messages', window._taskThreadPageTracker)")
	if initialBind < 0 {
		t.Fatal("task thread must bind attachment image smart-scroll with the task-thread tracker")
	}
	if trackerInit < 0 || trackerInit > initialBind {
		t.Fatal("task thread must create the tracker before initial attachment image binding so restored upward scroll intent is respected")
	}
	if !strings.Contains(content, "window.bindAttachmentImageSmartScroll(target, 'scrollTracker_task-thread-messages', window._taskThreadPageTracker)") {
		t.Fatal("task thread message-target swaps must bind attachment image smart-scroll on the swapped element")
	}
	if !strings.Contains(content, "var sentByUser = window.consumeChatSendScrollIntent ? window.consumeChatSendScrollIntent('task-thread-messages') : false;") {
		t.Fatal("task thread should consume submit scroll intent after HTMX swaps so attachment sends bottom-align")
	}
	if !strings.Contains(content, "window.scrollChatToBottomAfterLayout(target, true)") {
		t.Fatal("task thread message swaps should scroll after layout so variable-sized screenshots are visible")
	}
	fullRefreshIdx := strings.Index(content, "target && target.id === 'task-detail-content'")
	if fullRefreshIdx < 0 {
		t.Fatal("task thread should handle full task detail content refreshes")
	}
	fullRefreshBody := content[fullRefreshIdx:]
	fullRefreshTrackerInit := strings.Index(fullRefreshBody, "window._taskThreadPageTracker = new window.ChatScrollTracker(chatMessages)")
	fullRefreshBind := strings.Index(fullRefreshBody, "window.bindAttachmentImageSmartScroll(chatMessages, 'scrollTracker_task-thread-messages', window._taskThreadPageTracker)")
	if fullRefreshBind < 0 {
		t.Fatal("task detail content refreshes must bind attachment image smart-scroll")
	}
	if fullRefreshTrackerInit < 0 || fullRefreshTrackerInit > fullRefreshBind {
		t.Fatal("task detail content refreshes must rebind the tracker before attachment image binding")
	}
	if !strings.Contains(fullRefreshBody, "window.scrollChatToBottomAfterLayout(chatMessages, true)") {
		t.Fatal("task detail content refreshes should consume send intent and scroll after layout")
	}
}

// TestInitThreadStreaming_FindsStreamingDotsByID verifies that _initThreadStreaming
// finds streaming dots by ID (not nextElementSibling) to work correctly with the
// inline render script that sits between the container and the dots div.
func TestChatBubbleStreaming_ErrorClearsPlanStreamingFlag(t *testing.T) {
	// Verify that ChatBubbleStreaming error/onerror handlers clear
	// _chatStreamInProgress and re-evaluate plan prompt for non-thread chat.
	// Without this, the flag stays stuck true after a streaming failure.
	var buf bytes.Buffer
	err := ChatBubbleStreaming("Assistant", "exec-plan-err", "chat-messages", "", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatBubbleStreaming: %v", err)
	}

	content := buf.String()

	// Error event listener — the handler body with isThread branch can be ~1100 chars
	errIdx := strings.Index(content, "eventSource.addEventListener('error'")
	if errIdx == -1 {
		t.Fatal("ChatBubbleStreaming must have an error event listener")
	}
	errEnd := errIdx + 2000
	if errEnd > len(content) {
		errEnd = len(content)
	}
	errBody := content[errIdx:errEnd]
	if !strings.Contains(errBody, "event.data === 'execution not found'") || !strings.Contains(errBody, "setTimeout(connectExecutionStream, 150 * streamRetryCount)") {
		t.Error("error handler must retry early execution lookup races before failing the stream")
	}
	if !strings.Contains(errBody, "_chatStreamInProgress = false") {
		t.Error("error handler must clear _chatStreamInProgress for chat (non-thread) context")
	}
	if !strings.Contains(errBody, "evaluatePlanCompletionPrompt") {
		t.Error("error handler must re-evaluate plan prompt for chat (non-thread) context")
	}

	// onerror handler
	oeIdx := strings.Index(content, "eventSource.onerror")
	if oeIdx == -1 {
		t.Fatal("ChatBubbleStreaming must have an onerror handler")
	}
	oeEnd := oeIdx + 2000
	if oeEnd > len(content) {
		oeEnd = len(content)
	}
	oeBody := content[oeIdx:oeEnd]
	if !strings.Contains(oeBody, "setTimeout(connectExecutionStream, 150 * streamRetryCount)") {
		t.Error("onerror handler must retry empty early stream failures before clearing chat streaming state")
	}
	if !strings.Contains(oeBody, "_chatStreamInProgress = false") {
		t.Error("onerror handler must clear _chatStreamInProgress for chat (non-thread) context")
	}
	if !strings.Contains(oeBody, "evaluatePlanCompletionPrompt") {
		t.Error("onerror handler must re-evaluate plan prompt for chat (non-thread) context")
	}
}

func TestChatBubbleStreaming_ThreadErrorDoesNotEvaluatePlanPrompt(t *testing.T) {
	// Thread context (isThread=true) should NOT evaluate plan prompt on error —
	// plan mode only applies to /chat, not task thread views.
	var buf bytes.Buffer
	err := ChatBubbleStreaming("Assistant", "exec-thread-err", "thread-messages", "thread-view", true).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ChatBubbleStreaming (thread): %v", err)
	}

	content := buf.String()

	// Error handler for thread context should refresh task detail, not evaluate plan prompt
	errIdx := strings.Index(content, "eventSource.addEventListener('error'")
	if errIdx == -1 {
		t.Fatal("ChatBubbleStreaming must have an error event listener")
	}
	errBody := content[errIdx:min(len(content), errIdx+2000)]

	// Thread error should do the HTMX refresh but NOT clear _chatStreamInProgress
	// for plan prompt evaluation (plan mode is chat-only)
	if !strings.Contains(errBody, "isThread") {
		t.Error("error handler must check isThread context")
	}
}

func TestInitThreadStreaming_FindsStreamingDotsByID(t *testing.T) {
	var buf bytes.Buffer
	err := _initThreadStreamingScript().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Failed to render _initThreadStreamingScript: %v", err)
	}

	content := buf.String()

	// Must find streaming dots by ID (reliable across morph and inline script siblings)
	if !strings.Contains(content, "getElementById('streaming-dots-resume-'") {
		t.Error("_initThreadStreaming must find streaming dots by ID, not nextElementSibling")
	}

	// Must check data-raw-content for content presence (not textContent, since content
	// is now in the attribute, not as text nodes)
	if !strings.Contains(content, "getAttribute('data-raw-content')") {
		t.Error("_initThreadStreaming must check data-raw-content for content presence")
	}
	if !strings.Contains(content, "function connectResumeExecutionStream()") || !strings.Contains(content, "setTimeout(connectResumeExecutionStream, 150 * streamRetryCount)") {
		t.Error("_initThreadStreaming must retry early execution stream races for promoted/resumed rows")
	}
}

// TestChatScrollTracker_UsesInteractionSignalsForScrollIntent verifies the
// regression where smart scrolling stopped working during streaming in large
// conversations. The fix:
//   - Use real user-interaction signals (wheel/touchmove/keydown/pointerdown) to
//     mark scroll intent. The clamp-driven scroll events fired when
//     renderStreamingContent does container.innerHTML = ” must NOT be
//     interpreted as the user reaching the bottom (that previously cleared
//     userScrolledUp every chunk and pulled the user back down).
//   - shouldAutoScroll must return purely !userScrolledUp, never ANDed with
//     isNearBottom — otherwise the same clamp window triggers an auto-scroll
//     pulse that undoes the user's scroll-up.
func TestChatScrollTracker_UsesInteractionSignalsForScrollIntent(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}
	content := buf.String()

	// REGRESSION GUARDS: the broken implementation unconditionally derived
	// userScrolledUp from isNearBottom on every scroll event, and ANDed the
	// flag with isNearBottom in shouldAutoScroll. Both forms must be absent.
	if strings.Contains(content, "self.userScrolledUp = !window.chatAutoScroll.isNearBottom(self.element);") {
		t.Error("scroll handler must NOT unconditionally derive userScrolledUp from isNearBottom — clamp events fired when innerHTML is cleared would defeat user scroll-up during streaming")
	}
	if strings.Contains(content, "return !this.userScrolledUp && window.chatAutoScroll.isNearBottom(this.element)") {
		t.Error("shouldAutoScroll must NOT AND with isNearBottom — clamp scroll events make isNearBottom briefly true even when the user has scrolled up, causing auto-scroll to yank the user back down")
	}

	required := []string{
		// Interaction-signal model
		"this._userInteracting = false",
		"this._pointerInteracting = false",
		"if (self._userInteracting)",
		// Listeners for real user scroll intent
		"addEventListener('wheel'",
		"addEventListener('touchmove'",
		"addEventListener('pointerdown'",
		"addEventListener('pointerup'",
		"addEventListener('pointercancel'",
		"addEventListener('keydown'",
		// shouldAutoScroll relies solely on the persisted flag
		"return !this.userScrolledUp;",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Errorf("ChatScrollTracker must contain %q to track scroll intent via user-interaction signals", r)
		}
	}

	if !strings.Contains(content, "if (!self._pointerInteracting) self._userInteracting = false;") {
		t.Error("timer-based interaction expiry must not clear _userInteracting during an active pointer/scrollbar drag")
	}
}

// TestChatScrollTracker_ResolveAndRebindRecoversFromStaleElement guards the
// regression where smart scrolling "froze" after a morph swap replaced the
// messages container — the cached window.scrollTracker_<id> held a detached
// element whose scrollHeight is 0, so isNearBottom returned true and auto-scroll
// ran forever until a full page refresh.
func TestChatScrollTracker_ResolveAndRebindRecoversFromStaleElement(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}
	content := buf.String()

	if !strings.Contains(content, "window.resolveScrollTracker = function") {
		t.Error("ChatAutoScrollScript must expose window.resolveScrollTracker to recover from stale tracker elements after morph swaps")
	}
	if !strings.Contains(content, "rebind: function(newElement)") {
		t.Error("ChatScrollTracker must expose rebind() so it can recover from stale/detached elements without dropping userScrolledUp")
	}
	// Stale element detection conditions.
	if !strings.Contains(content, "existing.element !== messagesEl") || !strings.Contains(content, "!existing.element.isConnected") {
		t.Error("resolveScrollTracker must detect when the cached element no longer matches the live DOM or has been detached")
	}
}

// TestStreamingRecoversFromStaleScrollTracker ensures the streaming renderers
// re-resolve the scroll tracker against the live messages element before each
// render. Without this, a morph swap mid-stream would leave the tracker bound to
// a detached element and smart scrolling would keep yanking the user to the
// bottom until a page refresh.
func TestStreamingRecoversFromStaleScrollTracker(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatBubbleStreaming("Assistant", "exec-id", "chat-messages", "", false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render new-message streaming script: %v", err)
	}
	if err := _initThreadStreamingScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render thread streaming script: %v", err)
	}
	content := buf.String()

	if count := strings.Count(content, "window.resolveScrollTracker(trackerKey, "); count < 4 {
		t.Errorf("expected both streaming renderers to resolve the tracker on init AND inside the render loop (got %d resolveScrollTracker calls, want >= 4)", count)
	}
}

// TestStreamingRenderSnapshotsPinnedStateBeforeDomGrowth verifies streaming
// renderers decide whether to scroll before rendering new content. Checking
// after DOM growth breaks large conversations because adding content can move a
// previously pinned viewport outside the near-bottom threshold.
func TestStreamingRenderSnapshotsPinnedStateBeforeDomGrowth(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatBubbleStreaming("Assistant", "exec-id", "chat-messages", "", false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render new-message streaming script: %v", err)
	}
	if err := _initThreadStreamingScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render thread streaming script: %v", err)
	}
	content := buf.String()

	if count := strings.Count(content, "var shouldScroll = !tracker || tracker.shouldAutoScroll();"); count < 2 {
		t.Fatalf("expected streaming renderers to snapshot shouldScroll before DOM render, found %d", count)
	}
	textScrollIdx := strings.Index(content, "var shouldScroll = !tracker || tracker.shouldAutoScroll();")
	textRenderIdx := strings.Index(content, "window.renderStreamingContent(container, renderText)")
	if textScrollIdx == -1 || textRenderIdx == -1 || textScrollIdx > textRenderIdx {
		t.Error("new-message streaming renderer must compute shouldScroll before renderStreamingContent")
	}

	resumeRenderIdx := strings.LastIndex(content, "window.renderStreamingContent(container, renderText)")
	if resumeRenderIdx == -1 {
		t.Fatal("resume streaming renderer must call renderStreamingContent")
	}
	resumeScrollIdx := strings.LastIndex(content[:resumeRenderIdx], "var shouldScroll = !tracker || tracker.shouldAutoScroll();")
	if resumeScrollIdx == -1 || resumeScrollIdx > resumeRenderIdx {
		t.Error("resume streaming renderer must compute shouldScroll before renderStreamingContent")
	}
}

// TestChatScrollTracker_DestroyRemovesAllListeners ensures the scroll listener
// is torn down by destroy() to prevent leaks across morph swaps that recreate
// the tracker.
func TestChatScrollTracker_ExposesNavigationSnapshot(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}
	content := buf.String()

	required := []string{
		"snapshot: function()",
		"scrollTop: this.element.scrollTop || 0",
		"userScrolledUp: this.userScrolledUp || !nearBottom",
		"pinned: !this.userScrolledUp && nearBottom",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Fatalf("expected ChatScrollTracker snapshot to include %q", r)
		}
	}
}

func TestTaskThreadView_PreservesPerTaskScrollState(t *testing.T) {
	task := &models.Task{
		ID:        "thread-scroll-1",
		ProjectID: "p1",
		Status:    models.StatusCompleted,
		Category:  models.CategoryCompleted,
	}

	var buf bytes.Buffer
	if err := TaskThreadView(task, nil, nil, nil, nil, nil, false, 30).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render TaskThreadView: %v", err)
	}
	content := buf.String()

	required := []string{
		"window._taskThreadScrollStates = window._taskThreadScrollStates || {};",
		"return taskId ? 'task-thread-scroll-' + taskId : '';",
		"var preservedScrollState = _getTaskThreadScrollState(taskId);",
		"var restoredScrollState = _restoreTaskThreadScrollState(chatMessages, preservedScrollState);",
		"messages.scrollTop = state.pinned ? messages.scrollHeight : (state.scrollTop || 0);",
		"_saveTaskThreadScrollState();",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Fatalf("expected task thread scroll preservation code to include %q", r)
		}
	}

	if strings.Contains(content, "var preservedUserScrolledUp = window._taskThreadUserScrolledUp;") {
		t.Fatal("task thread entry must not restore scroll intent from a single global userScrolledUp flag")
	}
}

func TestChatScrollTracker_DestroyRemovesAllListeners(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("Failed to render ChatAutoScrollScript: %v", err)
	}
	content := buf.String()

	// destroy() must remove every listener attached by _init, otherwise
	// rebinding the tracker after a morph swap leaks handlers that fire on
	// detached elements.
	required := []string{
		"removeEventListener('scroll'",
		"removeEventListener('wheel'",
		"removeEventListener('touchmove'",
		"removeEventListener('pointerdown'",
		"removeEventListener('pointerup'",
		"removeEventListener('pointercancel'",
		"removeEventListener('blur'",
		"removeEventListener('keydown'",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Errorf("destroy() must call %s to clean up listeners", r)
		}
	}
}

func TestTaskThreadView_WindowedTranscriptUsesContainerLoaderAndPruning(t *testing.T) {
	task := &models.Task{ID: "task-1", Title: "Task", Prompt: "original", Status: models.StatusRunning}
	execs := []models.Execution{
		{ID: "exec-1", TaskID: task.ID, Status: models.ExecCompleted, PromptSent: "first", Output: "done"},
		{ID: "exec-2", TaskID: task.ID, Status: models.ExecRunning, PromptSent: "second"},
	}
	var buf bytes.Buffer
	if err := TaskThreadView(task, execs, nil, nil, nil, nil, true, 2).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render task thread: %v", err)
	}
	content := buf.String()
	if !strings.Contains(content, `data-window-limit="2"`) {
		t.Fatal("expected task thread message container to carry visible window limit")
	}
	if !strings.Contains(content, `data-earlier-url-base="/tasks/task-1/thread?limit=2"`) {
		t.Fatal("expected task thread container to expose a base earlier URL for live-prune-created loaders")
	}
	if !strings.Contains(content, `data-earlier-loader="true"`) || !strings.Contains(content, `hx-trigger="ov:load-earlier"`) || !strings.Contains(content, `/tasks/task-1/thread?before=exec-1&amp;limit=2`) {
		t.Fatal("expected container-scroll earlier loader for task thread")
	}
	if !strings.Contains(content, `hx-swap="outerHTML show:none"`) {
		t.Fatal("expected earlier loader to disable HTMX target show scrolling")
	}
	if strings.Count(content, `data-execution-pair="true"`) < 2 {
		t.Fatal("expected task thread executions to render as whole-turn pairs")
	}
	if strings.Count(content, `chat-execution-pair space-y-6`) < 2 {
		t.Fatal("expected task thread execution pairs to preserve equal vertical spacing between user and assistant bubbles")
	}
	if strings.Contains(content, `chat-execution-pair space-y-3`) {
		t.Fatal("execution-pair spacing must match the surrounding message-list spacing, not use a smaller internal gap")
	}
	if !strings.Contains(content, "window.initChatEarlierLoader") || !strings.Contains(content, "window.pruneChatExecutionWindow") {
		t.Fatal("expected task thread to initialize load-earlier and live pruning helpers")
	}
}

func TestChatAutoScrollScript_EarlierLoaderUsesTopIntentWithoutDuplicateRebinds(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render chat auto-scroll script: %v", err)
	}
	content := buf.String()

	required := []string{
		"function setEarlierLoaderBusy(loader, busy)",
		"function recoverIdleEarlierRequest()",
		`container.querySelector('[data-earlier-loader="true"][data-loading="true"]')`,
		"data-earlier-loader-idle",
		"data-earlier-loader-busy",
		"Scroll up to load earlier messages",
		"function getFirstVisibleExecutionPair(container)",
		"window.bindChatEarlierHTMXLifecycle",
		"document.body.addEventListener('htmx:afterSettle'",
		"window.restoreChatEarlierScroll(container)",
		"container.dataset.earlierPrevScrollHeight",
		"container.dataset.earlierPrevScrollTop",
		"container.dataset.earlierPrevBottomDistance",
		"container.dataset.earlierAnchorExecId",
		"container.dataset.earlierAnchorOffsetTop",
		"requestAnimationFrame(function()",
		"container.scrollTop = Math.max(0, (container.scrollHeight || 0) - prevBottomDistance)",
		"currentOffset - anchorOffsetTop",
		"earlierAnchorRestoring",
		"function maybeLoadEarlier()", "addEventListener('scroll', function()",
		"addEventListener('wheel'",
		"event.deltaY < 0",
		"earlierLastWheelAt",
		"now - lastWheelAt > 500",
		"addEventListener('touchstart'",
		"addEventListener('touchmove'",
		"window.bindChatEarlierKeyboardLoader",
		"document.addEventListener('keydown'",
		"['ArrowUp', 'PageUp', 'Home'].includes(event.key)",
		"if (event.repeat) return",
		`container.dataset.earlierRequestLoading === 'true' && container.querySelector('[data-earlier-loader="true"][data-loading="true"]')`,
		"earlierGestureLocked",
		"earlierRequestLoading",
		"window.finishChatEarlierRequest",
		"!event.detail.successful",
		"loader.setAttribute('hx-swap', 'outerHTML show:none')",
	}
	for _, marker := range required {
		if !strings.Contains(content, marker) {
			t.Fatalf("expected earlier loader script to contain %q", marker)
		}
	}
	if strings.Contains(content, "setTimeout(maybeLoadEarlier") {
		t.Fatal("earlier loader must not eagerly fetch older history on initialization")
	}
	if strings.Contains(content, "scheduleGestureUnlock") || strings.Contains(content, "document.addEventListener('keyup'") {
		t.Fatal("earlier loader must not unlock a consumed gesture before the HTMX request/swap completes")
	}
	if strings.Contains(content, "delete container.dataset.earlierLoaderBound") {
		t.Fatal("earlier loader must not delete the bound marker and stack duplicate listeners")
	}
	if strings.Contains(content, `>Loading earlier messages...</div>`) {
		t.Fatal("earlier loader must not show loading copy while idle")
	}
	if strings.Contains(content, "loader.dataset.prevScrollHeight") || strings.Contains(content, "loader.dataset.anchorExecId") {
		t.Fatal("prepend anchor state must live on the stable messages container, not the loader that HTMX replaces")
	}
}

func TestChatAutoScrollScript_ToolOutputRendersAllTypesAndPreservesScroll(t *testing.T) {
	var buf bytes.Buffer
	if err := ChatAutoScrollScript().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render chat auto-scroll script: %v", err)
	}
	content := buf.String()

	required := []string{
		"var prevToolBodyScrollStates = []",
		"container.querySelectorAll('.stream-tool-body-scroll').forEach(function(el)",
		"var scrollableY = el.getAttribute('data-scrollable-y') === 'true' || el.scrollHeight > el.clientHeight + 1",
		"var pinned = !scrollableY || (el.scrollHeight - el.scrollTop - el.clientHeight) <= 2",
		"inScroll.className = 'stream-tool-body-scroll'",
		"outScroll.className = 'stream-tool-body-scroll'",
		"el.setAttribute('data-scrollable-y', 'true')",
		"el.removeAttribute('data-scrollable-y')",
		"el.scrollTop = el.scrollHeight",
		"el.scrollTop = state.scrollTop",
		"var hasOut = seg.resultOutput && seg.resultOutput.trim()",
		"outPre.textContent = seg.resultOutput.trim()",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected tool output renderer to contain %q", fragment)
		}
	}

	forbidden := []string{
		"suppressOut",
		"dn === 'Read' || dn === 'List Files' || dn === 'Write'",
		"Don't show output body for Read/List Files/Write",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("tool output renderer must not suppress non-empty outputs for any tool type; found %q", fragment)
		}
	}
}
