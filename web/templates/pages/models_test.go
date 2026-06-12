package pages

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestModelsContent_NewModelVersionsInSelector(t *testing.T) {
	// Render the models page and verify the new model versions appear in the
	// HTML <option> elements and the JS modelOptionsByProvider catalog.
	agents := []models.LLMConfig{}
	var buf bytes.Buffer
	err := ModelsContent(agents, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render models content: %v", err)
	}
	out := buf.String()

	// HTML <option> elements
	for _, model := range []string{
		"claude-fable-5",
		"claude-mythos-5",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
	} {
		if !strings.Contains(out, `value="`+model+`"`) {
			t.Errorf("expected HTML option for %s", model)
		}
	}

	// JS modelOptionsByProvider entries
	for _, model := range []string{
		"gpt-5.5",
		"gpt-5.5-pro",
		"gpt-5.4-mini",
		"gpt-5.3-codex-spark",
		"claude-fable-5",
		"claude-mythos-5",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
	} {
		if !strings.Contains(out, `'`+model+`'`) {
			t.Errorf("expected JS model option for %s", model)
		}
	}

	if strings.Contains(out, "defaultMaxTokens") {
		t.Error("expected browser catalog not to expose internal output-token defaults")
	}
	if strings.Contains(out, "Max Output Tokens / Request") || strings.Contains(out, "model_max_tokens") {
		t.Error("expected model dialog not to expose internal output-token cap")
	}
	if !strings.Contains(out, "Claude Effort") {
		t.Error("expected Claude effort label in model dialog")
	}
	if !strings.Contains(out, "Matches Claude Code effort: low, medium, high, or max") {
		t.Error("expected Claude effort behavior to be explained")
	}
	if !strings.Contains(out, "{ value: 'claude-fable-5', label: 'Claude Fable 5', efforts: ['low', 'medium', 'high', 'max']") {
		t.Error("expected Claude Fable 5 effort options")
	}
	if !strings.Contains(out, "{ value: 'claude-mythos-5', label: 'Claude Mythos 5', efforts: ['low', 'medium', 'high', 'max']") {
		t.Error("expected Claude Mythos 5 effort options")
	}
	if !strings.Contains(out, "{ value: 'claude-opus-4-7', label: 'Claude Opus 4.7', efforts: ['low', 'medium', 'high', 'max']") {
		t.Error("expected Claude Opus 4.7 effort options")
	}
	if !strings.Contains(out, "{ value: 'claude-opus-4-8', label: 'Claude Opus 4.8', efforts: ['low', 'medium', 'high', 'max']") {
		t.Error("expected Claude Opus 4.8 effort options")
	}
}

func TestModelsContent_ModelFormUsesNativePostSubmit(t *testing.T) {
	agents := []models.LLMConfig{}
	var buf bytes.Buffer
	err := ModelsContent(agents, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render models content: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, `onsubmit="submitModelForm(event)"`) {
		t.Fatal("expected model form not to depend on custom submit JavaScript")
	}
	if !strings.Contains(out, `id="model_form" method="post" action="/models"`) {
		t.Fatal("expected model form to submit with native POST")
	}
	if !strings.Contains(out, "form.action = '/models/' + id;") {
		t.Fatal("expected edit flow to post to the existing model URL")
	}
	if !strings.Contains(out, "form.action = '/models';") {
		t.Fatal("expected create flow to post to /models")
	}
	if !strings.Contains(out, "form.dataset.mode = 'edit';") || !strings.Contains(out, "form.dataset.mode = 'create';") {
		t.Fatal("expected create/edit flow to track form mode")
	}
}

func TestModelsContent_ModelModalJavaScriptShape(t *testing.T) {
	var buf bytes.Buffer
	if err := ModelsContent(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render models content: %v", err)
	}
	out := buf.String()

	for _, fn := range []string{
		"function handleModelChange()",
		"function toggleProviderFields(selectedModel, selectedReasoningEffort)",
		"function editModelFromData(button)",
		"function openNewModelModal()",
		"function discoverOpenAICompatibleModels()",
	} {
		if !strings.Contains(out, fn) {
			t.Fatalf("expected rendered script to contain %s", fn)
		}
	}

	if err := balancedJavaScriptBraces(out); err != nil {
		t.Fatal(err)
	}

	for _, broken := range []string{
		"// In \"Create\" mode, update the per-request output token cap to the model-specific default.",
		"if (typeof syncToastContainerHost === 'function') syncToastContainerHost()\t\t\t\t\tfunction",
	} {
		if strings.Contains(out, broken) {
			t.Fatalf("rendered script contains known broken modal JavaScript fragment: %q", broken)
		}
	}
}

