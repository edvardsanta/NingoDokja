package handlers

import (
	"context"
	"errors"
	"fmt"
	store "read_books/dokja_store"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
	"time"
)

type ChatAIHealthChecker interface {
	Health(ctx context.Context) error
}

type MemeStatusReader interface {
	Status(ctx context.Context) (map[string]any, error)
}

// MemeScreener reports whether an image (with its caption) is safe for restricted channels.
type MemeScreener interface {
	Screen(ctx context.Context, url, caption string) (map[string]any, error)
}

// MemeMarker flags a meme as delivered so the scheduler does not repeat it.
type MemeMarker interface {
	MarkSent(ctx context.Context, url string) error
}

// ChatProfiles is the part of the shared database the orchestrator may touch: it lists
// profiles (masked) and selects one. Creating or editing a profile, which is where a
// token goes in, is not possible from here; that is done locally, never over this port.
type ChatProfiles interface {
	ListProfiles() ([]store.Profile, error)
	UseProfile(name string) error
}

type MemeFetcher interface {
	Fetch(ctx context.Context, limit *int) (map[string]any, error)
}

type DiscordMessageDeliverer interface {
	Deliver(ctx context.Context, channelID, content, attachmentURL string) error
}

type SystemDomainHandler struct {
	chatAI           ChatAIHealthChecker
	memeStatus       MemeStatusReader
	memeFetch        MemeFetcher
	discord          DiscordMessageDeliverer
	deliveryChannels []string
	safeOnlyChannels map[string]struct{}
	memeScreen       MemeScreener
	memeMarker       MemeMarker
	controls         *core.Controls
	profiles         ChatProfiles
}

func NewSystemDomainHandler(
	chatAI ChatAIHealthChecker,
	memeStatus MemeStatusReader,
	memeFetch MemeFetcher,
	discord DiscordMessageDeliverer,
	deliveryChannels string,
) *SystemDomainHandler {
	return &SystemDomainHandler{
		chatAI:           chatAI,
		memeStatus:       memeStatus,
		memeFetch:        memeFetch,
		discord:          discord,
		deliveryChannels: splitChannelIDs(deliveryChannels),
	}
}

// WithSafeOnlyChannels marks delivery channels that must only receive memes the NSFW
// screen approved (a comma-separated list). Every other channel receives everything.
func (h *SystemDomainHandler) WithSafeOnlyChannels(channels string) *SystemDomainHandler {
	h.safeOnlyChannels = map[string]struct{}{}
	for _, channelID := range splitChannelIDs(channels) {
		h.safeOnlyChannels[channelID] = struct{}{}
	}
	return h
}

// WithMemeTools gives admin sends what they need to enforce the safe-only rule
// (screener) and to keep the scheduler from repeating a hand-picked meme (marker).
func (h *SystemDomainHandler) WithMemeTools(screener MemeScreener, marker MemeMarker) *SystemDomainHandler {
	h.memeScreen = screener
	h.memeMarker = marker
	return h
}

// WithControls lets the handler read and change the operator's service and job switches.
func (h *SystemDomainHandler) WithControls(controls *core.Controls) *SystemDomainHandler {
	h.controls = controls
	return h
}

// WithProfiles enables listing and selecting chat provider profiles.
func (h *SystemDomainHandler) WithProfiles(profiles ChatProfiles) *SystemDomainHandler {
	h.profiles = profiles
	return h
}

func (h *SystemDomainHandler) serviceEnabled(name string) bool {
	return h.controls == nil || h.controls.ServiceEnabled(name)
}

func (h *SystemDomainHandler) Domain() core.Domain {
	return core.DomainSystem
}

