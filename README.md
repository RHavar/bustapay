bustapay
--------


This project is designed as a reference implementation of the bustapay standard, as well as a simple way to send or receive bustapay paymetns. It delegates out most the work to bitcoin core's wallet over rpc. When receiving transactions, for simplicity it stores the information in awkward flat-file style database.

Building
========
It's a pretty simple golang project, so with a go environment setup is simply:
```
dep ensure
go install
```

Which will create a self-contained executable `bustapay`


Configuration
=============
Global options are:

verbose  (default: false)
bitcoind_host  (default: localhost)
bitcoind_port  (default: 8332)
bitcoind_user  (no default)
bitcoind_pass  (no default)

They can be passed via command line  (e.g.  --verbose=true) or via the ~/.bustapay/config.yaml  (e.g.   verbose: true) or env variables (e.g. VERBOSE=true)


Sending
=======

bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN

This works pretty simply under the hood. Conceptually it's:

```
$UNFUNDED :=  createrawtransaction [] { ""$BITCOIN_ADDRESS": $AMOUNT_IN_BITCOIN }
$FUNDED := fundrawtransaction $UNFUNDED`
$TEMPLATE := signrawtransactionwithwallet $FUNDED`
$PARTIAL := curl -d $TEMPLATE_TRANSACTION $BUSTAPAY_URL
$FINAL := signrawtransactionwithwallet $PARTIAL # after some validation!
sendrawtransaction $FINAL
```

Receiving
=========

bustapay receive

Which will create an HTTP server that listens for bustapay payments. To avoid making it too opinionated and bringing in a proper database it stores bustapay transactions as a flat file. For each received bustapay transaction it will create the directory:

~/.bustapay/data/$FINAL_TRANSACTION_ID

and in that directory will create the follow files:

partial_transaction.hex  # the final (but partial) transaction (that the user needs to sign)
amount.txt # the amount the person is sending us in satoshis (thus it's an integer)
template_transaction.hex # the raw template transaction in hex


It is still the receivers responsibility to do the rest of payment processing, and detecting if a received transaction is a bustapay transaction or not.