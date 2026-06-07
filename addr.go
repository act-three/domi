package domi

import (
	"encoding/binary"
	"hash/fnv"
	"strconv"
)

// An addr is the identity of a position in the tree: 0 at the patch
// root, and for each child the fnv-64a hash of its parent's addr and
// the component that selects the child — its index for positional
// children, its key for keyed ones. Identity follows the differ's
// matching rules, so two renders assign equal addrs exactly where the
// differ matches their elements.
type addr uint64

// hash returns the hash of a, a domain-separation tag, and the given
// payloads.
func (a addr) hash(tag byte, payload ...[]byte) addr {
	h := fnv.New64a()
	var buf [9]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(a))
	buf[8] = tag
	h.Write(buf[:])
	for _, p := range payload {
		h.Write(p)
	}
	return addr(h.Sum64())
}

// index returns the address of the positional child at index i of a.
func (a addr) index(i int) addr {
	var buf [binary.MaxVarintLen64]byte
	return a.hash('i', buf[:binary.PutUvarint(buf[:], uint64(i))])
}

// key returns the address of the keyed child k of a.
func (a addr) key(k string) addr {
	return a.hash('k', []byte(k))
}

// handlerKey names the slot'th handler attribute (named name) of the
// element at a.
func (a addr) handlerKey(name string, slot int) string {
	var buf [binary.MaxVarintLen64]byte
	h := a.hash('s', buf[:binary.PutUvarint(buf[:], uint64(slot))], []byte(name))
	return strconv.FormatUint(uint64(h), 16)
}
