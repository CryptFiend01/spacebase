package gameserver

import (
	"encoding/binary"
	"net"

	"github.com/CryptFiend01/spacebase/pub"

	"github.com/golang/protobuf/proto"
	"github.com/wonderivan/logger"
)

type GameServer struct {
	endpoint string
	sessions map[int]*Session
}

const (
	CLoginCmd      = 1
	SLoginCmd      = 2
	SDisconnectCmd = 3
	CHeartBeatCmd  = 4
	SHeartBeatCmd  = 5
)

var (
	callbacks       map[int32]pub.ReqFunc
	svr             *GameServer
	IsLogMsg        bool
	CmdNameFunc     func(cmd int32) string
	PushMsgFunc     func(msg *pub.Request)
	DisconnectFunc  func(playerId int)
	RegisterMsgFunc func()
	MsgProtoName    string
)

func NewGameServer(endpoint string) *GameServer {
	svr = &GameServer{
		endpoint: endpoint,
		sessions: map[int]*Session{},
	}
	return svr
}

func check() bool {
	if RegisterMsgFunc != nil {
		logger.Error("forget to set RegisterMsgFunc.")
		return false
	}

	if MsgProtoName == "" {
		logger.Error("forget to set MsgProtoName.")
		return false
	}

	if CmdNameFunc == nil {
		logger.Error("forget to set CmdNameFunc.")
		return false
	}

	if PushMsgFunc == nil {
		logger.Error("forget to set PushMsgFunc.")
		return false
	}

	if DisconnectFunc == nil {
		logger.Error("forget to set DisconnectFunc.")
		return false
	}
	return true
}

func (svr *GameServer) Start() bool {
	if !check() {
		return false
	}

	RegisterMsgFunc()

	addr, err := net.ResolveTCPAddr("tcp4", svr.endpoint)
	if err != nil {
		logger.Error("Resolve addr failed! err=%s", err)
		return false
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		logger.Error("Start server failed! err=%s", err)
		return false
	}
	go svr.handleAccept(listener)
	return true
}

func (svr *GameServer) handleAccept(listener *net.TCPListener) {
	defer listener.Close()

	sessionIdGen := 1
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Accept error: %v, server close.", err)
			return
		}

		session := NewSession(listener, &conn, svr, sessionIdGen)
		svr.sessions[session.sessionId] = session
		sessionIdGen += 1
		go session.handleRead()
	}
}

func readLen(conn *net.Conn, buf []byte, num int) error {
	rest := num
	offset := 0
	for rest > 0 {
		n, err := (*conn).Read(buf[offset:])
		if err != nil {
			return err
		}

		rest -= n
		offset += n
	}
	return nil
}

// 读取网关转发的客户端数据
func readData(conn *net.Conn) ([]byte, int, int, string) {
	buf := make([]byte, 256)
	headLen := 12
	err := readLen(conn, buf[0:headLen], headLen)
	if err != nil {
		logger.Error("read header error: %v", err)
		return nil, 0, 0, ""
	}

	connectId := int(binary.LittleEndian.Uint32(buf[0:4]))
	dataLen := int(binary.LittleEndian.Uint16(buf[6:8]))
	reqId := int(binary.LittleEndian.Uint32(buf[8:12]))
	if dataLen > 256-headLen {
		logger.Error("data len %d is over max buf size.", dataLen)
		return nil, 0, 0, ""
	}
	if dataLen > 0 {
		err := readLen(conn, buf[headLen:headLen+dataLen], dataLen)
		if err != nil {
			logger.Error("read data error! err=%s", err)
			return nil, 0, 0, ""
		}
	}

	// 读取IP地址数据
	bip := readIp(conn)
	clientIp := ""
	if bip != nil {
		clientIp = string(bip)
	}
	return buf[:headLen+dataLen], reqId & 0xffff, connectId, clientIp
}

func readIp(conn *net.Conn) []byte {
	buf := make([]byte, 20)
	err := readLen(conn, buf[0:2], 2)
	if err != nil {
		logger.Error("read ip len error: %v", err)
		return nil
	}

	ipLen := binary.LittleEndian.Uint16(buf[0:2])
	if ipLen == 0 {
		return nil
	}

	err = readLen(conn, buf[2:ipLen+2], int(ipLen))
	if err != nil {
		logger.Error("read ip error: %v", err)
		return nil
	}

	return buf[2 : ipLen+2]
}

