package gateserver

import (
	"container/list"
	"encoding/binary"
	"fmt"
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

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	connections = map[int]*websocket.Conn{}
	connIds     = list.New()
	lock        sync.Mutex

	callbacks       map[int32]pub.GateReqFunc
	IsLogMsg        bool
	CmdNameFunc     func(cmd int32) string
	PushMsgFunc     func(msg *pub.GateRequest)
	DisconnectFunc  func(playerId int)
	RegisterMsgFunc func()
	MsgProtoName    string
	PackLoginFunc   func(ct90 string, tk90 string) proto.Message
)

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

func SendMsg(cid int, cmd int32, imsg proto.Message, reqId int) bool {
	var data []byte
	var err error
	if imsg != nil {
		data, err = proto.Marshal(imsg)
		if err != nil {
			logger.Error("Pack message error! err=%s\n", err)
			return false
		}
	} else {
		data = []byte{}
	}

	msgLen := len(data)
	len := 12 + msgLen
	buf := make([]byte, len)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[2:4], uint16(msgLen))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[8:12], 0)
	if msgLen > 0 {
		copy(buf[12:], data)
	}

	if !SendData(cid, data) {
		logger.Error("Send message %s failed!", CmdNameFunc(cmd))
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

func SendError(cid int, cmd int32, errNo int, reqId int) bool {
	len := 12
	buf := make([]byte, len)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(cmd))
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(reqId))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(errNo))

	if !SendData(cid, buf) {
		logger.Error("Send error %s failed!", CmdNameFunc(cmd))
		return false
	}

	if IsLogMsg {
		if cmd != gameserver.SHeartBeatCmd {
			cmdName := CmdNameFunc(cmd)
			logger.Debug("Send message %s len 0.", cmdName)
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

func AddMsgCallback(cmd int32, f pub.GateReqFunc) {
	callbacks[cmd] = f
}

func getCallback(cmd int32) pub.GateReqFunc {
	f, ok := callbacks[cmd]
	if !ok {
		return nil
	}
	return f
}

func onDisconnect(playerId int, reqId int, imsg proto.Message) {
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

func Init(port string) bool {
	if !check() {
		return false
	}

	http.HandleFunc("/", Session)
	http.ListenAndServe(fmt.Sprintf("0.0.0.0:%s", port), nil)
	return true
}
