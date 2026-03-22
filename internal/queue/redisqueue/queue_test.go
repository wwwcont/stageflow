package redisqueue

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/redisclient"
	"stageflow/internal/worker"
)

func TestQueue_EnqueueReserveHeartbeatRequeueAndAck(t *testing.T) {
	server := newFakeRedisServer(t)
	defer server.Close()
	client, err := redisclient.New(redisclient.Config{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	queue, err := New(client, Config{Prefix: "test"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	job := worker.Job{ID: "run-1", RunID: domain.RunID("run-1"), FlowID: domain.FlowID("flow-1"), Queue: "default", EnqueuedAt: time.Unix(100, 0).UTC()}
	if err := queue.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	lease, ok, err := queue.Reserve(ctx, "default", "worker-a", time.Unix(101, 0).UTC(), 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("Reserve() error = %v, ok = %v", err, ok)
	}
	lease, err = queue.Heartbeat(ctx, lease, time.Unix(105, 0).UTC(), 10*time.Second)
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if err := queue.Release(ctx, lease, time.Unix(110, 0).UTC()); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, ok, err := queue.Reserve(ctx, "default", "worker-b", time.Unix(109, 0).UTC(), time.Second); err != nil || ok {
		t.Fatalf("Reserve(before visible) = %v, %v", ok, err)
	}
	lease, ok, err = queue.Reserve(ctx, "default", "worker-b", time.Unix(110, 0).UTC(), time.Second)
	if err != nil || !ok {
		t.Fatalf("Reserve(after visible) error = %v, ok = %v", err, ok)
	}
	if err := queue.Ack(ctx, lease); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if _, ok, err := queue.Reserve(ctx, "default", "worker-c", time.Unix(111, 0).UTC(), time.Second); err != nil || ok {
		t.Fatalf("Reserve(after ack) = %v, %v", ok, err)
	}
}

func TestQueue_RequeueExpired(t *testing.T) {
	server := newFakeRedisServer(t)
	defer server.Close()
	client, err := redisclient.New(redisclient.Config{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	queue, err := New(client, Config{Prefix: "test"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	job := worker.Job{ID: "run-2", RunID: domain.RunID("run-2"), FlowID: domain.FlowID("flow-2"), Queue: "default", EnqueuedAt: time.Unix(200, 0).UTC()}
	if err := queue.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, ok, err := queue.Reserve(ctx, "default", "worker-a", time.Unix(201, 0).UTC(), time.Second); err != nil || !ok {
		t.Fatalf("Reserve() error = %v, ok = %v", err, ok)
	}
	count, err := queue.RequeueExpired(ctx, "default", time.Unix(203, 0).UTC(), 10)
	if err != nil {
		t.Fatalf("RequeueExpired() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	lease, ok, err := queue.Reserve(ctx, "default", "worker-b", time.Unix(204, 0).UTC(), time.Second)
	if err != nil || !ok || lease.Job.ID != job.ID {
		t.Fatalf("Reserve() after requeue = %#v, %v, %v", lease, ok, err)
	}
}

type fakeRedisServer struct {
	listener net.Listener
	mu       sync.Mutex
	hashes   map[string]map[string]string
	zsets    map[string]map[string]int64
}

func newFakeRedisServer(t *testing.T) *fakeRedisServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := &fakeRedisServer{listener: listener, hashes: map[string]map[string]string{}, zsets: map[string]map[string]int64{}}
	go server.serve()
	return server
}

func (s *fakeRedisServer) Addr() string { return s.listener.Addr().String() }
func (s *fakeRedisServer) Close()       { _ = s.listener.Close() }

func (s *fakeRedisServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeRedisServer) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		args, err := readCommand(reader)
		if err != nil {
			if err != io.EOF {
				_ = writeError(writer, err)
				_ = writer.Flush()
			}
			return
		}
		if len(args) == 0 {
			_ = writeError(writer, fmt.Errorf("empty command"))
			_ = writer.Flush()
			return
		}
		cmd := strings.ToUpper(args[0])
		switch cmd {
		case "PING", "AUTH", "SELECT":
			_ = writeSimpleString(writer, "OK")
		case "EVAL":
			value, evalErr := s.eval(args[1:])
			if evalErr != nil {
				_ = writeError(writer, evalErr)
			} else {
				_ = writeResp(writer, value)
			}
		default:
			_ = writeError(writer, fmt.Errorf("unsupported command %s", cmd))
		}
		_ = writer.Flush()
	}
}

func (s *fakeRedisServer) eval(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("invalid eval args")
	}
	script := args[0]
	numKeys, err := strconv.Atoi(args[1])
	if err != nil {
		return nil, err
	}
	if len(args) < 2+numKeys {
		return nil, fmt.Errorf("not enough eval args")
	}
	keys := args[2 : 2+numKeys]
	argv := args[2+numKeys:]
	s.mu.Lock()
	defer s.mu.Unlock()
	switch script {
	case enqueueScript:
		return s.evalEnqueue(keys, argv)
	case reserveScript:
		return s.evalReserve(keys, argv)
	case ackScript:
		return s.evalAck(keys, argv)
	case heartbeatScript:
		return s.evalHeartbeat(keys, argv)
	case releaseScript:
		return s.evalRelease(keys, argv)
	case requeueExpiredScript:
		return s.evalRequeueExpired(keys, argv)
	default:
		return nil, fmt.Errorf("unsupported script")
	}
}

func (s *fakeRedisServer) evalEnqueue(keys, argv []string) (any, error) {
	readyKey, jobsKey, inflightKey := keys[0], keys[1], keys[2]
	jobID, payload := argv[0], argv[1]
	score, _ := strconv.ParseInt(argv[2], 10, 64)
	if s.hexists(jobsKey, jobID) || s.zscore(readyKey, jobID) != nil || s.zscore(inflightKey, jobID) != nil {
		return int64(0), nil
	}
	s.hset(jobsKey, jobID, payload)
	s.zadd(readyKey, jobID, score)
	return int64(1), nil
}

func (s *fakeRedisServer) evalReserve(keys, argv []string) (any, error) {
	readyKey, inflightKey, jobsKey, ownersKey := keys[0], keys[1], keys[2], keys[3]
	now, _ := strconv.ParseInt(argv[0], 10, 64)
	leaseUntil, _ := strconv.ParseInt(argv[1], 10, 64)
	workerID := argv[2]
	entries := s.zrangeByScore(readyKey, now, 1)
	if len(entries) == 0 {
		return []any{}, nil
	}
	jobID := entries[0]
	if !s.zrem(readyKey, jobID) {
		return []any{}, nil
	}
	payload, ok := s.hget(jobsKey, jobID)
	if !ok {
		return []any{}, nil
	}
	s.zadd(inflightKey, jobID, leaseUntil)
	s.hset(ownersKey, jobID, workerID)
	return []any{jobID, payload}, nil
}

func (s *fakeRedisServer) evalAck(keys, argv []string) (any, error) {
	inflightKey, jobsKey, ownersKey := keys[0], keys[1], keys[2]
	jobID, workerID := argv[0], argv[1]
	if owner, ok := s.hget(ownersKey, jobID); ok && owner != workerID {
		return int64(0), nil
	}
	s.zrem(inflightKey, jobID)
	s.hdel(jobsKey, jobID)
	s.hdel(ownersKey, jobID)
	return int64(1), nil
}

func (s *fakeRedisServer) evalHeartbeat(keys, argv []string) (any, error) {
	inflightKey, ownersKey := keys[0], keys[1]
	jobID, workerID := argv[0], argv[1]
	leaseUntil, _ := strconv.ParseInt(argv[2], 10, 64)
	owner, ok := s.hget(ownersKey, jobID)
	if !ok || owner != workerID || s.zscore(inflightKey, jobID) == nil {
		return int64(0), nil
	}
	s.zadd(inflightKey, jobID, leaseUntil)
	return int64(1), nil
}

func (s *fakeRedisServer) evalRelease(keys, argv []string) (any, error) {
	readyKey, inflightKey, ownersKey := keys[0], keys[1], keys[2]
	jobID, workerID := argv[0], argv[1]
	availableAt, _ := strconv.ParseInt(argv[2], 10, 64)
	if owner, ok := s.hget(ownersKey, jobID); ok && owner != workerID {
		return int64(0), nil
	}
	s.zrem(inflightKey, jobID)
	s.hdel(ownersKey, jobID)
	s.zadd(readyKey, jobID, availableAt)
	return int64(1), nil
}

func (s *fakeRedisServer) evalRequeueExpired(keys, argv []string) (any, error) {
	readyKey, inflightKey, ownersKey := keys[0], keys[1], keys[2]
	now, _ := strconv.ParseInt(argv[0], 10, 64)
	limit, _ := strconv.Atoi(argv[1])
	expired := s.zrangeByScore(inflightKey, now, limit)
	count := int64(0)
	for _, jobID := range expired {
		if s.zrem(inflightKey, jobID) {
			s.hdel(ownersKey, jobID)
			s.zadd(readyKey, jobID, now)
			count++
		}
	}
	return count, nil
}

func (s *fakeRedisServer) hset(key, field, value string) {
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	s.hashes[key][field] = value
}
func (s *fakeRedisServer) hget(key, field string) (string, bool) {
	value, ok := s.hashes[key][field]
	return value, ok
}
func (s *fakeRedisServer) hdel(key, field string) {
	if s.hashes[key] != nil {
		delete(s.hashes[key], field)
	}
}
func (s *fakeRedisServer) hexists(key, field string) bool {
	_, ok := s.hget(key, field)
	return ok
}
func (s *fakeRedisServer) zadd(key, member string, score int64) {
	if s.zsets[key] == nil {
		s.zsets[key] = map[string]int64{}
	}
	s.zsets[key][member] = score
}
func (s *fakeRedisServer) zscore(key, member string) *int64 {
	if s.zsets[key] == nil {
		return nil
	}
	score, ok := s.zsets[key][member]
	if !ok {
		return nil
	}
	return &score
}
func (s *fakeRedisServer) zrem(key, member string) bool {
	if s.zsets[key] == nil {
		return false
	}
	_, ok := s.zsets[key][member]
	delete(s.zsets[key], member)
	return ok
}
func (s *fakeRedisServer) zrangeByScore(key string, max int64, limit int) []string {
	if s.zsets[key] == nil {
		return nil
	}
	type pair struct {
		member string
		score  int64
	}
	pairs := make([]pair, 0)
	for member, score := range s.zsets[key] {
		if score <= max {
			pairs = append(pairs, pair{member: member, score: score})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			return pairs[i].member < pairs[j].member
		}
		return pairs[i].score < pairs[j].score
	})
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	result := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		result = append(result, pair.member)
	}
	return result
}

func readCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if line == "" || line[0] != '*' {
		return nil, fmt.Errorf("expected array")
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if header == "" || header[0] != '$' {
			return nil, fmt.Errorf("expected bulk string")
		}
		length, err := strconv.Atoi(strings.TrimSpace(header[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:length]))
	}
	return args, nil
}

func writeSimpleString(writer *bufio.Writer, value string) error {
	_, err := writer.WriteString("+" + value + "\r\n")
	return err
}
func writeError(writer *bufio.Writer, err error) error {
	_, writeErr := writer.WriteString("-ERR " + err.Error() + "\r\n")
	return writeErr
}
func writeResp(writer *bufio.Writer, value any) error {
	switch typed := value.(type) {
	case int64:
		_, err := writer.WriteString(":" + strconv.FormatInt(typed, 10) + "\r\n")
		return err
	case string:
		_, err := writer.WriteString("$" + strconv.Itoa(len(typed)) + "\r\n" + typed + "\r\n")
		return err
	case []any:
		if _, err := writer.WriteString("*" + strconv.Itoa(len(typed)) + "\r\n"); err != nil {
			return err
		}
		for _, item := range typed {
			if err := writeResp(writer, item); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := writer.WriteString("*0\r\n")
		return err
	}
}
