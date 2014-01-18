package goredis_server

// import (
// 	. "../goredis"
// 	"./libs/levelredis"
// 	// "fmt"
// 	"github.com/latermoon/levigo"
// 	// "strings"
// 	"sync"
// )

// // 主从同步中的主库连接
// // new slave: sendSnapshot -> remoteRunloop -> aofRunloop -> aofToRemoteRunloop -> remoteRunloop
// // old slave: aofRunloop -> aofToRemoteRunloop -> remoteRunloop
// type SlaveSessionServer struct {
// 	session              *Session
// 	server               *GoRedisServer
// 	cmdbuffer            chan *Command
// 	currentCommand       *Command // 当前处理中的command
// 	sendmutex            sync.Mutex
// 	uid                  string                // 从库id
// 	aofEnabled           bool                  // 支持从库快照
// 	remoteExists         bool                  //是否存在远程连接
// 	shouldChangeToRemote bool                  // 应该转向写入远程
// 	aoflist              *levelredis.LevelList // 存放同步中断后指令
// }

// func NewSlaveSessionServer(server *GoRedisServer, session *Session, uid string) (s *SlaveSessionServer) {
// 	s = &SlaveSessionServer{}
// 	s.server = server
// 	s.session = session
// 	s.remoteExists = s.session != nil
// 	s.cmdbuffer = make(chan *Command, 100000)
// 	s.uid = uid
// 	s.aofEnabled = len(uid) > 0 //存在uid就可以访问本地快照
// 	if s.aofEnabled {
// 		s.aoflist = levelredis.NewLevelList(s.server.levelRedis, "__slaveaof:"+s.uid)
// 	}
// 	return
// }

// func (s *SlaveSessionServer) AofEnabled() bool {
// 	return s.aofEnabled
// }

// func (s *SlaveSessionServer) UID() string {
// 	return s.uid
// }

// func (s *SlaveSessionServer) SetSession(session *Session) {
// 	s.session = session
// 	s.remoteExists = s.session != nil
// }

// // 继续同步
// func (s *SlaveSessionServer) ContinueSync() {
// 	s.shouldChangeToRemote = true
// }

// func (s *SlaveSessionServer) ContinueAof() {
// 	go s.aofRunloop()
// }

// // 向远程写入
// func (s *SlaveSessionServer) remoteRunloop() {
// 	s.server.stdlog.Info("remote runloop start")
// 	defer s.server.stdlog.Info("remote runloop end")
// 	for {
// 		// 先消费别人的
// 		if s.currentCommand == nil {
// 			s.currentCommand = <-s.cmdbuffer
// 		}
// 		s.server.stdlog.Debug("remote send %s %s", s.session.RemoteAddr(), s.currentCommand)
// 		err := s.session.WriteCommand(s.currentCommand)
// 		if err != nil {
// 			s.server.stdlog.Warn("remote slave gone away %s", s.session.RemoteAddr())
// 			// 从库断开后写入本地aof
// 			if s.AofEnabled() {
// 				s.server.stdlog.Info("redirect to aof writer")
// 				go s.aofRunloop()
// 				return
// 			}
// 			return
// 		}
// 		s.currentCommand = <-s.cmdbuffer
// 	}
// }

// // 向本地aof写入
// func (s *SlaveSessionServer) aofRunloop() {
// 	s.server.stdlog.Info("aof wirte runloop start")
// 	defer s.server.stdlog.Info("aof write runloop end")
// 	for {
// 		if s.currentCommand == nil {
// 			s.currentCommand = <-s.cmdbuffer
// 		}
// 		s.server.stdlog.Debug("aof write %s", s.currentCommand)
// 		err := s.aoflist.RPush(s.currentCommand.Bytes())
// 		// 如果写入aof出错，应该废弃全部aof，重来snapshot
// 		if err != nil {
// 			s.server.stdlog.Error("aof write err %s", err)
// 			return
// 		}
// 		if s.shouldChangeToRemote {
// 			s.currentCommand = nil // 清空并跳转
// 			s.shouldChangeToRemote = false
// 			go s.aofToRemoteRunloop()
// 			return
// 		}
// 		s.currentCommand = <-s.cmdbuffer
// 	}
// }

