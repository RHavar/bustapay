package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send bitcoin to a bustapay url",
	Long: `A wrapper around a few bitcoin rpc calls.

usage: ./bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN
`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 3 {
			fmt.Errorf("usage: bustapay send $BITCOIN_ADDRESS $BUSTAPAY_URL $AMOUNT_IN_BITCOIN\n")
			return
		}

		fmt.Println("send called", args)
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
