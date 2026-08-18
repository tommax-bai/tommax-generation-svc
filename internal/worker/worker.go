// Package worker 消费 generation 队列：提交供应商作业 → 轮询 → 产物转存 → 落终态。
// 消息只带 task_id，详情回源 DB；QUEUED→RUNNING 的 CAS 保证不重复执行（docs/03 §1.3）。
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/hibiken/asynq"

	"github.com/tommax-bai/tommax-go-kit/objstore"

	"github.com/tommax-bai/tommax-generation-svc/internal/domain"
	"github.com/tommax-bai/tommax-generation-svc/internal/repo"
)

type Handler struct {
	repo         domain.TaskRepo
	catalog      domain.Catalog
	adapter      *AdapterClient
	store        *objstore.Store
	pollInterval time.Duration
	jobTimeout   time.Duration
	httpClient   *http.Client
}

func NewHandler(taskRepo domain.TaskRepo, catalog domain.Catalog, adapter *AdapterClient, store *objstore.Store, pollInterval, jobTimeout time.Duration) *Handler {
	return &Handler{
		repo: taskRepo, catalog: catalog, adapter: adapter, store: store,
		pollInterval: pollInterval, jobTimeout: jobTimeout,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (h *Handler) HandleDispatch(ctx context.Context, t *asynq.Task) error {
	var payload repo.DispatchPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("bad payload: %v: %w", err, asynq.SkipRetry)
	}
	log := slog.Default().With("taskId", payload.TaskID)

	task, err := h.repo.GetByID(ctx, payload.TaskID)
	if err != nil {
		return fmt.Errorf("load task: %v: %w", err, asynq.SkipRetry)
	}
	if task.Status.Terminal() {
		return nil // 已被取消/处理完毕
	}
	// 认领：QUEUED→RUNNING；重试场景下可能已是 RUNNING（上次执行中断），允许继续。
	claimed, err := h.repo.CASStatus(ctx, task.ID, domain.StatusQueued, domain.StatusRunning)
	if err != nil {
		return err
	}
	if !claimed && task.Status != domain.StatusRunning {
		return nil
	}

	model, ok := h.catalog.Get(task.ModelKey)
	if !ok {
		return h.fail(ctx, task.ID, "模型已下线")
	}

	// 提交供应商作业（task_id 幂等，重试不会重复出活）。
	jobID := task.ProviderJobID
	if jobID == "" {
		jobID, err = h.adapter.Submit(ctx, SubmitSpec{
			TaskID:        strconv.FormatInt(task.ID, 10),
			ProviderModel: model.ProviderModel,
			TaskType:      task.TaskType,
			Prompt:        task.Prompt,
			RefURLs:       task.RefAssetURLs,
			Params:        task.Params,
		})
		if err != nil {
			return h.retryOrFail(ctx, task.ID, fmt.Errorf("submit: %w", err))
		}
		if err := h.repo.SaveProviderJob(ctx, task.ID, jobID); err != nil {
			return err
		}
	}

	// 轮询直至终态 / 超时 / 被取消。
	deadline := time.Now().Add(h.jobTimeout)
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			h.adapter.Cancel(ctx, jobID)
			return h.fail(ctx, task.ID, "生成超时")
		}
		// 用户取消检查。
		current, err := h.repo.GetByID(ctx, task.ID)
		if err == nil && current.Status == domain.StatusCanceled {
			h.adapter.Cancel(ctx, jobID)
			log.Info("task canceled by user")
			return nil
		}

		snap, err := h.adapter.Query(ctx, jobID)
		if err != nil {
			return h.retryOrFail(ctx, task.ID, fmt.Errorf("query: %w", err))
		}
		switch snap.Status {
		case JobRunning:
			_ = h.repo.SaveProgress(ctx, task.ID, snap.Progress)
		case JobSucceeded:
			outputs, err := h.persistOutputs(ctx, task.ID, snap.Outputs)
			if err != nil {
				return h.retryOrFail(ctx, task.ID, fmt.Errorf("persist outputs: %w", err))
			}
			if err := h.repo.Finish(ctx, task.ID, domain.StatusSucceeded, outputs, ""); err != nil {
				return err
			}
			log.Info("task succeeded", "outputs", len(outputs))
			return nil
		case JobFailedRetryable:
			return h.retryOrFail(ctx, task.ID, fmt.Errorf("provider: %s", snap.ErrorMsg))
		case JobContentBlocked:
			return h.fail(ctx, task.ID, "内容不符合平台规范，请调整提示词")
		default: // FAILED_PERMANENT
			return h.fail(ctx, task.ID, "生成失败："+snap.ErrorMsg)
		}
	}
}

// persistOutputs 把供应商产物（bytes 或临时 URL）转存对象存储，返回资产化的产物列表。
func (h *Handler) persistOutputs(ctx context.Context, taskID int64, jobOutputs []JobOutput) ([]domain.Output, error) {
	outputs := make([]domain.Output, 0, len(jobOutputs))
	for i, o := range jobOutputs {
		data := o.Data
		if len(data) == 0 && o.URL != "" {
			fetched, err := h.download(ctx, o.URL)
			if err != nil {
				return nil, err
			}
			data = fetched
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("output %d has neither data nor url", i)
		}
		key := h.store.Key("generation", fmt.Sprintf("%d-%d", taskID, i), extByMime(o.MimeType))
		if err := h.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), o.MimeType); err != nil {
			return nil, err
		}
		outputs = append(outputs, domain.Output{
			AssetURL: h.store.PublicURL(key),
			MimeType: o.MimeType,
			Width:    o.Width,
			Height:   o.Height,
		})
	}
	return outputs, nil
}

func (h *Handler) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	const maxSize = 512 << 20 // 512MB 上限，防御异常产物
	return io.ReadAll(io.LimitReader(resp.Body, maxSize))
}

// retryOrFail：还有重试额度就交给 asynq 重试，耗尽则落 FAILED（终态一定有人负责）。
func (h *Handler) retryOrFail(ctx context.Context, taskID int64, cause error) error {
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	if retried >= maxRetry {
		_ = h.repo.Finish(ctx, taskID, domain.StatusFailed, nil, "多次重试仍失败，请稍后再试")
		return fmt.Errorf("retries exhausted: %v: %w", cause, asynq.SkipRetry)
	}
	slog.Warn("task attempt failed, will retry", "taskId", taskID, "attempt", retried, "err", cause)
	return cause
}

func (h *Handler) fail(ctx context.Context, taskID int64, reason string) error {
	if err := h.repo.Finish(ctx, taskID, domain.StatusFailed, nil, reason); err != nil {
		return err
	}
	return nil
}

func extByMime(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "audio/mpeg":
		return "mp3"
	default:
		return "bin"
	}
}
