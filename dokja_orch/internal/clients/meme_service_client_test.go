package clients

import (
	"context"
	"errors"
	"read_books/internal/infrastructure/repreq"
	"strings"
	"testing"
)

type fakeMemeRequester struct {
	requestBody string
	response    string
	err         error
}

func (f *fakeMemeRequester) Request(message string) (string, error) {
	f.requestBody = message
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeMemeRequester) Close() error {
	return nil
}

func TestMemeServiceClientDispatch(t *testing.T) {
	requester := &fakeMemeRequester{
		response: `{"status":"ok","result":{"count":1}}`,
	}

	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return requester, nil
	})

	response, err := client.Fetch(context.Background(), intPtr(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response["count"] != float64(1) {
		t.Fatalf("expected count 1, got %#v", response["count"])
	}
	if !strings.Contains(requester.requestBody, `"type":"meme.fetch"`) {
		t.Fatalf("expected request body to include event type, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientDispatchErrors(t *testing.T) {
	if _, err := (*MemeServiceClient)(nil).Fetch(context.Background(), nil); err == nil {
		t.Fatal("expected nil client error")
	}

	factoryErr := errors.New("factory")
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return nil, factoryErr
	})
	if _, err := client.Status(context.Background()); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}

	requestErr := errors.New("request")
	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{err: requestErr}, nil
	})
	if _, err := client.Status(context.Background()); !errors.Is(err, requestErr) {
		t.Fatalf("expected request error, got %v", err)
	}

	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{response: "not-json"}, nil
	})
	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("expected unmarshal error")
	}

	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{response: `{"status":"error","message":"boom"}`}, nil
	})
	if _, err := client.Status(context.Background()); err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestValidateMemeServiceEndpoint(t *testing.T) {
	if err := validateMemeServiceEndpoint("tcp://dokja-meme:5557"); err != nil {
		t.Fatalf("expected routable endpoint to be valid, got %v", err)
	}

	for _, endpoint := range []string{"tcp://*:5557", "tcp://0.0.0.0:5557"} {
		err := validateMemeServiceEndpoint(endpoint)
		if err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
		if !strings.Contains(err.Error(), "bind address") {
			t.Fatalf("expected bind address guidance for %q, got %v", endpoint, err)
		}
	}
}

func intPtr(value int) *int {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}

func TestMemeServiceClientTagHashtag(t *testing.T) {
	requester := &fakeMemeRequester{response: `{"status":"ok","result":{"hashtag":"#TioDoPave"}}`}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	result, err := client.TagHashtag(context.Background(), "https://x/a.png", "TioDoPave", "o tio pegou o pave")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["hashtag"] != "#TioDoPave" {
		t.Fatalf("unexpected result %#v", result)
	}
	for _, want := range []string{`"type":"meme.hashtag.tag"`, `"url":"https://x/a.png"`, `"hashtag":"TioDoPave"`, `"text":"o tio pegou o pave"`} {
		if !strings.Contains(requester.requestBody, want) {
			t.Fatalf("expected request body to include %s, got %q", want, requester.requestBody)
		}
	}
}

func TestMemeServiceClientTagHashtagOmitsEmptyText(t *testing.T) {
	requester := &fakeMemeRequester{response: `{"status":"ok","result":{}}`}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	if _, err := client.TagHashtag(context.Background(), "https://x/a.png", "TioDoPave", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(requester.requestBody, `"text"`) {
		t.Fatalf("expected no text field when it is empty, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientSuggestHashtag(t *testing.T) {
	requester := &fakeMemeRequester{
		response: `{"status":"ok","result":{"hashtag":"#TioDoPave","relevant":true}}`,
	}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	result, err := client.SuggestHashtag(context.Background(), "", "o tio comeu o pave", floatPtr(0.7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["relevant"] != true {
		t.Fatalf("unexpected result %#v", result)
	}
	for _, want := range []string{`"type":"meme.hashtag.suggest"`, `"text":"o tio comeu o pave"`, `"min_score":0.7`} {
		if !strings.Contains(requester.requestBody, want) {
			t.Fatalf("expected request body to include %s, got %q", want, requester.requestBody)
		}
	}
	if strings.Contains(requester.requestBody, `"url"`) {
		t.Fatalf("expected no url field when it is empty, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientSuggestHashtagWithoutMinScoreOmitsIt(t *testing.T) {
	requester := &fakeMemeRequester{response: `{"status":"ok","result":{}}`}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	if _, err := client.SuggestHashtag(context.Background(), "https://x/a.png", "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(requester.requestBody, `"min_score"`) {
		t.Fatalf("expected no min_score field when nil, got %q", requester.requestBody)
	}
	if !strings.Contains(requester.requestBody, `"url":"https://x/a.png"`) {
		t.Fatalf("expected the url field, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientListHashtags(t *testing.T) {
	requester := &fakeMemeRequester{response: `{"status":"ok","result":{"total":2}}`}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	result, err := client.ListHashtags(context.Background(), 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["total"] != float64(2) {
		t.Fatalf("unexpected result %#v", result)
	}
	for _, want := range []string{`"type":"meme.hashtag.list"`, `"limit":10`, `"offset":5`} {
		if !strings.Contains(requester.requestBody, want) {
			t.Fatalf("expected request body to include %s, got %q", want, requester.requestBody)
		}
	}
}

func TestMemeServiceClientUntagHashtag(t *testing.T) {
	requester := &fakeMemeRequester{response: `{"status":"ok","result":{"deleted":true}}`}
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })

	result, err := client.UntagHashtag(context.Background(), "https://x/a.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["deleted"] != true {
		t.Fatalf("unexpected result %#v", result)
	}
	if !strings.Contains(requester.requestBody, `"type":"meme.hashtag.untag"`) {
		t.Fatalf("expected the untag event type, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientHashtagMethodsPropagateErrors(t *testing.T) {
	factoryErr := errors.New("factory")
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return nil, factoryErr })
	if _, err := client.TagHashtag(context.Background(), "u", "h", "t"); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}
	if _, err := client.SuggestHashtag(context.Background(), "u", "", nil); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}
	if _, err := client.ListHashtags(context.Background(), 1, 0); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}
	if _, err := client.UntagHashtag(context.Background(), "u"); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}
}
