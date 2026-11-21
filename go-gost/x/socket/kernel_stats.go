package socket

import (
	"context"
	"sync"

	"github.com/go-gost/x/kernel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/encoding/protowire"
)

const xrayQueryStatsMethod = "/xray.app.stats.command.StatsService/QueryStats"

// RegisterKernelStats wires the gRPC StatsService collector into the default
// kernel manager for the full agent (cmd/agent-lite skips calling this).
func RegisterKernelStats(m *kernel.Manager) {
	if m == nil {
		return
	}
	c := &xrayStatsClient{conns: make(map[string]*grpc.ClientConn)}
	m.SetStatsQuerier(c.query)
}

type xrayStatsClient struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func (c *xrayStatsClient) conn(addr string) (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cc := c.conns[addr]; cc != nil {
		return cc, nil
	}
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	c.conns[addr] = cc
	return cc, nil
}

func (c *xrayStatsClient) query(ctx context.Context, addr string) ([]kernel.UserCounter, error) {
	cc, err := c.conn(addr)
	if err != nil {
		return nil, err
	}
	req := encodeQueryStatsRequest("user>>>", true)
	var resp []byte
	if err := cc.Invoke(ctx, xrayQueryStatsMethod, req, &resp, grpc.ForceCodec(rawProtoCodec{})); err != nil {
		return nil, err
	}
	return decodeQueryStatsResponse(resp)
}

type rawProtoCodec struct{}

func (rawProtoCodec) Name() string { return "proto" }

func (rawProtoCodec) Marshal(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return encoding.GetCodec("proto").Marshal(v)
}

func (rawProtoCodec) Unmarshal(data []byte, v any) error {
	if out, ok := v.(*[]byte); ok {
		*out = append((*out)[:0], data...)
		return nil
	}
	return encoding.GetCodec("proto").Unmarshal(data, v)
}

// encodeQueryStatsRequest builds xray.app.stats.command.QueryStatsRequest:
// field 1 (string pattern), field 2 (bool reset).
func encodeQueryStatsRequest(pattern string, reset bool) []byte {
	var b []byte
	if pattern != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, pattern)
	}
	if reset {
		b = protowire.AppendTag(b, 2, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	return b
}

// decodeQueryStatsResponse parses xray.app.stats.command.QueryStatsResponse:
// repeated Stat stat = 1 (name = 1, value = 2).
func decodeQueryStatsResponse(b []byte) ([]kernel.UserCounter, error) {
	var out []kernel.UserCounter
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			msg, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return nil, protowire.ParseError(m)
			}
			b = b[m:]
			if name, val, err := decodeStatEntry(msg); err == nil {
				if id, dir, ok := kernel.ParseXrayUserStat(name); ok && val > 0 {
					out = append(out, kernel.UserCounter{ID: id, Dir: dir, Bytes: val})
				}
			} else {
				return nil, err
			}
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return nil, protowire.ParseError(m)
		}
		b = b[m:]
	}
	return out, nil
}

func decodeStatEntry(b []byte) (string, int64, error) {
	var name string
	var val int64
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return "", 0, protowire.ParseError(n)
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.BytesType:
			s, m := protowire.ConsumeString(b)
			if m < 0 {
				return "", 0, protowire.ParseError(m)
			}
			name = s
			b = b[m:]
		case num == 2 && typ == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return "", 0, protowire.ParseError(m)
			}
			val = int64(v)
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return "", 0, protowire.ParseError(m)
			}
			b = b[m:]
		}
	}
	return name, val, nil
}
