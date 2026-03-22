package main

import (
	"context"
	"fmt"
	"os"

	"tuku/internal/app"
)

func main() {
	ctx := context.Background()
	cli := app.NewCLIApplication()
	if err := cli.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tuku: %v\n", err)
		os.Exit(1)
	}
}
