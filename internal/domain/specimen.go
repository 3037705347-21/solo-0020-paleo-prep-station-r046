package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type SpecimenStatus string

const (
	SpecimenRegistered SpecimenStatus = "registered"
	SpecimenPlanned    SpecimenStatus = "planned"
	SpecimenPreparing  SpecimenStatus = "preparing"
	SpecimenStabilized SpecimenStatus = "stabilized"
	SpecimenAvailable  SpecimenStatus = "available"
	SpecimenTerminated SpecimenStatus = "terminated"
)

// 默认初始库存数量：登记时未指定数量即视为一份完整标本。
const DefaultInitialQuantity = 1

type Specimen struct {
	ID              string         `json:"id"`
	CatalogNumber   string         `json:"catalog_number"`
	FieldLabel      string         `json:"field_label"`
	TaxonHint       string         `json:"taxon_hint"`
	MatrixKind      string         `json:"matrix_kind"`
	JacketNotes     string         `json:"jacket_notes"`
	Status          SpecimenStatus `json:"status"`
	InitialQuantity int            `json:"initial_quantity"`
	RemainingUnits  int            `json:"remaining_units"`
	RemovedUnits    int            `json:"removed_units"`
	Version         int            `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func NewSpecimen(
	id string,
	catalogNumber string,
	fieldLabel string,
	taxonHint string,
	matrixKind string,
	jacketNotes string,
	initialQuantity int,
	now time.Time,
) (Specimen, error) {
	if id == "" {
		return Specimen{}, contracts.Internal("missing_generated_id", "标本标识未生成", nil)
	}
	catalog, err := validateCatalogNumber(catalogNumber)
	if err != nil {
		return Specimen{}, err
	}
	field, err := requiredText("野外标签", fieldLabel, 80)
	if err != nil {
		return Specimen{}, err
	}
	taxon, err := optionalText("分类提示", taxonHint, 120)
	if err != nil {
		return Specimen{}, err
	}
	matrix, err := requiredText("基质类型", matrixKind, 60)
	if err != nil {
		return Specimen{}, err
	}
	notes, err := optionalText("夹克备注", jacketNotes, 500)
	if err != nil {
		return Specimen{}, err
	}
	quantity := DefaultInitialQuantity
	if initialQuantity != 0 {
		quantity, err = validatePositiveInt("初始库存数量", initialQuantity)
		if err != nil {
			return Specimen{}, err
		}
	}
	return Specimen{
		ID:              id,
		CatalogNumber:   catalog,
		FieldLabel:      field,
		TaxonHint:       taxon,
		MatrixKind:      matrix,
		JacketNotes:     notes,
		Status:          SpecimenRegistered,
		InitialQuantity: quantity,
		RemainingUnits:  quantity,
		RemovedUnits:    0,
		Version:         1,
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
	}, nil
}

func (s Specimen) CanCreatePlan() bool {
	return s.Status == SpecimenRegistered || s.Status == SpecimenPlanned
}

func (s Specimen) IsStable() bool {
	return s.Status == SpecimenStabilized
}

// IsReleased 已放行（available）标本只可查阅，不能再制备或取样。
func (s Specimen) IsReleased() bool {
	return s.Status == SpecimenAvailable
}

// IsTerminated 已终止标本的库存关系永久封存，拒绝一切去除。
func (s Specimen) IsTerminated() bool {
	return s.Status == SpecimenTerminated
}

// ReserveUnits 在一次原子变更内预占指定数量的库存。
// 库存不足返回冲突，保证不会出现剩余数量为负的母标本。
func (s *Specimen) ReserveUnits(units int, now time.Time) error {
	if units <= 0 || units > maxQuantity {
		return contracts.Invalid("invalid_quantity", "去除数量必须为正整数")
	}
	if s.IsReleased() {
		return contracts.Conflict("specimen_released", "已放行标本不能再新增子样本")
	}
	if s.IsTerminated() {
		return contracts.Conflict("specimen_terminated", "已终止标本不能再新增子样本")
	}
	if units > s.RemainingUnits {
		return contracts.Conflict("insufficient_quantity", "母标本当前库存数量不足")
	}
	s.RemainingUnits -= units
	s.RemovedUnits += units
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

// MarkTerminated 将标本置于终止终态。只有非终态标本可以终止，
// 已放行标本按业务规则不能终止（放行是另一条终态链路）。
func (s *Specimen) MarkTerminated(now time.Time) error {
	if s.Status == SpecimenTerminated {
		return nil
	}
	if s.Status == SpecimenAvailable {
		return contracts.Conflict("specimen_released", "已放行标本不能终止")
	}
	s.Status = SpecimenTerminated
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkPlanned(now time.Time) error {
	if s.Status == SpecimenPlanned {
		return nil
	}
	if s.Status != SpecimenRegistered {
		return contracts.Conflict("specimen_not_plannable", "当前标本状态不能创建修复方案")
	}
	s.Status = SpecimenPlanned
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkPreparing(now time.Time) error {
	if s.Status == SpecimenPreparing {
		return nil
	}
	if s.Status != SpecimenPlanned {
		return contracts.Conflict("specimen_not_preparing", "只有已规划标本才能进入制备状态")
	}
	s.Status = SpecimenPreparing
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkStabilized(now time.Time) error {
	if s.Status == SpecimenStabilized {
		return nil
	}
	if s.Status != SpecimenPreparing {
		return contracts.Conflict("specimen_not_stabilizable", "只有正在制备的标本才能进入稳定状态")
	}
	s.Status = SpecimenStabilized
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkAvailable(now time.Time) error {
	if s.Status != SpecimenStabilized {
		return contracts.Conflict("specimen_not_releasable", "只有已稳定标本可以办理放行")
	}
	s.Status = SpecimenAvailable
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}
