package display

import (
	"fmt"
	"github.com/fatih/color"
	"strings"
)

var formatterOk = color.New(color.FgHiGreen)
var formatterNok = color.New(color.FgHiRed)

func ReportSuccess(msg string) {
	dotsCount := 80 - len(msg)
	if dotsCount < 0 {
		dotsCount = 5
	}
	fmt.Printf("%s %s [%s]\n", msg, strings.Repeat(".", dotsCount), formatterOk.Sprintf("OK"))
}

func ReportFailure(msg string, err error) {
	dotsCount := 80 - len(msg)
	if dotsCount < 0 {
		dotsCount = 5
	}
	fmt.Printf("%s %s [%s]\n", msg, strings.Repeat(".", dotsCount), formatterNok.Sprintf("%s", err.Error()))
}
