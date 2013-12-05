package goredis_server

import (
	. "../goredis"
	"net"
)

// 从主库获取数据
// 对应 go_redis_server_sync.go
func (server *GoRedisServer) OnSLAVEOF(cmd *Command) (reply *Reply) {
	// connect to master
	host := cmd.StringAtIndex(1)
	port := cmd.StringAtIndex(2)
	hostPort := host + ":" + port

	conn, err := net.Dial("tcp", hostPort)
	if err != nil {
		reply = ErrorReply(err)
		return
	}
	reply = StatusReply("OK")
	// 异步处理
	session := NewSession(conn)
	slaveSession := NewSlaveSession(session)
	slaveClient := NewSlaveSessionClient(server, session)
	go slaveClient.Start()
	return
}
