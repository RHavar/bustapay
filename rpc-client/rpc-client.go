package rpc_client

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.com/rhavar/bustapay/util"
	"github.com/btcsuite/btcd/chaincfg"
	"regexp"
	"log"
)

// This is a wrapper around btcd/rpcclient to make it a bit easier to use
type RpcClient struct {
	rpcClient *rpcclient.Client
}

/// The caller must always becareful to call client.Shutdown()!
func NewRpcClient() (*RpcClient, error) {

	host := viper.GetString("bitcoind_host") + ":" + viper.GetString("bitcoind_port")
	user := viper.GetString("bitcoind_user")
	pass := viper.GetString("bitcoind_pass")

	util.VerboseLog("Connecting to ", host, " with ", user, " and using pass ", pass != "")


	cfg := &rpcclient.ConnConfig{
		Host:         host,
		User:         user,
		Pass:         pass,
		HTTPPostMode: true, // Bitcoin only supports HTTP POST mode
		DisableTLS:   true, // Bitcoin does not provide TLS by default
	}

	rpcClient, err := rpcclient.New(cfg, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &RpcClient{rpcClient: rpcClient}, nil
}

func (rc *RpcClient) Shutdown() {
	rc.rpcClient.Shutdown()
}


var memoizoidChain *chaincfg.Params
func (rc *RpcClient) GetChainParams() (*chaincfg.Params, error) {
	if memoizoidChain != nil {
		return memoizoidChain, nil
	}

	info, err := rc.rpcClient.GetBlockChainInfo()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	switch chain := info.Chain; chain {
	case "main":
		memoizoidChain = &chaincfg.MainNetParams
	case "test":
		memoizoidChain = &chaincfg.TestNet3Params
	case "regtest":
		memoizoidChain = &chaincfg.SimNetParams
	default:
		panic("unexpected chain: " + chain)
	}

	return memoizoidChain, nil
}


func (rc *RpcClient) GetNewAddress() (btcutil.Address, error) {
	addr, err := rc.rpcClient.GetNewAddress("")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return addr, nil
}

// hacky, eats errors...
func (rc *RpcClient) MempoolHasEntry(txid string) bool {
	entry, err := rc.rpcClient.GetMempoolEntry(txid)
	return err == nil && entry != nil

}

func (rc *RpcClient) CreateRawTransaction(address string, amount int64) (*wire.MsgTx, error) {

	addr, err := btcutil.DecodeAddress(address, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Avoiding rpc call due to: https://github.com/btcsuite/btcd/issues/1311
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	tx := wire.MsgTx{}
	tx.AddTxOut(wire.NewTxOut(amount, pkScript))

	// TODO: set the tx lock time to match core..

	return &tx, nil
}

type FRTResult struct {
	Hex string `json:"hex"`
}

func (rc *RpcClient) FundRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, error) {

	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		return nil, errors.WithStack(err)
	}

	j, err := json.Marshal(hex.EncodeToString(byteBuffer.Bytes()))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	rm, err := rc.rpcClient.RawRequest("fundrawtransaction", []json.RawMessage{j})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	res := FRTResult{}
	if err := json.Unmarshal(rm, &res); err != nil {
		return nil, errors.WithStack(err)
	}

	serializedTx, err := hex.DecodeString(res.Hex)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(bytes.NewReader(serializedTx)); err != nil {
		return nil, errors.WithStack(err)
	}

	return &msgTx, nil
}

func (rc *RpcClient) SendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	return rc.rpcClient.SendRawTransaction(tx, false)
}

func (rc *RpcClient) SignRawTransactionWithWallet(tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	txByteBuffer := bytes.Buffer{}
	err := tx.Serialize(&txByteBuffer)
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	jsonData, err := json.Marshal(hex.EncodeToString(txByteBuffer.Bytes()))
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	resultJson, err := rc.rpcClient.RawRequest("signrawtransactionwithwallet", []json.RawMessage{jsonData})
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	var result SignRawTransactionResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	txBytes, err := hex.DecodeString(result.Hex)
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	newTx, err := btcutil.NewTxFromBytes(txBytes)
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	return newTx.MsgTx(), result.Complete, nil
}

type SignRawTransactionResult struct {
	Hex      string        `json:"hex"`
	Complete bool          `json:"complete"`
	Errors   []interface{} `json:"errors"`
}

