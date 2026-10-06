package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// ParentKind identifies which aggregate a subsample was detached from.
type ParentKind string

const (
	ParentSpecimen  ParentKind = "specimen"
	ParentSubsample ParentKind = "subsample"
)

func (k ParentKind) Valid() bool {
	return k == ParentSpecimen || k == ParentSubsample
}

// MaterialKind classifies the detached material so downstream research
// registrations can distinguish fossil fragments from preparation waste.
type MaterialKind string

const (
	MaterialMatrix   MaterialKind = "matrix"
	MaterialFragment MaterialKind = "fragment"
	MaterialSample   MaterialKind = "sample"
)

func (k MaterialKind) Valid() bool {
	return k == MaterialMatrix || k == MaterialFragment || k == MaterialSample
}

// Subsample is the independent identity of material detached during one
// sampling operation. It permanently records the mother specimen lineage,
// the governing plan and the plan-scoped removal step, while carrying its
// own remaining inventory so it can later act as a sampling parent itself.
type Subsample struct {
	ID             string       `json:"id"`
	Code           string       `json:"code"`
	RootSpecimenID string       `json:"root_specimen_id"`
	ParentKind     ParentKind   `json:"parent_kind"`
	ParentID       string       `json:"parent_id"`
	PlanID         string       `json:"plan_id"`
	RemovalStep    int          `json:"removal_step"`
	Label          string       `json:"label"`
	MaterialKind   MaterialKind `json:"material_kind"`
	Quantity       int          `json:"quantity"`
	Remaining      int          `json:"remaining"`
	Notes          string       `json:"notes"`
	Version        int          `json:"version"`
	CreatedAt      time.Time    `json:"created_at"`
}

// NewSubsample assures identity, lineage and inventory fields. Business
// preconditions (plan state, mother inventory and duplicate codes) are
// enforced by the station orchestration before construction.
func NewSubsample(
	id string,
	code string,
	rootSpecimen Specimen,
	parentKind ParentKind,
	parentID string,
	plan PrepPlan,
	step int,
	label string,
	materialKind MaterialKind,
	quantity int,
	notes string,
	now time.Time,
) (Subsample, error) {
	if id == "" {
		return Subsample{}, contracts.Internal("missing_generated_id", "子样本标识未生成", nil)
	}
	normalizedCode, err := validateSubsampleCode(code)
	if err != nil {
		return Subsample{}, err
	}
	if !parentKind.Valid() {
		return Subsample{}, contracts.Invalid("invalid_parent_kind", "父级类型只能是 specimen 或 subsample")
	}
	if parentID == "" {
		return Subsample{}, contracts.Invalid("required_field", "父级标识不能为空")
	}
	if !materialKind.Valid() {
		return Subsample{}, contracts.Invalid("invalid_material_kind", "材料类型必须是 matrix、fragment 或 sample")
	}
	if err := validateRemovalQuantity(quantity); err != nil {
		return Subsample{}, err
	}
	if step <= 0 {
		return Subsample{}, contracts.Invalid("invalid_step", "去除步骤序号必须从一开始连续递增")
	}
	labelValue, err := requiredText("子样本名称", label, 120)
	if err != nil {
		return Subsample{}, err
	}
	notesValue, err := optionalText("子样本备注", notes, 500)
	if err != nil {
		return Subsample{}, err
	}
	return Subsample{
		ID:             id,
		Code:           normalizedCode,
		RootSpecimenID: rootSpecimen.ID,
		ParentKind:     parentKind,
		ParentID:       parentID,
		PlanID:         plan.ID,
		RemovalStep:    step,
		Label:          labelValue,
		MaterialKind:   materialKind,
		Quantity:       quantity,
		Remaining:      quantity,
		Notes:          notesValue,
		Version:        1,
		CreatedAt:      now.UTC(),
	}, nil
}

// HasStock reports whether further material can be detached from this
// subsample.
func (s Subsample) HasStock() bool {
	return s.Remaining > 0
}

// ConsumeStock deducts units from a subsample's remaining inventory. The
// original captured Quantity is preserved so the full removal ledger stays
// reconstructible.
func (s *Subsample) ConsumeStock(quantity int) error {
	if quantity <= 0 {
		return contracts.Invalid("invalid_quantity", "去除数量必须大于零")
	}
	if quantity > s.Remaining {
		return contracts.Conflict("insufficient_stock", "子样本当前余量不足以完成本次去除")
	}
	s.Remaining -= quantity
	s.Version++
	return nil
}