func (h *SystemDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil {
		return nil, fmt.Errorf("system domain handler is nil")
	}

	logger.Info(fmt.Sprintf(
		"system handler received event_id=%s type=%s action=%s",
		event.EventID,
		event.Type,
		workflow.Action,
	))

	if event.Type == "meme.dispatch.scheduled" {
		return h.handleScheduledMemeDispatch(ctx, event, workflow)
	}
	if event.Type == "discord.send" {
		return h.handleDiscordSend(ctx, event, workflow)
	}
	switch event.Type {
	case "services.set":
		return h.handleServiceSet(event)
	case "scheduler.jobs.set":
		return h.handleJobSet(event)
	case "scheduler.jobs.announce":
		return h.handleJobAnnounce(event)
	case "chat.profiles.list":
		return h.profilesView()
	case "chat.profile.use":
		return h.handleProfileUse(event)
	}

	services := map[string]any{}
	overallStatus := "ok"

	// A switched-off service is not probed and never counts as degraded.
	if h.chatAI != nil && h.serviceEnabled("chat_ai") {
		if err := h.chatAI.Health(ctx); err != nil {
			overallStatus = "degraded"
			services["chat_ai"] = map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
		} else {
			services["chat_ai"] = map[string]any{
				"status": "ok",
			}
		}
	}

	if h.memeStatus != nil && h.serviceEnabled("meme") {
		result, err := h.memeStatus.Status(ctx)
		if err != nil {
			overallStatus = "degraded"
			services["meme"] = map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
		} else {
			entry := map[string]any{
				"status": firstNonEmptyString(resultString(result, "status"), "ok"),
			}
			if unsentCount, ok := result["unsent_count"]; ok {
				entry["unsent_count"] = unsentCount
			}
			services["meme"] = entry
		}
	}
	h.applySwitches(services)

	response := map[string]any{
		"action":   workflow.Action,
		"status":   overallStatus,
		"services": services,
		"channels": h.channelSummary(),
	}
	if h.controls != nil {
		response["jobs"] = h.jobsView()
	}
	if h.profiles != nil {
		view, err := h.profilesView()
		if err != nil {
			view = map[string]any{"error": err.Error()}
		}
		response["chat_profiles"] = view
	}
	logger.Info(fmt.Sprintf(
		"system handler completed event_id=%s type=%s action=%s service_count=%d status=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		len(services),
		overallStatus,
	))
	return response, nil
}

func (h *SystemDomainHandler) handleScheduledMemeDispatch(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h.memeFetch == nil {
		return nil, fmt.Errorf("meme fetch client is not configured")
	}
	if h.discord == nil {
		return nil, fmt.Errorf("discord deliverer is not configured")
	}
	if len(h.deliveryChannels) == 0 {
		return nil, fmt.Errorf("scheduled meme delivery channel is not configured")
	}

	limit := intPointer(event.Payload["limit"])
	result, err := h.memeFetch.Fetch(ctx, limit)
	if err != nil {
		return nil, err
	}

	// A channel that fails is skipped for the rest of the run so one broken
	// destination does not block the others or get retried once per meme.
	delivered := 0
	failed := map[string]error{}
	skippedUnsafe := map[string]int{}
	for _, meme := range memesFromResult(result) {
		content, attachmentURL := formatScheduledMeme(meme)
		if content == "" {
			continue
		}
		safe, reason := memeSafety(meme)
		sent := false
		for _, channelID := range h.deliveryChannels {
			if _, skip := failed[channelID]; skip {
				continue
			}
			if _, safeOnly := h.safeOnlyChannels[channelID]; safeOnly && !safe {
				skippedUnsafe[channelID]++
				logger.Info(fmt.Sprintf(
					"system handler meme skipped for safe-only channel event_id=%s channel_id=%s reason=%s",
					event.EventID,
					channelID,
					reason,
				))
				continue
			}
			if err := h.discord.Deliver(ctx, channelID, content, attachmentURL); err != nil {
				logger.Info(fmt.Sprintf(
					"system handler meme delivery failed event_id=%s channel_id=%s error=%v",
					event.EventID,
					channelID,
					err,
				))
				failed[channelID] = err
				continue
			}
			sent = true
		}
		if sent {
			delivered++
		}
	}

	if len(failed) > 0 {
		var errs []error
		for _, channelID := range h.deliveryChannels {
			if err, ok := failed[channelID]; ok {
				errs = append(errs, fmt.Errorf("deliver meme to channel %s: %w", channelID, err))
			}
		}
		return nil, errors.Join(errs...)
	}

	response := map[string]any{
		"action":          workflow.Action,
		"channel_ids":     h.deliveryChannels,
		"delivered_count": delivered,
		"count":           delivered,
	}
	if len(skippedUnsafe) > 0 {
		response["skipped_unsafe"] = skippedUnsafe
	}
	logger.Info(fmt.Sprintf(
		"system handler completed event_id=%s type=%s action=%s delivered_count=%d channel_ids=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		delivered,
		strings.Join(h.deliveryChannels, ","),
	))
	return response, nil
}

