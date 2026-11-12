package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultDiscordInterfaceEndpoint = "http://127.0.0.1:8092/deliver"

type DiscordInterfaceClient struct {
	endpoint   string
	httpClient *http.Client
}

type DiscordDeliveryRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content,omitempty"`
	// AttachmentURL is also kept when the bytes are sent below, only to derive a
	// filename; the delivery interface does not fetch it again in that case.
	AttachmentURL         string `json:"attachment_url,omitempty"`
	AttachmentDataB64     string `json:"attachment_data_b64,omitempty"`
	AttachmentContentType string `json:"attachment_content_type,omitempty"`
}

type DiscordDeliveryResponse struct {
	Status        string `json:"status"`
	ChannelID     string `json:"channel_id,omitempty"`
	MessageID     string `json:"message_id,omitempty"`
	GuildID       string `json:"guild_id,omitempty"`
	HasAttachment bool   `json:"has_attachment,omitempty"`
	Message       string `json:"message,omitempty"`
}

func NewDiscordInterfaceClient(endpoint string) *DiscordInterfaceClient {
	resolvedEndpoint := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if resolvedEndpoint == "" {
		resolvedEndpoint = strings.TrimRight(strings.TrimSpace(os.Getenv("DISCORD_INTERFACE_ENDPOINT")), "/")
	}
	if resolvedEndpoint == "" {
		resolvedEndpoint = defaultDiscordInterfaceEndpoint
	}

	return &DiscordInterfaceClient{
		endpoint: resolvedEndpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *DiscordInterfaceClient) Deliver(ctx context.Context, channelID, content, attachmentURL string) error {
	if strings.TrimSpace(content) == "" && strings.TrimSpace(attachmentURL) == "" {
		return fmt.Errorf("discord delivery requires content or attachment_url")
	}
	_, err := c.deliver(ctx, DiscordDeliveryRequest{
		ChannelID:     strings.TrimSpace(channelID),
		Content:       strings.TrimSpace(content),
		AttachmentURL: strings.TrimSpace(attachmentURL),
	})
	return err
}

// DiscordDeliveryReceipt identifies the message a delivery created, so the
// caller can link what was posted later.
type DiscordDeliveryReceipt struct {
	GuildID   string
	ChannelID string
	MessageID string
}

// DeliverWithReceipt posts like Deliver and DeliverAttachment — data nil sends the
// attachment by URL; data set sends the bytes — and reports the created message.
func (c *DiscordInterfaceClient) DeliverWithReceipt(ctx context.Context, channelID, content, attachmentURL string, data []byte, contentType string) (DiscordDeliveryReceipt, error) {
	if strings.TrimSpace(content) == "" && strings.TrimSpace(attachmentURL) == "" && len(data) == 0 {
		return DiscordDeliveryReceipt{}, fmt.Errorf("discord delivery requires content or attachment_url")
	}
	request := DiscordDeliveryRequest{
		ChannelID:     strings.TrimSpace(channelID),
		Content:       strings.TrimSpace(content),
		AttachmentURL: strings.TrimSpace(attachmentURL),
	}
	if len(data) > 0 {
		request.AttachmentDataB64 = base64.StdEncoding.EncodeToString(data)
		request.AttachmentContentType = strings.TrimSpace(contentType)
	}
	response, err := c.deliver(ctx, request)
	if err != nil {
		return DiscordDeliveryReceipt{}, err
	}
	return DiscordDeliveryReceipt{
		GuildID:   response.GuildID,
		ChannelID: response.ChannelID,
		MessageID: response.MessageID,
	}, nil
}

// DeliverAttachment posts content with the attachment's bytes already in hand, for
// hosts a plain HTTP client cannot fetch directly (Cloudflare and similar bot checks).
// attachmentURL is kept only so the delivery interface can derive a filename from it;
// it is never fetched again.
func (c *DiscordInterfaceClient) DeliverAttachment(ctx context.Context, channelID, content string, data []byte, contentType, attachmentURL string) error {
	if len(data) == 0 {
		return fmt.Errorf("discord delivery requires non-empty attachment data")
	}
	_, err := c.deliver(ctx, DiscordDeliveryRequest{
		ChannelID:             strings.TrimSpace(channelID),
		Content:               strings.TrimSpace(content),
		AttachmentURL:         strings.TrimSpace(attachmentURL),
		AttachmentDataB64:     base64.StdEncoding.EncodeToString(data),
		AttachmentContentType: strings.TrimSpace(contentType),
	})
	return err
}

func (c *DiscordInterfaceClient) deliver(ctx context.Context, deliveryRequest DiscordDeliveryRequest) (DiscordDeliveryResponse, error) {
	if c == nil || c.httpClient == nil {
		return DiscordDeliveryResponse{}, fmt.Errorf("discord interface client is not configured")
	}
	if deliveryRequest.ChannelID == "" {
		return DiscordDeliveryResponse{}, fmt.Errorf("discord delivery channel_id is required")
	}

	body, err := json.Marshal(deliveryRequest)
	if err != nil {
		return DiscordDeliveryResponse{}, fmt.Errorf("marshal discord delivery request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return DiscordDeliveryResponse{}, fmt.Errorf("build discord delivery request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return DiscordDeliveryResponse{}, fmt.Errorf("call discord delivery interface: %w", err)
	}
	defer response.Body.Close()

	var deliveryResponse DiscordDeliveryResponse
	responseBody, readErr := io.ReadAll(response.Body)
	if readErr == nil && len(responseBody) > 0 {
		_ = json.Unmarshal(responseBody, &deliveryResponse)
	}

	if response.StatusCode >= 400 {
		message := strings.TrimSpace(deliveryResponse.Message)
		if message != "" {
			return DiscordDeliveryResponse{}, fmt.Errorf("discord delivery interface returned status %d: %s", response.StatusCode, message)
		}
		return DiscordDeliveryResponse{}, fmt.Errorf("discord delivery interface returned status %d", response.StatusCode)
	}
	if strings.TrimSpace(deliveryResponse.Status) != "" && deliveryResponse.Status != "ok" {
		if strings.TrimSpace(deliveryResponse.Message) != "" {
			return DiscordDeliveryResponse{}, fmt.Errorf("discord delivery interface returned status %q: %s", deliveryResponse.Status, deliveryResponse.Message)
		}
		return DiscordDeliveryResponse{}, fmt.Errorf("discord delivery interface returned status %q", deliveryResponse.Status)
	}
	return deliveryResponse, nil
}