func TestModelsContent_OpenAICompatibleDiscoveryUI(t *testing.T) {
	var buf bytes.Buffer
	if err := ModelsContent(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render models content: %v", err)
	}
	out := buf.String()

	presetOptions := map[string]string{
		"openrouter":          "OpenRouter",
		"nvidia_nim":          "NVIDIA NIM",
		"vllm":                "Local vLLM",
		"lm_studio":           "LM Studio",
		"sglang":              "SGLang",
		"litellm":             "LiteLLM",
		"deepinfra":           "DeepInfra",
		"fireworks":           "Fireworks",
		"groq":                "Groq",
		"mistral":             "Mistral",
		"cerebras":            "Cerebras",
		"together":            "Together",
		"huggingface_router":  "Hugging Face Router",
		"deepseek":            "DeepSeek",
		"moonshot":            "Moonshot",
		"dashscope":           "Qwen / DashScope",
		"dashscope_intl":      "Qwen / DashScope Intl",
		"alibaba_coding_plan": "Alibaba Coding Plan",
		"zai_glm":             "Z.AI / GLM",
		"novita":              "NovitaAI",
		"venice":              "Venice",
		"qianfan":             "Qianfan",
		"kilo_code":           "Kilo Code",
		"arcee":               "Arcee AI",
		"stepfun":             "StepFun",
		"stepfun_step_plan":   "StepFun Step Plan",
		"gmi_cloud":           "GMI Cloud",
		"chutes":              "Chutes",
		"tokenhub":            "Tencent TokenHub",
		"tokenhub_intl":       "Tencent TokenHub Intl",
		"xiaomi_mimo":         "Xiaomi MiMo",
		"inferrs":             "Inferrs Local",
		"ds4":                 "ds4 Local",
		"custom":              "Custom OpenAI-Compatible",
	}
	for slug, label := range presetOptions {
		want := `<option value="openai_compatible_` + slug + `">` + label + `</option>`
		if !strings.Contains(out, want) {
			t.Fatalf("expected provider dropdown to contain %q", want)
		}
	}

	presetDefaults := map[string]string{
		"openrouter":          "https://openrouter.ai/api/v1/",
		"nvidia_nim":          "https://integrate.api.nvidia.com/v1/",
		"vllm":                "http://127.0.0.1:8000/v1/",
		"lm_studio":           "http://127.0.0.1:1234/v1/",
		"sglang":              "http://127.0.0.1:30000/v1/",
		"litellm":             "http://localhost:4000/v1/",
		"deepinfra":           "https://api.deepinfra.com/v1/openai/",
		"fireworks":           "https://api.fireworks.ai/inference/v1/",
		"groq":                "https://api.groq.com/openai/v1/",
		"mistral":             "https://api.mistral.ai/v1/",
		"cerebras":            "https://api.cerebras.ai/v1/",
		"together":            "https://api.together.xyz/v1/",
		"huggingface_router":  "https://router.huggingface.co/v1/",
		"deepseek":            "https://api.deepseek.com/v1/",
		"moonshot":            "https://api.moonshot.ai/v1/",
		"dashscope":           "https://dashscope.aliyuncs.com/compatible-mode/v1/",
		"dashscope_intl":      "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/",
		"alibaba_coding_plan": "https://coding-intl.dashscope.aliyuncs.com/v1/",
		"zai_glm":             "https://api.z.ai/api/paas/v4/",
		"novita":              "https://api.novita.ai/openai/v1/",
		"venice":              "https://api.venice.ai/api/v1/",
		"qianfan":             "https://qianfan.baidubce.com/v2/",
		"kilo_code":           "https://api.kilo.ai/api/gateway/",
		"arcee":               "https://api.arcee.ai/api/v1/",
		"stepfun":             "https://api.stepfun.ai/v1/",
		"stepfun_step_plan":   "https://api.stepfun.ai/step_plan/v1/",
		"gmi_cloud":           "https://api.gmi-serving.com/v1/",
		"chutes":              "https://llm.chutes.ai/v1/",
		"tokenhub":            "https://tokenhub.tencentmaas.com/v1/",
		"tokenhub_intl":       "https://tokenhub-intl.tencentmaas.com/v1/",
		"xiaomi_mimo":         "https://api.xiaomimimo.com/v1/",
		"inferrs":             "http://127.0.0.1:8080/v1/",
		"ds4":                 "http://127.0.0.1:18000/v1/",
	}
	for slug, baseURL := range presetDefaults {
		if !strings.Contains(out, slug+": '"+baseURL+"'") {
			t.Fatalf("expected preset default %s -> %s", slug, baseURL)
		}
	}

	for _, want := range []string{
		`<input type="hidden" id="model_provider_value" name="provider" value="anthropic"`,
		`<select id="model_provider"`,
		`oninput="scheduleAutoDiscoverOpenAICompatibleModels()"`,
		`onsubmit="normalizeModelFormBeforeSubmit()"`,
		`<input type="hidden" id="model_openai_compatible_preset" name="preset_slug" value="custom"`,
		"OpenAI-compatible presets auto-load available models when selected; Custom stays manual.",
		"openai_compatible_openrouter: [",
		"openai_compatible_groq: [",
		"openai_compatible_deepseek: [",
		"openai_compatible_lm_studio: [",
		"openai_compatible_custom: [",
		"{ value: 'nvidia/nemotron-3-ultra-550b-a55b', label: 'NVIDIA Nemotron', efforts: [] }",
		"{ value: 'deepseek-chat', label: 'DeepSeek Chat', efforts: [] }",
		"{ value: 'local-model', label: 'LM Studio local model', efforts: [] }",
		"Enter model ID manually",
		"function modelOptionsForProvider(provider)",
		"isDiscoverableOpenAICompatiblePreset()",
		"runAutoDiscoverOpenAICompatibleModels();",
		"var forcePresetDefaults = selectedModel === undefined && selectedReasoningEffort === undefined;",
		"applyOpenAICompatiblePreset(forcePresetDefaults);",
		"if (!force && provider === 'openai_compatible_custom' && currentPreset !== 'custom' && !openAICompatiblePresetDefaults[currentPreset]) preset = currentPreset;",
		"var hasPresetDefault = Object.prototype.hasOwnProperty.call(openAICompatiblePresetDefaults, preset);",
		"var next = hasPresetDefault ? openAICompatiblePresetDefaults[preset] : '';",
		"if (hasPresetDefault && preset !== 'custom' && (force || (!isEditingModelForm() && !baseURL.value)))",
		"providerValue.value = 'openai_compatible';",
		"Enter the model ID manually for local or custom endpoints.",
		"/models/openai-compatible/available?",
		"new URLSearchParams({base_url: baseURL})",
		"X-OpenAI-Compatible-API-Key",
		"data.resolved_id",
		"setOpenAICompatibleModelValue(models[i].id, models[i].id + ' (discovered)', false)",
		"setOpenAICompatibleModelValue(data.resolved_id, data.resolved_id + ' (discovered)', true)",
		"if (!isDiscoverableOpenAICompatiblePreset())",
		"document.getElementById('model_provider').value !== provider",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected OpenAI-compatible discovery UI to contain %q", want)
		}
	}
	for _, forbidden := range []string{
		`<select id="model_openai_compatible_preset"`,
		`onchange="applyOpenAICompatiblePreset()"`,
		"Discover Models",
		`onclick="discoverOpenAICompatibleModels()"`,
		"api_key: apiKey",
		"api_key=",
		"openai_compatible_api_key",
		"Object.values(openAICompatiblePresetDefaults).indexOf(baseURL.value)",
		"Custom compatible model",
		"openai_compatible_xai",
		"GitHub Copilot",
		"Bedrock",
		"Gemini native",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("expected discovery UI not to contain %q", forbidden)
		}
	}
}

