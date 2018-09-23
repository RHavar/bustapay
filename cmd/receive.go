package cmd

import (
	"github.com/spf13/cobra"
)

var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "Start a bustapay server to listen for requests",
	Long: `Starts an HTTP server, and stores bustapay requests in the ~/.bustapay  directory

usage: bustapay receive
`,
	Run: func(cmd *cobra.Command, args []string) {


	},
}

func init() {
	rootCmd.AddCommand(receiveCmd)
}
