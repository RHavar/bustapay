package util

import (
	"github.com/spf13/viper"
	"log"
)

func VerboseLog(vals ...interface{}) {
	if viper.GetBool("verbose") {
		log.Println(vals...)
	}
}
