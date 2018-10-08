package send

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/btcsuite/btcd/wire"
	"github.com/pkg/errors"
	"github.com/rhavar/bustapay/rpc-client"
	"github.com/rhavar/bustapay/util"
	"io/ioutil"
	"net/http"
)

func Send(address string, url string, amount int64) error {
	util.VerboseLog("Sending ", amount, " satoshis to ", address, " via url ", url)

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
	util.VerboseLog("Created unfunded transaction: ", unfunded)

	// Step 2. Run coin selection, and add change (if applicable)
	funded, err := rpcClient.FundRawTransaction(unfunded)
	if err != nil {
		return err
	}
	util.VerboseLog("Funded transaction: ", util.HexifyTransaction(funded))

	// Step 3. Sign the transaction
	template, _, err := rpcClient.SignRawTransactionWithWallet(funded)
	if err != nil {
		return err
	}
	util.VerboseLog("Template transaction: ", util.HexifyTransaction(template))

	// Step 4. Send transaction to receiver
	partial, err := httpPost(template, url)
	if err != nil {
		return err
	}
	util.VerboseLog("Got partial transaction back: ", util.HexifyTransaction(partial))

	// Step 5. Validate the receiver didn't give us anything funny
	err = validate(template, partial)
	if err != nil {
		return err
	}

	// Step 6. sign the partial transaction
	final, _, err := rpcClient.SignRawTransactionWithWallet(partial)
	if err != nil {
		return err
	}
	util.VerboseLog("Final transaction: ", util.HexifyTransaction(final))

	// Step 7. broadcast the raw transaction
	_, err = rpcClient.SendRawTransaction(final)
	if err != nil {
		return err
	}
	util.VerboseLog("Broadcasted final transaction")

	fmt.Println(final.TxHash())

	return nil
}

func httpPost(tx *wire.MsgTx, url string) (*wire.MsgTx, error) {

	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		return nil, errors.WithStack(err)
	}

	util.VerboseLog("HTTP POSTing template transaction to ", url)
	response, err := http.Post(url, "application/binary", &byteBuffer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if response.StatusCode != 200 {
		util.VerboseLog("Got http status code: ", response.StatusCode)

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		util.VerboseLog("Http response body: ", string(body))
		return nil, errors.New("got http error from server")
	}

	if err != nil {
		return nil, errors.WithStack(err)
	}

	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(response.Body); err != nil {
		return nil, errors.WithStack(err)
	}

	return &msgTx, nil
}

func validate(template *wire.MsgTx, partial *wire.MsgTx) error {

	if template.LockTime != partial.LockTime {
		return errors.New("lock time changed")
	}

	if template.Version != partial.Version {
		return errors.New("version changed")
	}

	if len(template.TxOut) != len(partial.TxOut) {
		return errors.New("number of outputs changed")
	}

	if len(partial.TxIn) <= len(template.TxIn) {
		return errors.New("partial transaction should add inputs")
	}

	// validate the txins
	usedContributedInputs := make(map[wire.OutPoint] struct{}) // What inputs did the receiver contribute
	usedTemplateInputs :=  make(map[wire.OutPoint] struct{}) // Which have we seen


	templateTxIns := make(map[wire.OutPoint]*wire.TxIn)
	for _, txIn := range template.TxIn {
		templateTxIns[txIn.PreviousOutPoint] = txIn
	}
	util.Assert(len(templateTxIns) == len(template.TxIn))

	for _, txIn := range partial.TxIn {

		templateTxIn, contains := templateTxIns[txIn.PreviousOutPoint]

		if contains {

			if templateTxIn.Sequence != txIn.Sequence {
				return errors.New("input sequence has been changed")
			}

			if !bytes.Equal(templateTxIn.SignatureScript, txIn.SignatureScript) {
				return errors.New("input sig script has been changed")
			}

			if len(txIn.Witness) != 0 {
				return errors.New("input witness has not been cleared")
			}

			usedTemplateInputs[txIn.PreviousOutPoint] = struct{}{}

		} else {
			// No need to validate, (if it's invalid, the transaction won't propagate..)
			// but we can sanity check that it has witnesses

			if len(txIn.Witness) == 0 {
				return errors.New("found contributed input, but without witness")
			}

			usedContributedInputs[txIn.PreviousOutPoint] = struct{}{}
		}
	}

	if len(usedTemplateInputs) != len(template.TxIn) {
		return errors.New("partial transaction did contain all original txins")
	}


	if len(usedContributedInputs) + len(usedTemplateInputs) != len(partial.TxIn) {
		return errors.New("partial transaction contained dupe inputs")
	}


	originalTxOuts := make(map[string]*wire.TxOut)
	for _, txOut := range template.TxOut {
		originalTxOuts[hex.EncodeToString(txOut.PkScript)] = txOut
	}

	for _, txOut := range partial.TxOut {
		pkScriptHex := hex.EncodeToString(txOut.PkScript)
		original, contains := originalTxOuts[pkScriptHex]
		if !contains {
			return errors.New("partial transaction has output that original didn't")
		}

		// Check if this is the changed output
		if txOut.Value < original.Value {
			return errors.New("an output decreased. wtf?")
		}

		delete(originalTxOuts, pkScriptHex)
	}

	util.Assert(len(originalTxOuts) == 0)


	return nil
}
