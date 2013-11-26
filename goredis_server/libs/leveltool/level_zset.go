package leveltool

/**
基于leveldb实现的zset，用于海量存储，并节约内存
__key[user_rank]zset = 2
__zset[user_rank]score:1378000907596:0 = 100422
__zset[user_rank]score:1378000907596:2 = 100428
...
__zset[user_rank]key:100422 = 1378000907596:0
__zset[user_rank]key:300000 = 1378000907596:2
...

*/

import (
	"bytes"
	// "fmt"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"strconv"
	"strings"
	"sync"
)

// score和member都是字符串
type ZSetElem struct {
	Score  []byte
	Member []byte
}

func NewZSetElem(score, member []byte) (elem *ZSetElem) {
	elem = &ZSetElem{Score: score, Member: member}
	return
}

type LevelSortedSet struct {
	db          *leveldb.DB
	ro          *opt.ReadOptions
	wo          *opt.WriteOptions
	entryKey    string
	totalCount  int
	mu          sync.Mutex
	maxScoreLen int
}

func NewLevelSortedSet(db *leveldb.DB, entryKey string) (l *LevelSortedSet) {
	l = &LevelSortedSet{}
	l.db = db
	l.ro = &opt.ReadOptions{}
	l.wo = &opt.WriteOptions{}
	l.entryKey = entryKey
	l.maxScoreLen = 20 // int64
	l.initInfo()
	return
}

func (l *LevelSortedSet) Size() int {
	return 0
}

func (l *LevelSortedSet) initInfo() {
	// init totalCount
	data, _ := l.db.Get([]byte(l.infoKey()), l.ro)
	if data != nil {
		l.totalCount, _ = strconv.Atoi(string(data))
	} else {
		l.totalCount = 0
	}
}

func (l *LevelSortedSet) infoKey() []byte {
	return []byte(strings.Join([]string{KEY_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, ZSET_SUFFIX}, ""))
}

func (l *LevelSortedSet) infoValue() []byte {
	s := strconv.Itoa(l.totalCount)
	return []byte(s)
}

// 从 100422 拼出 __zset[user_rank]member#100422
func (l *LevelSortedSet) memberKey(member []byte) []byte {
	return []byte(strings.Join([]string{ZSET_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, "member", SEP, string(member)}, ""))
}

// __zset[user_rank]score#
func (l *LevelSortedSet) scoreKeyPrefix() []byte {
	return []byte(strings.Join([]string{ZSET_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, "score", SEP}, ""))
}

// __zset[user_rank]score#00000000000000001001#
func (l *LevelSortedSet) baseScoreKey(score []byte) (key []byte) {
	zero := strings.Repeat("0", l.maxScoreLen-len(score))
	return []byte(strings.Join([]string{ZSET_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, "score", SEP, zero, string(score), SEP}, ""))
}

// 从 __zset[user_rank]score#00000000000000001001#2 分解出 00000000000000001001#2
func (l *LevelSortedSet) scoreKeyWithNoPrefix(scorekey string) (key []byte) {
	return []byte(scorekey[strings.Index(scorekey, SEP)+1:])
}

// 从 00000000000000001001#2 拼出 __zset[user_rank]score#00000000000000001001#2
func (l *LevelSortedSet) joinScoreKeyWithSuffix(suffix []byte) (key []byte) {
	return []byte(strings.Join([]string{ZSET_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, "score", SEP, string(suffix)}, ""))
}

// 从 __zset[user_rank]score#00000000000000001001#2 分解出 00000000000000001001
func (l *LevelSortedSet) fullScoreInKey(scorekey []byte) (fullscore []byte) {
	start := bytes.Index(scorekey, []byte(SEP)) + 1
	end := bytes.LastIndex(scorekey, []byte(SEP))
	fullscore = copyBytes(scorekey[start:end])
	return
}

// 从 __zset[user_rank]score#00000000000000001001#2 分解出 1001
func (l *LevelSortedSet) scoreInScoreKey(scorekey []byte) (score []byte) {
	fullscore := l.fullScoreInKey(scorekey)
	score = copyBytes(bytes.TrimLeft(fullscore, "0"))
	return
}

