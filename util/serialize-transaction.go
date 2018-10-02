package util

import (
	"bytes"
	"encoding/hex"
	"github.com/btcsuite/btcd/wire"
)

func SerializeTransaction(tx *wire.MsgTx) []byte {
	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		panic(err)
	}

	return byteBuffer.Bytes()
}

func HexifyTransaction(tx *wire.MsgTx) string {
	return hex.EncodeToString(SerializeTransaction(tx))
}
