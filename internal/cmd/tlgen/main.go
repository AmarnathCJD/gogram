package main

import (
	"fmt"
	"os"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/gen"
	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

const helpMsg = `tlgen
usage: tlgen input_file.tl output_dir/
THIS TOOL IS USING ONLY FOR AUTOMATIC CODE
GENERATION, DO NOT GENERATE FILES BY HAND!
No, seriously. Don't. go generate is amazing. You
are amazing too, but lesser 😏
`
const license = `-`

func main() {
	if len(os.Args) != 3 {
		fmt.Println(helpMsg)
		return
	}

	if err := root(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func root(tlfile, outdir string) error {
	b, err := os.ReadFile(tlfile)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}

	schema, err := tlparser.ParseSchema(string(b))
	if err != nil {
		return fmt.Errorf("parse schema file: %w", err)
	}

	g, err := gen.NewGenerator(schema, license, outdir)
	if err != nil {
		return err
	}

	return g.Generate()
}
