package handlers

import (
	"context"
	"errors"
	"fmt"
	communications "read_books/dokja_domain/dokja_communications"
	moderation "read_books/dokja_domain/dokja_moderation"
	store "read_books/dokja_store"
	"read_books/internal/core"
	"read_books/internal/logger"
	"slices"
	"strings"
	"time"
)

type ChatProviderHealthChecker interface {
	Health(ctx context.Context) error
}

type MemeStatusReader interface {
	Status(ctx context.Context) (map[string]any, error)
}

// MemeScreener reports whether an image (with its caption) is safe for restricted channels.
type MemeScreener = moderation.MediaScreener

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

// MemeHashtagSuggester finds a learned hashtag for a meme's OCR text.
type MemeHashtagSuggester interface {
	SuggestHashtag(ctx context.Context, url, text string, minScore *float64) (map[string]any, error)
}

type DiscordMessageDeliverer = communications.ChannelSender

// MemeAttachmentFetcher downloads a pooled meme's raw bytes when the origin host blocks
// a plain HTTP client (Cloudflare and similar bot checks) but the meme service's own
// client can get through.
type MemeAttachmentFetcher interface {
	FetchAttachment(ctx context.Context, url string) (data []byte, contentType string, err error)
}

type SystemDomainHandler struct {
	contentPlugins   bool
	chatProvider     ChatProviderHealthChecker
	memeStatus       MemeStatusReader
	memeFetch        MemeFetcher
	memeHashtags     MemeHashtagSuggester
	discord          DiscordMessageDeliverer
	deliveryChannels []string
	safeOnlyChannels map[string]struct{}
	memeScreen       MemeScreener
	memeMarker       MemeMarker
	memeAttachments  MemeAttachmentFetcher
	controls         *core.Controls
	profiles         ChatProfiles
	hashtagNotes     HashtagExperienceNotes
	hashtagTask      HashtagSuggestionTask
	hashtagAttempts  HashtagAttemptNotes
	probes           map[string]ServiceProbe
	interfaceProbes  map[string]InterfaceProbe
	links            map[string]InterfaceLinker
	whatsapp         WhatsAppSender
}

