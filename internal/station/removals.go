package station

import (
	"context"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// maxLineageDepth bounds ancestor-chain traversal while defending the
// invariant that subsample parent pointers never form a cycle.
const maxLineageDepth = 10_000

// RemovalResult is the atomic outcome of one sampling registration: the
// permanent removal ledger entry plus the independent subsample identity it
// produced.
type RemovalResult struct {
	Removal   domain.MaterialRemoval `json:"removal"`
	Subsample domain.Subsample       `json:"subsample"`
}

// RecordRemoval registers one material-removal sampling operation against an
// active plan. All precondition checks and every write happen inside a single
// repository mutation, so failure leaves neither an orphan removal record nor
// an orphan subsample. The repository mutex serializes concurrent removals:
// step numbers stay gap-free and stock can never go negative.
func (s *Service) RecordRemoval(
	ctx context.Context,
	planID string,
	request contracts.RecordRemovalRequest,
) (RemovalResult, error) {
	if err := ctx.Err(); err != nil {
		return RemovalResult{}, err
	}

	var result RemovalResult
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		root, ok := m.GetSpecimen(plan.SpecimenID)
		if !ok {
			return notFoundSpecimen(plan.SpecimenID)
		}

		// Terminal guards: terminated plans and released mother specimens can
		// never accept new sampling. The plan is checked first because its
		// terminal state outlives the specimen's preparation stage.
		if !plan.AcceptsRemoval() {
			return contracts.Conflict("plan_not_accepting_removal", "方案未在进行中或已终止，不能登记材料去除")
		}
		if root.IsReleased() {
			return contracts.Conflict("specimen_released_blocks_removal", "母标本已研究放行，不能新增子样本")
		}
		if !root.CanYieldMaterial() {
			return contracts.Conflict("specimen_not_yielding_material", "母标本当前状态不能新增子样本")
		}

		parentKind, err := resolveParentKind(request.ParentKind)
		if err != nil {
			return err
		}

		// Resolve the direct sampling parent and its inventory. A specimen
		// parent must be the plan's own mother specimen; a subsample parent
		// must root at that same specimen.
		var (
			parentSpecimen *domain.Specimen
			parentSample   *domain.Subsample
			parentID       string
		)
		switch parentKind {
		case domain.ParentSpecimen:
			requested := strings.TrimSpace(request.ParentID)
			if requested != "" && requested != root.ID {
				return contracts.Conflict("parent_plan_lineage_mismatch", "父标本与方案所属母标本不一致")
			}
			specimen := root
			parentSpecimen = &specimen
			parentID = root.ID
		case domain.ParentSubsample:
			parentID = strings.TrimSpace(request.ParentID)
			if parentID == "" {
				return contracts.Invalid("required_field", "父级标识不能为空")
			}
			parent, ok := m.GetSubsample(parentID)
			if !ok {
				return notFoundSubsample(parentID)
			}
			if parent.RootSpecimenID != root.ID {
				return contracts.Conflict("parent_plan_lineage_mismatch", "父子样本与方案不属于同一母标本")
			}
			if err := assertAcyclicLineage(m, parent); err != nil {
				return err
			}
			parentSample = &parent
		}

		// Step numbers are assigned inside the serialized mutation, so they
		// are always contiguous per plan even under concurrent sampling.
		step := len(m.RemovalsForPlan(plan.ID)) + 1

		subsample, err := domain.NewSubsample(
			s.nextID("subsample"),
			request.Code,
			root,
			parentKind,
			parentID,
			plan,
			step,
			request.Label,
			domain.MaterialKind(strings.TrimSpace(request.MaterialKind)),
			request.Quantity,
			request.Notes,
			s.now(),
		)
		if err != nil {
			return err
		}
		if _, exists := m.FindSubsampleByCode(subsample.Code); exists {
			return contracts.Conflict("duplicate_subsample_code", "子样本编码已存在，重复取样不能再次登记")
		}

		// Stock guard on the direct parent, rechecked at deduction time.
		if parentSpecimen != nil && request.Quantity > parentSpecimen.StockQuantity {
			return contracts.Conflict("insufficient_stock", "母标本当前库存不足以完成本次去除")
		}
		if parentSample != nil && request.Quantity > parentSample.Remaining {
			return contracts.Conflict("insufficient_stock", "父子样本当前余量不足以完成本次去除")
		}

		removal, err := domain.NewMaterialRemoval(
			s.nextID("removal"),
			plan,
			step,
			parentKind,
			parentID,
			subsample,
			request.Quantity,
			subsample.MaterialKind,
			request.Operator,
			request.Purpose,
			s.now(),
		)
		if err != nil {
			return err
		}

		// Deduct inventory last; all values are local until every commit below
		// succeeds together.
		if parentSpecimen != nil {
			if err := parentSpecimen.ConsumeStock(request.Quantity, s.now()); err != nil {
				return err
			}
			m.PutSpecimen(*parentSpecimen)
		} else {
			if err := parentSample.ConsumeStock(request.Quantity); err != nil {
				return err
			}
			m.PutSubsample(*parentSample)
		}
		m.PutSubsample(subsample)
		m.AppendRemoval(removal)

		result = RemovalResult{Removal: removal, Subsample: subsample}
		return nil
	})
	return result, err
}

