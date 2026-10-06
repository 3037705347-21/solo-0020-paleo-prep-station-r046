package repository

import (
	"sort"
	"sync"

	"example.com/paleo-prep-station/internal/domain"
)

type state struct {
	specimens    map[string]domain.Specimen
	plans        map[string]domain.PrepPlan
	progress     map[string][]domain.ProgressEntry
	releases     map[string]domain.ReleaseRecord
	subsamples   map[string]domain.Subsample
	removals     map[string][]domain.MaterialRemoval
	terminations map[string]domain.TerminationRecord
}

type MemoryRepository struct {
	mu    sync.Mutex
	state state
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		state: state{
			specimens:    make(map[string]domain.Specimen),
			plans:        make(map[string]domain.PrepPlan),
			progress:     make(map[string][]domain.ProgressEntry),
			releases:     make(map[string]domain.ReleaseRecord),
			subsamples:   make(map[string]domain.Subsample),
			removals:     make(map[string][]domain.MaterialRemoval),
			terminations: make(map[string]domain.TerminationRecord),
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
		specimens:    make(map[string]domain.Specimen, len(source.specimens)),
		plans:        make(map[string]domain.PrepPlan, len(source.plans)),
		progress:     make(map[string][]domain.ProgressEntry, len(source.progress)),
		releases:     make(map[string]domain.ReleaseRecord, len(source.releases)),
		subsamples:   make(map[string]domain.Subsample, len(source.subsamples)),
		removals:     make(map[string][]domain.MaterialRemoval, len(source.removals)),
		terminations: make(map[string]domain.TerminationRecord, len(source.terminations)),
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
	for key, value := range source.terminations {
		copied.terminations[key] = value
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

// ProgressEntry 在方案的有序进度中按标识查找一条操作步骤。
func (m *Mutation) GetProgress(planID, progressID string) (domain.ProgressEntry, bool) {
	for _, value := range m.state.progress[planID] {
		if value.ID == progressID {
			return value, true
		}
	}
	return domain.ProgressEntry{}, false
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

func (m *Mutation) FindSubsampleByCode(code string) (domain.Subsample, bool) {
	for _, value := range m.state.subsamples {
		if value.Code == code {
			return value, true
		}
	}
	return domain.Subsample{}, false
}

func (m *Mutation) ListSubsamples() []domain.Subsample {
	values := make([]domain.Subsample, 0, len(m.state.subsamples))
	for _, value := range m.state.subsamples {
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

// ListSubsamplesForParent 返回直接挂在指定母对象下的子样本。
func (m *Mutation) ListSubsamplesForParent(parentID string) []domain.Subsample {
	values := make([]domain.Subsample, 0)
	for _, value := range m.state.subsamples {
		if value.ParentID == parentID {
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

func (m *Mutation) PutSubsample(value domain.Subsample) {
	m.state.subsamples[value.ID] = value
}

// RemovalsForTarget 返回挂在任一库存对象（标本或子样本）上的有序去除记录。
func (m *Mutation) RemovalsForTarget(targetID string) []domain.MaterialRemoval {
	return append([]domain.MaterialRemoval(nil), m.state.removals[targetID]...)
}

// LastRemoval 返回某母对象的最后一条去除记录，用于分配连续序号。
func (m *Mutation) LastRemoval(targetID string) (domain.MaterialRemoval, bool) {
	values := m.state.removals[targetID]
	if len(values) == 0 {
		return domain.MaterialRemoval{}, false
	}
	return values[len(values)-1], true
}

// FindRemovalRequest 在某母对象的去除历史中查找相同幂等键的记录。
func (m *Mutation) FindRemovalRequest(targetID, requestKey string) (domain.MaterialRemoval, bool) {
	if requestKey == "" {
		return domain.MaterialRemoval{}, false
	}
	for _, value := range m.state.removals[targetID] {
		if value.RequestKey == requestKey {
			return value, true
		}
	}
	return domain.MaterialRemoval{}, false
}

func (m *Mutation) GetRemoval(id string) (domain.MaterialRemoval, bool) {
	for _, values := range m.state.removals {
		for _, value := range values {
			if value.ID == id {
				return value, true
			}
		}
	}
	return domain.MaterialRemoval{}, false
}

func (m *Mutation) AppendRemoval(value domain.MaterialRemoval) {
	m.state.removals[value.ParentID] = append(m.state.removals[value.ParentID], value)
}

func (m *Mutation) PutTermination(value domain.TerminationRecord) {
	// 以对象标识为键，天然保证每个对象最多一条终止记录。
	m.state.terminations[value.TargetID] = value
}

func (m *Mutation) TerminationFor(targetID string) (domain.TerminationRecord, bool) {
	value, ok := m.state.terminations[targetID]
	return value, ok
}