// handleDiscordSend posts an admin-supplied message to configured channels. It only
// accepts channels the orchestrator already delivers to, so the request port cannot
// be used to post anywhere the bot can reach.
func (h *SystemDomainHandler) handleDiscordSend(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h.discord == nil {
		return nil, fmt.Errorf("discord deliverer is not configured")
	}

	content := strings.TrimSpace(resultString(event.Payload, "content"))
	attachmentURL := strings.TrimSpace(resultString(event.Payload, "attachment_url"))
	if content == "" && attachmentURL == "" {
		return nil, fmt.Errorf("discord send requires content or attachment_url")
	}

	var targets []string
	if all, _ := event.Payload["all"].(bool); all {
		targets = h.deliveryChannels
	} else {
		targets = uniqueStrings(stringSlice(event.Payload["channel_ids"]))
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("discord send requires channel_ids or all")
	}

	allowed := map[string]struct{}{}
	for _, channelID := range h.deliveryChannels {
		allowed[channelID] = struct{}{}
	}
	for _, channelID := range targets {
		if _, ok := allowed[channelID]; !ok {
			return nil, fmt.Errorf("channel %s is not a configured delivery channel", channelID)
		}
	}

	// The safe-only rule applies here too: an image bound for a restricted channel is
	// screened server-side, so no client can vouch for it. A failed screen counts as unsafe.
	blocked := map[string]struct{}{}
	blockReason := ""
	if attachmentURL != "" {
		restricted := []string{}
		for _, channelID := range targets {
			if _, ok := h.safeOnlyChannels[channelID]; ok {
				restricted = append(restricted, channelID)
			}
		}
		if len(restricted) > 0 {
			if safe, reason := h.screenAttachment(ctx, attachmentURL, content); !safe {
				blockReason = reason
				for _, channelID := range restricted {
					blocked[channelID] = struct{}{}
				}
			}
		}
	}

	sent := []string{}
	skipped := []string{}
	var errs []error
	for _, channelID := range targets {
		if _, isBlocked := blocked[channelID]; isBlocked {
			skipped = append(skipped, channelID)
			continue
		}
		if err := h.discord.Deliver(ctx, channelID, content, attachmentURL); err != nil {
			errs = append(errs, fmt.Errorf("send to channel %s: %w", channelID, err))
			continue
		}
		sent = append(sent, channelID)
	}
	logger.Info(fmt.Sprintf(
		"system handler discord send event_id=%s sent=%s skipped_unsafe=%s failed=%d has_attachment=%t",
		event.EventID,
		strings.Join(sent, ","),
		strings.Join(skipped, ","),
		len(errs),
		attachmentURL != "",
	))
	if len(errs) > 0 {
		return nil, fmt.Errorf("delivered to [%s]: %w", strings.Join(sent, ","), errors.Join(errs...))
	}
	if len(sent) == 0 {
		return nil, fmt.Errorf("nothing sent: image is not safe for %s (%s)", strings.Join(skipped, ","), blockReason)
	}

	response := map[string]any{
		"action":         workflow.Action,
		"sent_to":        sent,
		"has_attachment": attachmentURL != "",
	}
	if len(skipped) > 0 {
		response["skipped_unsafe"] = skipped
		response["unsafe_reason"] = blockReason
	}
	if markSent, _ := event.Payload["mark_sent"].(bool); markSent && attachmentURL != "" && h.memeMarker != nil && h.serviceEnabled("meme") {
		err := h.memeMarker.MarkSent(ctx, attachmentURL)
		if err != nil {
			logger.Info(fmt.Sprintf("system handler could not mark meme sent event_id=%s error=%v", event.EventID, err))
		}
		response["marked_sent"] = err == nil
	}
	return response, nil
}

func (h *SystemDomainHandler) screenAttachment(ctx context.Context, url, caption string) (bool, string) {
	if h.memeScreen == nil {
		return false, "no screener configured"
	}
	// With the meme service switched off nothing may use its filter, and an image that
	// cannot be checked never reaches a safe-only channel.
	if !h.serviceEnabled("meme") {
		return false, "meme service is disabled, so the image cannot be screened"
	}
	result, err := h.memeScreen.Screen(ctx, url, caption)
	if err != nil {
		return false, "screen failed: " + err.Error()
	}
	if safe, _ := result["safe"].(bool); safe {
		return true, ""
	}
	return false, firstNonEmptyString(resultString(result, "reason"), "flagged by the nsfw screen")
}

