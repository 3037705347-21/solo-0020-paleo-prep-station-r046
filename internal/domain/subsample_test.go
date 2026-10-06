package domain

import (
	"testing"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

func validSpecimen(t *testing.T) Specimen {
	t.Helper()
	specimen, err := NewSpecimen("spec-1", "DOM-01", "野外-01", "兽脚类", "泥岩", "", 5, time.Now())
	if err != nil {
		t.Fatalf("new specimen: %v", err)
	}
	return specimen
}

func removalFor(t *testing.T, specimen Specimen, id, subsampleID string, qty, remaining int) MaterialRemoval {
	t.Helper()
	record, err := NewMaterialRemoval(NewMaterialRemovalInput{
		ID:              id,
		ParentID:        specimen.ID,
		InitialQuantity: specimen.InitialQuantity,
		Sequence:        1,
		Kind:            RemovalSubsample,
		MaterialType:    "骨片",
		Quantity:        qty,
		Unit:            "块",
		RemainingAfter:  remaining,
		Reason:          "切片",
		RecordedBy:      "prep-1",
		SubsampleID:     subsampleID,
		Now:             time.Now(),
	})
	if err != nil {
		t.Fatalf("new removal: %v", err)
	}
	return record
}

func expectDomainCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", want)
	}
	appErr, ok := contracts.AsError(err)
	if !ok {
		t.Fatalf("expected contracts error, got %T: %v", err, err)
	}
	if appErr.Code != want {
		t.Fatalf("expected code %s, got %s", want, appErr.Code)
	}
}

func TestNewSubsampleRejectsDuplicateAncestor(t *testing.T) {
	specimen := validSpecimen(t)
	removal := removalFor(t, specimen, "rem-1", "sub-new", 1, 4)
	_, err := NewSubsample(NewSubsampleInput{
		ID:           "sub-new",
		Code:         "DOM-01-S1",
		ParentID:     specimen.ID,
		AncestorIDs:  []string{"spec-1", "spec-1"},
		Removal:      removal,
		MaterialType: "骨片",
		Quantity:     1,
		Unit:         "块",
		Depth:        1,
		CreatedBy:    "prep-1",
		Now:          time.Now(),
	})
	expectDomainCode(t, err, "subsample_cycle")
}

func TestNewSubsampleRejectsQuantityMismatch(t *testing.T) {
	specimen := validSpecimen(t)
	removal := removalFor(t, specimen, "rem-1", "sub-new", 2, 3)
	_, err := NewSubsample(NewSubsampleInput{
		ID:           "sub-new",
		Code:         "DOM-01-S1",
		ParentID:     specimen.ID,
		AncestorIDs:  []string{"spec-1"},
		Removal:      removal,
		MaterialType: "骨片",
		Quantity:     1, // 与去除数量 2 不一致
		Unit:         "块",
		Depth:        1,
		CreatedBy:    "prep-1",
		Now:          time.Now(),
	})
	expectDomainCode(t, err, "quantity_mismatch")
}

func TestSpecimenReserveUnitsEnforcesInventoryAndSealedStates(t *testing.T) {
	specimen := validSpecimen(t)
	err := specimen.ReserveUnits(6, time.Now())
	expectDomainCode(t, err, "insufficient_quantity")

	if err := specimen.ReserveUnits(2, time.Now()); err != nil {
		t.Fatalf("reserve within inventory: %v", err)
	}
	if specimen.RemainingUnits != 3 || specimen.RemovedUnits != 2 {
		t.Fatalf("inventory bookkeeping wrong: %+v", specimen)
	}

	released := validSpecimen(t)
	if err := released.MarkPlanned(time.Now()); err != nil {
		t.Fatalf("mark planned: %v", err)
	}
	if err := released.MarkPreparing(time.Now()); err != nil {
		t.Fatalf("mark preparing: %v", err)
	}
	if err := released.MarkStabilized(time.Now()); err != nil {
		t.Fatalf("mark stabilized: %v", err)
	}
	if err := released.MarkAvailable(time.Now()); err != nil {
		t.Fatalf("mark available: %v", err)
	}
	err = released.ReserveUnits(1, time.Now())
	expectDomainCode(t, err, "specimen_released")

	terminated := validSpecimen(t)
	if err := terminated.MarkTerminated(time.Now()); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if terminated.Status != SpecimenTerminated {
		t.Fatalf("expected terminated status, got %s", terminated.Status)
	}
	err = terminated.ReserveUnits(1, time.Now())
	expectDomainCode(t, err, "specimen_terminated")

	// 已放行标本不能终止。
	err = released.MarkTerminated(time.Now())
	expectDomainCode(t, err, "specimen_released")
}

func TestValidateIdempotencyKey(t *testing.T) {
	if _, err := ValidateIdempotencyKey(""); err != nil {
		t.Fatalf("empty key should be allowed: %v", err)
	}
	if _, err := ValidateIdempotencyKey("run-2026.10:01"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if _, err := ValidateIdempotencyKey("bad key with spaces"); err == nil {
		t.Fatal("invalid key accepted")
	}
}
