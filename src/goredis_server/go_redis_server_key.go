package goredis_server

import (
	"./storage"
	//. "github.com/latermoon/GoRedis/src/goredis"
	. "../goredis"
)

func (server *GoRedisServer) OnDEL(cmd *Command) (reply *Reply) {
	keys := cmd.StringArgs()[1:]
	count := 0
	for _, key := range keys {
		switch server.Storages.KeyTypeStorage.GetType(key) {
		case storage.KeyTypeString:
			n, _ := server.Storages.StringStorage.Del([]string{key}...)
			count += n
		default:
		}
	}
	reply = IntegerReply(count)
	return
}

/*
	KeyTypeUnknown
	KeyTypeString
	KeyTypeHash
	KeyTypeList
	KeyTypeSet
	KeyTypeSortedSet
*/
func (server *GoRedisServer) OnTYPE(cmd *Command) (reply *Reply) {
	key := cmd.StringAtIndex(1)
	keytype := server.Storages.KeyTypeStorage.GetType(key)
	typestr := "none"
	switch keytype {
	case storage.KeyTypeString:
		typestr = "string"
	case storage.KeyTypeHash:
		typestr = "hash"
	case storage.KeyTypeList:
		typestr = "list"
	case storage.KeyTypeSet:
		typestr = "set"
	case storage.KeyTypeSortedSet:
		typestr = "zset"
	default:
		typestr = "none"
	}
	reply = StatusReply(typestr)
	return
}