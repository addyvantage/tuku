package main

import (
	"context"
	"fmt"
	"os"

	"tuku/internal/app"
)

func main() {
	ctx := context.Background()
	daemon := app.NewDaemonApplication()
	if err := daemon.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "tukud: %v\n", err)
		os.Exit(1)
	}
}
