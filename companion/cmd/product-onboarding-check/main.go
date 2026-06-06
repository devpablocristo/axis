package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/devpablocristo/companion/internal/productcontracts"
	"github.com/devpablocristo/companion/internal/productevals"
)

func main() {
	contractPath := flag.String("contract", "", "Path to a product onboarding contract JSON file")
	evalPackPath := flag.String("eval-pack", "", "Optional path to a product golden eval pack JSON file")
	flag.Parse()

	if *contractPath == "" {
		fmt.Fprintln(os.Stderr, "-contract is required")
		os.Exit(2)
	}
	spec, err := productcontracts.LoadSpec(*contractPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load contract: %v\n", err)
		os.Exit(2)
	}
	if *evalPackPath != "" {
		pack, err := productevals.LoadPack(*evalPackPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load eval pack: %v\n", err)
			os.Exit(2)
		}
		spec.EvalPack = &pack
	}
	report := productcontracts.ValidateSpec(spec)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(2)
	}
	if report.Status != productcontracts.StatusPassed {
		os.Exit(1)
	}
}