// // 从aof读取向远程写入
// func (s *SlaveSessionServer) aofToRemoteRunloop() {
// 	s.server.stdlog.Info("aof to remote runloop start")
// 	defer s.server.stdlog.Info("aof to remote runloop end")
// 	// 从aof向远程写时，不应该有待处理的数据
// 	if s.currentCommand != nil {
// 		s.server.stdlog.Error("where are you come from? %s", s.currentCommand)
// 		return
// 	}
// 	sendCount := 0
// 	for {
// 		elem, e1 := s.aoflist.Index(0)
// 		if e1 != nil {
// 			// 如果aof出错，应该废弃全部aof，重来snapshot
// 			s.server.stdlog.Error("aof to remote peek error %s", e1)
// 			return
// 		}
// 		// 同步完毕，转向直接远程写入
// 		if elem == nil {
// 			s.server.stdlog.Info("aof to remote finish, send %d cmd", sendCount)
// 			go s.remoteRunloop()
// 			return
// 		}
// 		sendCount++
// 		// bs come from cmd.Bytes()
// 		bs := elem.Value.([]byte)
// 		n, e2 := s.session.Write(bs)
// 		if e2 != nil {
// 			s.server.stdlog.Error("aof to remote send error n(%d), %s", n, e2)
// 			return
// 		}
// 		// 移除
// 		_, e3 := s.aoflist.LPop()
// 		if e3 != nil {
// 			// 如果aof出错，应该废弃全部aof，重来snapshot
// 			s.server.stdlog.Error("aof to remote pop error %s", e3)
// 			return
// 		}
// 	}
// }

// func (s *SlaveSessionServer) AsyncSendCommand(cmd *Command) {
// 	s.cmdbuffer <- cmd
// }

// // 向从库发送数据库快照
// // 时间关系，暂时使用了 []byte -> Entry -> Command -> slave 的方法，
// // 应该改为官方发送rdb数据的方式
// func (s *SlaveSessionServer) SendSnapshot(snapshot *levigo.Snapshot) {
// 	s.server.stdlog.Info("snapshot send runloop start")
// 	defer s.server.stdlog.Info("snapshot send runloop end")
// 	s.sendmutex.Lock()
// 	defer s.sendmutex.Unlock()

// 	// iter := snapshot.NewIterator(&opt.ReadOptions{})
// 	// defer func() {
// 	// 	// 必须释放Iterator和Snapshot
// 	// 	iter.Release()
// 	// 	snapshot.Release()
// 	// }()

// 	// leveltool.PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
// 	// })

// 	// for iter.Next() {
// 	// 	// 跳过系统数据
// 	// 	key := string(iter.Key())
// 	// 	if strings.HasPrefix(key, "__goredis:") {
// 	// 		continue
// 	// 	}
// 	// 	entry, e1 := s.toEntry(iter.Value())
// 	// 	if e1 != nil {
// 	// 		s.server.stdlog.Warn("snapshot fetch entry error %s", e1)
// 	// 		continue
// 	// 	}
// 	// 	cmd := entryToCommand(iter.Key(), entry)
// 	// 	if cmd == nil {
// 	// 		s.server.stdlog.Warn("snapshot entry to command error %s, %s", string(iter.Key()), string(iter.Value()))
// 	// 		continue
// 	// 	}
// 	// 	// s.server.stdlog.Debug("snapshot send %s", cmd)
// 	// 	e2 := s.session.WriteCommand(cmd)
// 	// 	if e2 != nil {
// 	// 		// 销毁整个slave
// 	// 	}
// 	// }
// 	// 构建aof
// 	if s.AofEnabled() {
// 		s.server.snapshotSentCallback(s)
// 	}
// 	// 开始消费
// 	go s.remoteRunloop()
// }