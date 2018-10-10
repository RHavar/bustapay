package receive

import (
	"net/http"
	"github.com/rhavar/bustapay/rpc-client"
	"fmt"
	"github.com/btcsuite/btcutil"
	"sync"
)

// This is an insanely bad method designed for testing. Do not consider use anywhere near a production site..

// Instead of always getting a new address, we only do so when the last one has been used
// this is a pretty poorly designed function, but it's a quick hack to prevent a simple and
// annoying DoS of requesting billions of addresses


var lastNewishAddress btcutil.Address
var newishMutex sync.Mutex // not scoped particularly well...


func getNewishAddress(w http.ResponseWriter, r *http.Request) {

	newishMutex.Lock()
	defer newishMutex.Unlock()

	// keep error handling centralized
	address, err := func() (btcutil.Address, error) {

		rpcClient, err := rpc_client.NewRpcClient()
		if err != nil {
			return nil, err
		}
		defer rpcClient.Shutdown()



		if lastNewishAddress == nil {
			lastNewishAddress, err = rpcClient.GetNewAddress()
			if err != nil {
				return nil, err
			}
		}

		fresh, err := rpcClient.IsMyFreshMyAddress(lastNewishAddress.String())
		if err != nil {
			return nil, err
		}

		if !fresh {
			lastNewishAddress, err = rpcClient.GetNewAddress()
			if err != nil {
				return nil, err
			}
		}

		return lastNewishAddress, nil

	}()

	if err != nil {
		w.WriteHeader(400)
		fmt.Fprint(w, "internal error")
		fmt.Println("[ERROR] get-newish-address error", err)
		return
	}

	fmt.Fprint(w, address.String())
}