func (l *LevelSortedSet) Add2(scoreMembers ...[]byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := len(scoreMembers)
	for i := 0; i < count; i += 2 {
		score := scoreMembers[i]
		member := scoreMembers[i+1]
		// update
		memberkey := l.memberKey(member)
		exist := l.removeMember(member)
		if !exist {
			l.totalCount++
		}
		// seek
		scorekey, _ := l.findScoreKey(score, member)
		infokey := l.infoKey()
		// insert
		batch := new(leveldb.Batch)
		batch.Put([]byte(scorekey), member)
		batch.Put(memberkey, l.scoreKeyWithNoPrefix(scorekey))
		batch.Put(infokey, l.infoValue())
		err = l.db.Write(batch, l.wo)
		if err != nil {
			break
		}
		n++
	}
	return
}

func (l *LevelSortedSet) Add(elems ...*ZSetElem) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, elem := range elems {
		// update
		memberkey := l.memberKey(elem.Member)
		exist := l.removeMember(elem.Member)
		if !exist {
			l.totalCount++
		}
		// seek
		scorekey, _ := l.findScoreKey(elem.Score, elem.Member)
		infokey := l.infoKey()
		// insert
		batch := new(leveldb.Batch)
		batch.Put([]byte(scorekey), elem.Member)
		batch.Put(memberkey, l.scoreKeyWithNoPrefix(scorekey))
		batch.Put(infokey, l.infoValue())
		err = l.db.Write(batch, l.wo)
		if err != nil {
			break
		}
		n++
	}
	return
}

// @param key 完整的key
// @param idx 后续索引
func (l *LevelSortedSet) findScoreKey(score []byte, member []byte) (key string, idx int) {
	iter := l.db.NewIterator(l.ro)
	defer iter.Release()
	prefix := l.baseScoreKey(score)
	var lastkey []byte
	var memberExist bool
	PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
		lastkey = copyBytes(iter.Key())
		if bytes.Compare(iter.Value(), member) == 0 {
			memberExist = true
			*quit = true
		}
	}, "next")
	// 存在相同score
	if lastkey != nil {
		// member已存在
		if memberExist {
			key = string(lastkey)
			idx, _ = strconv.Atoi(key[strings.LastIndex(key, SEP)+1:])
		} else {
			// 在相同score下增加member
			key, idx = l.incrScoreKey(string(lastkey))
		}
	} else {
		key = string(prefix) + "0"
		idx = 0
	}
	return
}

// @param member 注意这是member，不是memberkey
func (l *LevelSortedSet) removeMember(member []byte) (ok bool) {
	memberkey := l.memberKey(member)
	suffix, err := l.db.Get(memberkey, l.ro)
	if err != nil {
		return false
	}
	if suffix == nil {
		return false
	}
	// batch remove
	scorekey := l.joinScoreKeyWithSuffix(suffix)
	batch := new(leveldb.Batch)
	batch.Delete(memberkey)
	batch.Delete(scorekey)
	err = l.db.Write(batch, l.wo)
	if err != nil {
		return false
	}
	ok = true
	return
}

func copyBytes(src []byte) (dst []byte) {
	dst = make([]byte, len(src))
	copy(dst, src)
	return
}

func (l *LevelSortedSet) incrScoreKey(scorekey string) (key string, idx int) {
	pos := strings.LastIndex(scorekey, SEP)
	prefix := scorekey[:pos]
	idx, _ = strconv.Atoi(scorekey[pos+1:])
	idx++
	key = prefix + SEP + strconv.Itoa(idx)
	return
}

/**
 * @param start int
 * @param stop int stop==-1 表示无限制
 */
func (l *LevelSortedSet) RangeByIndex(start, stop int) (elems []*ZSetElem) {
	iter := l.db.NewIterator(l.ro)
	defer iter.Release()
	elems = make([]*ZSetElem, 0, 10)
	// enumerate
	prefix := l.scoreKeyPrefix()
	PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
		if i < start {
			return // return as continue
		} else if i >= start && (stop == -1 || i <= stop) {
			score := l.scoreInScoreKey(iter.Key())
			member := copyBytes(iter.Value())
			elem := NewZSetElem(score, member)
			elems = append(elems, elem)
		} else {
			*quit = true
		}
	}, "next")
	return
}

