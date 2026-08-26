package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/iml885203/orbit/internal/distribution"
)

func main() {
	metadata, err := distribution.Export()
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(metadata)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "distribution metadata: %v\n", err)
		os.Exit(1)
	}
}
