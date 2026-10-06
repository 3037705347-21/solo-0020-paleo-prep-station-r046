package repository

import (
	"sort"
	"sync"

	"example.com/paleo-prep-station/internal/domain"
)

type state struct {
	specimens  map[string]domain.Specimen
	plans      map[string]domain.PrepPlan
	progress   map[string][]domain.ProgressEntry
	releases   map[string]domain.ReleaseRecord
	subsamples map[string]domain.Subsample
	removals   map[string][]domain.MaterialRemoval
}

type MemoryRepository struct {
	mu    sync.Mutex
	state state
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		state: state{
			specimens:  make(map[string]domain.Specimen),
			plans:      make(map[string]domain.PrepPlan),
			progress:   make(map[string][]domain.ProgressEntry),
			releases:   make(map[string]domain.ReleaseRecord),
			subsamples: make(map[string]domain.Subsample),
			removals:   make(map[string][]domain.MaterialRemoval),
		},
	}
}

func (r *MemoryRepository) Read(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(cloneMutation(r.state))
}

func (r *MemoryRepository) Mutate(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := cloneMutation(r.state)
	if err := fn(tx); err != nil {
		return err
	}
	r.state = tx.snapshot()
	return nil
}

func cloneMutation(source state) *Mutation {
	copied := state{
		specimens:  make(map[string]domain.Specimen, len(source.specimens)),
		plans:      make(map[string]domain.PrepPlan, len(source.plans)),
		progress:   make(map[string][]domain.ProgressEntry, len(source.progress)),
		releases:   make(map[string]domain.ReleaseRecord, len(source.releases)),
		subsamples: make(map[string]domain.Subsample, len(source.subsamples)),
		removals:   make(map[string][]domain.MaterialRemoval, len(source.removals)),
	}
	for key, value := range source.specimens {
		copied.specimens[key] = value
	}
	for key, value := range source.plans {
		copied.plans[key] = value
	}
	for key, values := range source.progress {
		copied.progress[key] = append([]domain.ProgressEntry(nil), values...)
	}
	for key, value := range source.releases {
		copied.releases[key] = value
	}
	for key, value := range source.subsamples {
		copied.subsamples[key] = value
	}
	for key, values := range source.removals {
		copied.removals[key] = append([]domain.MaterialRemoval(nil), values...)
	}
	return &Mutation{state: copied}
}

type Mutation struct {
	state state
}

func (m *Mutation) snapshot() state {
	return m.state
}

func (m *Mutation) GetSpecimen(id string) (domain.Specimen, bool) {
	value, ok := m.state.specimens[id]
	return value, ok
}

func (m *Mutation) FindSpecimenByCatalog(catalogNumber string) (domain.Specimen, bool) {
	for _, value := range m.state.specimens {
		if value.CatalogNumber == catalogNumber {
			return value, true
		}
	}
	return domain.Specimen{}, false
}

func (m *Mutation) ListSpecimens() []domain.Specimen {
	values := make([]domain.Specimen, 0, len(m.state.specimens))
	for _, value := range m.state.specimens {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutSpecimen(value domain.Specimen) {
	m.state.specimens[value.ID] = value
}

func (m *Mutation) GetPlan(id string) (domain.PrepPlan, bool) {
	value, ok := m.state.plans[id]
	return value, ok
}

func (m *Mutation) ListPlansForSpecimen(specimenID string) []domain.PrepPlan {
	values := make([]domain.PrepPlan, 0)
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutPlan(value domain.PrepPlan) {
	m.state.plans[value.ID] = value
}

func (m *Mutation) HasOpenPlan(specimenID string) bool {
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID && value.IsOpen() {
			return true
		}
	}
	return false
}

func (m *Mutation) ProgressForPlan(planID string) []domain.ProgressEntry {
	return append([]domain.ProgressEntry(nil), m.state.progress[planID]...)
}

func (m *Mutation) AppendProgress(value domain.ProgressEntry) {
	m.state.progress[value.PlanID] = append(m.state.progress[value.PlanID], value)
}

func (m *Mutation) ReleaseForSpecimen(specimenID string) (domain.ReleaseRecord, bool) {
	for _, value := range m.state.releases {
		if value.SpecimenID == specimenID {
			return value, true
		}
	}
	return domain.ReleaseRecord{}, false
}

func (m *Mutation) PutRelease(value domain.ReleaseRecord) {
	m.state.releases[value.ID] = value
}

func (m *Mutation) GetSubsample(id string) (domain.Subsample, bool) {
	value, ok := m.state.subsamples[id]
	return value, ok
}

// FindSubsampleByCode enforces the globally unique subsample identity used to
// reject duplicate sampling registrations.
func (m *Mutation) FindSubsampleByCode(code string) (domain.Subsample, bool) {
	for _, value := range m.state.subsamples {
		if value.Code == code {
			return value, true
		}
	}
	return domain.Subsample{}, false
}

func (m *Mutation) PutSubsample(value domain.Subsample) {
	m.state.subsamples[value.ID] = value
}

// ListSubsamplesForSpecimen returns every subsample whose lineage roots at
// the given mother specimen, oldest sampling first.
func (m *Mutation) ListSubsamplesForSpecimen(specimenID string) []domain.Subsample {
	values := make([]domain.Subsample, 0)
	for _, value := range m.state.subsamples {
		if value.RootSpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sortSubsamples(values)
	return values
}

func (m *Mutation) RemovalsForPlan(planID string) []domain.MaterialRemoval {
	return append([]domain.MaterialRemoval(nil), m.state.removals[planID]...)
}

// RemovalsForSpecimen returns the removal ledger across all plans of one
// mother specimen by joining through the subsample identities.
func (m *Mutation) RemovalsForSpecimen(specimenID string) []domain.MaterialRemoval {
	rootSubsamples := make(map[string]struct{})
	for _, value := range m.state.subsamples {
		if value.RootSpecimenID == specimenID {
			rootSubsamples[value.ID] = struct{}{}
		}
	}
	values := make([]domain.MaterialRemoval, 0)
	for _, entries := range m.state.removals {
		for _, entry := range entries {
			if _, ok := rootSubsamples[entry.SubsampleID]; ok {
				values = append(values, entry)
			}
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if !values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].CreatedAt.Before(values[j].CreatedAt)
		}
		return values[i].ID < values[j].ID
	})
	return values
}

// AppendRemoval stores a ledger entry at the end of its plan-scoped sequence.
func (m *Mutation) AppendRemoval(value domain.MaterialRemoval) {
	m.state.removals[value.PlanID] = append(m.state.removals[value.PlanID], value)
}

func sortSubsamples(values []domain.Subsample) {
	sort.Slice(values, func(i, j int) bool {
		if !values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].CreatedAt.Before(values[j].CreatedAt)
		}
		return values[i].ID < values[j].ID
	})
}
