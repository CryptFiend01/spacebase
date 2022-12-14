package gateserver

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/CryptFiend01/spacebase/gameserver"
	"github.com/CryptFiend01/spacebase/pub"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/wonderivan/logger"
)

type GateServer struct {
	endpoint string
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	connections = map[int]*websocket.Conn{}
	connIds     = list.New()
	lock        sync.Mutex

	callbacks       map[int32]pub.ReqFunc
	IsLogMsg        bool
	CmdNameFunc     func(cmd int32) string
	PushMsgFunc     func(msg *pub.GateRequest)
	DisconnectFunc  func(playerId int)
	RegisterMsgFunc func()
	MsgProtoName    string
	PackLoginFunc   func(ct90 string, tk90 string) proto.Message
)

func NewGateServer(endpoint string) *GateServer {
	callbacks = map[int32]pub.ReqFunc{}
	return &GateServer{endpoint: endpoint}
}

func (svr *GateServer) Broadcast(cmd int32, msg proto.Message) bool {
	return SendMsg(nil, cmd, msg, 0, 0)
}

func (svr *GateServer) SendPlayer(playerId int, cmd int32, msg proto.Message) bool {
	return SendMsg(nil, cmd, msg, 0, playerId)
}

func (svr *GateServer) SendPlayerErr(playerId int, cmd int32, errNo int) bool {
	return SendError(nil, cmd, errNo, 0, playerId)
}

func (svr *GateServer) GetConnectionCount() int {
	lock.Lock()
	defer lock.Unlock()
	return len(connections)
}

func (svr *GateServer) GetMaxConnectionCount() int {
	return 60000
}

// 将游戏服数据广播给所有连接
func Broadcast(data []byte) {
	lock.Lock()
	defer lock.Unlock()

	for _, ws := range connections {
		ws.WriteMessage(websocket.BinaryMessage, data)
	}
}

// 游戏数据发给连接号为cid的客户端
func SendData(cid int, data []byte) bool {
	if cid == 0 {
		// 连接号为0的表示广播给所有连接
		Broadcast(data)
	} else {
		// 不为0的表示发送给特定连接
		lock.Lock()
		defer lock.Unlock()

		conn, ok := connections[cid]
		if ok {
			err := conn.WriteMessage(websocket.BinaryMessage, data)
			if err != nil {
				logger.Error("send to connection %d error: %v", cid, err)
				return false
			}
		} else {
			logger.Error("Error connectId %d", cid)
			return false
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

	msgLen := len(data)
	bufLen := 12 + msgLen
	buf := make([]byte, bufLen)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[2:4], uint16(msgLen))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[8:12], 0)
	if msgLen > 0 {
		copy(buf[12:], data)
	}

	if !SendData(connectId, buf) {
		logger.Error("Send message %s failed!", CmdNameFunc(cmd))
		return false
	}

	if IsLogMsg {
		cmdName := CmdNameFunc(cmd)
		if cmdName != "SHeartBeat" {
			logger.Info("Send message %s len %d total len %d.", cmdName, msgLen, len(buf))
		}
	}
	return true
}

func SendError(conn *net.Conn, cmd int32, errNo int, reqId int, connectId int) bool {
	len := 12
	buf := make([]byte, len)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(errNo))

	if !SendData(connectId, buf) {
		logger.Error("Send message %s error failed!", CmdNameFunc(cmd))
		return false
	}

	if IsLogMsg {
		if cmd != gameserver.SHeartBeatCmd {
			cmdName := CmdNameFunc(cmd)
			logger.Debug("Send error %s.", cmdName)
		}
	}
	return true
}

func Close(cid int) {
	lock.Lock()
	defer lock.Unlock()
	conn, ok := connections[cid]
	if ok {
		conn.Close()
	}
}

func CloseAll() {
	lock.Lock()
	defer lock.Unlock()

	for _, conn := range connections {
		conn.Close()
	}
}

func Check(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "success")
}