// channelSummary lists where the orchestrator delivers, so admin tools can offer them.
func (h *SystemDomainHandler) channelSummary() map[string]any {
	safeOnly := make([]string, 0, len(h.safeOnlyChannels))
	for _, channelID := range h.deliveryChannels {
		if _, ok := h.safeOnlyChannels[channelID]; ok {
			safeOnly = append(safeOnly, channelID)
		}
	}
	return map[string]any{
		"meme":      h.deliveryChannels,
		"safe_only": safeOnly,
	}
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		return splitChannelIDs(typed)
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func resultString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func intPointer(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case float64:
		parsed := int(typed)
		return &parsed
	default:
		return nil
	}
}

// memeSafety reads the meme service's NSFW label. A meme without a label counts as
// unsafe, so a service that does not screen can never leak into a safe-only channel.
func memeSafety(meme map[string]any) (bool, string) {
	label, _ := meme["nsfw"].(map[string]any)
	safe, ok := label["safe"].(bool)
	if !ok {
		return false, "no nsfw label"
	}
	return safe, resultString(label, "reason")
}

func memesFromResult(result map[string]any) []map[string]any {
	raw, _ := result["memes"].([]any)
	memes := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		meme, ok := item.(map[string]any)
		if ok {
			memes = append(memes, meme)
		}
	}
	return memes
}

// splitChannelIDs parses a comma-separated channel list, dropping blanks and duplicates.
func splitChannelIDs(raw string) []string {
	var channels []string
	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		channels = append(channels, id)
	}
	return channels
}

func formatScheduledMeme(meme map[string]any) (string, string) {
	title := firstNonEmptyString(resultString(meme, "title"))
	url := firstNonEmptyString(resultString(meme, "url"))
	switch {
	case title != "":
		return title, url
	case url != "":
		return "meme agendado", url
	default:
		return "", ""
	}
}

// applySwitches adds each switchable service's enabled flag, marking switched-off ones.
func (h *SystemDomainHandler) applySwitches(services map[string]any) {
	if h.controls == nil {
		return
	}
	for _, info := range h.controls.Services() {
		entry, _ := services[info.Name].(map[string]any)
		if entry == nil {
			entry = map[string]any{"status": "unchecked"}
		}
		entry["enabled"] = info.Enabled
		if info.Name == "scheduler" {
			h.describeScheduler(entry)
		}
		if !info.Enabled {
			entry["status"] = "disabled"
		}
		services[info.Name] = entry
	}
}

// schedulerFresh is how long the scheduler may stay silent before it counts as stopped
// (it announces about once a minute).
const schedulerFresh = 3 * time.Minute

func (h *SystemDomainHandler) describeScheduler(entry map[string]any) {
	last := h.controls.LastAnnounce()
	switch {
	case last.IsZero():
		entry["status"], entry["detail"] = "stopped", "never announced (stopped?)"
	case time.Since(last) > schedulerFresh:
		entry["status"], entry["detail"] = "stopped", fmt.Sprintf("no announce for %dm (stopped?)", int(time.Since(last).Minutes()))
	default:
		entry["status"], entry["detail"] = "ok", "rodando"
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (h *SystemDomainHandler) jobsView() []map[string]any {
	jobs := h.controls.Jobs()
	view := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		entry := map[string]any{
			"name":              job.Name,
			"enabled":           job.Enabled,
			"interval_override": job.IntervalOverride,
			"next_at":           formatTime(job.NextAt),
			"last_at":           formatTime(job.LastAt),
			"last_outcome":      job.LastOutcome,
			"last_error":        job.LastError,
			"announced_at":      formatTime(job.AnnouncedAt),
		}
		if job.Interval > 0 {
			entry["interval"] = job.Interval.String()
		}
		view = append(view, entry)
	}
	return view
}

func (h *SystemDomainHandler) handleServiceSet(event core.Event) (map[string]any, error) {
	if h.controls == nil {
		return nil, fmt.Errorf("service switches are not configured")
	}
	name := strings.TrimSpace(resultString(event.Payload, "name"))
	enabled, ok := event.Payload["enabled"].(bool)
	if !ok {
		return nil, fmt.Errorf("services.set requires a boolean enabled")
	}
	if err := h.controls.SetService(name, enabled); err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf("system handler service switch event_id=%s name=%s enabled=%t", event.EventID, name, enabled))
	return map[string]any{"name": name, "enabled": enabled}, nil
}

