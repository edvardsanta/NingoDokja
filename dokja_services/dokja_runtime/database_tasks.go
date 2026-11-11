package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
)

func (s *DatabaseSettings) taskSnapshot() (domain.TaskProfiles, error) {
	c, exists, err := s.DB.TaskConfiguration()
	if err != nil {
		return domain.TaskProfiles{}, fmt.Errorf("cannot read task profiles")
	}
	if !exists {
		c, err = s.DB.InitializeTaskConfiguration(domain.DefaultTaskProfileValues())
		if err != nil {
			return domain.TaskProfiles{}, fmt.Errorf("cannot initialize task profiles")
		}
	}
	return domain.DecodeTaskProfiles(c.Revision, c.Values)
}
func (s *DatabaseSettings) ReadTaskProfiles() (domain.TaskProfiles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskSnapshot()
}
func (s *DatabaseSettings) WriteTaskProfiles(p domain.TaskProfiles) (domain.TaskProfiles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.taskSnapshot(); err != nil {
		return domain.TaskProfiles{}, err
	}
	values := map[string]string{}
	for name, rows := range p.Profiles {
		data, err := json.Marshal(rows)
		if err != nil {
			return domain.TaskProfiles{}, err
		}
		values[name] = string(data)
	}
	c, err := s.DB.UpdateTaskConfiguration(p.Revision, values)
	if errors.Is(err, store.ErrConfigurationConflict) {
		return domain.TaskProfiles{}, domain.ErrProviderConflict
	}
	if err != nil {
		return domain.TaskProfiles{}, fmt.Errorf("cannot save task profiles")
	}
	return domain.DecodeTaskProfiles(c.Revision, c.Values)
}