func Session(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Upgrade error: %v", err)
		return
	}

	// 为每个连接分配一个连接号
	lock.Lock()
	if connIds.Len() == 0 {
		logger.Warn("Connection is all used.")
		lock.Unlock()
		ws.Close()
		return
	}
	e := connIds.Front()
	connIds.Remove(e)
	connectId := e.Value.(int)
	connections[connectId] = ws
	lock.Unlock()

	addr := ws.RemoteAddr().String()
	i := strings.LastIndex(addr, ":")
	clientIp := addr[:i]

	var ct90, tk90 string
	c, err := r.Cookie("ct90")
	if err == nil {
		ct90 = c.Value
	}
	c, err = r.Cookie("tk90")
	if err == nil {
		tk90 = c.Value
	}

	logger.Debug("%d ip %s connect.", connectId, clientIp)
	logger.Debug("ct90: %s, tk90: %s", ct90, tk90)

	cookies := r.Cookies()
	for _, cookie := range cookies {
		logger.Debug("cookie-%s: %s", cookie.Name, cookie.Value)
	}

	logger.Debug("headers: %v", r.Header)

	errCh := make(chan bool)
	go func() {
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				logger.Error("ReadMessage error: %v", err)
				errCh <- true
				return
			}

			if mt != websocket.BinaryMessage {
				if mt == websocket.TextMessage {
					logger.Debug("recv message: %s", string(data))
					ws.WriteMessage(mt, data)
				}
			} else if mt == websocket.BinaryMessage {
				// 游戏只处理二进制数据，其余忽略
				cmd := int32(binary.LittleEndian.Uint16(data[0:2]))
				if cmd == gameserver.CHeartBeatCmd {
					SendMsg(nil, gameserver.SHeartBeatCmd, nil, 0, connectId)
					continue
				}

				if IsLogMsg {
					cmdName := CmdNameFunc(cmd)
					logger.Debug("Send error %s.", cmdName)
				}

				reqId := int(binary.LittleEndian.Uint32(data[4:8]))
				f := getCallback(cmd)
				if f == nil {
					logger.Error("Not register msg: %s", CmdNameFunc(cmd))
					continue
				}
				if cmd == gameserver.CLoginCmd {
					if tk90 != "" && ct90 != "" && PackLoginFunc != nil {
						loginMsg := PackLoginFunc(ct90, tk90)

						req := pub.GateRequest{
							PlayerId: connectId,
							ClientIp: clientIp,
							ReqId:    reqId,
							Msg:      loginMsg,
							Fproc:    f,
						}
						PushMsgFunc(&req)
						continue
					}
				}

				msgName := fmt.Sprintf("%s.%s", MsgProtoName, CmdNameFunc(cmd))
				msgType := proto.MessageType(msgName)
				if msgType == nil {
					logger.Error("not find msg %s", msgName)
					continue
				}
				msg := reflect.Indirect(reflect.New(msgType.Elem())).Addr().Interface().(proto.Message)
				err := proto.Unmarshal(data[8:], msg)
				if err != nil {
					logger.Error("Parse %s failed! err: %v", msgName, err)
					continue
				}
				req := pub.GateRequest{
					PlayerId: connectId,
					ClientIp: clientIp,
					ReqId:    reqId,
					Msg:      msg,
					Fproc:    f,
				}
				PushMsgFunc(&req)
			}
		}
	}()

	<-errCh
	close(errCh)

	// 客户端断开连接，通知游戏服务器
	req := pub.GateRequest{
		PlayerId: connectId,
		ClientIp: clientIp,
		ReqId:    0,
		Msg:      nil,
		Fproc:    onDisconnect,
	}
	PushMsgFunc(&req)

	logger.Debug("close connect %d", connectId)
	lock.Lock()
	delete(connections, connectId)
	connIds.PushBack(connectId)
	lock.Unlock()
	ws.Close()
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

func onDisconnect(conn *net.Conn, playerId int, reqId int, imsg proto.Message) {
	DisconnectFunc(playerId)
}

func check() bool {
	if RegisterMsgFunc == nil {
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

func (svr *GateServer) Start() bool {
	if !check() {
		return false
	}

	for i := 1; i <= 60000; i++ {
		connIds.PushBack(i)
	}

	RegisterMsgFunc()

	http.HandleFunc("/", Session)
	go http.ListenAndServe(svr.endpoint, nil)
	return true
}
