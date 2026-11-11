package runtime

import (
	"encoding/json"
	"fmt"
)

type TaskProfiles struct {
	Revision uint64                   `json:"configuration_revision"`
	Profiles map[string][]ChatBinding `json:"profiles"`
}
type TaskProfileSettingsStore interface {
	ReadTaskProfiles() (TaskProfiles, error)
	WriteTaskProfiles(TaskProfiles) (TaskProfiles, error)
}

func DefaultTaskProfileValues() map[string]string {
	return map[string]string{"lightweight": "[]", "analysis": "[]"}
}
func DecodeTaskProfiles(revision uint64, values map[string]string) (TaskProfiles, error) {
	p := TaskProfiles{Revision: revision, Profiles: map[string][]ChatBinding{}}
	if revision < 1 || revision > MaxProviderRevision || len(values) != 2 {
		return p, fmt.Errorf("invalid task profiles")
	}
	for _, name := range []string{"lightweight", "analysis"} {
		var rows []ChatBinding
		if err := json.Unmarshal([]byte(values[name]), &rows); err != nil || rows == nil || len(rows) > 16 {
			return p, fmt.Errorf("invalid task profiles")
		}
		p.Profiles[name] = rows
	}
	return p, nil
}
func (d *Domain) TaskProfiles() (TaskProfiles, error) {
	if store, ok := d.providerSettings.(TaskProfileSettingsStore); ok {
		return store.ReadTaskProfiles()
	}
	return DecodeTaskProfiles(1, DefaultTaskProfileValues())
}
func (d *Domain) ConfigureTaskProfiles(p TaskProfiles) (TaskProfiles, error) {
	store, ok := d.providerSettings.(TaskProfileSettingsStore)
	if !ok {
		return p, fmt.Errorf("task profile configuration unavailable")
	}
	if p.Revision < 1 || p.Revision >= MaxProviderRevision || len(p.Profiles) != 2 {
		return p, fmt.Errorf("invalid task profiles")
	}
	for _, name := range []string{"lightweight", "analysis"} {
		rows, ok := p.Profiles[name]
		if !ok || rows == nil || len(rows) > 16 {
			return p, fmt.Errorf("invalid task profile")
		}
		seen := map[string]bool{}
		for _, b := range rows {
			key := b.Provider + "/" + b.Model
			if seen[key] || b.Format != "" {
				return p, fmt.Errorf("invalid task priority")
			}
			seen[key] = true
			if err := d.validateChat(b); err != nil {
				return p, err
			}
		}
	}
	return store.WriteTaskProfiles(p)
}
