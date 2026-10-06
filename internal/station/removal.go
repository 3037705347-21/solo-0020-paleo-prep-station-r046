package station

import (
	"context"
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// RemovalResult 把一次去除登记产生的全部记录作为一个快照返回，
// 保证调用方读到的去除记录、母对象库存和（可选的）子样本来自同一提交。
type RemovalResult struct {
	Removal   domain.MaterialRemoval `json:"removal"`
	Subsample *domain.Subsample      `json:"subsample,omitempty"`
}

// resolveRemovalContext 在事务内解析一次去除所引用的方案与操作步骤。
// planID/progressID 均可空；一旦提供就必须存在、归属正确且方案未处于终态。
func resolveRemovalContext(
	m *repository.Mutation,
	parent materialNode,
	planID string,
	progressID string,
) (*domain.PrepPlan, *domain.ProgressEntry, error) {
	planID = strings.TrimSpace(planID)
	progressID = strings.TrimSpace(progressID)
	if progressID != "" && planID == "" {
		return nil, nil, contracts.Invalid("progress_without_plan", "指定操作步骤时必须同时指定修复方案")
	}
	if planID == "" {
		return nil, nil, nil
	}
	plan, ok := m.GetPlan(planID)
	if !ok {
		return nil, nil, notFoundPlan(planID)
	}
	if plan.SpecimenID != parent.id {
		return nil, nil, contracts.Conflict("plan_specimen_mismatch", "修复方案不属于该母标本，不能据此登记去除")
	}
	// 已完成或已取消的方案不再接受新的材料去除；制备仍在进行时才允许登记。
	if plan.Status == domain.PlanCompleted || plan.Status == domain.PlanCancelled {
		return nil, nil, contracts.Conflict("plan_closed", "方案已结束，不能再登记材料去除")
	}
	if progressID == "" {
		return &plan, nil, nil
	}
	progress, ok := m.GetProgress(planID, progressID)
	if !ok {
		return nil, nil, contracts.NotFound("progress_not_found", "未找到操作步骤 "+progressID)
	}
	return &plan, &progress, nil
}

func (s *Service) CreateSubsample(
	ctx context.Context,
	parentID string,
	request contracts.SubsampleRequest,
) (RemovalResult, error) {
	if err := ctx.Err(); err != nil {
		return RemovalResult{}, err
	}

	var result RemovalResult
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		parent, err := resolveMaterialNode(m, parentID)
		if err != nil {
			return err
		}
		key, err := domain.ValidateIdempotencyKey(request.RequestKey)
		if err != nil {
			return err
		}
		// 幂等重放优先于其它校验：重复取样必须折叠为首次的确定结果，
		// 不能再次扣减库存，也不能因为母对象状态变化而报冲突。
		if previous, ok := m.FindRemovalRequest(parentID, key); ok {
			return replayRemoval(m, previous, &result)
		}
		if parent.sealed() {
			return sealedConflict(parent)
		}
		if _, exists := m.FindSubsampleByCode(normalizeCode(request.Code)); exists {
			return contracts.Conflict("duplicate_subsample_code", "子样本编号已存在，不能覆盖原有记录")
		}
		plan, progress, err := resolveRemovalContext(m, parent, request.PlanID, request.ProgressID)
		if err != nil {
			return err
		}
		line, err := resolveLineage(m, parent)
		if err != nil {
			return err
		}

		now := s.now()
		removalID := s.nextID("removal")
		subsampleID := s.nextID("subsample")

		// 先在母对象副本上预占库存；数量不足时直接失败，什么都不写入。
		if err := reserveNode(&parent, request.Quantity, now); err != nil {
			return err
		}
		removal, err := domain.NewMaterialRemoval(domain.NewMaterialRemovalInput{
			ID:              removalID,
			ParentID:        parent.id,
			InitialQuantity: parent.initial,
			Plan:            plan,
			Progress:        progress,
			Sequence:        nextRemovalSequence(m, parent.id),
			Kind:            domain.RemovalSubsample,
			MaterialType:    request.MaterialType,
			Quantity:        request.Quantity,
			Unit:            request.Unit,
			Reason:          request.Reason,
			Method:          request.Method,
			RecordedBy:      request.RecordedBy,
			RequestKey:      key,
			SubsampleID:     subsampleID,
			RemainingAfter:  parent.remaining,
			Now:             now,
		})
		if err != nil {
			return err
		}
		subsample, err := domain.NewSubsample(domain.NewSubsampleInput{
			ID:           subsampleID,
			Code:         request.Code,
			ParentID:     parent.id,
			AncestorIDs:  line.chain,
			Plan:         plan,
			Progress:     progress,
			Removal:      removal,
			MaterialType: request.MaterialType,
			Quantity:     request.Quantity,
			Unit:         request.Unit,
			Note:         request.Note,
			// line.depth 是从根到母对象（含）的节点数，恰为子样本所处的代数深度。
			Depth:     line.depth,
			CreatedBy: request.RecordedBy,
			Now:       now,
		})
		if err != nil {
			return err
		}

		// 去除记录、子样本身份和母对象库存三者在同一提交内生效，
		// 任一步失败都由仓储整体丢弃，不会留下孤立去除或孤立子样本。
		m.AppendRemoval(removal)
		m.PutSubsample(subsample)
		parent.commit(m)
		result = RemovalResult{Removal: removal, Subsample: &subsample}
		return nil
	})
	return result, err
}

