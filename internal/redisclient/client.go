package redisclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type Client struct {
	cfg Config
}

type Value struct {
	kind    byte
	str     string
	integer int64
	array   []Value
	nil     bool
}

func New(cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 5 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	return &Client{cfg: cfg}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Do(ctx, "PING")
	return err
}

func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...string) (Value, error) {
	parts := []string{"EVAL", script, strconv.Itoa(len(keys))}
	parts = append(parts, keys...)
	parts = append(parts, args...)
	return c.Do(ctx, parts...)
}

func (c *Client) Do(ctx context.Context, args ...string) (Value, error) {
	if len(args) == 0 {
		return Value{}, fmt.Errorf("redis command is required")
	}
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Addr)
	if err != nil {
		return Value{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.ReadTimeout + c.cfg.WriteTimeout))
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if c.cfg.Password != "" {
		if err := writeCommand(writer, "AUTH", c.cfg.Password); err != nil {
			return Value{}, err
		}
		if err := writer.Flush(); err != nil {
			return Value{}, err
		}
		if _, err := readValue(reader); err != nil {
			return Value{}, err
		}
	}
	if c.cfg.DB != 0 {
		if err := writeCommand(writer, "SELECT", strconv.Itoa(c.cfg.DB)); err != nil {
			return Value{}, err
		}
		if err := writer.Flush(); err != nil {
			return Value{}, err
		}
		if _, err := readValue(reader); err != nil {
			return Value{}, err
		}
	}
	if err := writeCommand(writer, args...); err != nil {
		return Value{}, err
	}
	if err := writer.Flush(); err != nil {
		return Value{}, err
	}
	return readValue(reader)
}

func (v Value) String() (string, error) {
	if v.nil {
		return "", nil
	}
	switch v.kind {
	case '+', '$':
		return v.str, nil
	case ':':
		return strconv.FormatInt(v.integer, 10), nil
	default:
		return "", fmt.Errorf("redis value kind %q is not a string", string(v.kind))
	}
}

func (v Value) Int64() (int64, error) {
	if v.nil {
		return 0, nil
	}
	switch v.kind {
	case ':':
		return v.integer, nil
	case '+', '$':
		return strconv.ParseInt(v.str, 10, 64)
	default:
		return 0, fmt.Errorf("redis value kind %q is not an integer", string(v.kind))
	}
}

func (v Value) Array() ([]Value, error) {
	if v.nil {
		return nil, nil
	}
	if v.kind != '*' {
		return nil, fmt.Errorf("redis value kind %q is not an array", string(v.kind))
	}
	return v.array, nil
}

func writeCommand(writer *bufio.Writer, args ...string) error {
	if _, err := writer.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := writer.WriteString("$" + strconv.Itoa(len(arg)) + "\r\n" + arg + "\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func readValue(reader *bufio.Reader) (Value, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return Value{}, err
	}
	line, err := readLine(reader)
	if err != nil {
		return Value{}, err
	}
	switch prefix {
	case '+':
		return Value{kind: '+', str: line}, nil
	case '-':
		return Value{}, fmt.Errorf(strings.TrimSpace(line))
	case ':':
		integer, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return Value{}, err
		}
		return Value{kind: ':', integer: integer}, nil
	case '$':
		length, err := strconv.Atoi(line)
		if err != nil {
			return Value{}, err
		}
		if length < 0 {
			return Value{kind: '$', nil: true}, nil
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return Value{}, err
		}
		return Value{kind: '$', str: string(buf[:length])}, nil
	case '*':
		count, err := strconv.Atoi(line)
		if err != nil {
			return Value{}, err
		}
		if count < 0 {
			return Value{kind: '*', nil: true}, nil
		}
		items := make([]Value, 0, count)
		for i := 0; i < count; i++ {
			item, err := readValue(reader)
			if err != nil {
				return Value{}, err
			}
			items = append(items, item)
		}
		return Value{kind: '*', array: items}, nil
	default:
		return Value{}, fmt.Errorf("unsupported redis response prefix %q", string(prefix))
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
