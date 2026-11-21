package socket

import (
	"context"
	"net"
	"testing"

	"github.com/go-gost/x/kernel"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestXrayStatsClientRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := grpc.NewServer(grpc.ForceServerCodec(rawProtoCodec{}))
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "xray.app.stats.command.StatsService",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "QueryStats",
				Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
					var req []byte
					if err := dec(&req); err != nil {
						return nil, err
					}
					// Encode Stat entries into QueryStatsResponse (field 1 repeated message).
					encodeStat := func(name string, val int64) []byte {
						var s []byte
						s = protowire.AppendTag(s, 1, protowire.BytesType)
						s = protowire.AppendString(s, name)
						s = protowire.AppendTag(s, 2, protowire.VarintType)
						s = protowire.AppendVarint(s, uint64(val))
						return s
					}
					var resp []byte
					for _, e := range []struct {
						name string
						val  int64
					}{
						{"user>>>u7@flux>>>traffic>>>uplink", 1234},
						{"user>>>u7@flux>>>traffic>>>downlink", 5678},
						{"inbound>>>in-1>>>traffic>>>uplink", 9999},
					} {
						entry := encodeStat(e.name, e.val)
						resp = protowire.AppendTag(resp, 1, protowire.BytesType)
						resp = protowire.AppendBytes(resp, entry)
					}
					return resp, nil
				},
			},
		},
	}, struct{}{})
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop()

	client := &xrayStatsClient{conns: make(map[string]*grpc.ClientConn)}
	got, err := client.query(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(got) != 2 || got[0] != (kernel.UserCounter{ID: 7, Dir: "up", Bytes: 1234}) ||
		got[1] != (kernel.UserCounter{ID: 7, Dir: "down", Bytes: 5678}) {
		t.Fatalf("unexpected decoded stats: %+v", got)
	}
}