func (h *SystemDomainHandler) handleJobSet(event core.Event) (map[string]any, error) {
	if h.controls == nil {
		return nil, fmt.Errorf("job switches are not configured")
	}
	name := strings.TrimSpace(resultString(event.Payload, "name"))
	patch := core.JobPatch{}
	changed := false

	if raw, present := event.Payload["enabled"]; present {
		enabled, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("enabled must be a boolean")
		}
		patch.Enabled, changed = &enabled, true
	}
	if raw, present := event.Payload["interval"]; present {
		text, _ := raw.(string)
		text = strings.TrimSpace(text)
		if text == "" || text == "default" {
			patch.ClearInterval = true
		} else {
			interval, err := time.ParseDuration(text)
			if err != nil {
				return nil, fmt.Errorf("interval %q is not a duration like 45m or 6h", text)
			}
			patch.Interval = &interval
		}
		changed = true
	}
	if !changed {
		return nil, fmt.Errorf("scheduler.jobs.set needs enabled and/or interval")
	}
	if err := h.controls.SetJob(name, patch); err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf("system handler job switch event_id=%s name=%s payload_keys=%d", event.EventID, name, len(event.Payload)))

	for _, job := range h.controls.Jobs() {
		if job.Name == name {
			view := map[string]any{"name": name, "enabled": job.Enabled, "interval_override": job.IntervalOverride}
			if job.Interval > 0 {
				view["interval"] = job.Interval.String()
			}
			return view, nil
		}
	}
	return map[string]any{"name": name}, nil
}

func (h *SystemDomainHandler) handleJobAnnounce(event core.Event) (map[string]any, error) {
	if h.controls == nil {
		return map[string]any{"accepted": 0}, nil
	}
	raw, _ := event.Payload["jobs"].([]any)
	announces := make([]core.JobAnnounce, 0, len(raw))
	for _, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		interval, _ := time.ParseDuration(resultString(fields, "interval"))
		lastFire, _ := time.Parse(time.RFC3339, resultString(fields, "last_fire"))
		nextFire, _ := time.Parse(time.RFC3339, resultString(fields, "next_fire"))
		announces = append(announces, core.JobAnnounce{
			Name:     strings.TrimSpace(resultString(fields, "name")),
			Interval: interval,
			LastFire: lastFire,
			NextFire: nextFire,
		})
	}
	h.controls.Announce(announces, time.Now().UTC())
	return map[string]any{"accepted": len(announces)}, nil
}

// profilesView lists the profiles with a masked key hint. The token is not in the data
// this handler can reach at all (ChatProfiles never returns it).
func (h *SystemDomainHandler) profilesView() (map[string]any, error) {
	if h.profiles == nil {
		return nil, fmt.Errorf("chat profiles are not configured (set DOKJA_DB_FILE)")
	}
	profiles, err := h.profiles.ListProfiles()
	if err != nil {
		return nil, fmt.Errorf("list chat profiles: %w", err)
	}
	active := ""
	entries := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Active {
			active = profile.Name
		}
		entries = append(entries, map[string]any{
			"name":     profile.Name,
			"base_url": profile.BaseURL,
			"model":    profile.Model,
			"key_hint": profile.KeyHint,
			"active":   profile.Active,
		})
	}
	return map[string]any{"active": active, "profiles": entries}, nil
}

func (h *SystemDomainHandler) handleProfileUse(event core.Event) (map[string]any, error) {
	if h.profiles == nil {
		return nil, fmt.Errorf("chat profiles are not configured (set DOKJA_DB_FILE)")
	}
	name := strings.TrimSpace(resultString(event.Payload, "name"))
	if name == "" || len(name) > 32 {
		return nil, fmt.Errorf("chat.profile.use requires the name of an existing profile")
	}
	if err := h.profiles.UseProfile(name); err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf("system handler chat profile selected event_id=%s name=%s", event.EventID, name))
	return map[string]any{"active": name}, nil
}
