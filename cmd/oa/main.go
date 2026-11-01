package main

import (
	"os"

	"github.com/0x46616c6b/openaleph-cli/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