func balancedJavaScriptBraces(value string) error {
	depth := 0
	inSingle := false
	inDouble := false
	inTemplate := false
	inLineComment := false
	inBlockComment := false
	escaped := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		var next byte
		if i+1 < len(value) {
			next = value[i+1]
		}
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inSingle || inDouble || inTemplate {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if inSingle && ch == '\'' {
				inSingle = false
			}
			if inDouble && ch == '"' {
				inDouble = false
			}
			if inTemplate && ch == '`' {
				inTemplate = false
			}
			continue
		}
		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}
		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '`':
			inTemplate = true
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("rendered JavaScript has an unmatched closing brace near byte %d", i)
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("rendered JavaScript has %d unclosed brace(s)", depth)
	}
	return nil
}

// TestModelsContent_NoCLIOptionInAuthSelects verifies that the rendered model
// setup dialog no longer exposes the "CLI (OAuth via terminal)" option for
// Anthropic or OpenAI connection-method selects.
func TestModelsContent_NoCLIOptionInAuthSelects(t *testing.T) {
	var buf bytes.Buffer
	if err := ModelsContent(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render models content: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, `value="cli">CLI`) {
		t.Error("expected CLI option to be removed from auth/connection method selects, but found it in rendered HTML")
	}
	if strings.Contains(out, "CLI (OAuth via terminal)") {
		t.Error("expected CLI (OAuth via terminal) label to be absent from rendered auth selects")
	}

	// Auth method selects should each have only the oauth option remaining
	if !strings.Contains(out, `value="oauth">API (OAuth via web)`) {
		t.Error("expected OAuth option to remain in auth/connection method select")
	}
}

func TestModelsContent_OAuthLinksLaunchInSystemBrowser(t *testing.T) {
	agents := []models.LLMConfig{
		{
			ID:         "openai-oauth",
			Name:       "OpenAI OAuth",
			Provider:   models.ProviderOpenAI,
			AuthMethod: models.AuthMethodOAuth,
			Model:      "gpt-5.4",
		},
	}

	var buf bytes.Buffer
	err := ModelsContent(agents, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render models content: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "return launchOAuthInSystemBrowser(this.dataset.oauthPath)") {
		t.Fatal("expected OAuth links to launch through system-browser helper")
	}
	if !strings.Contains(out, "data-oauth-path=\"/models/openai-oauth/oauth/initiate\"") {
		t.Fatal("expected OAuth links to expose model-specific oauth path via data attribute")
	}
	if !strings.Contains(out, "external=1") {
		t.Fatal("expected system-browser helper to request backend external launch mode")
	}
	if !strings.Contains(out, "fetch(externalURL") {
		t.Fatal("expected OAuth launcher to call backend in background via fetch")
	}
	if strings.Contains(out, "window.location.href = externalURL") {
		t.Fatal("expected OAuth launcher to avoid page navigation")
	}
}
