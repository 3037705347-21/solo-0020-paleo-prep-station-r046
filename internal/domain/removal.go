package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// RemovalKind 描述从母标本上剥离材料的去向类型。
type RemovalKind string

const (
	// RemovalSubsample 剥离的材料形成一份具有独立身份的子样本。
	RemovalSubsample RemovalKind = "subsample"
	// RemovalConsumed 剥离的材料在检测/制片中被消耗，不形成可登记对象。
	RemovalConsumed RemovalKind = "consumed"
	// RemovalDiscarded 剥离的基质或碎片被废弃，不形成可登记对象。
	RemovalDiscarded RemovalKind = "discarded"
)

func validRemovalKind(kind RemovalKind) bool {
	switch kind {
	case RemovalSubsample, RemovalConsumed, RemovalDiscarded:
		return true
	default:
		return false
	}
}

// MaterialRemoval 记录一次从母标本上的材料去除。
// 它同时承载取样溯源三元组（母标本、方案、操作步骤）与库存扣减结果，
// 使得“去除了什么、为何去除、去除后剩多少”可以独立追溯。
type MaterialRemoval struct {
	ID             string      `json:"id"`
	ParentID       string      `json:"parent_id"`
	PlanID         string      `json:"plan_id,omitempty"`
	ProgressID     string      `json:"progress_id,omitempty"`
	Sequence       int         `json:"sequence"`
	Kind           RemovalKind `json:"kind"`
	MaterialType   string      `json:"material_type"`
	Quantity       int         `json:"quantity"`
	Unit           string      `json:"unit"`
	RemainingAfter int         `json:"remaining_after"`
	Reason         string      `json:"reason"`
	Method         string      `json:"method"`
	RecordedBy     string      `json:"recorded_by"`
	// SubsampleID 仅在 Kind == subsample 且子样本成功建立时非空。
	SubsampleID string `json:"subsample_id,omitempty"`
	// RequestKey 保存客户端幂等键，用于把重复取样折叠为同一结果。
	RequestKey string    `json:"request_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewMaterialRemovalInput 聚合构造一次去除记录所需的全部上下文。
type NewMaterialRemovalInput struct {
	ID string
	// ParentID 指向母标本或母样本；InitialQuantity 是其初始库存，
	// 用于校验 RemainingAfter 快照的上界。
	ParentID        string
	InitialQuantity int
	Plan            *PrepPlan
	Progress        *ProgressEntry
	Sequence        int
	Kind            RemovalKind
	MaterialType    string
	Quantity        int
	Unit            string
	RemainingAfter  int
	Reason          string
	Method          string
	RecordedBy      string
	RequestKey      string
	SubsampleID     string
	Now             time.Time
}

// NewMaterialRemoval 只负责字段校验与记录装配；
// 母标本状态门控、方案/步骤归属、库存预占和子样本身份唯一性
// 由 station 层在同一仓储事务内先行保证。
func NewMaterialRemoval(input NewMaterialRemovalInput) (MaterialRemoval, error) {
	if input.ID == "" {
		return MaterialRemoval{}, contracts.Internal("missing_generated_id", "去除记录标识未生成", nil)
	}
	if !validRemovalKind(input.Kind) {
		return MaterialRemoval{}, contracts.Invalid("invalid_removal_kind", "去除类型必须是 subsample、consumed 或 discarded")
	}
	if input.ParentID == "" {
		return MaterialRemoval{}, contracts.NotFound("specimen_not_found", "未找到母标本")
	}
	if input.Kind == RemovalSubsample && input.SubsampleID == "" {
		return MaterialRemoval{}, contracts.Internal("missing_subsample_link", "子样本去除必须关联子样本标识", nil)
	}
	quantity, err := validatePositiveInt("去除数量", input.Quantity)
	if err != nil {
		return MaterialRemoval{}, err
	}
	if input.RemainingAfter < 0 || input.RemainingAfter > input.InitialQuantity {
		return MaterialRemoval{}, contracts.Internal("invalid_remaining_snapshot", "去除后库存快照不合法", nil)
	}
	material, err := requiredText("材料类型", input.MaterialType, 80)
	if err != nil {
		return MaterialRemoval{}, err
	}
	unit, err := requiredText("计量单位", input.Unit, 20)
	if err != nil {
		return MaterialRemoval{}, err
	}
	reasonValue, err := requiredText("取样说明", input.Reason, 300)
	if err != nil {
		return MaterialRemoval{}, err
	}
	methodValue, err := optionalText("取样方法", input.Method, 160)
	if err != nil {
		return MaterialRemoval{}, err
	}
	recorder, err := requiredText("记录人", input.RecordedBy, 80)
	if err != nil {
		return MaterialRemoval{}, err
	}
	key, err := ValidateIdempotencyKey(input.RequestKey)
	if err != nil {
		return MaterialRemoval{}, err
	}
	if input.Sequence <= 0 {
		return MaterialRemoval{}, contracts.Internal("invalid_removal_sequence", "去除序号必须为正数", nil)
	}
	if input.Plan != nil {
		if input.Plan.SpecimenID != input.ParentID {
			return MaterialRemoval{}, contracts.Conflict(
				"plan_specimen_mismatch",
				"修复方案不属于该母标本，不能据此登记去除",
			)
		}
		if input.Progress != nil && input.Progress.PlanID != input.Plan.ID {
			return MaterialRemoval{}, contracts.Conflict(
				"progress_plan_mismatch",
				"操作步骤不属于该修复方案，不能据此登记去除",
			)
		}
	} else if input.Progress != nil {
		return MaterialRemoval{}, contracts.Invalid("progress_without_plan", "指定操作步骤时必须同时指定修复方案")
	}
	return MaterialRemoval{
		ID:             input.ID,
		ParentID:       input.ParentID,
		PlanID:         planID(input.Plan),
		ProgressID:     progressID(input.Progress),
		Sequence:       input.Sequence,
		Kind:           input.Kind,
		MaterialType:   material,
		Quantity:       quantity,
		Unit:           unit,
		RemainingAfter: input.RemainingAfter,
		Reason:         reasonValue,
		Method:         methodValue,
		RecordedBy:     recorder,
		SubsampleID:    input.SubsampleID,
		RequestKey:     key,
		CreatedAt:      input.Now.UTC(),
	}, nil
}

func planID(plan *PrepPlan) string {
	if plan == nil {
		return ""
	}
	return plan.ID
}

func progressID(progress *ProgressEntry) string {
	if progress == nil {
		return ""
	}
	return progress.ID
}