// Note this is only segwit compatible. Don't use it to sign non-segwit inputs
func (rc *RpcClient) SafeSignRawTransactionWithWallet(tx *wire.MsgTx, inputToSign int) (*wire.MsgTx, bool, error) {
	res, complete, err := rc.SignRawTransactionWithWallet(tx)
	if err != nil {
		return nil, false, errors.WithStack(err)
	}

	for i := 0; i < len(tx.TxIn); i++ {
		originalTxIn := tx.TxIn[i]
		newTxIn := res.TxIn[i]

		we := witnessEqual(originalTxIn.Witness, newTxIn.Witness)

		if i == inputToSign {
			if we {
				return nil, false, errors.New(fmt.Sprint("witness did not change for input ", i, " in tx ", tx.TxHash().String(), " that we should have signed"))
			}
		} else if !we {
			return nil, false, errors.New(fmt.Sprint("witness changed for input ", i, " in tx ", tx.TxHash().String(), " but we should have only signed ", inputToSign))
		} else if !bytes.Equal(originalTxIn.SignatureScript, newTxIn.SignatureScript) {
			return nil, false, errors.New(fmt.Sprint("signature script changed for input ", i, " in tx ", tx.TxHash().String()))
		}
	}

	return res, complete, nil
}

func witnessEqual(w1 wire.TxWitness, w2 wire.TxWitness) bool {
	if len(w1) != len(w2) {
		return false
	}

	for i := 0; i < len(w1); i++ {
		sw1 := w1[0]
		sw2 := w2[1]

		if !bytes.Equal(sw1, sw2) {
			return false
		}
	}
	return true
}

func (rc *RpcClient) GetTxOut(txid *chainhash.Hash, vout uint32) (*btcjson.GetTxOutResult, error) {
	return rc.rpcClient.GetTxOut(txid, vout, false)
}

type MemPoolAcceptResult struct {
	Txid         string `json:"txid"`
	Allowed      bool   `json:"allowed"`
	RejectReason string `json:"reject-reason"`
}

func (rc *RpcClient) TestMempoolAccept(tx *wire.MsgTx) (bool, error) {
	// NOTE: requires bitcoin core 0.17.x

	txByteBuffer := bytes.Buffer{}
	err := tx.Serialize(&txByteBuffer)
	if err != nil {
		return false, err
	}

	jsonData, err := json.Marshal([]string{hex.EncodeToString(txByteBuffer.Bytes())})
	if err != nil {
		return false, err
	}

	resultJson, err := rc.rpcClient.RawRequest("testmempoolaccept", []json.RawMessage{jsonData})
	if err != nil {
		return false, err
	}

	var result []MemPoolAcceptResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return false, err
	}

	return result[0].Allowed, nil
}

// This is extremely unoptimized! It will be painful on a large wallet
func (rc *RpcClient) IsMyFreshMyAddress(address string) (bool, error) {

	info, err := rc.GetAddressInfo(address)
	if err != nil {
		return false, nil
	}

	if !info.IsMine || info.IsChange {
		return false, nil
	}

	receives, err := rc.rpcClient.ListReceivedByAddress()
	if err != nil {
		return false, err
	}

	for _, receive := range receives {
		if receive.Address == address { // this address has been used before :/
			return false, err
		}

	}

	return true, nil
}

func (rc *RpcClient) ListUnspent() ([]btcjson.ListUnspentResult, error) {
	unspent, err := rc.rpcClient.ListUnspent()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return unspent, nil
}

type AddressInfoResult struct {
	Address   string `json:"address"`
	IsMine    bool   `json:"ismine"`
	HdKeyPath string `json:"hdkeypath"`
	IsChange  bool
}

// hack to detect change. Change hdpath looks like  m/0'/1'/9999999'
var changeHdPathRegex = regexp.MustCompile(`m/0'/1'/\d+'`)

func (rc *RpcClient) GetAddressInfo(address string) (*AddressInfoResult, error) {

	jsonData, err := json.Marshal(address)
	if err != nil {
		return nil, err
	}

	resultJson, err := rc.rpcClient.RawRequest("getaddressinfo", []json.RawMessage{jsonData})
	if err != nil {
		return nil, err
	}

	var result AddressInfoResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return nil, err
	}

	result.IsChange = changeHdPathRegex.MatchString(result.HdKeyPath)

	log.Println("Address: ", address, " is change: ", result.IsChange)


	return &result, nil
}
