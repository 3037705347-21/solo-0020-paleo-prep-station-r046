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
	"sync"
	"sync/atomic"
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
	StockQuantity int    `json:"stock_quantity"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

type removalResultView struct {
	Removal struct {
		ID          string `json:"id"`
		PlanID      string `json:"plan_id"`
		Step        int    `json:"step"`
		ParentKind  string `json:"parent_kind"`
		ParentID    string `json:"parent_id"`
		SubsampleID string `json:"subsample_id"`
		Quantity    int    `json:"quantity"`
	} `json:"removal"`
	Subsample struct {
		ID             string `json:"id"`
		Code           string `json:"code"`
		RootSpecimenID string `json:"root_specimen_id"`
		ParentKind     string `json:"parent_kind"`
		ParentID       string `json:"parent_id"`
		Quantity       int    `json:"quantity"`
		Remaining      int    `json:"remaining"`
	} `json:"subsample"`
}

type removalEntryView struct {
	ID          string `json:"id"`
	Step        int    `json:"step"`
	SubsampleID string `json:"subsample_id"`
	Quantity    int    `json:"quantity"`
}

type subsampleEntryView struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	RootSpecimenID string `json:"root_specimen_id"`
	Remaining      int    `json:"remaining"`
}

type errorView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
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

	case "register-subsample":
		return checkRegisterSubsample(client, server.URL)
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
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func checkRegisterSubsample(client *http.Client, baseURL string) error {
	// Guard: an approved-but-not-started plan must reject material removal.
	earlySpecimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return err
	}
	earlyPlan, err := createPlan(client, baseURL, earlySpecimen.ID)
	if err != nil {
		return err
	}
	if err := requestJSON(client, http.MethodPost, baseURL+"/plans/"+earlyPlan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"}, http.StatusOK, &planView{}); err != nil {
		return err
	}
	if err := expectConflictCode(client, baseURL+"/plans/"+earlyPlan.ID+"/removals",
		removalRequest("SUB-EARLY", 1, "", ""), "plan_not_accepting_removal"); err != nil {
		return err
	}

	// Main lineage: active plan.
	specimen, plan, err := completedPlanSetup(client, baseURL)
	if err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan.ID, 30, "矩阵初剥"); err != nil {
		return err
	}

	first, err := postRemoval(client, baseURL, plan.ID, removalRequest("SUB-A-001", 10, "", ""))
	if err != nil {
		return err
	}
	if first.Removal.Step != 1 || first.Removal.ParentKind != "specimen" ||
		first.Removal.ParentID != specimen.ID || first.Removal.SubsampleID != first.Subsample.ID {
		return errors.New("first removal did not bind plan step, mother specimen and subsample together")
	}
	if first.Subsample.RootSpecimenID != specimen.ID || first.Subsample.Remaining != 10 {
		return errors.New("subsample lineage or initial inventory incorrect")
	}
	if err := expectSpecimenStock(client, baseURL, specimen.ID, 90); err != nil {
		return err
	}

	// Duplicate sampling code is a conflict and leaves no extra records.
	if err := expectConflictCode(client, baseURL+"/plans/"+plan.ID+"/removals",
		removalRequest("SUB-A-001", 1, "", ""), "duplicate_subsample_code"); err != nil {
		return err
	}

	// Nested sampling: detach from the first subsample. The mother specimen
	// inventory is untouched, the parent subsample loses material.
	nested, err := postRemoval(client, baseURL, plan.ID,
		removalRequest("SUB-A-001-N1", 3, "subsample", first.Subsample.ID))
	if err != nil {
		return err
	}
	if nested.Removal.Step != 2 || nested.Subsample.ParentKind != "subsample" ||
		nested.Subsample.ParentID != first.Subsample.ID || nested.Subsample.RootSpecimenID != specimen.ID {
		return errors.New("nested subsample lineage is not traceable to the mother specimen")
	}
	var parentSample subsampleEntryView
	if err := requestJSON(client, http.MethodGet, baseURL+"/subsamples/"+first.Subsample.ID, nil,
		http.StatusOK, &parentSample); err != nil {
		return err
	}
	if parentSample.Remaining != 7 {
		return fmt.Errorf("expected parent subsample remaining 7, got %d", parentSample.Remaining)
	}
	if err := expectSpecimenStock(client, baseURL, specimen.ID, 90); err != nil {
		return err
	}

	// Concurrent removals against the same plan serialize into contiguous
	// steps with exactly-once commits.
	const concurrent = 5
	type concurrentOutcome struct {
		result removalResultView
		err    error
	}
	outcomes := make(chan concurrentOutcome, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			req := removalRequest(fmt.Sprintf("SUB-C-%d", index), 5, "", "")
			result, err := postRemoval(client, baseURL, plan.ID, req)
			outcomes <- concurrentOutcome{result: result, err: err}
		}(i)
	}
	wg.Wait()
	close(outcomes)
	seenSteps := map[int]bool{}
	for outcome := range outcomes {
		if outcome.err != nil {
			return fmt.Errorf("concurrent removal failed: %w", outcome.err)
		}
		if seenSteps[outcome.result.Removal.Step] {
			return errors.New("concurrent removals produced a duplicated step")
		}
		seenSteps[outcome.result.Removal.Step] = true
	}
	for step := 3; step < 3+concurrent; step++ {
		if !seenSteps[step] {
			return fmt.Errorf("concurrent removals left step %d unassigned", step)
		}
	}
	if err := expectSpecimenStock(client, baseURL, specimen.ID, 65); err != nil {
		return err
	}

	// Insufficient stock is rejected; no orphan removal or subsample appears.
	if err := expectConflictCode(client, baseURL+"/plans/"+plan.ID+"/removals",
		removalRequest("SUB-OVER", 100, "", ""), "insufficient_stock"); err != nil {
		return err
	}
	var subsampleList struct {
		Items []subsampleEntryView `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID+"/subsamples", nil,
		http.StatusOK, &subsampleList); err != nil {
		return err
	}
	if len(subsampleList.Items) != 2+concurrent {
		return fmt.Errorf("expected %d subsamples, got %d (orphan or missing records)", 2+concurrent, len(subsampleList.Items))
	}
	var removalList struct {
		Items []removalEntryView `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID+"/removals", nil,
		http.StatusOK, &removalList); err != nil {
		return err
	}
	if len(removalList.Items) != 2+concurrent {
		return fmt.Errorf("expected %d removals, got %d (ledger diverged from subsamples)", 2+concurrent, len(removalList.Items))
	}

	// Terminated plan and released mother specimen reject further sampling.
	if err := recordProgress(client, baseURL, plan.ID, 100, "完成制备"); err != nil {
		return err
	}
	if err := expectConflictCode(client, baseURL+"/plans/"+plan.ID+"/removals",
		removalRequest("SUB-END", 1, "", ""), "plan_not_accepting_removal"); err != nil {
		return err
	}
	if err := requestJSON(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release",
		contracts.ReleaseRequest{Purpose: "同位素取样研究", Recipient: "地层研究组", ApprovedBy: "keeper-zhou"},
		http.StatusCreated, &releaseView{}); err != nil {
		return err
	}
	if err := expectConflictCode(client, baseURL+"/plans/"+plan.ID+"/removals",
		removalRequest("SUB-AFTER-RELEASE", 1, "", ""), "plan_not_accepting_removal"); err != nil {
		return err
	}
	return nil
}

func removalRequest(code string, quantity int, parentKind, parentID string) contracts.RecordRemovalRequest {
	return contracts.RecordRemovalRequest{
		ParentKind:   parentKind,
		ParentID:     parentID,
		Code:         code,
		Label:        "剥离基质样 " + code,
		MaterialKind: "matrix",
		Quantity:     quantity,
		Notes:        "子样本链路检查",
		Operator:     "preparator-chen",
		Purpose:      "薄片与粒度分析",
	}
}

func postRemoval(client *http.Client, baseURL, planID string, request contracts.RecordRemovalRequest) (removalResultView, error) {
	var result removalResultView
	err := requestJSON(client, http.MethodPost, baseURL+"/plans/"+planID+"/removals",
		request, http.StatusCreated, &result)
	return result, err
}

func expectSpecimenStock(client *http.Client, baseURL, specimenID string, want int) error {
	var fetched specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimenID, nil,
		http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.StockQuantity != want {
		return fmt.Errorf("expected mother stock %d, got %d", want, fetched.StockQuantity)
	}
	return nil
}

func expectConflictCode(client *http.Client, url string, body any, wantCode string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusConflict {
		return fmt.Errorf("expected 409 from %s, got %d: %s", url, response.StatusCode, string(raw))
	}
	var bodyView errorView
	if err := json.Unmarshal(raw, &bodyView); err != nil {
		return err
	}
	if bodyView.Code != wantCode {
		return fmt.Errorf("expected conflict code %s, got %s", wantCode, bodyView.Code)
	}
	return nil
}

var registrationSeq atomic.Int64

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	catalog := fmt.Sprintf("PPS-2026-%03d", registrationSeq.Add(1))
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
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
