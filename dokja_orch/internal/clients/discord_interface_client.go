package clients

import (
	"bytes"
	"context"
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
	ChannelID     string `json:"channel_id"`
	Content       string `json:"content,omitempty"`
	AttachmentURL string `json:"attachment_url,omitempty"`
}

type DiscordDeliveryResponse struct {
	Status        string `json:"status"`
	ChannelID     string `json:"channel_id,omitempty"`
	MessageID     string `json:"message_id,omitempty"`
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
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("discord interface client is not configured")
	}
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("discord delivery channel_id is required")
	}
	if strings.TrimSpace(content) == "" && strings.TrimSpace(attachmentURL) == "" {
		return fmt.Errorf("discord delivery requires content or attachment_url")
	}

	body, err := json.Marshal(DiscordDeliveryRequest{
		ChannelID:     strings.TrimSpace(channelID),
		Content:       strings.TrimSpace(content),
		AttachmentURL: strings.TrimSpace(attachmentURL),
	})
	if err != nil {
		return fmt.Errorf("marshal discord delivery request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build discord delivery request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call discord delivery interface: %w", err)
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
			return fmt.Errorf("discord delivery interface returned status %d: %s", response.StatusCode, message)
		}
		return fmt.Errorf("discord delivery interface returned status %d", response.StatusCode)
	}
	if strings.TrimSpace(deliveryResponse.Status) != "" && deliveryResponse.Status != "ok" {
		if strings.TrimSpace(deliveryResponse.Message) != "" {
			return fmt.Errorf("discord delivery interface returned status %q: %s", deliveryResponse.Status, deliveryResponse.Message)
		}
		return fmt.Errorf("discord delivery interface returned status %q", deliveryResponse.Status)
	}
	return nil
}
