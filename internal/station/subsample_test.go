package station_test

import (
	"context"
	"sync"
	"testing"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

func newTestService() *station.Service {
	return station.New(repository.NewMemory())
}

func registerStockedSpecimen(t *testing.T, svc *station.Service, catalog string, qty int) domain.Specimen {
	t.Helper()
	specimen, err := svc.RegisterSpecimen(context.Background(), contracts.RegisterSpecimenRequest{
		CatalogNumber:   catalog,
		FieldLabel:      "北坡-01",
		MatrixKind:      "粉砂岩",
		InitialQuantity: qty,
	})
	if err != nil {
		t.Fatalf("register specimen: %v", err)
	}
	return specimen
}

func approvePlan(t *testing.T, svc *station.Service, specimenID string, target int) domain.PrepPlan {
	t.Helper()
	plan, err := svc.CreatePlan(context.Background(), specimenID, contracts.CreatePlanRequest{
		Objective:      "暴露断面",
		Method:         "显微剥离",
		TargetExposure: target,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	plan, err = svc.ApprovePlan(context.Background(), plan.ID, contracts.ApprovalRequest{ApprovedBy: "curator-lin"})
	if err != nil {
		t.Fatalf("approve plan: %v", err)
	}
	return plan
}

func TestCreateSubsamplePreservesMotherInventoryAndTraceability(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-TRACE-01", 10)
	plan := approvePlan(t, svc, mother.ID, 80)
	progress, err := svc.LogProgress(context.Background(), plan.ID, contracts.ProgressRequest{
		Stage: "基质剥离", Percent: 30, RecordedBy: "prep-1",
	})
	if err != nil {
		t.Fatalf("log progress: %v", err)
	}

	result, err := svc.CreateSubsample(context.Background(), mother.ID, contracts.SubsampleRequest{
		Code:         "T-TRACE-01-S1",
		PlanID:       plan.ID,
		ProgressID:   progress.ID,
		MaterialType: "骨片",
		Quantity:     3,
		Unit:         "块",
		Reason:       "切片",
		RecordedBy:   "prep-1",
		RequestKey:   "key-1",
	})
	if err != nil {
		t.Fatalf("create subsample: %v", err)
	}
	if result.Subsample == nil {
		t.Fatal("expected linked subsample")
	}
	if result.Subsample.ParentID != mother.ID || result.Subsample.RootSpecimenID != mother.ID {
		t.Fatalf("lineage mismatch: parent=%s root=%s", result.Subsample.ParentID, result.Subsample.RootSpecimenID)
	}
	if result.Subsample.Depth != 1 || result.Removal.PlanID != plan.ID || result.Removal.ProgressID != progress.ID {
		t.Fatalf("traceability triple missing: %+v", result.Subsample)
	}
	updated, err := svc.GetSpecimen(context.Background(), mother.ID)
	if err != nil {
		t.Fatalf("reload mother: %v", err)
	}
	if updated.RemainingUnits != 7 || updated.RemovedUnits != 3 {
		t.Fatalf("inventory mismatch: remaining=%d removed=%d", updated.RemainingUnits, updated.RemovedUnits)
	}
	if result.Removal.RemainingAfter != 7 {
		t.Fatalf("remaining snapshot mismatch: %d", result.Removal.RemainingAfter)
	}
}

func TestCreateSubsampleIdempotentReplayDoesNotDoubleRemove(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-IDEM-01", 5)
	req := contracts.SubsampleRequest{
		Code: "T-IDEM-01-S1", MaterialType: "骨片", Quantity: 2, Unit: "块",
		Reason: "分析", RecordedBy: "prep-1", RequestKey: "idem-key",
	}
	first, err := svc.CreateSubsample(context.Background(), mother.ID, req)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	replay, err := svc.CreateSubsample(context.Background(), mother.ID, req)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if first.Removal.ID != replay.Removal.ID || first.Subsample.ID != replay.Subsample.ID {
		t.Fatal("idempotent replay created new records")
	}
	updated, _ := svc.GetSpecimen(context.Background(), mother.ID)
	if updated.RemainingUnits != 3 || updated.RemovedUnits != 2 {
		t.Fatalf("inventory changed on replay: %+v", updated)
	}
	removals, err := svc.ListRemovals(context.Background(), mother.ID)
	if err != nil {
		t.Fatalf("list removals: %v", err)
	}
	if len(removals) != 1 {
		t.Fatalf("expected exactly one removal, got %d", len(removals))
	}
}

func TestConcurrentSubsampleRequestsHaveDeterministicOutcome(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-CONC-01", 10)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	successes := make(chan string, goroutines)
	failures := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			req := contracts.SubsampleRequest{
				Code:         "", // 每个请求使用不同编号
				MaterialType: "骨片",
				Quantity:     1,
				Unit:         "块",
				Reason:       "并发取样",
				RecordedBy:   "prep-1",
				RequestKey:   "",
			}
			// 不提供客户端编号时由 station 生成不现实——编号是必填的，因此这里显式给唯一编号。
			req.Code = "T-CONC-01-S" + string(rune('A'+i))
			_, err := svc.CreateSubsample(context.Background(), mother.ID, req)
			if err != nil {
				if appErr, ok := contracts.AsError(err); ok {
					failures <- appErr.Code
					return
				}
				failures <- err.Error()
				return
			}
			successes <- "ok"
		}()
	}
	wg.Wait()
	close(successes)
	close(failures)

	okCount := 0
	for range successes {
		okCount++
	}
	denied := 0
	for code := range failures {
		if code != "insufficient_quantity" {
			t.Fatalf("unexpected concurrent error code: %s", code)
		}
		denied++
	}
	if okCount != 10 || denied != 10 {
		t.Fatalf("expected 10 successes and 10 denials, got %d/%d", okCount, denied)
	}
	updated, _ := svc.GetSpecimen(context.Background(), mother.ID)
	if updated.RemainingUnits != 0 || updated.RemovedUnits != 10 {
		t.Fatalf("inventory inconsistent after concurrent removals: %+v", updated)
	}
	children, err := svc.ListSubsamplesForParent(context.Background(), mother.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 10 {
		t.Fatalf("expected 10 child subsamples, got %d (no orphan records)", len(children))
	}
}

func TestConcurrentSameIdempotencyKeyCollapsesToOne(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-CONC-02", 10)

	const goroutines = 12
	var wg sync.WaitGroup
	wg.Add(goroutines)
	ids := make(chan string, goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			result, err := svc.CreateSubsample(context.Background(), mother.ID, contracts.SubsampleRequest{
				Code: "T-CONC-02-S1", MaterialType: "骨片", Quantity: 1, Unit: "块",
				Reason: "并发同键", RecordedBy: "prep-1", RequestKey: "same-key",
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- result.Removal.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent same-key request failed: %v", err)
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("expected exactly one removal id, got %d", len(unique))
	}
	updated, _ := svc.GetSpecimen(context.Background(), mother.ID)
	if updated.RemovedUnits != 1 {
		t.Fatalf("same-key concurrency removed %d units", updated.RemovedUnits)
	}
}

func TestMultiLevelLineageAndNoOrphanAfterFailure(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-LINE-01", 4)
	first, err := svc.CreateSubsample(context.Background(), mother.ID, contracts.SubsampleRequest{
		Code: "T-LINE-01-S1", MaterialType: "骨片", Quantity: 2, Unit: "块",
		Reason: "取样", RecordedBy: "prep-1",
	})
	if err != nil {
		t.Fatalf("first subsample: %v", err)
	}
	grand, err := svc.CreateSubsample(context.Background(), first.Subsample.ID, contracts.SubsampleRequest{
		Code: "T-LINE-01-S1-A", MaterialType: "薄片", Quantity: 1, Unit: "片",
		Reason: "磨片", RecordedBy: "prep-1",
	})
	if err != nil {
		t.Fatalf("grandchild: %v", err)
	}
	if grand.Subsample.Depth != 2 || grand.Subsample.RootSpecimenID != mother.ID {
		t.Fatalf("grandchild lineage wrong: depth=%d root=%s", grand.Subsample.Depth, grand.Subsample.RootSpecimenID)
	}

	// 子样本仅剩 1，请求 2 必须失败，且不得留下去除记录或孙样本。
	_, err = svc.CreateSubsample(context.Background(), first.Subsample.ID, contracts.SubsampleRequest{
		Code: "T-LINE-01-S1-B", MaterialType: "薄片", Quantity: 2, Unit: "片",
		Reason: "过量", RecordedBy: "prep-1", RequestKey: "oversize",
	})
	if code := expectCode(t, err); code != "insufficient_quantity" {
		t.Fatalf("expected insufficient_quantity, got %v", err)
	}
	removals, _ := svc.ListRemovals(context.Background(), first.Subsample.ID)
	if len(removals) != 1 {
		t.Fatalf("failed removal must not leave an orphan record, got %d", len(removals))
	}
	children, _ := svc.ListSubsamplesForParent(context.Background(), first.Subsample.ID)
	if len(children) != 1 {
		t.Fatalf("failed removal must not leave an orphan child, got %d", len(children))
	}
}

func TestReleasedAndTerminatedMotherRejectsSubsample(t *testing.T) {
	svc := newTestService()

	// 已放行标本。
	mother := registerStockedSpecimen(t, svc, "T-SEAL-01", 3)
	plan := approvePlan(t, svc, mother.ID, 100)
	if _, err := svc.LogProgress(context.Background(), plan.ID, contracts.ProgressRequest{
		Stage: "完成", Percent: 100, RecordedBy: "prep-1",
	}); err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	if _, err := svc.ReleaseSpecimen(context.Background(), mother.ID, contracts.ReleaseRequest{
		Purpose: "研究", Recipient: "外单位", ApprovedBy: "keeper-zhou",
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	_, err := svc.CreateSubsample(context.Background(), mother.ID, contracts.SubsampleRequest{
		Code: "T-SEAL-01-S1", MaterialType: "骨片", Quantity: 1, Unit: "块",
		Reason: "取样", RecordedBy: "prep-1",
	})
	if code := expectCode(t, err); code != "specimen_released" {
		t.Fatalf("released mother: expected specimen_released, got %v", err)
	}

	// 已终止子样本。
	other := registerStockedSpecimen(t, svc, "T-SEAL-02", 3)
	sub, err := svc.CreateSubsample(context.Background(), other.ID, contracts.SubsampleRequest{
		Code: "T-SEAL-02-S1", MaterialType: "骨片", Quantity: 1, Unit: "块",
		Reason: "取样", RecordedBy: "prep-1",
	})
	if err != nil {
		t.Fatalf("create subsample: %v", err)
	}
	if _, err := svc.TerminateMaterial(context.Background(), sub.Subsample.ID, contracts.TerminateRequest{
		Reason: "归档", RecordedBy: "keeper-zhou",
	}); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	_, err = svc.CreateSubsample(context.Background(), sub.Subsample.ID, contracts.SubsampleRequest{
		Code: "T-SEAL-02-S1-A", MaterialType: "薄片", Quantity: 1, Unit: "片",
		Reason: "取样", RecordedBy: "prep-1",
	})
	if code := expectCode(t, err); code != "material_terminated" {
		t.Fatalf("terminated mother: expected material_terminated, got %v", err)
	}
}

func TestDuplicateSubsampleCodeRejected(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-DUP-01", 5)
	req := contracts.SubsampleRequest{
		Code: "DUP-CODE", MaterialType: "骨片", Quantity: 1, Unit: "块",
		Reason: "取样", RecordedBy: "prep-1", RequestKey: "a",
	}
	if _, err := svc.CreateSubsample(context.Background(), mother.ID, req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	req.RequestKey = "b"
	_, err := svc.CreateSubsample(context.Background(), mother.ID, req)
	if code := expectCode(t, err); code != "duplicate_subsample_code" {
		t.Fatalf("expected duplicate_subsample_code, got %v", err)
	}
	removals, _ := svc.ListRemovals(context.Background(), mother.ID)
	if len(removals) != 1 {
		t.Fatalf("duplicate code attempt must not add a removal, got %d", len(removals))
	}
}

func TestClosedPlanCannotAnchorRemoval(t *testing.T) {
	svc := newTestService()
	mother := registerStockedSpecimen(t, svc, "T-PLAN-01", 3)
	plan := approvePlan(t, svc, mother.ID, 100)
	if _, err := svc.LogProgress(context.Background(), plan.ID, contracts.ProgressRequest{
		Stage: "完成", Percent: 100, RecordedBy: "prep-1",
	}); err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	_, err := svc.RecordMaterialRemoval(context.Background(), mother.ID, contracts.MaterialRemovalRequest{
		Kind: "discarded", PlanID: plan.ID, MaterialType: "基质", Quantity: 1, Unit: "块",
		Reason: "废弃", RecordedBy: "prep-1",
	})
	if code := expectCode(t, err); code != "plan_closed" {
		t.Fatalf("expected plan_closed, got %v", err)
	}
}

func expectCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	appErr, ok := contracts.AsError(err)
	if !ok {
		t.Fatalf("expected contracts error, got %T: %v", err, err)
	}
	return appErr.Code
}
