package gameserver

import (
	"encoding/binary"
	"fmt"
	"net"
	"reflect"

	"github.com/CryptFiend01/spacebase/pub"

	"github.com/golang/protobuf/proto"
	"github.com/wonderivan/logger"
)

type Session struct {
	conn        *net.Conn
	listener    *net.TCPListener
	server      *GameServer
	sessionId   int
	isWebSocket bool
	players     map[int]bool
	playerNum   int
}

func NewSession(listener *net.TCPListener, conn *net.Conn, s *GameServer, sessionId int) *Session {
	return &Session{
		conn:        conn,
		listener:    listener,
		server:      s,
		sessionId:   sessionId,
		isWebSocket: false,
		players:     map[int]bool{},
		playerNum:   0,
	}
}

func (sess *Session) handleRead() {
	defer func() {
		logger.Info("Connection lose.")
		(*sess.conn).Close()

		for playerId := range sess.players {
			req := &pub.Request{
				Conn:     sess.conn,
				PlayerId: playerId,
				Reqid:    0,
				Msg:      nil,
				Fproc:    onPlayerDisconnect,
			}
			PushMsgFunc(req)
		}

		delete(sess.server.sessions, sess.sessionId)
	}()

	for {
		data, reqId, connectId, clientIp := readData(sess.conn)
		if data == nil {
			return
		}

		cmd := int32(binary.LittleEndian.Uint16(data[4:6]))
		dataLen := binary.LittleEndian.Uint16(data[6:8])
		cmdName := CmdNameFunc(cmd)
		if cmdName != "CHeartBeat" && IsLogMsg {
			logger.Info("Receive GameServer message %s", cmdName)
		}

		if cmd == CHeartBeatCmd {
			onHeartBeat(sess.conn, connectId, reqId, data)
		} else {
			f := getCallback(cmd)
			if f == nil {
				logger.Error("Not register msg: %s", cmdName)
				continue
			}

			// 可能有多个gate，为了防止playerId冲突，在其基础上加上sessionId*100000
			playerId := connectId + sess.sessionId*100000

			if cmd == CLoginCmd {
				sess.players[playerId] = true
				sess.playerNum += 1
			} else if cmd == SDisconnectCmd {
				delete(sess.players, playerId)
				sess.playerNum = len(sess.players)
			}

			if dataLen > 0 {
				msgName := fmt.Sprintf("bustabit_msg.%s", cmdName)
				msgType := proto.MessageType(msgName)
				if msgType == nil {
					logger.Error("not find msg %s", msgName)
					continue
				}
				msg := reflect.Indirect(reflect.New(msgType.Elem())).Addr().Interface().(proto.Message)
				err := proto.Unmarshal(data[12:], msg)
				if err != nil {
					logger.Error("Parse %s failed! err: %v", msgName, err)
					continue
				}

				req := &pub.Request{
					Conn:     sess.conn,
					PlayerId: playerId,
					ClientIp: clientIp,
					Reqid:    reqId,
					Msg:      msg,
					Fproc:    f,
				}
				PushMsgFunc(req)
			} else {
				req := &pub.Request{
					Conn:     sess.conn,
					PlayerId: playerId,
					Reqid:    reqId,
					Msg:      nil,
					Fproc:    f,
				}
				PushMsgFunc(req)
			}
		}
	}
}

func onPlayerDisconnect(conn *net.Conn, playerId int, reqId int, imsg proto.Message) {
	DisconnectFunc(playerId)
}
