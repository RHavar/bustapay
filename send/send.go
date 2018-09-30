package send

import (
	"net/http"
	"bytes"
	"github.com/btcsuite/btcd/wire"
	"encoding/hex"
	"github.com/btcsuite/btcd/txscript"
	"github.com/rhavar/bustapay/rpc-client"
	"log"
	"github.com/rhavar/bustapay/util"
	"github.com/pkg/errors"
	"io/ioutil"
)

func Send(address string, url string, amount int64) error {
	log.Println("Sending ", amount, " satoshis to ", address, " via url ", url)


	rpcClient, err := rpc_client.NewRpcClient()
	if err != nil {
		return err
	}
	defer rpcClient.Shutdown()


	// Step 1. Create a transaction with correct output
	unfunded, err := rpcClient.CreateRawTransaction(address, amount)
	if err != nil {
		return err
	}
	log.Println("Created unfunded transaction: ", util.HexifyTransaction(unfunded))

	// Step 2. Run coin selection, and add change (if applicable)
	funded, err := rpcClient.FundRawTransaction(unfunded)
	if err != nil {
		return err
	}
	log.Println("Funded transaction: ", util.HexifyTransaction(funded))


	// Step 3. Sign the transaction
	template, _, err := rpcClient.SignRawTransactionWithWallet(funded)
	if err != nil {
		return err
	}
	log.Println("Template transaction: ", util.HexifyTransaction(template))


	// Step 4. Send transaction to receiver
	log.Println("HTTP POSTing template transaction to ", url)
	partial, err := httpPost(template, url)
	if err != nil {
		return err
	}
	log.Println("Got partial transaction back: ", util.HexifyTransaction(partial))


	// Step 5. Validate the receiver didn't give us anything funny
	err = validate(rpcClient, template, partial)
	if err != nil {
		return err
	}

	// Step 6. sign the partial transaction
	final, _, err := rpcClient.SignRawTransactionWithWallet(partial)
	if err != nil {
		return err
	}
	log.Println("Final transaction: ", util.HexifyTransaction(final))


	// Step 7. broadcast the raw transaction
	_, err = rpcClient.SendRawTransaction(final)
	if err != nil {
		return err
	}
	log.Println("Broadcasted  final transaction: ", final.TxHash())

	return nil
}


func httpPost(tx *wire.MsgTx, url string) (*wire.MsgTx, error) {

	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		return nil, errors.WithStack(err)
	}

	response, err := http.Post(url, "application/binary", &byteBuffer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	log.Println("Http response: ", string(body))

	// TODO: check response code


	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(bytes.NewReader(body)); err != nil {
		return nil, errors.WithStack(err)
	}


	return &msgTx, nil
}

func validate(rpcClient *rpc_client.RpcClient, template *wire.MsgTx, partial  *wire.MsgTx) error {

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