func NewSystemDomainHandler(
	chatProvider ChatProviderHealthChecker,
	memeStatus MemeStatusReader,
	memeFetch MemeFetcher,
	discord DiscordMessageDeliverer,
	deliveryChannels string,
) *SystemDomainHandler {
	return &SystemDomainHandler{
		chatProvider:     chatProvider,
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

func communicationSendRequest(event core.Event) communications.SendRequest {
	all, _ := event.Payload["all"].(bool)
	force, _ := event.Payload["force_nsfw"].(bool)
	return communications.SendRequest{Action: event.Type, Source: string(event.Source), Interface: resultString(event.Context, "interface"), Content: resultString(event.Payload, "content"), AttachmentURL: resultString(event.Payload, "attachment_url"), Targets: stringSlice(event.Payload["channel_ids"]), All: all, ForceNSFW: force}
}
func (h *SystemDomainHandler) deliveryPolicy() communications.DeliveryPolicy {
	return communications.DeliveryPolicy{Channels: h.deliveryChannels, SafeOnly: h.safeOnlyChannels, Disabled: map[string]bool{"discord": !h.serviceEnabled("discord"), "whatsapp": !h.serviceEnabled("whatsapp")}}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// WithMemeTools gives admin sends what they need to enforce the safe-only rule
// (screener) and to keep the scheduler from repeating a hand-picked meme (marker).
func (h *SystemDomainHandler) WithMemeTools(screener MemeScreener, marker MemeMarker) *SystemDomainHandler {
	h.memeScreen = screener
	h.memeMarker = marker
	return h
}

// WithHashtagSuggestions enables appending a learned hashtag to scheduled meme text.
// A missing/low-confidence suggestion never blocks delivery.
func (h *SystemDomainHandler) WithHashtagSuggestions(suggester MemeHashtagSuggester) *SystemDomainHandler {
	h.memeHashtags = suggester
	return h
}

// WithHashtagExperience notes each learned hashtag the handler appends, so the experience
// memory can later score how often the operator keeps it.
func (h *SystemDomainHandler) WithHashtagExperience(notes HashtagExperienceNotes) *SystemDomainHandler {
	h.hashtagNotes = notes
	return h
}

// HashtagAttemptNotes records a hashtag task attempt whose delivery reached a channel.
type HashtagAttemptNotes interface {
	Executed(url string, attempt HashtagAttempt)
}

// WithHashtagTask runs suggestions through the typed hashtag task (step 06) instead of
// WithHashtagSuggestions/WithHashtagExperience: the shadow prediction is frozen before
// delivery, and an attempt is recorded only once a channel received the meme. Leaving it
// out keeps the earlier path.
func (h *SystemDomainHandler) WithHashtagTask(task HashtagSuggestionTask, notes HashtagAttemptNotes) *SystemDomainHandler {
	h.hashtagTask, h.hashtagAttempts = task, notes
	return h
}

// WithMemeAttachments routes attachment delivery through the meme service's own
// download, for hosts (Cloudflare and similar) that block a plain HTTP client. Without
// it, attachments are sent by URL and the delivery interface fetches them itself.
func (h *SystemDomainHandler) WithMemeAttachments(fetcher MemeAttachmentFetcher) *SystemDomainHandler {
	h.memeAttachments = fetcher
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
	if event.Type == "discord.send" || event.Type == "communications.send" {
		return h.handleDiscordSend(ctx, event, workflow)
	}
	switch event.Type {
	case "communications.link.status", "communications.link.start", "communications.link.reset":
		return h.handleInterfaceLink(ctx, event)
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
	if h.chatProvider != nil && h.serviceEnabled("chat") {
		if err := h.chatProvider.Health(ctx); err != nil {
			overallStatus = "degraded"
			services["chat"] = map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
		} else {
			services["chat"] = map[string]any{
				"status": "ok",
			}
		}
	}

	if !h.contentPlugins && h.memeStatus != nil && h.serviceEnabled("meme") {
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
	if h.probeServices(ctx, services) {
		overallStatus = "degraded"
	}
	if h.probeInterfaces(ctx, services) {
		overallStatus = "degraded"
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
	// A channel of an interface that is switched off is left out, not failed: it would otherwise
	// log a failure on every tick for as long as the switch stays off.
	switchedOff := map[string]struct{}{}
	for _, meme := range memesFromResult(result) {
		content, attachmentURL := formatScheduledMeme(meme)
		if content == "" {
			continue
		}
		content, attempt := h.appendLearnedHashtag(ctx, event, content, attachmentURL)
		label, _ := meme["nsfw"].(map[string]any)
		assessment := moderation.AssessMediaLabel(label)
		safe, reason := assessment.Safe, assessment.Reason
		sent := false
		for _, channelID := range h.deliveryChannels {
			if _, skip := failed[channelID]; skip {
				continue
			}
			policy := h.deliveryPolicy()
			if policy.Disabled[communications.PlatformOf(channelID)] {
				switchedOff[channelID] = struct{}{}
				continue
			}
			if !policy.Eligible(channelID, safe, false) {
				skippedUnsafe[channelID]++
				logger.Info(fmt.Sprintf(
					"system handler meme skipped for safe-only channel event_id=%s channel_id=%s reason=%s",
					event.EventID,
					channelID,
					reason,
				))
				continue
			}
			if err := h.deliverAttachment(ctx, channelID, content, attachmentURL); err != nil {
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
			h.noteHashtagAttempt(attachmentURL, attempt)
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
	if len(switchedOff) > 0 {
		response["skipped_interfaces"] = sortedKeys(switchedOff)
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

// appendLearnedHashtag adds a relevant learned hashtag to content. With the hashtag task it
// also returns the attempt, to be recorded only after a delivery succeeds.
func (h *SystemDomainHandler) appendLearnedHashtag(ctx context.Context, event core.Event, content, url string) (string, *HashtagAttempt) {
	if url == "" || !h.serviceEnabled("meme") {
		return content, nil
	}
	if h.hashtagTask != nil {
		attempt := h.hashtagTask.Suggest(ctx, event, url)
		if attempt.Hashtag == "" {
			return content, nil
		}
		return withHashtag(content, attempt.Hashtag), &attempt
	}
	if h.memeHashtags == nil {
		return content, nil
	}
	result, err := h.memeHashtags.SuggestHashtag(ctx, url, "", nil)
	if err != nil {
		logger.Info(fmt.Sprintf("system handler hashtag suggestion unavailable url=%s error=%v", url, err))
		return content, nil
	}
	relevant, _ := result["relevant"].(bool)
	hashtag := strings.TrimSpace(resultString(result, "hashtag"))
	if !relevant || hashtag == "" {
		return content, nil
	}
	if h.hashtagNotes != nil {
		score, _ := result["score"].(float64)
		h.hashtagNotes.Suggested(url, resultString(result, "query_text"), hashtag, HashtagMatch{
			Score: score, Text: resultString(result, "matched_text"),
		})
	}
	return withHashtag(content, hashtag), nil
}

func withHashtag(content, hashtag string) string {
	hashtag = strings.TrimSpace(hashtag)
	for _, word := range strings.Fields(content) {
		word = strings.Trim(word, ".,!?;:()[]{}")
		if strings.EqualFold(word, hashtag) {
			return content
		}
	}
	if content == "" {
		return hashtag
	}
	return fmt.Sprintf("%s\n%s", content, hashtag)
}

// noteHashtagAttempt records a task attempt after its meme reached at least one channel. A
// failed or skipped delivery is not an example of the suggestion being used (ADR 0004).
func (h *SystemDomainHandler) noteHashtagAttempt(url string, attempt *HashtagAttempt) {
	if attempt != nil && h.hashtagAttempts != nil {
		h.hashtagAttempts.Executed(url, *attempt)
	}
}

// deliverAttachment posts content to a channel. When the meme service's own download is
// configured (WithMemeAttachments), it fetches the attachment there first, since some
// hosts (Cloudflare and similar) block a plain HTTP client; a failed fetch falls back to
// sending the attachment by URL, same as when the feature is not enabled at all.
func (h *SystemDomainHandler) deliverAttachment(ctx context.Context, channelID, content, attachmentURL string) error {
	if attachmentURL == "" || h.memeAttachments == nil || !h.serviceEnabled("meme") {
		return h.discord.Deliver(ctx, channelID, content, attachmentURL)
	}
	data, contentType, err := h.memeAttachments.FetchAttachment(ctx, attachmentURL)
	if err != nil {
		logger.Info(fmt.Sprintf(
			"system handler attachment fetch fell back to url url=%s error=%v",
			attachmentURL, err,
		))
		return h.discord.Deliver(ctx, channelID, content, attachmentURL)
	}
	return h.discord.DeliverAttachment(ctx, channelID, content, data, contentType, attachmentURL)
}

// handleDiscordSend posts an admin-supplied message to configured channels. It only
// accepts channels the orchestrator already delivers to, so the request port cannot
// be used to post anywhere the bot can reach.
func (h *SystemDomainHandler) handleDiscordSend(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if channels := stringSlice(event.Payload["channel_ids"]); communications.HasConversationTarget(channels) {
		return h.handleWhatsAppSend(ctx, event, workflow, channels)
	}
	if h.discord == nil {
		return nil, fmt.Errorf("discord deliverer is not configured")
	}

	policy := h.deliveryPolicy()
	plan, err := policy.PlanChannelSend(communicationSendRequest(event))
	if err != nil {
		return nil, err
	}
	content, attachmentURL, forceNSFW := plan.Content, plan.AttachmentURL, plan.ForceNSFW
	targets, switchedOff := plan.Targets, plan.SwitchedOff
	var attempt *HashtagAttempt
	if skip, _ := event.Payload["skip_hashtag"].(bool); !skip {
		content, attempt = h.appendLearnedHashtag(ctx, event, content, attachmentURL)
	}

	// The safe-only rule applies here too. Only an explicit TUI --force bypasses it;
	// otherwise a failed server-side screen counts as unsafe.
	blocked := map[string]struct{}{}
	blockReason := ""
	restricted := []string{}
	if attachmentURL != "" {
		for _, channelID := range targets {
			if policy.RequiresSafe(channelID) {
				restricted = append(restricted, channelID)
			}
		}
		if len(restricted) > 0 && !forceNSFW {
			safe, reason := h.screenAttachment(ctx, attachmentURL, content)
			for _, channelID := range restricted {
				if !policy.Eligible(channelID, safe, forceNSFW) {
					blockReason = reason
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
		if err := h.deliverAttachment(ctx, channelID, content, attachmentURL); err != nil {
			errs = append(errs, fmt.Errorf("send to channel %s: %w", channelID, err))
			continue
		}
		sent = append(sent, channelID)
	}
	if len(sent) > 0 {
		h.noteHashtagAttempt(attachmentURL, attempt)
	}
	logger.Info(fmt.Sprintf(
		"system handler discord send event_id=%s sent=%s skipped_unsafe=%s failed=%d has_attachment=%t nsfw_bypassed=%t",
		event.EventID,
		strings.Join(sent, ","),
		strings.Join(skipped, ","),
		len(errs),
		attachmentURL != "",
		forceNSFW && len(restricted) > 0,
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
	if forceNSFW && len(restricted) > 0 {
		response["nsfw_bypassed"] = true
	}
	if len(switchedOff) > 0 {
		response["skipped_interfaces"] = switchedOff
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
	decision := (moderation.MediaPolicy{
		Screener: h.memeScreen,
		Enabled:  h.serviceEnabled("meme"),
	}).Screen(ctx, url, caption)
	return decision.Safe, decision.Reason
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
	// Known interfaces remain in the catalog even before setup. Routing enablement
	// remains separate from whether their connection endpoint is configured.
	for _, name := range core.KnownInterfaces {
		enabled := h.controls.ServiceEnabled(name)
		entry, _ := services[name].(map[string]any)
		if entry == nil || !enabled {
			entry = map[string]any{"kind": "interface", "configured": h.interfaceProbes[name] != nil}
		}
		entry["enabled"] = enabled
		if !enabled {
			// The status says it is off; a sentence of its own would only repeat it, in English.
			entry["status"] = "disabled"
		}
		services[name] = entry
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

// WithContentPlugins reports content providers through Feeds instead of standalone services.
func (h *SystemDomainHandler) WithContentPlugins(enabled bool) *SystemDomainHandler {
	h.contentPlugins = enabled
	return h
}
