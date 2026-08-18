package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// 任务队列命名（docs/04 §1.5：tommax.<domain>.<queue>-queue 的 asynq 视角）。
const (
	TypeDispatch  = "generation:dispatch"
	QueueDispatch = "generation"
)

type DispatchPayload struct {
	TaskID int64 `json:"taskId"`
}

// QueueAsynq 实现 service.Enqueuer。
type QueueAsynq struct {
	client   *asynq.Client
	maxRetry int
	timeout  time.Duration
}

func NewQueueAsynq(redisAddr string, maxRetry int, timeout time.Duration) *QueueAsynq {
	return &QueueAsynq{
		client:   asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr}),
		maxRetry: maxRetry,
		timeout:  timeout,
	}
}

func (q *QueueAsynq) EnqueueDispatch(ctx context.Context, taskID int64) error {
	payload, _ := json.Marshal(DispatchPayload{TaskID: taskID})
	_, err := q.client.EnqueueContext(ctx,
		asynq.NewTask(TypeDispatch, payload),
		asynq.Queue(QueueDispatch),
		asynq.MaxRetry(q.maxRetry),
		asynq.Timeout(q.timeout),
		// 幂等：同 task 重复入队直接去重（worker 侧另有 CAS 兜底）。
		asynq.TaskID(fmt.Sprintf("dispatch-%d", taskID)),
	)
	if err != nil && err != asynq.ErrTaskIDConflict {
		return fmt.Errorf("enqueue dispatch %d: %w", taskID, err)
	}
	return nil
}

func (q *QueueAsynq) Close() error { return q.client.Close() }
