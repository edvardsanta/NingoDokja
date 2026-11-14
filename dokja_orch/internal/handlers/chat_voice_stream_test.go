package handlers

import (
	"context"
	"reflect"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

// runtimeStyleGenerator is the Runtime provider: GenerateMessages answers at once, GenerateMessagesStream
// shows the text written so far first. Each preview repeats everything so far.
type runtimeStyleGenerator struct {
	previews []string
	reply    string
	// afterPreview runs right after each preview was handed to the handler and before the reply is
	// returned, so a test can see what had already been spoken while the model was still writing.
	afterPreview func(index int)
	streamCalls  int
	plainCalls   int
}

func (g *runtimeStyleGenerator) Generate(context.Context, string) (map[string]any, error) {
	return map[string]any{"reply": g.reply}, nil
}

func (g *runtimeStyleGenerator) GenerateMessages(context.Context, []map[string]string) (map[string]any, error) {
	g.plainCalls++
	return map[string]any{"reply": g.reply}, nil
}

func (g *runtimeStyleGenerator) GenerateMessagesStream(_ context.Context, _ []map[string]string, onPreview func(string)) (map[string]any, error) {
	g.streamCalls++
	for index, text := range g.previews {
		onPreview(text)
		if g.afterPreview != nil {
			g.afterPreview(index)
		}
	}
	return map[string]any{"reply": g.reply}, nil
}

func voiceEvent(text string) core.Event {
	event := chatEvent(text)
	event.Context = map[string]any{"session_key": "voice-room", "voice_turns": []chat.VoiceTurn{{UserID: "member", Text: text}}}
	return event
}

func TestRuntimeVoiceReplySpeaksEachSentenceWhileTheModelIsStillWriting(t *testing.T) {
	var spoken []string
	var spokenWhileWriting [][]string
	generator := &runtimeStyleGenerator{
		previews: []string{"O céu", "O céu é azul por causa", "O céu é azul por causa do espalhamento. A luz azul", "O céu é azul por causa do espalhamento. A luz azul se espalha mais."},
		reply:    "O céu é azul por causa do espalhamento. A luz azul se espalha mais.",
	}
	generator.afterPreview = func(int) { spokenWhileWriting = append(spokenWhileWriting, append([]string(nil), spoken...)) }
	handler := NewChatDomainHandler(generator)
	ctx := core.WithVoiceSpeech(t.Context(), func(text string) { spoken = append(spoken, text) })

	result, err := handler.Handle(ctx, voiceEvent("Ningo, por que o céu é azul?"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if generator.streamCalls != 1 || generator.plainCalls != 0 {
		t.Fatalf("stream calls=%d plain calls=%d", generator.streamCalls, generator.plainCalls)
	}
	// After the third preview the model has written one whole sentence and half of the next: the first is
	// already spoken while the model is still writing, and nothing is spoken before a sentence is whole.
	if len(spokenWhileWriting[1]) != 0 || !reflect.DeepEqual(spokenWhileWriting[2], []string{"O céu é azul por causa do espalhamento."}) {
		t.Fatalf("spoken while the model was still writing: %q", spokenWhileWriting)
	}
	if !reflect.DeepEqual(spoken, []string{"O céu é azul por causa do espalhamento.", "A luz azul se espalha mais."}) {
		t.Fatalf("spoken: %q", spoken)
	}
	// The final reply still opens with everything already spoken, which is how the voice client drops it.
	if result["reply"] != "O céu é azul por causa do espalhamento. A luz azul se espalha mais." {
		t.Fatalf("final reply: %#v", result["reply"])
	}
}

func TestRuntimeVoiceReplyIsStillBoundedToTwoSentences(t *testing.T) {
	var spoken []string
	generator := &runtimeStyleGenerator{
		previews: []string{"Primeira. Segunda. Terceira. Quarta."},
		reply:    "Primeira. Segunda. Terceira. Quarta.",
	}
	handler := NewChatDomainHandler(generator)
	ctx := core.WithVoiceSpeech(t.Context(), func(text string) { spoken = append(spoken, text) })

	result, err := handler.Handle(ctx, voiceEvent("Ningo, por que o céu é azul?"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spoken, []string{"Primeira.", "Segunda."}) || result["reply"] != "Primeira. Segunda." {
		t.Fatalf("an ordinary spoken reply is two sentences: spoken=%q reply=%#v", spoken, result["reply"])
	}
}

func TestRuntimeVoiceReplyFromAProviderWithoutPreviewsIsStillSpokenWhole(t *testing.T) {
	var spoken []string
	generator := &runtimeStyleGenerator{reply: "Resposta inteira. E mais uma."} // no previews: Claude Code, Kimi
	handler := NewChatDomainHandler(generator)
	ctx := core.WithVoiceSpeech(t.Context(), func(text string) { spoken = append(spoken, text) })

	result, err := handler.Handle(ctx, voiceEvent("Ningo, por que o céu é azul?"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if len(spoken) != 0 || result["reply"] != "Resposta inteira. E mais uma." {
		t.Fatalf("nothing was written early, so the reply is returned whole: spoken=%q reply=%#v", spoken, result["reply"])
	}
}

func TestRuntimeVoiceReplyFromAGeneratorThatCannotStreamStillWorks(t *testing.T) {
	var spoken []string
	handler := NewChatDomainHandler(&plainGenerator{})
	ctx := core.WithVoiceSpeech(t.Context(), func(text string) { spoken = append(spoken, text) })

	result, err := handler.Handle(ctx, voiceEvent("Ningo, que bom te ouvir"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if len(spoken) != 0 || result["reply"] != "ok" {
		t.Fatalf("spoken=%q reply=%#v", spoken, result["reply"])
	}
}

func TestATextTurnNeverSpeaksEvenWhenTheRequestCarriesAVoiceCallback(t *testing.T) {
	var spoken []string
	generator := &runtimeStyleGenerator{previews: []string{"Uma frase completa."}, reply: "Uma frase completa."}
	handler := NewChatDomainHandler(generator)
	var previewed []string
	ctx := core.WithReplyPreview(core.WithVoiceSpeech(t.Context(), func(text string) { spoken = append(spoken, text) }), func(text string) { previewed = append(previewed, text) })

	if _, err := handler.Handle(ctx, chatEvent("Explique o céu azul"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(spoken) != 0 || !reflect.DeepEqual(previewed, []string{"Uma frase completa."}) {
		t.Fatalf("a text turn only previews: spoken=%q previewed=%q", spoken, previewed)
	}
}
