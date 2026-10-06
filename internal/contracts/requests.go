package contracts

type RegisterSpecimenRequest struct {
	CatalogNumber   string `json:"catalog_number"`
	FieldLabel      string `json:"field_label"`
	TaxonHint       string `json:"taxon_hint"`
	MatrixKind      string `json:"matrix_kind"`
	JacketNotes     string `json:"jacket_notes"`
	InitialQuantity int    `json:"initial_quantity"`
}

type CreatePlanRequest struct {
	Objective      string `json:"objective"`
	Method         string `json:"method"`
	Adhesive       string `json:"adhesive"`
	TargetExposure int    `json:"target_exposure"`
}

type ApprovalRequest struct {
	ApprovedBy string `json:"approved_by"`
}

type ProgressRequest struct {
	Stage      string `json:"stage"`
	Percent    int    `json:"percent"`
	Note       string `json:"note"`
	RecordedBy string `json:"recorded_by"`
}

type ReleaseRequest struct {
	Purpose    string `json:"purpose"`
	Recipient  string `json:"recipient"`
	ApprovedBy string `json:"approved_by"`
}

// SubsampleRequest 在一次原子操作内完成“材料去除 + 子样本登记”。
// 母标本可以是一级标本，也可以是已有子样本，由 station 层统一解析。
type SubsampleRequest struct {
	// Code 为子样本业务编号，在全部子样本中唯一。
	Code string `json:"code"`
	// PlanID 与 ProgressID 可选，提供时用于溯源到具体方案与操作步骤。
	PlanID     string `json:"plan_id"`
	ProgressID string `json:"progress_id"`
	// MaterialType 描述剥离材料，如“骨片”“围岩基质”。
	MaterialType string `json:"material_type"`
	Quantity     int    `json:"quantity"`
	Unit         string `json:"unit"`
	Reason       string `json:"reason"`
	Method       string `json:"method"`
	Note         string `json:"note"`
	RecordedBy   string `json:"recorded_by"`
	// RequestKey 为客户端幂等键；重复提交返回首次结果而不是第二次去除。
	RequestKey string `json:"request_key"`
}

// MaterialRemovalRequest 登记一次不形成子样本的材料去除（消耗或废弃）。
type MaterialRemovalRequest struct {
	PlanID       string `json:"plan_id"`
	ProgressID   string `json:"progress_id"`
	Kind         string `json:"kind"`
	MaterialType string `json:"material_type"`
	Quantity     int    `json:"quantity"`
	Unit         string `json:"unit"`
	Reason       string `json:"reason"`
	Method       string `json:"method"`
	RecordedBy   string `json:"recorded_by"`
	RequestKey   string `json:"request_key"`
}

// TerminateRequest 终止一份标本或子样本的库存生命周期。
type TerminateRequest struct {
	Reason     string `json:"reason"`
	RecordedBy string `json:"recorded_by"`
}
