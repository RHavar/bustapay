package receive

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil/txsort"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rhavar/bustapay/rpc-client"
	"github.com/rhavar/bustapay/util"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"time"
)

func createBustpayTransaction(templateTx *wire.MsgTx) ([]byte, error) {

	for _, txIn := range templateTx.TxIn {
		if len(txIn.Witness) == 0 {
			return nil, newClientError("all inputs must be segwit and signed")
		}
	}

	// Some sanity checking the transaction..
	if len(templateTx.TxIn) == 0 {
		return nil, newClientError("provided transaction isn't mempool eligible")
	}

	rpcClient, err := rpc_client.NewRpcClient()
	if err != nil {
		return nil, err
	}
	defer rpcClient.Shutdown()

	// This is **essential** for preventing txid malleability
	// otherwise we can be given invalid scriptSig's and then they get mallaeted to correct them
	// which will change the txid, but not invalidate the signatures
	acceptable, err := rpcClient.TestMempoolAccept(templateTx)

	if err != nil {
		return nil, err
	}

	if !acceptable {
		return nil, newClientError("provided transaction isn't mempool eligible")
	}

	var paymentTargetAddress string
	var paymentTargetAmount int64
	var paymentTargetVout int



	chainParams, err := rpcClient.GetChainParams()
	if err != nil {
		return nil, err
	}

	// We need to find which address of ours it's paying (if in fact it even is...)
	// this is a super inefficient naive way of doing it
	for vout, txout := range templateTx.TxOut {

		_, addresses, _, err := txscript.ExtractPkScriptAddrs(txout.PkScript, chainParams)

		if err != nil || len(addresses) != 1 {
			log.Println("Warning: Could not exact address got: ", err, addresses)
			continue
		}

		address := addresses[0].String()

		isMine, err := rpcClient.IsMyFreshMyAddress(address)
		if err != nil {
			return nil, err
		}

		if isMine {
			paymentTargetAddress = address
			paymentTargetAmount = txout.Value
			paymentTargetVout = vout

			break
		}
	}

	if paymentTargetAddress == "" {
		return nil, newClientError("transaction does not pay a wallet address")
	}

	// We're going to reveal one of our unspent, but we're going to base it off
	// what they sent us. This means they can't keep querying us to find out our unspent
	// because we'll keep giving them the same one back
	contributingUnspent, err := getRandomUnspent(rpcClient, templateTx)
	if err != nil {
		return nil, err
	}

	// Now we're going to create the partially signed transaction
	partialTransaction := templateTx.Copy()

	// Since we're going to modify the transaction, we're going invalidate all signatures
	for _, txin := range partialTransaction.TxIn {
		txin.Witness = nil // clear the witness
	}

	contribAmount := int64(math.Round(contributingUnspent.Amount * 1e8))
	partialTransaction.TxOut[paymentTargetVout].Value += contribAmount

	inputHash, err := chainhash.NewHashFromStr(contributingUnspent.TxID)
	if err != nil {
		return nil, err
	}
	contribInputOutpoint := wire.NewOutPoint(inputHash, contributingUnspent.Vout)
	contribTxIn := wire.NewTxIn(contribInputOutpoint, nil, nil)
	contribTxIn.Sequence = templateTx.TxIn[0].Sequence // copy the first sequence number

	// Now let's insert the txin
	partialTransaction.TxIn = append(partialTransaction.TxIn, contribTxIn)

	if txsort.IsSorted(templateTx) { // if it was originally bip69, we want to preserve this
		txsort.InPlaceSort(partialTransaction)
	} else {
		// shuffle
		for i := 0; i < len(partialTransaction.TxIn)-1; i++ {
			moveTo := i + rand.Intn(len(partialTransaction.TxIn)-i)
			partialTransaction.TxIn[moveTo], partialTransaction.TxIn[i] = partialTransaction.TxIn[i], partialTransaction.TxIn[moveTo]
		}
	}

	contributedInputIndex := -1
	for i, txIn := range partialTransaction.TxIn {
		if txIn.PreviousOutPoint == *contribInputOutpoint {
			contributedInputIndex = i
			break
		}
	}
	util.Assert(contributedInputIndex >= 0)

	partialTransaction, _, err = rpcClient.SignRawTransactionWithWallet(partialTransaction)
	if err != nil {
		return nil, err
	}

	// Out of abundant paranoia, we're going to clear the witnesses for all inputs we should not have signed
	for i, txIn := range partialTransaction.TxIn {
		if i != contributedInputIndex {
			txIn.Witness = nil
		}
	}

	util.VerboseLog("Final partial transaction: ", util.HexifyTransaction(partialTransaction))

	partialTransactionByteBuffer := bytes.Buffer{}
	err = partialTransaction.Serialize(&partialTransactionByteBuffer)
	if err != nil {
		return nil, err
	}

	templateTransactionByteBuffer := bytes.Buffer{}
	err = templateTx.Serialize(&templateTransactionByteBuffer)
	if err != nil {
		return nil, err
	}

	// Now we're going to create a dir  ~./bustapay/$finaltxid

	finalTxId := partialTransaction.TxHash().String()
	txDir := dataDirectory + "/" + finalTxId
	err = os.Mkdir(txDir, 0700)
	if err != nil {
		return nil, err
	}

	// Write the amount the person is sending us
	file, err := os.Create(txDir + "/amount.txt")
	fmt.Fprintf(file, "%v", paymentTargetAmount)
	file.Close()

	// Write the template transaction
	file, err = os.Create(txDir + "/template_transaction.hex")
	fmt.Fprint(file, hex.EncodeToString(templateTransactionByteBuffer.Bytes()))
	file.Close()

	// Write the partial transaction
	file, err = os.Create(txDir + "/partial_transaction.hex")
	fmt.Fprint(file, hex.EncodeToString(partialTransactionByteBuffer.Bytes()))
	file.Close()

	go func() {
		// We're going to use this convoluted loop (instead of doing it directly) so we don't keep the bitcoinRpc connection
		// open overly long
		loop := func() bool {
			rpcClient, err := rpc_client.NewRpcClient()
			if err != nil {
				fmt.Errorf("could not create bitcoin rpc client %v\n", err)
				return false // dont keep going
			}
			defer rpcClient.Shutdown()

			if rpcClient.MempoolHasEntry(partialTransaction.TxHash().String()) {
				fmt.Errorf("Yay. Finalized transaction %v was found in mempool! Monitoring the situation\n", finalTxId)
				// TODO: we should probably log it..
				return true // keep looping
			}

			// The main two cases that the finalized transaction might not be in the mempool is because it's already confirmed
			// or because it was never created. If we were smart we could differintiate the two, but really it doesn't matter.
			// We'll just blindly try send the original and if the finalized one has already confirmed it will just conflict
			// and be filtered out.

			_, err = rpcClient.SendRawTransaction(templateTx)
			util.VerboseLog("Blindly trying to send template transaction ", partialTransaction.TxHash().String(), " got error: ", err)
			return false // we're all done
		}

		continueLooping := true
		for continueLooping {
			time.Sleep(5 * time.Minute)
			continueLooping = loop()
		}
	}()

	return partialTransactionByteBuffer.Bytes(), nil
}