func (s *Service) RecordMaterialRemoval(
	ctx context.Context,
	parentID string,
	request contracts.MaterialRemovalRequest,
) (domain.MaterialRemoval, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialRemoval{}, err
	}

	var created domain.MaterialRemoval
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		parent, err := resolveMaterialNode(m, parentID)
		if err != nil {
			return err
		}
		key, err := domain.ValidateIdempotencyKey(request.RequestKey)
		if err != nil {
			return err
		}
		if previous, ok := m.FindRemovalRequest(parentID, key); ok {
			if previous.Kind != domain.RemovalKind(strings.TrimSpace(request.Kind)) {
				return contracts.Conflict("idempotency_key_reused", "同一幂等键已用于不同类型的去除请求")
			}
			created = previous
			return nil
		}
		if parent.sealed() {
			return sealedConflict(parent)
		}
		kind := domain.RemovalKind(strings.TrimSpace(request.Kind))
		if kind == domain.RemovalSubsample {
			return contracts.Invalid("invalid_removal_kind", "子样本登记必须使用子样本专用接口")
		}
		if kind != domain.RemovalConsumed && kind != domain.RemovalDiscarded {
			return contracts.Invalid("invalid_removal_kind", "去除类型必须是 consumed 或 discarded")
		}
		plan, progress, err := resolveRemovalContext(m, parent, request.PlanID, request.ProgressID)
		if err != nil {
			return err
		}

		now := s.now()
		if err := reserveNode(&parent, request.Quantity, now); err != nil {
			return err
		}
		removal, err := domain.NewMaterialRemoval(domain.NewMaterialRemovalInput{
			ID:              s.nextID("removal"),
			ParentID:        parent.id,
			InitialQuantity: parent.initial,
			Plan:            plan,
			Progress:        progress,
			Sequence:        nextRemovalSequence(m, parent.id),
			Kind:            kind,
			MaterialType:    request.MaterialType,
			Quantity:        request.Quantity,
			Unit:            request.Unit,
			Reason:          request.Reason,
			Method:          request.Method,
			RecordedBy:      request.RecordedBy,
			RequestKey:      key,
			RemainingAfter:  parent.remaining,
			Now:             now,
		})
		if err != nil {
			return err
		}
		m.AppendRemoval(removal)
		parent.commit(m)
		created = removal
		return nil
	})
	return created, err
}

// TerminationResult 区分新建终止与幂等重放，便于传输层选择状态码。
type TerminationResult struct {
	Record  domain.TerminationRecord `json:"record"`
	Created bool                     `json:"created"`
}

