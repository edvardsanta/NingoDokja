package book

import "context"

// ProcessingPort hosts replaceable document mechanisms. The book event names
// remain a compatibility facade while readers share the Feeds plugin lifecycle.
type ProcessingPort interface {
	Invoke(context.Context, string, string, map[string]any) (map[string]any, error)
}

type ReadingService struct{ reading ProcessingPort }

func NewReadingService(reading ProcessingPort) *ReadingService {
	return &ReadingService{reading: reading}
}
func (s *ReadingService) Classify(ctx context.Context, input map[string]any) (map[string]any, error) {
	return s.reading.Invoke(ctx, "document.process.v1", "classify", input)
}
func (s *ReadingService) Summarize(ctx context.Context, input map[string]any) (map[string]any, error) {
	return s.reading.Invoke(ctx, "document.process.v1", "summarize", input)
}
