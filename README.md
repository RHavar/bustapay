bustapay
--------


This project is designed as a reference implementation of the bustapay standard. It does all the work via bitcoin core  over rpc. It can be used to either send or receive bustapay transactions. Sending bustapay transactions is considerably more straight forward.

Sending
=======

./bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN

This works pretty simply under the hood. Conceptually it's:

```
$UNFUNDED :=  createrawtransaction [] { ""$BITCOIN_ADDRESS": $AMOUNT_IN_BITCOIN }
$FUNDED := fundrawtransaction $UNFUNDED`
$TEMPLATE := signrawtransaction $FUNDED`
$PARTIAL := curl -d $TEMPLATE_TRANSACTION $BUSTAPAY_URL
$FINAL := signrawtransactionwithwallet $PARTIAL # after some validation!
sendrawtransaction $FINAL
```

Receiving
=========

This is a little more involved. To avoid making it too opinionated and bringing in a proper database it stores bustapay transactions as a flat file. For each received bustapay transaction it will create the directory:

~/.bustapay/$FINAL_TRANSACTION_ID

and in that directory will create the follow files:

partial_transaction.hex  # the final (but partial) transaction (that the user needs to sign)
amount.txt # the amount the person is sending us in satoshis (thus it's an integer)
template_transaction.hex # the raw template transaction in hex


It is still the receivers responsibility to do the rest of payment processing, and detecting if a received transaction is a bustapay transaction or not.