package main

import (
	"encoding/json"
	"os"

	"github.com/devpablocristo/companion-v2/internal/clinicalcapabilities"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(clinicalcapabilities.Definitions()); err != nil {
		panic(err)
	}
}
