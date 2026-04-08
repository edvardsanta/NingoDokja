package clients

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBookServiceClientSummarizeBuildsRequestPayload(t *testing.T) {
	var requestBody string
	client := &BookServiceClient{
		endpoint: "http://book-service.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("unexpected read body error: %v", err)
				}
				requestBody = string(body)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"status":"ok","result":{"kind":"summary","title":"Clean Code"}}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	result, err := client.Summarize(context.Background(), map[string]any{"title": "Clean Code", "content": "Meaningful names matter."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["kind"] != "summary" {
		t.Fatalf("expected summary result, got %#v", result)
	}
	if !strings.Contains(requestBody, `"title":"Clean Code"`) {
		t.Fatalf("expected request body to contain title, got %s", requestBody)
	}
}

func TestBookServiceClientClassifyPropagatesContractErrorMessage(t *testing.T) {
	client := &BookServiceClient{
		endpoint: "http://book-service.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"status":"error","message":"book content is required"}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	err := func() error {
		_, err := client.Classify(context.Background(), map[string]any{"filename": "book.pdf"})
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "book content is required") {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
