package store

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// crockford is the ULID alphabet (Crockford base32: no I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a 26-character ULID: a 48-bit millisecond timestamp followed by
// 80 bits of cryptographic randomness, encoded in Crockford base32. IDs are
// lexicographically sortable by creation time and unguessable enough to not leak
// counts, while remaining opaque to clients.
func NewID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand failure is not something we can sensibly continue past.
		panic("myshare: crypto/rand unavailable: " + err.Error())
	}
	return encodeULID(b)
}

func encodeULID(b [16]byte) string {
	// 128 bits -> 26 base32 chars (130 bits, top 2 bits always 0).
	dst := make([]byte, 26)
	// Treat the 16 bytes as a big-endian 128-bit number, emit 5 bits at a time.
	hi := binary.BigEndian.Uint64(b[0:8])
	lo := binary.BigEndian.Uint64(b[8:16])
	for i := 25; i >= 0; i-- {
		dst[i] = crockford[lo&0x1f]
		lo = (lo >> 5) | (hi << 59)
		hi >>= 5
	}
	return string(dst)
}
