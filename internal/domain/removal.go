package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// MaterialRemoval is the immutable ledger entry created for one sampling
// operation. It records which plan and plan-scoped step detached how much
// material from which parent, and points at the subsample identity the
// removed material received. The two records are committed together, so a
// removal can never exist without its subsample (or vice versa).
type MaterialRemoval struct {
	ID           string       `json:"id"`
	PlanID       string       `json:"plan_id"`
	Step         int          `json:"step"`
	ParentKind   ParentKind   `json:"parent_kind"`
	ParentID     string       `json:"parent_id"`
	SubsampleID  string       `json:"subsample_id"`
	Quantity     int          `json:"quantity"`
	MaterialKind MaterialKind `json:"material_kind"`
	Operator     string       `json:"operator"`
	Purpose      string       `json:"purpose"`
	CreatedAt    time.Time    `json:"created_at"`
}

// NewMaterialRemoval validates ledger fields and reasserts the terminal
// plan guard: only an active plan may record material removal, so a plan
// terminated concurrently inside the same transaction is still rejected.
func NewMaterialRemoval(
	id string,
	plan PrepPlan,
	step int,
	parentKind ParentKind,
	parentID string,
	subsample Subsample,
	quantity int,
	materialKind MaterialKind,
	operator string,
	purpose string,
	now time.Time,
) (MaterialRemoval, error) {
	if id == "" {
		return MaterialRemoval{}, contracts.Internal("missing_generated_id", "去除记录标识未生成", nil)
	}
	if !plan.AcceptsRemoval() {
		return MaterialRemoval{}, contracts.Conflict("plan_not_accepting_removal", "只有进行中的修复方案可以登记材料去除")
	}
	if !parentKind.Valid() {
		return MaterialRemoval{}, contracts.Invalid("invalid_parent_kind", "父级类型只能是 specimen 或 subsample")
	}
	if parentID == "" {
		return MaterialRemoval{}, contracts.Invalid("required_field", "父级标识不能为空")
	}
	if step <= 0 {
		return MaterialRemoval{}, contracts.Invalid("invalid_step", "去除步骤序号必须从一开始连续递增")
	}
	if err := validateRemovalQuantity(quantity); err != nil {
		return MaterialRemoval{}, err
	}
	if !materialKind.Valid() {
		return MaterialRemoval{}, contracts.Invalid("invalid_material_kind", "材料类型必须是 matrix、fragment 或 sample")
	}
	operatorValue, err := requiredText("操作人", operator, 80)
	if err != nil {
		return MaterialRemoval{}, err
	}
	purposeValue, err := requiredText("去除用途", purpose, 200)
	if err != nil {
		return MaterialRemoval{}, err
	}
	return MaterialRemoval{
		ID:           id,
		PlanID:       plan.ID,
		Step:         step,
		ParentKind:   parentKind,
		ParentID:     parentID,
		SubsampleID:  subsample.ID,
		Quantity:     quantity,
		MaterialKind: materialKind,
		Operator:     operatorValue,
		Purpose:      purposeValue,
		CreatedAt:    now.UTC(),
	}, nil
}
