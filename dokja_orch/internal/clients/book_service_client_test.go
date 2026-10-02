package clients

import (
	"context"
	"fmt"
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

func bookClientAnswering(status int, body string, seen *string) *BookServiceClient {
	return &BookServiceClient{
		endpoint: "http://book-service.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if seen != nil {
					*seen = req.Method + " " + req.URL.Path
				}
				return &http.Response{
					StatusCode: status,
					Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}
}

func TestBookServiceClientHealthCallsTheHealthEndpoint(t *testing.T) {
	var seen string
	client := bookClientAnswering(http.StatusOK, `{"status":"ok","service":"dokja-book"}`, &seen)

	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen != "GET /health" {
		t.Fatalf("expected GET /health, got %q", seen)
	}
}

func TestBookServiceClientHealthReportsEveryWayItCanFail(t *testing.T) {
	for name, tc := range map[string]struct {
		client *BookServiceClient
		want   string
	}{
		"http error":     {bookClientAnswering(http.StatusServiceUnavailable, `{"status":"error","message":"model is loading"}`, nil), "model is loading"},
		"non json error": {bookClientAnswering(http.StatusBadGateway, `<html>bad gateway</html>`, nil), "502"},
		"non ok status":  {bookClientAnswering(http.StatusOK, `{"status":"starting"}`, nil), "non-ok"},
		"not json":       {bookClientAnswering(http.StatusOK, `ok`, nil), "decode"},
		"unconfigured":   {nil, "not configured"},
	} {
		err := tc.client.Health(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error containing %q, got %v", name, tc.want, err)
		}
	}
}

func TestBookServiceClientHealthGivesUpWhenTheServiceNeverAnswers(t *testing.T) {
	client := &BookServiceClient{
		endpoint: "http://book-service.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := client.Health(ctx); err == nil {
		t.Fatal("expected an error from a cancelled probe")
	}
}