// GetSubsample reads a single subsample identity.
func (s *Service) GetSubsample(ctx context.Context, subsampleID string) (domain.Subsample, error) {
	if err := ctx.Err(); err != nil {
		return domain.Subsample{}, err
	}
	var value domain.Subsample
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetSubsample(subsampleID)
		if !ok {
			return notFoundSubsample(subsampleID)
		}
		value = found
		return nil
	})
	return value, err
}

// ListSubsamples returns the traceable subsample records rooted at one mother
// specimen, oldest sampling first.
func (s *Service) ListSubsamples(ctx context.Context, specimenID string) ([]domain.Subsample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.Subsample
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.ListSubsamplesForSpecimen(specimenID)
		return nil
	})
	return values, err
}

// ListRemovals returns the material-removal ledger rooted at one mother
// specimen across all of its plans.
func (s *Service) ListRemovals(ctx context.Context, specimenID string) ([]domain.MaterialRemoval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.MaterialRemoval
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.RemovalsForSpecimen(specimenID)
		return nil
	})
	return values, err
}

func resolveParentKind(value string) (domain.ParentKind, error) {
	kind := domain.ParentKind(strings.ToLower(strings.TrimSpace(value)))
	if kind == "" {
		return domain.ParentSpecimen, nil
	}
	if !kind.Valid() {
		return "", contracts.Invalid("invalid_parent_kind", "父级类型只能是 specimen 或 subsample")
	}
	return kind, nil
}

// assertAcyclicLineage walks subsample parent pointers up to the root
// specimen. Parent pointers are immutable, so a cycle cannot arise through
// the public API; this guard also rejects a broken (unlinked) chain, which
// would otherwise leave sampling without a provable mother specimen.
func assertAcyclicLineage(m *repository.Mutation, start domain.Subsample) error {
	visited := map[string]struct{}{start.ID: {}}
	current := start
	for depth := 0; depth < maxLineageDepth; depth++ {
		switch current.ParentKind {
		case domain.ParentSpecimen:
			if _, ok := m.GetSpecimen(current.ParentID); !ok {
				return contracts.Conflict("subsample_lineage_broken", "父子样本缺少可追溯的母标本")
			}
			return nil
		case domain.ParentSubsample:
			if _, seen := visited[current.ParentID]; seen {
				return contracts.Conflict("subsample_lineage_cycle", "父子样本关系形成循环，不能新增子样本")
			}
			parent, ok := m.GetSubsample(current.ParentID)
			if !ok {
				return contracts.Conflict("subsample_lineage_broken", "父子样本链缺少上级子样本")
			}
			visited[current.ParentID] = struct{}{}
			current = parent
		default:
			return contracts.Conflict("subsample_lineage_broken", "父子样本链包含未知父级类型")
		}
	}
	return contracts.Conflict("subsample_lineage_cycle", "父子样本链超出最大深度")
}