func (l *LevelSortedSet) RangeByScore(min, max []byte, limitOffset, limitCount int) (elems []*ZSetElem) {
	iter := l.db.NewIterator(l.ro)
	defer iter.Release()
	elems = make([]*ZSetElem, 0, 10)
	// enumerate
	prefix := l.scoreKeyPrefix()
	pmin := []byte(strings.Repeat("0", l.maxScoreLen-len(min)) + string(min))
	pmax := []byte(strings.Repeat("0", l.maxScoreLen-len(max)) + string(max))

	PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
		fullscore := l.fullScoreInKey(iter.Key())

		if bytes.Compare(fullscore, pmin) < 0 {
			return // return as continue
		} else if bytes.Compare(fullscore, pmin) >= 0 && (bytes.Compare(max, []byte("-1")) == 0 || bytes.Compare(fullscore, pmax) <= 0) {
			score := l.scoreInScoreKey(iter.Key())
			member := copyBytes(iter.Value())
			elem := NewZSetElem(score, member)
			elems = append(elems, elem)
		} else {
			*quit = true
		}
	}, "next")
	return
}

func (l *LevelSortedSet) Remove(members ...[]byte) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n = 0
	for _, member := range members {
		exist := l.removeMember(member)
		if exist {
			n++
		}
	}
	l.totalCount -= n
	infokey := l.infoKey()
	// 数量为0时删除count
	if l.totalCount == 0 {
		l.db.Delete(infokey, l.wo)
	} else {
		l.db.Put(infokey, l.infoValue(), l.wo)
	}
	return
}

func (l *LevelSortedSet) RemoveByIndex(start, stop int) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	iter := l.db.NewIterator(l.ro)
	defer iter.Release()
	// enumerate
	prefix := l.scoreKeyPrefix()
	batch := new(leveldb.Batch)
	n = 0
	PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
		if i < start {
			return // return as continue
		} else if i >= start && (stop == -1 || i <= stop) {
			key := copyBytes(iter.Key())
			memberkey := l.memberKey(iter.Value())
			batch.Delete(key)
			batch.Delete(memberkey)
			n++
		} else {
			*quit = true
		}
	}, "next")
	l.totalCount -= n
	infokey := l.infoKey()
	if l.totalCount == 0 {
		l.db.Delete(infokey, l.wo)
	} else {
		l.db.Put(infokey, l.infoValue(), l.wo)
	}
	l.db.Write(batch, l.wo)
	return
}

func (l *LevelSortedSet) RemoveByScore(min, max []byte) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	iter := l.db.NewIterator(l.ro)
	defer iter.Release()
	// enumerate
	prefix := l.scoreKeyPrefix()
	pmin := []byte(strings.Repeat("0", l.maxScoreLen-len(min)) + string(min))
	pmax := []byte(strings.Repeat("0", l.maxScoreLen-len(max)) + string(max))

	batch := new(leveldb.Batch)
	n = 0
	PrefixEnumerate(iter, prefix, func(i int, iter iterator.Iterator, quit *bool) {
		fullscore := l.fullScoreInKey(iter.Key())

		if bytes.Compare(fullscore, pmin) < 0 {
			return // return as continue
		} else if bytes.Compare(fullscore, pmin) >= 0 && (bytes.Compare(max, []byte("-1")) == 0 || bytes.Compare(fullscore, pmax) <= 0) {
			key := copyBytes(iter.Key())
			memberkey := l.memberKey(iter.Value())
			batch.Delete(key)
			batch.Delete(memberkey)
			n++
		} else {
			*quit = true
		}
	}, "next")
	l.totalCount -= n
	infokey := l.infoKey()
	if l.totalCount == 0 {
		l.db.Delete(infokey, l.wo)
	} else {
		l.db.Put(infokey, l.infoValue(), l.wo)
	}
	l.db.Write(batch, l.wo)
	return
}

func (l *LevelSortedSet) Score(member []byte) (score []byte) {
	memberkey := l.memberKey(member)
	data, err := l.db.Get(memberkey, l.ro)
	if err != nil || data == nil {
		return
	}
	idx := bytes.LastIndex(data, []byte(SEP))
	score = bytes.TrimLeft(data[:idx], "0")
	return
}

func (l *LevelSortedSet) Count() (n int) {
	return l.totalCount
}

func (l *LevelSortedSet) Drop() (n int) {
	return
}
