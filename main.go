package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/snyk/snyk-code-review-exercise/api"
)

func main() {
	handler := api.New()
	// review: this is running on HTTP which is unsecure, use TLS
	fmt.Println("Server running on http://localhost:3000/")

	// idea: add logging, context, timeouts
	// idea: add global/root scoped cache of top `n` recently/frequently used packages to prevent re-fetching across different user requests
	// | - idea: consider storing the values compressed, if space becomes an issue
	// idea: add npm PGP signature verification to requests
	if err := http.ListenAndServe("localhost:3000", handler); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