// TerminateMaterial 终止一份标本或子样本。终止记录与对象状态在同一提交内写入，
// 重复终止返回同一条既有记录（幂等），已放行标本拒绝终止。
func (s *Service) TerminateMaterial(
	ctx context.Context,
	targetID string,
	request contracts.TerminateRequest,
) (TerminationResult, error) {
	if err := ctx.Err(); err != nil {
		return TerminationResult{}, err
	}

	var result TerminationResult
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		node, err := resolveMaterialNode(m, targetID)
		if err != nil {
			return err
		}
		if existing, ok := m.TerminationFor(targetID); ok {
			result = TerminationResult{Record: existing, Created: false}
			return nil
		}
		if node.released {
			return contracts.Conflict("specimen_released", "已放行标本不能终止")
		}
		now := s.now()
		record, err := domain.NewTerminationRecord(
			s.nextID("termination"),
			node.subject,
			targetID,
			request.Reason,
			request.RecordedBy,
			now,
		)
		if err != nil {
			return err
		}
		if node.specimen != nil {
			if err := node.specimen.MarkTerminated(now); err != nil {
				return err
			}
		} else {
			node.subsample.MarkTerminated(now)
		}
		m.PutTermination(record)
		node.commit(m)
		result = TerminationResult{Record: record, Created: true}
		return nil
	})
	return result, err
}

func (s *Service) GetSubsample(ctx context.Context, subsampleID string) (domain.Subsample, error) {
	if err := ctx.Err(); err != nil {
		return domain.Subsample{}, err
	}
	var value domain.Subsample
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetSubsample(subsampleID)
		if !ok {
			return contracts.NotFound("subsample_not_found", "未找到子样本 "+subsampleID)
		}
		value = found
		return nil
	})
	return value, err
}

func (s *Service) ListSubsamplesForParent(
	ctx context.Context,
	parentID string,
) ([]domain.Subsample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.Subsample
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, err := resolveMaterialNode(m, parentID); err != nil {
			return err
		}
		values = m.ListSubsamplesForParent(parentID)
		return nil
	})
	return values, err
}

func (s *Service) ListRemovals(
	ctx context.Context,
	targetID string,
) ([]domain.MaterialRemoval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.MaterialRemoval
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, err := resolveMaterialNode(m, targetID); err != nil {
			return err
		}
		values = m.RemovalsForTarget(targetID)
		return nil
	})
	return values, err
}

func (s *Service) GetRemoval(ctx context.Context, removalID string) (domain.MaterialRemoval, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialRemoval{}, err
	}
	var value domain.MaterialRemoval
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetRemoval(removalID)
		if !ok {
			return contracts.NotFound("removal_not_found", "未找到去除记录 "+removalID)
		}
		value = found
		return nil
	})
	return value, err
}

// replayRemoval 把幂等命中的历史去除重放为本次响应。
// subsample 类型还必须能读回当初创建的子样本，否则视为存储不一致。
func replayRemoval(
	m *repository.Mutation,
	previous domain.MaterialRemoval,
	result *RemovalResult,
) error {
	if previous.Kind != domain.RemovalSubsample {
		return contracts.Conflict("idempotency_key_reused", "同一幂等键已用于非子样本去除，请使用去除查询接口")
	}
	subsample, ok := m.GetSubsample(previous.SubsampleID)
	if !ok {
		return contracts.Internal("orphaned_removal", "历史去除记录缺少对应子样本", nil)
	}
	*result = RemovalResult{Removal: previous, Subsample: &subsample}
	return nil
}

// reserveNode 在母对象副本上扣减库存，并保持 materialNode.remaining 同步，
// 供随后构造 RemainingAfter 快照。
func reserveNode(node *materialNode, quantity int, now time.Time) error {
	var err error
	if node.specimen != nil {
		err = node.specimen.ReserveUnits(quantity, now)
	} else {
		err = node.subsample.ReserveUnits(quantity, now)
	}
	if err != nil {
		return err
	}
	node.remaining -= quantity
	return nil
}

func nextRemovalSequence(m *repository.Mutation, parentID string) int {
	if last, ok := m.LastRemoval(parentID); ok {
		return last.Sequence + 1
	}
	return 1
}

func sealedConflict(node materialNode) error {
	if node.released {
		return contracts.Conflict("specimen_released", "母对象已放行，不能再新增去除")
	}
	return contracts.Conflict("material_terminated", "母对象已终止，不能再新增去除")
}

func normalizeCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
