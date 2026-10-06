package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// Subsample 是从母标本上剥离材料后形成的独立登记对象。
// 它保存与母标本、修复方案和具体操作步骤的溯源三元组，
// 自身也携带库存数量，因此可以继续作为下一级子样本的母标本。
type Subsample struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	ParentID        string    `json:"parent_id"`
	RootSpecimenID  string    `json:"root_specimen_id"`
	PlanID          string    `json:"plan_id,omitempty"`
	ProgressID      string    `json:"progress_id,omitempty"`
	RemovalID       string    `json:"removal_id"`
	MaterialType    string    `json:"material_type"`
	Quantity        int       `json:"quantity"`
	Unit            string    `json:"unit"`
	Note            string    `json:"note"`
	Depth           int       `json:"depth"`
	InitialQuantity int       `json:"initial_quantity"`
	RemainingUnits  int       `json:"remaining_units"`
	RemovedUnits    int       `json:"removed_units"`
	Terminated      bool      `json:"terminated"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// NewSubsampleInput 聚合子样本构造参数。
type NewSubsampleInput struct {
	ID string
	// Code 为子样本业务编号，由 station 层保证全局唯一。
	Code string
	// ParentID 指向直接母对象（一级标本或另一份子样本）。
	ParentID string
	// AncestorIDs 包含母对象本身及其全部祖先（根标本在前）。
	AncestorIDs  []string
	Plan         *PrepPlan
	Progress     *ProgressEntry
	Removal      MaterialRemoval
	MaterialType string
	Quantity     int
	Unit         string
	Note         string
	Depth        int
	CreatedBy    string
	Now          time.Time
}

// NewSubsample 校验子样本字段并装配溯源链路。
// 父子循环由 station 层基于仓储中的真实链路检测；
// 此处再次防御 AncestorIDs 内部出现重复标识。
func NewSubsample(input NewSubsampleInput) (Subsample, error) {
	if input.ID == "" {
		return Subsample{}, contracts.Internal("missing_generated_id", "子样本标识未生成", nil)
	}
	code, err := validateSubsampleCode(input.Code)
	if err != nil {
		return Subsample{}, err
	}
	if input.ParentID == "" {
		return Subsample{}, contracts.NotFound("specimen_not_found", "未找到母标本")
	}
	if input.Removal.ID == "" || input.Removal.ParentID != input.ParentID {
		return Subsample{}, contracts.Internal("invalid_removal_link", "子样本必须关联母标本的去除记录", nil)
	}
	if input.Removal.Kind != RemovalSubsample {
		return Subsample{}, contracts.Internal("invalid_removal_link", "去除记录不是待建子样本类型", nil)
	}
	if input.Removal.SubsampleID != "" && input.Removal.SubsampleID != input.ID {
		return Subsample{}, contracts.Internal("invalid_removal_link", "去除记录已关联其他子样本", nil)
	}
	quantity, err := validatePositiveInt("子样本数量", input.Quantity)
	if err != nil {
		return Subsample{}, err
	}
	if quantity != input.Removal.Quantity {
		return Subsample{}, contracts.Conflict("quantity_mismatch", "子样本数量必须与去除数量一致")
	}
	material, err := requiredText("材料类型", input.MaterialType, 80)
	if err != nil {
		return Subsample{}, err
	}
	unit, err := requiredText("计量单位", input.Unit, 20)
	if err != nil {
		return Subsample{}, err
	}
	noteValue, err := optionalText("子样本备注", input.Note, 500)
	if err != nil {
		return Subsample{}, err
	}
	creator, err := requiredText("登记人", input.CreatedBy, 80)
	if err != nil {
		return Subsample{}, err
	}
	if input.Depth < 1 {
		return Subsample{}, contracts.Internal("invalid_lineage_depth", "子样本溯源深度不合法", nil)
	}
	if input.Plan != nil {
		if input.Plan.SpecimenID != input.ParentID {
			return Subsample{}, contracts.Conflict("plan_specimen_mismatch", "修复方案不属于该母标本")
		}
		if input.Progress != nil && input.Progress.PlanID != input.Plan.ID {
			return Subsample{}, contracts.Conflict("progress_plan_mismatch", "操作步骤不属于该修复方案")
		}
	} else if input.Progress != nil {
		return Subsample{}, contracts.Invalid("progress_without_plan", "指定操作步骤时必须同时指定修复方案")
	}
	seen := make(map[string]struct{}, len(input.AncestorIDs))
	for _, ancestorID := range input.AncestorIDs {
		if ancestorID == "" {
			return Subsample{}, contracts.Internal("invalid_lineage", "溯源链路存在空标识", nil)
		}
		if _, duplicate := seen[ancestorID]; duplicate {
			return Subsample{}, contracts.Conflict("subsample_cycle", "父子链路出现循环，不能登记子样本")
		}
		seen[ancestorID] = struct{}{}
	}
	rootID := input.ParentID
	if len(input.AncestorIDs) > 0 {
		rootID = input.AncestorIDs[0]
	}
	return Subsample{
		ID:              input.ID,
		Code:            code,
		ParentID:        input.ParentID,
		RootSpecimenID:  rootID,
		PlanID:          planID(input.Plan),
		ProgressID:      progressID(input.Progress),
		RemovalID:       input.Removal.ID,
		MaterialType:    material,
		Quantity:        quantity,
		Unit:            unit,
		Note:            noteValue,
		Depth:           input.Depth,
		InitialQuantity: quantity,
		RemainingUnits:  quantity,
		RemovedUnits:    0,
		Terminated:      false,
		CreatedBy:       creator,
		CreatedAt:       input.Now.UTC(),
		UpdatedAt:       input.Now.UTC(),
	}, nil
}

// MarkTerminated 封存一份子样本；终止后不能继续从其上取样。
func (s *Subsample) MarkTerminated(now time.Time) {
	if s.Terminated {
		return
	}
	s.Terminated = true
	s.UpdatedAt = now.UTC()
}

// ReserveUnits 允许子样本作为母标本继续向下取样，规则与标本一致。
func (s *Subsample) ReserveUnits(units int, now time.Time) error {
	if s.Terminated {
		return contracts.Conflict("subsample_terminated", "已终止子样本不能再新增取样")
	}
	if units <= 0 || units > maxQuantity {
		return contracts.Invalid("invalid_quantity", "去除数量必须为正整数")
	}
	if units > s.RemainingUnits {
		return contracts.Conflict("insufficient_quantity", "母样本当前库存数量不足")
	}
	s.RemainingUnits -= units
	s.RemovedUnits += units
	s.UpdatedAt = now.UTC()
	return nil
}
