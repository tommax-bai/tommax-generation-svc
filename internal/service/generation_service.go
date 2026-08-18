// Package service 应用层：用例编排与事务边界（docs/03 §1.1）。
// Phase 1 无积分冻结与合规前置（docs/05 §1.1），受理编排 = 校验 → 建任务 → 入队。
package service

import (
	"context"
	"fmt"

	"github.com/tommax-bai/tommax-go-kit/errs"
	"github.com/tommax-bai/tommax-go-kit/idgen"
	"github.com/tommax-bai/tommax-go-kit/logx"

	"github.com/tommax-bai/tommax-generation-svc/internal/domain"
)

// Enqueuer 任务入队接口（接口定义在消费方，repo 层用 asynq 实现）。
type Enqueuer interface {
	EnqueueDispatch(ctx context.Context, taskID int64) error
}

type GenerationService struct {
	repo    domain.TaskRepo
	catalog domain.Catalog
	queue   Enqueuer
}

func NewGenerationService(repo domain.TaskRepo, catalog domain.Catalog, queue Enqueuer) *GenerationService {
	return &GenerationService{repo: repo, catalog: catalog, queue: queue}
}

type SubmitInput struct {
	TaskType     string
	ModelKey     string
	Prompt       string
	RefAssetURLs []string
	Params       map[string]string
	RequestID    string
	CanvasCtx    map[string]string
}

// Submit 受理生成任务：幂等 → 校验 → 建任务 → 入队。
func (s *GenerationService) Submit(ctx context.Context, in SubmitInput) (*domain.Task, error) {
	userID := logx.UserID(ctx)
	if userID == "" {
		return nil, errs.ErrUnauthorized
	}

	// 幂等：同一用户同一 requestId 返回已有任务（docs/04 §2.4）。
	if in.RequestID != "" {
		if existing, err := s.repo.GetByUserRequestID(ctx, userID, in.RequestID); err == nil && existing != nil {
			return existing, nil
		}
	} else {
		in.RequestID = idgen.NextString()
	}

	model, ok := s.catalog.Get(in.ModelKey)
	if !ok {
		return nil, domain.ErrModelOffline
	}
	if model.Capability != in.TaskType {
		return nil, domain.ErrTaskTypeMismatch.WithMessagef("模型 %s 不支持 %s", in.ModelKey, in.TaskType)
	}
	if in.Prompt == "" && len(in.RefAssetURLs) == 0 {
		return nil, domain.ErrPromptRequired
	}

	task := &domain.Task{
		ID:           mustID(),
		UserID:       userID,
		RequestID:    in.RequestID,
		TaskType:     in.TaskType,
		ModelKey:     in.ModelKey,
		Prompt:       in.Prompt,
		RefAssetURLs: in.RefAssetURLs,
		Params:       in.Params,
		CanvasCtx:    in.CanvasCtx,
		Status:       domain.StatusPending,
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	if err := s.queue.EnqueueDispatch(ctx, task.ID); err != nil {
		// 入队失败即失败终态：不留悬挂的 PENDING（无人认领）。
		_ = s.repo.Finish(ctx, task.ID, domain.StatusFailed, nil, "enqueue failed")
		return nil, fmt.Errorf("enqueue task %d: %w", task.ID, err)
	}
	if _, err := s.repo.CASStatus(ctx, task.ID, domain.StatusPending, domain.StatusQueued); err != nil {
		return nil, fmt.Errorf("mark queued %d: %w", task.ID, err)
	}
	// 回读以携带 DB 生成的时间戳与最新状态。
	return s.repo.GetByID(ctx, task.ID)
}

// Get 查询任务（owner 校验在应用层，docs/03 §1.2）。
func (s *GenerationService) Get(ctx context.Context, id int64) (*domain.Task, error) {
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.UserID != logx.UserID(ctx) {
		return nil, errs.ErrForbidden
	}
	return task, nil
}

// List 分页查询生成历史（游标 = 上一页最后一条的 id）。
func (s *GenerationService) List(ctx context.Context, taskType string, pageSize int, beforeID int64) ([]*domain.Task, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.List(ctx, logx.UserID(ctx), taskType, pageSize, beforeID)
}

// Cancel 标记取消；执行中的作业由 worker 观察状态后停止轮询并尽力取消供应商作业。
func (s *GenerationService) Cancel(ctx context.Context, id int64) (*domain.Task, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.Status.Cancelable() {
		return nil, domain.ErrNotCancelable
	}
	for _, from := range []domain.Status{domain.StatusPending, domain.StatusQueued, domain.StatusRunning} {
		if ok, err := s.repo.CASStatus(ctx, id, from, domain.StatusCanceled); err != nil {
			return nil, fmt.Errorf("cancel %d: %w", id, err)
		} else if ok {
			break
		}
	}
	return s.repo.GetByID(ctx, id)
}

// Models 返回模型目录（前端展示用）。
func (s *GenerationService) Models(context.Context) []domain.ModelInfo {
	return s.catalog.List()
}

func mustID() int64 {
	id, err := idgen.NextID()
	if err != nil {
		panic(err)
	}
	return id
}
