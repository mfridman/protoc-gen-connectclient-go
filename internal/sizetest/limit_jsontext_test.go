//go:build go1.27

package sizetest

// leanLimit is the most that connectclient may add to a stripped binary. With
// Go 1.27, it decodes error bodies with jsontext. Decoding them with
// encoding/json instead would add about 450 KiB and fail this limit.
const leanLimit = 192 << 10
