package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		ID      string `json:"id"`
		Percent int    `json:"percent"`
	} `json:"progress"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

type specimenInventoryView struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	InitialQuantity int    `json:"initial_quantity"`
	RemainingUnits  int    `json:"remaining_units"`
	RemovedUnits    int    `json:"removed_units"`
}

type subsampleView struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	ParentID       string `json:"parent_id"`
	RootSpecimenID string `json:"root_specimen_id"`
	PlanID         string `json:"plan_id"`
	ProgressID     string `json:"progress_id"`
	RemovalID      string `json:"removal_id"`
	Depth          int    `json:"depth"`
	RemainingUnits int    `json:"remaining_units"`
	Terminated     bool   `json:"terminated"`
}

type removalResultView struct {
	Removal struct {
		ID             string `json:"id"`
		ParentID       string `json:"parent_id"`
		PlanID         string `json:"plan_id"`
		ProgressID     string `json:"progress_id"`
		Sequence       int    `json:"sequence"`
		Kind           string `json:"kind"`
		Quantity       int    `json:"quantity"`
		RemainingAfter int    `json:"remaining_after"`
		SubsampleID    string `json:"subsample_id"`
		RequestKey     string `json:"request_key"`
	} `json:"removal"`
	Subsample *subsampleView `json:"subsample"`
}

type removalView struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

type listItems[T any] struct {
	Items []T `json:"items"`
}

type errorView struct {
	Code string `json:"code"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID)
		if err != nil {
			return err
		}
		var approved planView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/plans/"+plan.ID+"/approval",
			contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
			http.StatusOK,
			&approved,
		); err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "log-preparation":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
			return errors.New("plan did not complete with exactly two progress entries")
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "release-for-study":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var release releaseView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/specimens/"+specimen.ID+"/release",
			contracts.ReleaseRequest{
				Purpose:    "比较解剖研究",
				Recipient:  "地层研究组",
				ApprovedBy: "keeper-zhou",
			},
			http.StatusCreated,
			&release,
		); err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return nil

	case "record-subsample":
		return checkSubsampleWorkflow(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

// checkSubsampleWorkflow 覆盖取样登记的成功路径与关键冲突边界：
// 子样本溯源三元组、母标本库存扣减、幂等重放、数量不足和终止后拒绝取样。
func checkSubsampleWorkflow(client *http.Client, baseURL string) error {
	// 登记一份显式库存为 5 块的标本，独立于其它链路使用的目录号。
	var specimen specimenInventoryView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber:   "PPS-2026-099",
			FieldLabel:      "北坡-09",
			TaxonHint:       "兽脚类",
			MatrixKind:      "泥岩",
			JacketNotes:     "完整夹克",
			InitialQuantity: 5,
		},
		http.StatusCreated,
		&specimen,
	); err != nil {
		return err
	}
	if specimen.InitialQuantity != 5 || specimen.RemainingUnits != 5 {
		return errors.New("initial inventory not recorded correctly")
	}

	// 创建并审批方案，再记录一条操作步骤，供子样本溯源。
	var plan planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露肢骨断面",
			Method:         "气动剥离",
			Adhesive:       "稀 Paraloid",
			TargetExposure: 60,
		},
		http.StatusCreated,
		&plan,
	); err != nil {
		return err
	}
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		new(struct{}),
	); err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan.ID, 40, "围岩清理"); err != nil {
		return err
	}
	var planData planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &planData); err != nil {
		return err
	}
	if len(planData.Progress) != 1 {
		return errors.New("expected one progress entry to reference from the subsample")
	}
	progressID := planData.Progress[0].ID

	request := contracts.SubsampleRequest{
		Code:         "PPS-2026-099-S1",
		PlanID:       plan.ID,
		ProgressID:   progressID,
		MaterialType: "骨片碎屑",
		Quantity:     2,
		Unit:         "块",
		Reason:       "切片分析取样",
		Method:       "显微剥离",
		Note:         "保留关节连接",
		RecordedBy:   "preparator-chen",
		RequestKey:   "wf-subsample-001",
	}

	var first removalResultView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/"+specimen.ID+"/subsamples",
		request,
		http.StatusCreated,
		&first,
	); err != nil {
		return err
	}
	if first.Subsample == nil {
		return errors.New("subsample response missing created subsample")
	}
	if first.Subsample.ParentID != specimen.ID || first.Subsample.RootSpecimenID != specimen.ID {
		return errors.New("subsample lineage does not point back to the mother specimen")
	}
	if first.Subsample.PlanID != plan.ID || first.Subsample.ProgressID != progressID {
		return errors.New("subsample is missing plan/step traceability")
	}
	if first.Subsample.Depth != 1 || first.Removal.Sequence != 1 {
		return errors.New("subsample depth or removal sequence incorrect")
	}
	if first.Removal.Quantity != 2 || first.Removal.RemainingAfter != 3 {
		return fmt.Errorf("removal quantity/remaining snapshot incorrect: qty=%d after=%d",
			first.Removal.Quantity, first.Removal.RemainingAfter)
	}

	var mother specimenInventoryView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, http.StatusOK, &mother); err != nil {
		return err
	}
	if mother.RemovedUnits != 2 || mother.RemainingUnits != 3 {
		return fmt.Errorf("mother inventory not updated atomically: remaining=%d removed=%d", mother.RemainingUnits, mother.RemovedUnits)
	}

	// 幂等重放：同样请求键再来一次必须返回同一条记录，库存不得二次扣减。
	var replay removalResultView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/"+specimen.ID+"/subsamples",
		request,
		http.StatusCreated,
		&replay,
	); err != nil {
		return err
	}
	if replay.Removal.ID != first.Removal.ID || replay.Subsample.ID != first.Subsample.ID {
		return errors.New("idempotent replay created a second removal")
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, http.StatusOK, &mother); err != nil {
		return err
	}
	if mother.RemovedUnits != 2 || mother.RemainingUnits != 3 {
		return errors.New("idempotent replay changed inventory")
	}

	// 在子样本上继续取一级孙样本，验证多级溯源链。
	grandchildReq := contracts.SubsampleRequest{
		Code:         "PPS-2026-099-S1-A",
		MaterialType: "薄片",
		Quantity:     1,
		Unit:         "片",
		Reason:       "磨片观察",
		RecordedBy:   "preparator-chen",
		RequestKey:   "wf-subsample-grand-001",
	}
	var grandchild removalResultView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/"+first.Subsample.ID+"/subsamples",
		grandchildReq,
		http.StatusCreated,
		&grandchild,
	); err != nil {
		return err
	}
	if grandchild.Subsample.ParentID != first.Subsample.ID ||
		grandchild.Subsample.RootSpecimenID != specimen.ID ||
		grandchild.Subsample.Depth != 2 {
		return errors.New("grandchild lineage chain incorrect")
	}

	// 子样本编号重复必须冲突，且不能产生第二条去除。
	dupReq := request
	dupReq.RequestKey = "wf-subsample-dup-code"
	if status, code, err := requestExpectConflict(client, http.MethodPost,
		baseURL+"/materials/"+specimen.ID+"/subsamples", dupReq); err != nil {
		return err
	} else if code != "duplicate_subsample_code" {
		return fmt.Errorf("expected duplicate_subsample_code, got status=%d code=%s", status, code)
	}

	// 数量不足必须是确定性冲突（剩余 3，请求 4）。
	overReq := request
	overReq.Code = "PPS-2026-099-S2"
	overReq.Quantity = 4
	overReq.RequestKey = "wf-subsample-002"
	if status, code, err := requestExpectConflict(client, http.MethodPost,
		baseURL+"/materials/"+specimen.ID+"/subsamples", overReq); err != nil {
		return err
	} else if code != "insufficient_quantity" {
		return fmt.Errorf("expected insufficient_quantity, got status=%d code=%s", status, code)
	}

	// 终止孙样本后不能再取样，终止记录可重放。
	var termination struct {
		ID       string `json:"id"`
		TargetID string `json:"target_id"`
		Subject  string `json:"subject"`
	}
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/"+grandchild.Subsample.ID+"/termination",
		contracts.TerminateRequest{Reason: "样本已耗尽并归档", RecordedBy: "keeper-zhou"},
		http.StatusCreated,
		&termination,
	); err != nil {
		return err
	}
	if termination.TargetID != grandchild.Subsample.ID || termination.Subject != "subsample" {
		return errors.New("termination record does not reference the subsample")
	}
	blockedReq := grandchildReq
	blockedReq.Code = "PPS-2026-099-S1-A-2"
	blockedReq.RequestKey = "wf-subsample-blocked"
	if status, code, err := requestExpectConflict(client, http.MethodPost,
		baseURL+"/materials/"+grandchild.Subsample.ID+"/subsamples", blockedReq); err != nil {
		return err
	} else if code != "material_terminated" {
		return fmt.Errorf("expected material_terminated, got status=%d code=%s", status, code)
	}

	// 非子样本去除（废弃基质）同样扣减库存并留痕。
	discardReq := contracts.MaterialRemovalRequest{
		Kind:         "discarded",
		PlanID:       plan.ID,
		MaterialType: "泥岩基质",
		Quantity:     1,
		Unit:         "块",
		Reason:       "无化石围岩废弃",
		RecordedBy:   "preparator-chen",
		RequestKey:   "wf-removal-discard-001",
	}
	var discarded removalView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/"+specimen.ID+"/removals",
		discardReq,
		http.StatusCreated,
		&discarded,
	); err != nil {
		return err
	}
	if discarded.Kind != "discarded" || discarded.Sequence != 2 {
		return errors.New("standalone removal not appended with a continuous sequence")
	}

	// 去除历史与子样本列表都必须可回溯到母标本。
	var removals listItems[removalView]
	if err := requestJSON(client, http.MethodGet, baseURL+"/materials/"+specimen.ID+"/removals", nil, http.StatusOK, &removals); err != nil {
		return err
	}
	if len(removals.Items) != 2 {
		return fmt.Errorf("expected two removals on mother, got %d", len(removals.Items))
	}
	var children listItems[subsampleView]
	if err := requestJSON(client, http.MethodGet, baseURL+"/materials/"+specimen.ID+"/subsamples", nil, http.StatusOK, &children); err != nil {
		return err
	}
	if len(children.Items) != 1 || children.Items[0].ID != first.Subsample.ID {
		return errors.New("subsample listing does not preserve the mother-child relation")
	}

	// 已放行标本不能新增子样本：在一份新的已放行标本上验证封存。
	released, err := releasedSpecimenSetup(client, baseURL)
	if err != nil {
		return err
	}
	releasedReq := request
	releasedReq.Code = "PPS-2026-100-S1"
	releasedReq.PlanID = ""
	releasedReq.ProgressID = ""
	releasedReq.Quantity = 1
	releasedReq.RequestKey = "wf-subsample-released"
	if status, code, err := requestExpectConflict(client, http.MethodPost,
		baseURL+"/materials/"+released.ID+"/subsamples", releasedReq); err != nil {
		return err
	} else if code != "specimen_released" {
		return fmt.Errorf("expected specimen_released, got status=%d code=%s", status, code)
	}
	return nil
}

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: "PPS-2026-001",
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func completedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: "preparator-chen",
		},
		http.StatusCreated,
		&ignored,
	)
}

