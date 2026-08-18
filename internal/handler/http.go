// Package handler 接口层：REST 路由与 DTO（契约对齐 tommax-proto generation/v1，字段 lowerCamelCase）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tommax-bai/tommax-go-kit/errs"
	"github.com/tommax-bai/tommax-go-kit/httpx"

	"github.com/tommax-bai/tommax-generation-svc/internal/domain"
	"github.com/tommax-bai/tommax-generation-svc/internal/service"
)

type GenerationHandler struct {
	svc *service.GenerationService
}

func NewGenerationHandler(svc *service.GenerationService) *GenerationHandler {
	return &GenerationHandler{svc: svc}
}

// Mount 注册路由（docs/04 §1.2 命名）。
func (h *GenerationHandler) Mount(r chi.Router) {
	r.Post("/v1/generations", h.submit)
	r.Get("/v1/generations", h.list)
	r.Get("/v1/generations/{id}", h.get)
	r.Post("/v1/generations/{id}:cancel", h.cancel)
	r.Get("/v1/models", h.models)
}

// ---- DTO 与 assembler ----

type submitReq struct {
	TaskType     string            `json:"taskType"`
	ModelKey     string            `json:"modelKey"`
	Prompt       string            `json:"prompt"`
	RefAssetURLs []string          `json:"refAssetUrls"`
	Params       map[string]string `json:"params"`
	RequestID    string            `json:"requestId"`
	CanvasCtx    map[string]string `json:"canvasCtx"`
}

type outputDTO struct {
	AssetURL string `json:"assetUrl"`
	MimeType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type generationDTO struct {
	ID           string            `json:"id"`
	TaskType     string            `json:"taskType"`
	ModelKey     string            `json:"modelKey"`
	Prompt       string            `json:"prompt"`
	RefAssetURLs []string          `json:"refAssetUrls"`
	Params       map[string]string `json:"params"`
	Status       string            `json:"status"`
	Progress     int               `json:"progress"`
	Outputs      []outputDTO       `json:"outputs"`
	ErrorReason  string            `json:"errorReason,omitempty"`
	CreatedAt    string            `json:"createdAt"`
	UpdatedAt    string            `json:"updatedAt"`
}

func toDTO(t *domain.Task) generationDTO {
	dto := generationDTO{
		ID:           strconv.FormatInt(t.ID, 10),
		TaskType:     t.TaskType,
		ModelKey:     t.ModelKey,
		Prompt:       t.Prompt,
		RefAssetURLs: t.RefAssetURLs,
		Params:       t.Params,
		Status:       string(t.Status),
		Progress:     t.Progress,
		Outputs:      []outputDTO{},
		ErrorReason:  t.ErrorReason,
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, o := range t.Outputs {
		dto.Outputs = append(dto.Outputs, outputDTO(o))
	}
	return dto
}

// ---- handlers ----

func (h *GenerationHandler) submit(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if err := httpx.Bind(r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if req.TaskType == "" || req.ModelKey == "" {
		httpx.Fail(w, r, errs.ErrInvalidParam.WithMessagef("taskType 和 modelKey 必填"))
		return
	}
	task, err := h.svc.Submit(r.Context(), service.SubmitInput{
		TaskType:     req.TaskType,
		ModelKey:     req.ModelKey,
		Prompt:       req.Prompt,
		RefAssetURLs: req.RefAssetURLs,
		Params:       req.Params,
		RequestID:    req.RequestID,
		CanvasCtx:    req.CanvasCtx,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, r, toDTO(task))
}

func (h *GenerationHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	task, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, r, toDTO(task))
}

func (h *GenerationHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	var beforeID int64
	if token := q.Get("pageToken"); token != "" {
		beforeID, _ = strconv.ParseInt(token, 10, 64)
	}
	tasks, err := h.svc.List(r.Context(), q.Get("taskType"), pageSize, beforeID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	items := make([]generationDTO, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, toDTO(t))
	}
	next := ""
	if len(tasks) > 0 && len(tasks) == pageSizeOrDefault(pageSize) {
		next = strconv.FormatInt(tasks[len(tasks)-1].ID, 10)
	}
	httpx.OK(w, r, map[string]any{"items": items, "nextPageToken": next})
}

func (h *GenerationHandler) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	task, err := h.svc.Cancel(r.Context(), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, r, toDTO(task))
}

func (h *GenerationHandler) models(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, r, map[string]any{"items": h.svc.Models(r.Context())})
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errs.ErrInvalidParam.WithMessagef("非法的任务 id")
	}
	return id, nil
}

func pageSizeOrDefault(n int) int {
	if n <= 0 || n > 100 {
		return 20
	}
	return n
}
