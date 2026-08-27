// SPDX-License-Identifier: Apache-2.0

// Package order makes fractional index keys for the order_key column.
//
// docs/PLAN.md §6.1 promises "a fractional index for order", so that two
// clients which reorder a list at the same time both keep a valid order. This
// package is that index. Every client needs it, so the licence matches the
// other shared packages.
//
// A key is a string of digits after an implied "0.". The alphabet runs in
// ASCII order, so a plain byte comparison of two keys gives the same answer as
// a comparison of the two fractions. SQLite can therefore sort the column with
// no help, and so can Room and JavaScript.
//
//	Between("", "")   the first key of an empty list
//	Between(a, "")    a key after a, at the end of the list
//	Between("", b)    a key before b, at the start of the list
//	Between(a, b)     a key between two neighbours
//
// One rule holds for every key: a key never ends with the lowest digit. That
// keeps one value to one key, so two different keys are always two different
// positions.
package order

import (
	"errors"
	"strings"
)

// alphabet holds 62 digits in ASCII order: 0 to 9, then A to Z, then a to z.
// Byte order and digit order are the same, which is the whole point.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const base = len(alphabet)

// ErrOutOfOrder reports that the two neighbours are equal, or the wrong way
// round. A caller that sees this has a bug in its own list, so the package
// refuses to invent an answer.
var ErrOutOfOrder = errors.New("order: the left key must be smaller than the right key")

// ErrBadKey reports a key with a character that is not a digit of the
// alphabet, or a key that ends with the lowest digit.
var ErrBadKey = errors.New("order: the key is not a fractional index")

// Between returns a key that sorts after left and before right.
//
// An empty left means the start of the list. An empty right means the end of
// the list. Two empty strings give the first key of an empty list.
func Between(left, right string) (string, error) {
	lo, err := digits(left)
	if err != nil {
		return "", err
	}
	hi, err := digits(right)
	if err != nil {
		return "", err
	}
	if right != "" && left >= right {
		return "", ErrOutOfOrder
	}
	var out []int
	for i := 0; ; i++ {
		l := digitAt(lo, i, 0)
		h := digitAt(hi, i, base)
		if right != "" && i >= len(hi) {
			// The right key cannot run out first. A shared prefix that reaches
			// the end of the right key means left >= right, which the check
			// above already refused.
			return "", ErrOutOfOrder
		}
		if l == h {
			out = append(out, l)
			continue
		}
		if h-l >= 2 {
			out = append(out, (l+h)/2)
			return render(out), nil
		}
		// The two digits touch, so the answer keeps the left digit and then
		// needs a tail that is larger than the tail of the left key.
		out = append(out, l)
		for j := i + 1; ; j++ {
			d := digitAt(lo, j, 0)
			if d <= base-2 {
				out = append(out, d+(base-d)/2)
				return render(out), nil
			}
			out = append(out, d)
		}
	}
}

// Valid reports whether s is a key this package can read.
func Valid(s string) bool {
	_, err := digits(s)
	return err == nil
}

func digits(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	out := make([]int, 0, len(s))
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(alphabet, s[i])
		if d < 0 {
			return nil, ErrBadKey
		}
		out = append(out, d)
	}
	if out[len(out)-1] == 0 {
		return nil, ErrBadKey
	}
	return out, nil
}

// digitAt reads one digit. Past the end of the key it returns pad: zero for
// the left key, because the missing digits of a fraction are zeros, and the
// base for the right key, because a missing right key means the value one.
func digitAt(d []int, i, pad int) int {
	if i < len(d) {
		return d[i]
	}
	return pad
}

func render(d []int) string {
	b := make([]byte, len(d))
	for i, v := range d {
		b[i] = alphabet[v]
	}
	return string(b)
}
