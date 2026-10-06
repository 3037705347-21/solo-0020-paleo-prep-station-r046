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

// RecordRemovalRequest registers one material-removal sampling operation.
// The parent is a specimen by default (parent_kind omitted) or an existing
// subsample when parent_kind is "subsample".
type RecordRemovalRequest struct {
	ParentKind   string `json:"parent_kind"`
	ParentID     string `json:"parent_id"`
	Code         string `json:"code"`
	Label        string `json:"label"`
	MaterialKind string `json:"material_kind"`
	Quantity     int    `json:"quantity"`
	Notes        string `json:"notes"`
	Operator     string `json:"operator"`
	Purpose      string `json:"purpose"`
}
