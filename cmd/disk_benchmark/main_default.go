//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintf(os.Stderr, "Error: disk_benchmark is only supported on Linux systems.\n")
	fmt.Fprintf(os.Stderr, "Please run this benchmark on a Linux VM.\n")
	os.Exit(1)
}

