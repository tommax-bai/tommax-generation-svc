// Package repo 基础设施层：domain 接口的 PostgreSQL / asynq / 配置文件实现。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tommax-bai/tommax-generation-svc/internal/domain"
)

type TaskPG struct {
	pool *pgxpool.Pool
}

func NewTaskPG(pool *pgxpool.Pool) *TaskPG { return &TaskPG{pool: pool} }

const taskColumns = `id, user_id, request_id, task_type, model_key, prompt,
	ref_asset_urls, params, canvas_ctx, status, progress, outputs, error_reason,
	provider_job_id, created_at, updated_at`

func (r *TaskPG) Create(ctx context.Context, t *domain.Task) error {
	refs, _ := json.Marshal(orEmptySlice(t.RefAssetURLs))
	params, _ := json.Marshal(orEmptyMap(t.Params))
	canvasCtx, _ := json.Marshal(orEmptyMap(t.CanvasCtx))
	_, err := r.pool.Exec(ctx, `
		INSERT INTO generation_tasks
			(id, user_id, request_id, task_type, model_key, prompt, ref_asset_urls, params, canvas_ctx, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID, t.UserID, t.RequestID, t.TaskType, t.ModelKey, t.Prompt, refs, params, canvasCtx, string(t.Status))
	if err != nil {
		return fmt.Errorf("insert generation_task: %w", err)
	}
	return nil
}

func (r *TaskPG) GetByID(ctx context.Context, id int64) (*domain.Task, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM generation_tasks WHERE id=$1`, id)
	return scanTask(row)
}

func (r *TaskPG) GetByUserRequestID(ctx context.Context, userID, requestID string) (*domain.Task, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+taskColumns+` FROM generation_tasks WHERE user_id=$1 AND request_id=$2`, userID, requestID)
	return scanTask(row)
}

func (r *TaskPG) List(ctx context.Context, userID, taskType string, limit int, beforeID int64) ([]*domain.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM generation_tasks WHERE user_id=$1`
	args := []any{userID}
	if taskType != "" {
		args = append(args, taskType)
		query += fmt.Sprintf(" AND task_type=$%d", len(args))
	}
	if beforeID > 0 {
		args = append(args, beforeID)
		query += fmt.Sprintf(" AND id<$%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list generation_tasks: %w", err)
	}
	defer rows.Close()
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *TaskPG) CASStatus(ctx context.Context, id int64, from, to domain.Status) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE generation_tasks SET status=$1, updated_at=now() WHERE id=$2 AND status=$3`,
		string(to), id, string(from))
	if err != nil {
		return false, fmt.Errorf("cas status %d %s→%s: %w", id, from, to, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *TaskPG) SaveProgress(ctx context.Context, id int64, progress int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE generation_tasks SET progress=$1, updated_at=now() WHERE id=$2 AND status='RUNNING'`, progress, id)
	if err != nil {
		return fmt.Errorf("save progress %d: %w", id, err)
	}
	return nil
}

func (r *TaskPG) SaveProviderJob(ctx context.Context, id int64, jobID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE generation_tasks SET provider_job_id=$1, updated_at=now() WHERE id=$2`, jobID, id)
	if err != nil {
		return fmt.Errorf("save provider job %d: %w", id, err)
	}
	return nil
}

func (r *TaskPG) Finish(ctx context.Context, id int64, status domain.Status, outputs []domain.Output, errorReason string) error {
	out, _ := json.Marshal(outputs)
	if outputs == nil {
		out = []byte("[]")
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE generation_tasks
		SET status=$1, outputs=$2, error_reason=$3, progress=CASE WHEN $1='SUCCEEDED' THEN 100 ELSE progress END, updated_at=now()
		WHERE id=$4`,
		string(status), out, errorReason, id)
	if err != nil {
		return fmt.Errorf("finish task %d: %w", id, err)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanTask(row rowScanner) (*domain.Task, error) {
	var t domain.Task
	var refs, params, canvasCtx, outputs []byte
	var status string
	err := row.Scan(&t.ID, &t.UserID, &t.RequestID, &t.TaskType, &t.ModelKey, &t.Prompt,
		&refs, &params, &canvasCtx, &status, &t.Progress, &outputs, &t.ErrorReason,
		&t.ProviderJobID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTaskNotFound
		}
		return nil, fmt.Errorf("scan generation_task: %w", err)
	}
	t.Status = domain.Status(status)
	_ = json.Unmarshal(refs, &t.RefAssetURLs)
	_ = json.Unmarshal(params, &t.Params)
	_ = json.Unmarshal(canvasCtx, &t.CanvasCtx)
	_ = json.Unmarshal(outputs, &t.Outputs)
	return &t, nil
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
