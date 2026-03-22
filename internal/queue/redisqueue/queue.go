package redisqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"stageflow/internal/redisclient"
	"stageflow/internal/worker"
)

type Config struct {
	Prefix string
}

type Queue struct {
	client *redisclient.Client
	prefix string
}

func New(client *redisclient.Client, cfg Config) (*Queue, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "stageflow"
	}
	return &Queue{client: client, prefix: prefix}, nil
}

func (q *Queue) Enqueue(ctx context.Context, job worker.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job %q: %w", job.ID, err)
	}
	_, err = q.client.Eval(ctx, enqueueScript, []string{q.readyKey(job.Queue), q.jobsKey(job.Queue), q.inflightKey(job.Queue)}, job.ID, string(payload), strconv.FormatInt(job.EnqueuedAt.UTC().UnixMilli(), 10))
	if err != nil {
		return fmt.Errorf("enqueue job %q: %w", job.ID, err)
	}
	return nil
}

func (q *Queue) Reserve(ctx context.Context, queueName, workerID string, now time.Time, leaseDuration time.Duration) (worker.Lease, bool, error) {
	value, err := q.client.Eval(ctx, reserveScript, []string{q.readyKey(queueName), q.inflightKey(queueName), q.jobsKey(queueName), q.ownersKey(queueName)}, strconv.FormatInt(now.UTC().UnixMilli(), 10), strconv.FormatInt(now.UTC().Add(leaseDuration).UnixMilli(), 10), workerID)
	if err != nil {
		return worker.Lease{}, false, fmt.Errorf("reserve job: %w", err)
	}
	items, err := value.Array()
	if err != nil {
		return worker.Lease{}, false, err
	}
	if len(items) == 0 {
		return worker.Lease{}, false, nil
	}
	payload, err := items[1].String()
	if err != nil {
		return worker.Lease{}, false, err
	}
	job := worker.Job{}
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		return worker.Lease{}, false, fmt.Errorf("decode reserved job: %w", err)
	}
	return worker.Lease{Job: job, WorkerID: workerID, LeaseExpiresAt: now.UTC().Add(leaseDuration)}, true, nil
}

func (q *Queue) Ack(ctx context.Context, lease worker.Lease) error {
	_, err := q.client.Eval(ctx, ackScript, []string{q.inflightKey(lease.Job.Queue), q.jobsKey(lease.Job.Queue), q.ownersKey(lease.Job.Queue)}, lease.Job.ID, lease.WorkerID)
	if err != nil {
		return fmt.Errorf("ack job %q: %w", lease.Job.ID, err)
	}
	return nil
}

func (q *Queue) Heartbeat(ctx context.Context, lease worker.Lease, now time.Time, leaseDuration time.Duration) (worker.Lease, error) {
	value, err := q.client.Eval(ctx, heartbeatScript, []string{q.inflightKey(lease.Job.Queue), q.ownersKey(lease.Job.Queue)}, lease.Job.ID, lease.WorkerID, strconv.FormatInt(now.UTC().Add(leaseDuration).UnixMilli(), 10))
	if err != nil {
		return worker.Lease{}, fmt.Errorf("heartbeat job %q: %w", lease.Job.ID, err)
	}
	updated, err := value.Int64()
	if err != nil {
		return worker.Lease{}, err
	}
	if updated == 0 {
		return worker.Lease{}, fmt.Errorf("job %q lease is no longer owned by worker %q", lease.Job.ID, lease.WorkerID)
	}
	lease.LeaseExpiresAt = now.UTC().Add(leaseDuration)
	return lease, nil
}

func (q *Queue) Release(ctx context.Context, lease worker.Lease, availableAt time.Time) error {
	_, err := q.client.Eval(ctx, releaseScript, []string{q.readyKey(lease.Job.Queue), q.inflightKey(lease.Job.Queue), q.ownersKey(lease.Job.Queue)}, lease.Job.ID, lease.WorkerID, strconv.FormatInt(availableAt.UTC().UnixMilli(), 10))
	if err != nil {
		return fmt.Errorf("release job %q: %w", lease.Job.ID, err)
	}
	return nil
}

