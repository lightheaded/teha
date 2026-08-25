// SPDX-License-Identifier: Apache-2.0

// Package id makes short, sortable, collision-safe row ids.
//
// A UUID costs 36 characters on the wire. Every MCP result and every sync row
// carries one id, so a list of 200 tasks pays about 2 000 tokens for the ids
// alone. An id here costs 15 characters after the prefix:
//
//	8 characters   millisecond time in base32, so ids sort in creation order
//	3 characters   a counter inside that millisecond, so one process never
//	               repeats itself even during a bulk import
//	4 characters   randomness, so two processes writing the same database do
//	               not meet
//
// The counter matters. A random tail alone collided in a test after four
// thousand ids inside one millisecond, which an import reaches easily.
package id

import (
	"crypto/rand"
	"sync"
	"time"
)

// alphabet is Crockford base32 without I, L, O and U, so an id survives being
// read aloud or typed by hand.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMs   int64
	sequence int64
)

// New returns an id with the given prefix, for example New("t") for a task.
func New(prefix string) string {
	ms, seq := tick()
	return prefix + "_" + encode(ms, 8) + encode(seq, 3) + random(4)
}

// tick returns the current millisecond and the next sequence number inside it.
// When the counter fills, the clock value moves on by one millisecond, so ids
// stay unique and stay in order.
func tick() (int64, int64) {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now().UnixMilli()
	switch {
	case now > lastMs:
		lastMs, sequence = now, 0
	default:
		sequence++
		if sequence >= 32768 { // 3 base32 characters hold 32768 values
			lastMs++
			sequence = 0
		}
	}
	return lastMs, sequence
}

func encode(v int64, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = alphabet[v&31]
		v >>= 5
	}
	return string(out)
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // a system without randomness cannot serve requests safely
	}
	for i := range b {
		b[i] = alphabet[int(b[i])&31]
	}
	return string(b)
}
