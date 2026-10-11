package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/goyek/goyek/v2"

	"github.com/spachava753/cpe/internal/pathcollate/generate"
)

var _ = goyek.Define(goyek.Task{
	Name:  "collation-data",
	Usage: "Regenerate pinned ICU 78.3 English/root collation tables",
	Action: func(a *goyek.A) {
		const url = "https://raw.githubusercontent.com/unicode-org/icu/release-78.3/icu4c/source/data/unidata/FractionalUCA.txt"
		request, err := http.NewRequestWithContext(
			a.Context(),
			http.MethodGet,
			url,
			nil,
		)
		if err != nil {
			a.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			a.Fatal(err)
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				a.Error(err)
			}
		}()
		if response.StatusCode != http.StatusOK {
			a.Fatal(fmt.Errorf(
				"download collation data: %s",
				response.Status,
			))
		}
		var output bytes.Buffer
		if err := generate.Tables(
			io.LimitReader(
				response.Body,
				2<<20,
			),
			&output,
		); err != nil {
			a.Fatal(err)
		}
		if err := os.WriteFile(
			"internal/pathcollate/tables.go",
			output.Bytes(),
			0o644,
		); err != nil {
			a.Fatal(err)
		}
	},
})