func (q *Queue) RequeueExpired(ctx context.Context, queueName string, now time.Time, limit int) (int, error) {
	value, err := q.client.Eval(ctx, requeueExpiredScript, []string{q.readyKey(queueName), q.inflightKey(queueName), q.ownersKey(queueName)}, strconv.FormatInt(now.UTC().UnixMilli(), 10), strconv.Itoa(limit))
	if err != nil {
		return 0, fmt.Errorf("requeue expired jobs: %w", err)
	}
	count, err := value.Int64()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func (q *Queue) readyKey(queueName string) string {
	return fmt.Sprintf("%s:queue:%s:ready", q.prefix, queueName)
}
func (q *Queue) inflightKey(queueName string) string {
	return fmt.Sprintf("%s:queue:%s:inflight", q.prefix, queueName)
}
func (q *Queue) jobsKey(queueName string) string {
	return fmt.Sprintf("%s:queue:%s:jobs", q.prefix, queueName)
}
func (q *Queue) ownersKey(queueName string) string {
	return fmt.Sprintf("%s:queue:%s:owners", q.prefix, queueName)
}

const enqueueScript = `
local readyKey = KEYS[1]
local jobsKey = KEYS[2]
local inflightKey = KEYS[3]
local jobID = ARGV[1]
local payload = ARGV[2]
local score = tonumber(ARGV[3])
if redis.call('HEXISTS', jobsKey, jobID) == 1 or redis.call('ZSCORE', readyKey, jobID) or redis.call('ZSCORE', inflightKey, jobID) then
  return 0
end
redis.call('HSET', jobsKey, jobID, payload)
redis.call('ZADD', readyKey, score, jobID)
return 1
`

const reserveScript = `
local readyKey = KEYS[1]
local inflightKey = KEYS[2]
local jobsKey = KEYS[3]
local ownersKey = KEYS[4]
local now = tonumber(ARGV[1])
local leaseUntil = tonumber(ARGV[2])
local workerID = ARGV[3]
local entries = redis.call('ZRANGEBYSCORE', readyKey, '-inf', now, 'LIMIT', 0, 1)
if #entries == 0 then
  return {}
end
local jobID = entries[1]
if redis.call('ZREM', readyKey, jobID) == 0 then
  return {}
end
local payload = redis.call('HGET', jobsKey, jobID)
if not payload then
  return {}
end
redis.call('ZADD', inflightKey, leaseUntil, jobID)
redis.call('HSET', ownersKey, jobID, workerID)
return {jobID, payload}
`

const ackScript = `
local inflightKey = KEYS[1]
local jobsKey = KEYS[2]
local ownersKey = KEYS[3]
local jobID = ARGV[1]
local workerID = ARGV[2]
local owner = redis.call('HGET', ownersKey, jobID)
if owner and owner ~= workerID then
  return 0
end
redis.call('ZREM', inflightKey, jobID)
redis.call('HDEL', jobsKey, jobID)
redis.call('HDEL', ownersKey, jobID)
return 1
`

const heartbeatScript = `
local inflightKey = KEYS[1]
local ownersKey = KEYS[2]
local jobID = ARGV[1]
local workerID = ARGV[2]
local leaseUntil = tonumber(ARGV[3])
local owner = redis.call('HGET', ownersKey, jobID)
if not owner or owner ~= workerID then
  return 0
end
if not redis.call('ZSCORE', inflightKey, jobID) then
  return 0
end
redis.call('ZADD', inflightKey, leaseUntil, jobID)
return 1
`

const releaseScript = `
local readyKey = KEYS[1]
local inflightKey = KEYS[2]
local ownersKey = KEYS[3]
local jobID = ARGV[1]
local workerID = ARGV[2]
local availableAt = tonumber(ARGV[3])
local owner = redis.call('HGET', ownersKey, jobID)
if owner and owner ~= workerID then
  return 0
end
redis.call('ZREM', inflightKey, jobID)
redis.call('HDEL', ownersKey, jobID)
redis.call('ZADD', readyKey, availableAt, jobID)
return 1
`

const requeueExpiredScript = `
local readyKey = KEYS[1]
local inflightKey = KEYS[2]
local ownersKey = KEYS[3]
local now = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local expired = redis.call('ZRANGEBYSCORE', inflightKey, '-inf', now, 'LIMIT', 0, limit)
local count = 0
for _, jobID in ipairs(expired) do
  if redis.call('ZREM', inflightKey, jobID) == 1 then
    redis.call('HDEL', ownersKey, jobID)
    redis.call('ZADD', readyKey, now, jobID)
    count = count + 1
  end
end
return count
`

var _ worker.Queue = (*Queue)(nil)
