package runtime

import (
	"fmt"
	domain "read_books/dokja_domain/dokja_runtime"
)

func (s *DatabaseSettings) ReadChat() (domain.ChatBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, exists, err := s.DB.ChatConfiguration()
	if err != nil {
		return domain.ChatBinding{}, fmt.Errorf("cannot read chat configuration")
	}
	if !exists {
		c, err = s.DB.InitializeChatConfiguration(map[string]string{"provider": "", "model": ""})
		if err != nil {
			return domain.ChatBinding{}, fmt.Errorf("cannot initialize chat configuration")
		}
	}
	if len(c.Values) != 2 {
		return domain.ChatBinding{}, fmt.Errorf("invalid chat configuration")
	}
	p, pok := c.Values["provider"]
	m, mok := c.Values["model"]
	if !pok || !mok {
		return domain.ChatBinding{}, fmt.Errorf("invalid chat configuration")
	}
	return domain.ChatBinding{Revision: c.Revision, Provider: p, Model: m}, nil
}
func (s *DatabaseSettings) WriteChat(b domain.ChatBinding) (domain.ChatBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.DB.UpdateChatConfiguration(b.Revision, map[string]string{"provider": b.Provider, "model": b.Model})
	if err != nil {
		return domain.ChatBinding{}, fmt.Errorf("chat configuration changed or cannot be saved; reload before retrying")
	}
	b.Revision = c.Revision
	return b, nil
}
