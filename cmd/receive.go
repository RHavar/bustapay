package cmd

import (
	"github.com/rhavar/bustapay/receive"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "Start a bustapay server to listen for requests",
	Long: `Starts an HTTP server, and stores bustapay requests in the ~/.bustapay/data  directory

usage: bustapay receive
`,
	Run: func(cmd *cobra.Command, args []string) {

		receive.StartServer(viper.GetInt32("port"))
	},
}

func init() {
	receiveCmd.Flags().Int32P("port", "p", 8080, "Which port to listen to")
	viper.BindPFlag("port", receiveCmd.Flags().Lookup("port"))
	rootCmd.AddCommand(receiveCmd)
}
