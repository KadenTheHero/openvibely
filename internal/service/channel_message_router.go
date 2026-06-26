package service

import (
	"context"
	"encoding/json"
	"fmt"
	netmail "net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

const SendMessageAllowExplicitTargetsSetting = "send_message_allow_explicit_targets"

type channelTargetStore interface {
	ListByProject(ctx context.Context, projectID string) ([]models.ChannelTarget, error)
	FindHome(ctx context.Context, projectID, platform string) (*models.ChannelTarget, error)
	FindByName(ctx context.Context, projectID, platform, name string) (*models.ChannelTarget, error)
	FindByTarget(ctx context.Context, projectID, platform, targetID, threadID string) (*models.ChannelTarget, error)
	RecordSend(ctx context.Context, send models.ChannelMessageSend) error
}

type outboundSlackSender interface {
	SendOutboundMessage(ctx context.Context, channelID, threadTS, text string) SendMessageResult
}

type outboundTelegramSender interface {
	SendOutboundMessage(ctx context.Context, chatID int64, threadID int, text string) SendMessageResult
}

type outboundEmailSender interface {
	SendOutboundMessage(ctx context.Context, to, subject, body string) SendMessageResult
}

type ChannelMessageRouter struct {
	slack        outboundSlackSender
	telegram     outboundTelegramSender
	email        outboundEmailSender
	targets      channelTargetStore
	settings     *repository.SettingsRepo
	newID        func() string
	auditSurface string
	auditUser    string
}

type ChannelTarget struct {
	ProjectID      string `json:"project_id"`
	Platform       string `json:"platform"`
	Name           string `json:"name,omitempty"`
	TargetID       string `json:"target_id"`
	ThreadID       string `json:"thread_id,omitempty"`
	Home           bool   `json:"home"`
	DefaultSubject string `json:"default_subject,omitempty"`
}

type SendMessageRequest struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Message string `json:"message"`
	Subject string `json:"subject,omitempty"`
}

