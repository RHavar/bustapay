package main

import (
	"crypto/rand"
	"golang.org/x/crypto/ripemd160"
)

// This is a cryptographically strong hash function which is impossible for an external party to know how it works
// i.e.  obsfuhash(x)  is equiv to cryptographicHash(x + hiddenValue)

// Note 1 that this is *NOT* consistent between runs. If it was desirable to make it so, the hiddenValue could be
// stored on disk


func Obfuhash(pre ...[]byte) []byte {
	h := ripemd160.New()
	for _, p := range pre {
		h.Write(p)
	}
	h.Write(hashObfuscationSeed) // add at the end to prevent length extension attacks..
	return h.Sum(nil)
}



var hashObfuscationSeed []byte

func init() {
	seedSize := 16

	hashObsfucationSeed := make([]byte, seedSize)
	if _, err := rand.Read(hashObsfucationSeed) ; err != nil {
		panic(err)
	}
}
