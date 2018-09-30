package cmd

import (
	"github.com/spf13/cobra"
	"errors"
	"github.com/btcsuite/btcutil"
	"github.com/rhavar/bustapay/send"
	"strconv"
	"math"
	"log"
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send bitcoin to a bustapay url",
	Long: `A wrapper around a few bitcoin rpc calls.

usage: bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN

`,
	Args: func(cmd *cobra.Command, args []string) error {

		if len(args) != 3 {
			return errors.New("Incorrect usage. Used like: bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN")
		}

		if _, err := btcutil.DecodeAddress(args[0], nil); err != nil {
			return errors.New("could not decode bitcoin address " + args[0])
		}

		if _, err := strconv.ParseFloat(args[2], 64); err != nil {
			return errors.New("could not parse " + args[2] + " as a floating point number")
		}


		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {

		bitcoinAddress := args[0]
		bustapayUrl  := args[1]
		amountBtc, _ := strconv.ParseFloat(args[2], 64)


		amount := int64(math.Round(amountBtc * 1e8))

		err := send.Send(bitcoinAddress, bustapayUrl, amount)
		if err != nil {
			log.Printf("%+v\n", err)
		}

	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
