package utils

import (
	"fmt"
	"log"
)

var logger = log.New(log.Writer(), "", log.LstdFlags)

func Log(message string) {
	logger.Println(message)
}
func LogRed(message string) {
	fmt.Printf("%s%s%s\n", Red, message, Reset)
}

func LogGreen(message string) {
	fmt.Printf("%s%s%s\n", Green, message, Reset)
}

func LogYellow(message string) {
	fmt.Printf("%s%s%s\n", Yellow, message, Reset)
}

func LogCyan(message string) {
	fmt.Printf("%s%s%s\n", Cyan, message, Reset)
}
