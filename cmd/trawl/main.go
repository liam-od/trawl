package main

import (
	"fmt"
	"os"
)

var version = "0.0.0-dev"

func main() {
	fmt.Fprintf(os.Stderr, "trawl %s\n", version)
	fmt.Fprintln(os.Stderr, "usage: trawl [user@host[:path]]  (not implemented yet)")
	os.Exit(0)
}
