package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

func newServer() (*httptest.Server, *station.Service) {
	svc := station.New(repository.NewMemory())
	return httptest.NewServer(httpapi.NewRouter(svc)), svc
}

func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response.StatusCode, decoded
}

func registerViaAPI(t *testing.T, baseURL, catalog string, qty int) string {
	t.Helper()
	status, body := doJSON(t, http.MethodPost, baseURL+"/specimens", contracts.RegisterSpecimenRequest{
		CatalogNumber: catalog, FieldLabel: "野外-1", MatrixKind: "泥岩", InitialQuantity: qty,
	})
	if status != http.StatusCreated {
		t.Fatalf("register status=%d body=%v", status, body)
	}
	return body["id"].(string)
}

func TestSubsampleHTTPSuccessAndConflictStatusCodes(t *testing.T) {
	server, _ := newServer()
	defer server.Close()

	motherID := registerViaAPI(t, server.URL, "HTTP-01", 3)

	status, body := doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/subsamples", contracts.SubsampleRequest{
		Code: "HTTP-01-S1", MaterialType: "骨片", Quantity: 1, Unit: "块",
		Reason: "取样", RecordedBy: "prep-1", RequestKey: "k1",
	})
	if status != http.StatusCreated {
		t.Fatalf("create subsample status=%d body=%v", status, body)
	}
	removal := body["removal"].(map[string]any)
	subsample := body["subsample"].(map[string]any)
	if removal["parent_id"] != motherID || subsample["parent_id"] != motherID {
		t.Fatal("traceability fields missing in JSON")
	}

	// 404：母对象不存在。
	status, body = doJSON(t, http.MethodPost, server.URL+"/materials/unknown/subsamples", contracts.SubsampleRequest{
		Code: "X", MaterialType: "骨片", Quantity: 1, Unit: "块", Reason: "x", RecordedBy: "p",
	})
	if status != http.StatusNotFound || body["code"] != "material_not_found" {
		t.Fatalf("expected 404 material_not_found, got %d %v", status, body)
	}

	// 400：非法输入。
	status, body = doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/subsamples", contracts.SubsampleRequest{
		Code: "HTTP-01-S2", MaterialType: "骨片", Quantity: 0, Unit: "块", Reason: "x", RecordedBy: "p",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero quantity, got %d %v", status, body)
	}

	// 409：数量不足（已扣 1，剩 2，请求 3）。
	status, body = doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/subsamples", contracts.SubsampleRequest{
		Code: "HTTP-01-S3", MaterialType: "骨片", Quantity: 3, Unit: "块",
		Reason: "x", RecordedBy: "p", RequestKey: "k3",
	})
	if status != http.StatusConflict || body["code"] != "insufficient_quantity" {
		t.Fatalf("expected 409 insufficient_quantity, got %d %v", status, body)
	}
}

func TestTerminationReturns201Then200(t *testing.T) {
	server, _ := newServer()
	defer server.Close()

	motherID := registerViaAPI(t, server.URL, "HTTP-02", 2)
	req := contracts.TerminateRequest{Reason: "长期封藏", RecordedBy: "keeper-zhou"}
	status, body := doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/termination", req)
	if status != http.StatusCreated {
		t.Fatalf("first termination expected 201, got %d %v", status, body)
	}
	firstID := body["id"]
	status, body = doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/termination", req)
	if status != http.StatusOK {
		t.Fatalf("idempotent termination expected 200, got %d %v", status, body)
	}
	if body["id"] != firstID {
		t.Fatal("repeated termination created a new record")
	}

	// 终止后取样必须 409。
	status, body = doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/subsamples", contracts.SubsampleRequest{
		Code: "HTTP-02-S1", MaterialType: "骨片", Quantity: 1, Unit: "块",
		Reason: "x", RecordedBy: "p",
	})
	if status != http.StatusConflict || body["code"] != "material_terminated" {
		t.Fatalf("expected 409 material_terminated, got %d %v", status, body)
	}
}

func TestRemovalListAndSubsampleReadback(t *testing.T) {
	server, _ := newServer()
	defer server.Close()

	motherID := registerViaAPI(t, server.URL, "HTTP-03", 4)
	_, body := doJSON(t, http.MethodPost, server.URL+"/materials/"+motherID+"/subsamples", contracts.SubsampleRequest{
		Code: "HTTP-03-S1", MaterialType: "骨片", Quantity: 2, Unit: "块",
		Reason: "切片", RecordedBy: "prep-1", RequestKey: "k",
	})
	subID := body["subsample"].(map[string]any)["id"].(string)

	status, fetched := doJSON(t, http.MethodGet, server.URL+"/subsamples/"+subID, nil)
	if status != http.StatusOK || fetched["id"] != subID {
		t.Fatalf("read back subsample failed: %d %v", status, fetched)
	}

	status, list := doJSON(t, http.MethodGet, server.URL+"/materials/"+motherID+"/removals", nil)
	if status != http.StatusOK {
		t.Fatalf("list removals status=%d", status)
	}
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one removal, got %d", len(items))
	}
}
