package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/clinicalcapabilities"
)

func main() {
	productSurface := flag.String("product-surface", os.Getenv("AXIS_PRODUCT_SURFACE"), "consumer-owned product surface")
	flag.Parse()
	if strings.TrimSpace(*productSurface) == "" {
		_, _ = fmt.Fprintln(os.Stderr, "-product-surface or AXIS_PRODUCT_SURFACE is required")
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(clinicalcapabilities.Definitions(*productSurface)); err != nil {
		panic(err)
	}
}