// releasedSpecimenSetup 构造一份已走完整制备并放行的标本。
func releasedSpecimenSetup(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: "PPS-2026-100",
			FieldLabel:    "北坡-10",
			MatrixKind:    "页岩",
		},
		http.StatusCreated,
		&specimen,
	); err != nil {
		return specimenView{}, err
	}
	var plan planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/plans",
		contracts.CreatePlanRequest{Objective: "完整暴露", Method: "手工清理", TargetExposure: 100},
		http.StatusCreated,
		&plan,
	); err != nil {
		return specimenView{}, err
	}
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		new(struct{}),
	); err != nil {
		return specimenView{}, err
	}
	if err := recordProgress(client, baseURL, plan.ID, 100, "完成"); err != nil {
		return specimenView{}, err
	}
	var release releaseView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release",
		contracts.ReleaseRequest{Purpose: "展览", Recipient: "博物馆", ApprovedBy: "keeper-zhou"},
		http.StatusCreated,
		&release,
	); err != nil {
		return specimenView{}, err
	}
	return specimen, nil
}

// requestExpectConflict 断言请求返回 409，并解码稳定错误码。
func requestExpectConflict(client *http.Client, method, url string, body any) (int, string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, "", err
	}
	var decoded errorView
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return response.StatusCode, "", fmt.Errorf("decode error body for %s: %w (%s)", url, err, string(raw))
	}
	if response.StatusCode != http.StatusConflict {
		return response.StatusCode, decoded.Code, fmt.Errorf("expected 409 from %s, got %d: %s", url, response.StatusCode, string(raw))
	}
	return response.StatusCode, decoded.Code, nil
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
