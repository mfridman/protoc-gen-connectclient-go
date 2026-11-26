//go:build !go1.27

package sizetest

// leanLimit is the most that connectclient may add to a stripped binary. With
// Go 1.26, it decodes error bodies with encoding/json, which adds about
// 130 KiB.
const leanLimit = 384 << 10