// We pick a random unspent, using seed. We intentionally make it very stable, so as long as the seed
// is the same it'll almost always pick the same unspent (even if the unspent set considerably changes)
func getRandomUnspent(rpcClient *rpc_client.RpcClient, templateTx *wire.MsgTx) (*btcjson.ListUnspentResult, error) {

	var seed chainhash.Hash // zero initialized

	for _, txIn := range templateTx.TxIn {
		if bytes.Compare(seed[:], txIn.PreviousOutPoint.Hash[:]) < 1 {
			seed = txIn.PreviousOutPoint.Hash
		}
	}
	util.Assert(!bytes.Equal(seed[:], new(chainhash.Hash)[:])) // seed shouldn't stay zero init..

	unspents, err := rpcClient.ListUnspent()
	if err != nil {
		return nil, err
	}

	// We are going to sort these elements by the hash of their  (txid,vout,seed)
	// this ensures that it is very stable (as unspent changes, it'll rarely change)
	// but if the seed changes, it's a totally different sort

	sort.Slice(unspents, func(i, j int) bool {
		a := util.Obfuhash([]byte(unspents[i].TxID), uintToByteSlice(unspents[i].Vout), seed[:])
		b := util.Obfuhash([]byte(unspents[j].TxID), uintToByteSlice(unspents[j].Vout), seed[:])

		return bytes.Compare(a, b) < 0
	})


	// Just really for testing, when doing a bustapay to ourselves we
	// never want to pick an unspent that is already in the bustapay transaction
	for _, unspent := range unspents {

		alreadyContains := false

		for _, txIn := range templateTx.TxIn {
			if txIn.PreviousOutPoint.String() == fmt.Sprintf("%v:%v", unspent.TxID, unspent.Vout) {
				log.Println("Warning: Tried to pick an already used input. Skipping ", unspent.TxID, ":", unspent.Vout)
				alreadyContains = true
				break
			}
		}

		if !alreadyContains {
			return &unspent, nil
		}
	}

	return nil, errors.New("no available unspents :/")
}

func uintToByteSlice(x uint32) []byte {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, x)
	return bs
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(400)
		fmt.Fprint(w,  `
bustapay recieve

Newish addresses can get requested at /get-newish-address  and bustapay transactions can be POST'd here
`)
		return
	}

	if r.ContentLength <= 0 {
		w.WriteHeader(400)
		fmt.Fprint(w, "missing a content-length")
		return
	}

	if r.ContentLength >= 100000 {
		w.WriteHeader(400)
		fmt.Fprint(w, "lol, little big transaction you got there. no?")
		return
	}

	txBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		fmt.Fprint(w, "could not read all http body")
		return
	}

	if r.Header.Get("Content-type") == "text/plain" {

		txBytes, err = hex.DecodeString(string(txBytes))
		if err != nil {
			w.WriteHeader(400)
			fmt.Fprint(w, "http body doesn't appear to be hex-encoded, but content-type was text/plain")
			return
		}

	}

	msgTx := &wire.MsgTx{}
	if err := msgTx.Deserialize(bytes.NewBuffer(txBytes)); err != nil {
		w.WriteHeader(400)
		fmt.Fprint(w, "http body was not a valid bitcoin transaction")
		return
	}

	util.VerboseLog("Got a template transaction: ", msgTx.TxHash(), " hex: ", util.HexifyTransaction(msgTx))

	templateTransaction, err := createBustpayTransaction(msgTx)

	if err != nil {
		w.WriteHeader(400)
		fmt.Fprint(w, "got an internal error")
		fmt.Println("proxy transaction error: ", err)
		return
	}

	w.Write(templateTransaction)
}

func StartServer(port int32) {
	log.Println("Listening on port: ", port)


	http.HandleFunc("/", handler)


	// This is extremely unsuitable for production. Just for development!
	http.HandleFunc("/get-newish-address", getNewishAddress)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", port), nil))
}

func newClientError(s string) error {
	return errors.New(s)
}

var dataDirectory string

func init() {
	dir, err := homedir.Dir()
	if err != nil {
		panic(err)
	}

	dataDirectory = dir + "/.bustapay"
	os.Mkdir(dataDirectory, 0700)

	dataDirectory = dataDirectory + "/data"
	os.Mkdir(dataDirectory, 0700)
}