func sendData(conn *net.Conn, cmd int32, data []byte, reqId int, connectId int) bool {
	var err error
	msgLen := len(data)
	len := 16 + msgLen
	buf := make([]byte, len)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(connectId%100000))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[6:8], uint16(msgLen))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[12:16], 0)
	if msgLen > 0 {
		copy(buf[16:], data)
	}
	_, err = (*conn).Write(buf)
	if err != nil {
		logger.Error("Send message %s failed! err=%s\n", CmdNameFunc(cmd), err)
		return false
	}

	if IsLogMsg {
		cmdName := CmdNameFunc(cmd)
		if cmdName != "SHeartBeat" {
			logger.Info("Send message %s len %d.", cmdName, msgLen)
		}
	}
	return true
}

func SendError(conn *net.Conn, cmd int32, errNo int, reqId int, connectId int) bool {
	len := 16
	buf := make([]byte, len)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(connectId%100000))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[6:8], 0)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(errNo))

	_, err := (*conn).Write(buf)
	if err != nil {
		logger.Error("Send message %s failed! err=%s\n", CmdNameFunc(cmd), err)
		return false
	}

	if IsLogMsg {
		if cmd != SHeartBeatCmd {
			cmdName := CmdNameFunc(cmd)
			logger.Debug("Send message %s len 0.", cmdName)
		}
	}
	return true
}

func SendMsg(conn *net.Conn, cmd int32, msg proto.Message, reqId int, connectId int) bool {
	var data []byte
	var err error
	if msg != nil {
		data, err = proto.Marshal(msg)
		if err != nil {
			logger.Error("Pack message error! err=%s\n", err)
			return false
		}
	} else {
		data = []byte{}
	}

	return sendData(conn, cmd, data, reqId, connectId)
}

func (svr *GameServer) Broadcast(cmd int32, msg proto.Message) bool {
	var data []byte
	var err error
	if msg != nil {
		data, err = proto.Marshal(msg)
		if err != nil {
			logger.Error("Pack message error! err=%s\n", err)
			return false
		}
	} else {
		data = []byte{}
	}
	msgLen := len(data)
	totalLen := 16 + msgLen
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(0))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[6:8], uint16(msgLen))
	binary.LittleEndian.PutUint32(buf[8:12], 0)
	binary.LittleEndian.PutUint32(buf[12:16], 0)
	if msgLen > 0 {
		copy(buf[16:], data)
	}

	n := 0
	for _, sess := range svr.sessions {
		(*sess.conn).Write(buf)
		n += sess.playerNum
	}

	if IsLogMsg && n > 0 {
		cmdName := CmdNameFunc(cmd)
		logger.Info("Broadcast %s to %d client.", cmdName, n)
	}
	return true
}

func (svr *GameServer) SendPlayer(playerId int, cmd int32, msg proto.Message) bool {
	if playerId == 0 {
		return false
	}

	sess, ok := svr.sessions[int(playerId/100000)]
	if !ok {
		return false
	}

	SendMsg(sess.conn, cmd, msg, 0, playerId)
	return true
}

func (svr *GameServer) SendPlayerErr(playerId int, cmd int32, errNo int) bool {
	sess, ok := svr.sessions[int(playerId/100000)]
	if !ok {
		return false
	}
	SendError(sess.conn, cmd, errNo, 0, playerId)
	return true
}

func (svr *GameServer) GetConnectionCount() int {
	n := 0
	for _, sess := range svr.sessions {
		n += sess.playerNum
	}
	return n
}

func (svr *GameServer) GetMaxConnectionCount() int {
	n := 0
	for _, sess := range svr.sessions {
		if sess.playerNum > n {
			n = sess.playerNum
		}
	}
	return n
}

func onHeartBeat(conn *net.Conn, connectId int, reqId int, data []byte) {
	SendMsg(conn, SHeartBeatCmd, nil, 0, connectId)
}

func AddMsgCallback(cmd int32, f pub.ReqFunc) {
	callbacks[cmd] = f
}

func getCallback(cmd int32) pub.ReqFunc {
	f, ok := callbacks[cmd]
	if !ok {
		return nil
	}
	return f
}