type SendMessageResult struct {
	OK        bool   `json:"ok"`
	Platform  string `json:"platform,omitempty"`
	Target    string `json:"target,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func NewChannelMessageRouter(targets channelTargetStore, settings *repository.SettingsRepo) *ChannelMessageRouter {
	return &ChannelMessageRouter{targets: targets, settings: settings, newID: repository.NewID}
}

func (r *ChannelMessageRouter) SetSlackService(svc outboundSlackSender)       { r.slack = svc }
func (r *ChannelMessageRouter) SetTelegramService(svc outboundTelegramSender) { r.telegram = svc }
func (r *ChannelMessageRouter) SetEmailService(svc outboundEmailSender)       { r.email = svc }
func (r *ChannelMessageRouter) WithAuditContext(surface, user string) *ChannelMessageRouter {
	if r == nil {
		return nil
	}
	copy := *r
	copy.auditSurface = strings.TrimSpace(surface)
	copy.auditUser = strings.TrimSpace(user)
	return &copy
}

func (r *ChannelMessageRouter) ListTargets(ctx context.Context, projectID string) ([]ChannelTarget, error) {
	if r == nil || r.targets == nil {
		return nil, fmt.Errorf("channel message router is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	stored, err := r.targets.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelTarget, 0, len(stored))
	for _, t := range stored {
		out = append(out, ChannelTarget{ProjectID: t.ProjectID, Platform: t.Platform, Name: t.Name, TargetID: t.TargetID, ThreadID: t.ThreadID, Home: t.Home, DefaultSubject: t.DefaultSubject})
	}
	return out, nil
}

func (r *ChannelMessageRouter) Send(ctx context.Context, projectID string, req SendMessageRequest) SendMessageResult {
	if r == nil {
		return sendMessageError("channel message router is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return sendMessageError("project id is required")
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "send"
	}
	if action == "list" {
		targets, err := r.ListTargets(ctx, projectID)
		if err != nil {
			return sendMessageError(err.Error())
		}
		b, _ := json.Marshal(map[string]interface{}{"ok": true, "targets": targets})
		return SendMessageResult{OK: true, MessageID: string(b)}
	}
	if action != "send" {
		return r.auditAndReturn(ctx, projectID, "", "", "", req.Message, sendMessageError("unsupported send_message action"))
	}
	if strings.TrimSpace(req.Target) == "" {
		return r.auditAndReturn(ctx, projectID, "", "", "", req.Message, sendMessageError("send_message requires target; call send_message with action=list to see configured targets"))
	}
	if strings.TrimSpace(req.Message) == "" {
		return r.auditAndReturn(ctx, projectID, "", "", "", req.Message, sendMessageError("send_message requires message"))
	}
	resolved, err := r.resolveTarget(ctx, projectID, req.Target)
	if err != nil {
		return r.auditAndReturn(ctx, projectID, "", "", "", req.Message, sendMessageError(err.Error()))
	}
	result := r.dispatch(ctx, req, resolved)
	return r.auditAndReturn(ctx, projectID, resolved.Platform, resolved.TargetID, resolved.ThreadID, req.Message, result)
}

func ExecuteSendMessageTool(ctx context.Context, router *ChannelMessageRouter, projectID string, input json.RawMessage) (string, error) {
	var req SendMessageRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	result := router.Send(ctx, projectID, req)
	if reqAction := strings.ToLower(strings.TrimSpace(req.Action)); reqAction == "list" && result.OK && result.MessageID != "" {
		return result.MessageID, nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

type resolvedMessageTarget struct {
	Platform       string
	TargetID       string
	ThreadID       string
	DefaultSubject string
}

func (r *ChannelMessageRouter) resolveTarget(ctx context.Context, projectID, raw string) (resolvedMessageTarget, error) {
	platform, ref, threadID, err := parseSendMessageTarget(raw)
	if err != nil {
		return resolvedMessageTarget{}, err
	}
	if r.targets == nil {
		return resolvedMessageTarget{}, fmt.Errorf("no outbound channel targets are configured")
	}
	if ref == "" {
		target, err := r.targets.FindHome(ctx, projectID, platform)
		if err != nil {
			return resolvedMessageTarget{}, err
		}
		if target == nil {
			return resolvedMessageTarget{}, fmt.Errorf("No home target configured for %s; call send_message with action=list", platform)
		}
		return fromStoredTarget(*target), nil
	}
	if strings.HasPrefix(ref, "#") {
		name := strings.TrimPrefix(ref, "#")
		target, err := r.targets.FindByName(ctx, projectID, platform, name)
		if err != nil {
			return resolvedMessageTarget{}, err
		}
		if target == nil {
			return resolvedMessageTarget{}, fmt.Errorf("No saved %s target named #%s; call send_message with action=list", platform, name)
		}
		return fromStoredTarget(*target), nil
	}
	if platform == "email" {
		normalized, err := NormalizeOutboundEmailForTarget(ref)
		if err != nil {
			return resolvedMessageTarget{}, err
		}
		ref = normalized
	}
	if !isNativeTarget(platform, ref) {
		return resolvedMessageTarget{}, fmt.Errorf("Invalid %s target %q; call send_message with action=list", platform, ref)
	}
	if platform == "telegram" && threadID != "" {
		if _, err := strconv.Atoi(threadID); err != nil {
			return resolvedMessageTarget{}, fmt.Errorf("telegram thread id must be an integer")
		}
	}
	if saved, err := r.targets.FindByTarget(ctx, projectID, platform, ref, threadID); err != nil {
		return resolvedMessageTarget{}, err
	} else if saved != nil {
		return fromStoredTarget(*saved), nil
	}
	if !r.allowExplicitTargets(ctx, projectID) {
		return resolvedMessageTarget{}, fmt.Errorf("Explicit %s target is not saved for this project; call send_message with action=list", platform)
	}
	return resolvedMessageTarget{Platform: platform, TargetID: ref, ThreadID: threadID}, nil
}

func (r *ChannelMessageRouter) dispatch(ctx context.Context, req SendMessageRequest, target resolvedMessageTarget) SendMessageResult {
	switch target.Platform {
	case "slack":
		if r.slack == nil {
			return sendMessageError("slack channel is not configured")
		}
		return r.slack.SendOutboundMessage(ctx, target.TargetID, target.ThreadID, req.Message)
	case "telegram":
		if r.telegram == nil {
			return sendMessageError("telegram channel is not configured")
		}
		chatID, err := strconv.ParseInt(target.TargetID, 10, 64)
		if err != nil {
			return sendMessageError("telegram chat id must be an integer")
		}
		threadID := 0
		if target.ThreadID != "" {
			threadID, err = strconv.Atoi(target.ThreadID)
			if err != nil {
				return sendMessageError("telegram thread id must be an integer")
			}
		}
		return r.telegram.SendOutboundMessage(ctx, chatID, threadID, req.Message)
	case "email":
		if r.email == nil {
			return sendMessageError("email channel is not configured")
		}
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			subject = strings.TrimSpace(target.DefaultSubject)
		}
		return r.email.SendOutboundMessage(ctx, target.TargetID, subject, req.Message)
	default:
		return sendMessageError("unknown platform")
	}
}

func (r *ChannelMessageRouter) auditAndReturn(ctx context.Context, projectID, platform, targetID, threadID, message string, result SendMessageResult) SendMessageResult {
	if result.Platform == "" {
		result.Platform = platform
	}
	if result.Target == "" && platform != "" && targetID != "" {
		result.Target = formatResolvedMessageTarget(platform, targetID, threadID)
	}
	if r.targets == nil {
		return result
	}
	id := repository.NewID()
	if r.newID != nil {
		id = r.newID()
	}
	_ = r.targets.RecordSend(ctx, models.ChannelMessageSend{
		ID:                 id,
		ProjectID:          projectID,
		Platform:           firstNonEmptyMessageString(platform, result.Platform),
		TargetID:           targetID,
		ThreadID:           threadID,
		RequestedBySurface: r.auditSurface,
		RequestedByUser:    r.auditUser,
		MessagePreview:     truncateSendMessagePreview(message, 500),
		Success:            result.OK,
		Error:              result.Error,
	})
	return result
}

func parseSendMessageTarget(raw string) (platform, ref, threadID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("send_message target is required")
	}
	parts := strings.Split(raw, ":")
	platform = strings.ToLower(strings.TrimSpace(parts[0]))
	switch platform {
	case "slack", "telegram", "email":
	default:
		return "", "", "", fmt.Errorf("Unknown send_message platform %q", platform)
	}
	if len(parts) == 1 {
		return platform, "", "", nil
	}
	if platform == "email" {
		return platform, strings.TrimSpace(strings.Join(parts[1:], ":")), "", nil
	}
	ref = strings.TrimSpace(parts[1])
	if len(parts) > 2 {
		threadID = strings.TrimSpace(strings.Join(parts[2:], ":"))
	}
	return platform, ref, threadID, nil
}

func fromStoredTarget(target models.ChannelTarget) resolvedMessageTarget {
	return resolvedMessageTarget{Platform: strings.ToLower(strings.TrimSpace(target.Platform)), TargetID: strings.TrimSpace(target.TargetID), ThreadID: strings.TrimSpace(target.ThreadID), DefaultSubject: strings.TrimSpace(target.DefaultSubject)}
}

var slackNativeTargetPattern = regexp.MustCompile(`^[CGD][A-Z0-9]+$`)

func isNativeTarget(platform, ref string) bool {
	ref = strings.TrimSpace(ref)
	switch platform {
	case "slack":
		return slackNativeTargetPattern.MatchString(ref)
	case "telegram":
		_, err := strconv.ParseInt(ref, 10, 64)
		return err == nil
	case "email":
		return ref != "" && strings.Contains(ref, "@")
	default:
		return false
	}
}

func NormalizeOutboundEmailForTarget(email string) (string, error) {
	addr, err := netmail.ParseAddress(strings.TrimSpace(email))
	if err != nil || addr == nil || strings.TrimSpace(addr.Address) == "" {
		return "", fmt.Errorf("invalid email recipient")
	}
	return repository.NormalizeEmailAddress(addr.Address), nil
}

func (r *ChannelMessageRouter) allowExplicitTargets(ctx context.Context, projectID string) bool {
	if r.settings == nil {
		return false
	}
	if strings.TrimSpace(projectID) != "" {
		val, _ := r.settings.Get(ctx, SendMessageAllowExplicitTargetsSetting+":"+strings.TrimSpace(projectID))
		if strings.TrimSpace(val) != "" {
			return strings.EqualFold(strings.TrimSpace(val), "true")
		}
	}
	val, _ := r.settings.Get(ctx, SendMessageAllowExplicitTargetsSetting)
	return strings.EqualFold(strings.TrimSpace(val), "true")
}

func formatResolvedMessageTarget(platform, targetID, threadID string) string {
	if threadID != "" {
		return platform + ":" + targetID + ":" + threadID
	}
	return platform + ":" + targetID
}

func sendMessageError(msg string) SendMessageResult {
	return SendMessageResult{OK: false, Error: strings.TrimSpace(msg)}
}

func truncateSendMessagePreview(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}

func firstNonEmptyMessageString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
