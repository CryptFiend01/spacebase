package pub

import (
	"net"

	"github.com/golang/protobuf/proto"
)

type ReqFunc func(conn *net.Conn, playerId int, reqId int, imsg proto.Message)

type Request struct {
	Conn     *net.Conn
	PlayerId int
	ClientIp string
	Reqid    int
	Msg      proto.Message
	Fproc    ReqFunc
}

type GateRequest struct {
	PlayerId int
	ClientIp string
	ReqId    int
	Msg      proto.Message
	Fproc    ReqFunc
}
