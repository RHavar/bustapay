package main

import (
	"net/http"
	"bytes"
	"github.com/btcsuite/btcd/wire"
	"errors"
	"encoding/hex"
	"github.com/btcsuite/btcd/txscript"
)

func Send(address string, url string, amount int64) error {

	rpcClient, err := NewRpcClient()
	if err != nil {
		return err
	}
	defer rpcClient.Shutdown()


	unfunded, err := rpcClient.CreateRawTransaction(address, amount)
	if err != nil {
		return err
	}

	funded, err := rpcClient.FundRawTransaction(unfunded)
	if err != nil {
		return err
	}

	template, _, err := rpcClient.SignRawTransactionWithWallet(funded)
	if err != nil {
		return err
	}

	partial, err := post(template, url)
	if err != nil {
		return err
	}

	err = validate(rpcClient, template, partial)
	if err != nil {
		return err
	}


	final, _, err := rpcClient.SignRawTransactionWithWallet(partial)
	if err != nil {
		return err
	}

	_, err = rpcClient.SendRawTransaction(final)
	if err != nil {
		return err
	}


	return nil
}


func post(tx *wire.MsgTx, url string) (*wire.MsgTx, error) {

	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		return nil, err
	}

	response, err := http.Post(url, "application/binary", &byteBuffer)
	if err != nil {
		return nil, err
	}


	// TODO: consider hex encoded responses..


	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(response.Body); err != nil {
		return nil, err
	}



	return &msgTx, nil
}

func validate(rpcClient *RpcClient, template *wire.MsgTx, partial  *wire.MsgTx) error {

	if template.LockTime != partial.LockTime {
		return errors.New("lock time changed")
	}

	if template.Version != partial.Version {
		return errors.New("version changed")
	}

	if len(template.TxOut) != len(partial.TxOut) {
		return errors.New("number of outputs changed")
	}

	if len(template.TxIn) != len(partial.TxIn)+1 {
		return errors.New("partial transaction should have 1 additional input")
	}


	// validate the txins
	contributedInputAmount := int64(0)
	foundContributedInput := false

	originalTxIns := make(map[wire.OutPoint]*wire.TxIn)
	for _, txIn := range template.TxIn {
		originalTxIns[txIn.PreviousOutPoint] = txIn
	}
	for i, txIn := range partial.TxIn {

		originalTxIn, contains := originalTxIns[txIn.PreviousOutPoint]

		if contains {

			if originalTxIn.Sequence != txIn.Sequence {
				return errors.New("input sequence has been changed")
			}

			if !bytes.Equal(originalTxIn.SignatureScript, txIn.SignatureScript) {
				return errors.New("input sig script has been changed")
			}

			if len(txIn.Witness) != 0 {
				return errors.New("input witness has not been cleared")
			}
		} else {
			if foundContributedInput {
				return errors.New("found 2 contributed inputs")
			}
			foundContributedInput = true

			txOut,err := rpcClient.GetTxOut(&txIn.PreviousOutPoint.Hash, txIn.PreviousOutPoint.Index)
			if err != nil {
				return err
			}

			scriptPubKey, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
			if err != nil {
				return err
			}

			engine, err := txscript.NewEngine(scriptPubKey, partial, i, txscript.StandardVerifyFlags, nil, nil, contributedInputAmount)
			if err != nil {
				return err
			}

			if err := engine.Execute(); err != nil {
				return err
			}

		}


		originalTxOuts := make(map[string]*wire.TxOut)
		for _, txOut := range template.TxOut {
			originalTxOuts[hex.EncodeToString(txOut.PkScript)] = txOut
		}

		seenDestination := false

		for _, txOut := range partial.TxOut {
			pkScriptHex := hex.EncodeToString(txOut.PkScript)
			original, contains := originalTxOuts[pkScriptHex]
			if !contains {
				return errors.New("partial transaction has output that original didn't")
			}
			delete(originalTxOuts, pkScriptHex)

			// Check if this is the changed output
			if txOut.Value != original.Value {
				if seenDestination {
					return errors.New("more than 1 output has had its value changed")
				}
				seenDestination = true

				if original.Value + contributedInputAmount != txOut.Value {
					return errors.New("output value doesnt match original + contributed input amount")
				}
			}
		}
	}

	return nil
}
