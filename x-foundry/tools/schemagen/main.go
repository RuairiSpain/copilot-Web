// Command schemagen publishes the generated schema files under schemas/.
//
//	go run ./tools/schemagen           # regenerate
//	go run ./tools/schemagen -check    # fail if schemas/ is stale
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/schemapub"
)

func main() {
	check := flag.Bool("check", false, "fail if the published files are out of date")
	root := flag.String("root", ".", "x-foundry module directory")
	flag.Parse()
	files, err := schemapub.Render(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		if stale := schemapub.Stale(*root, files); len(stale) > 0 {
			fmt.Fprintf(os.Stderr, "stale published schema files:\n  %s\n", strings.Join(stale, "\n  "))
			os.Exit(1)
		}
		return
	}
	if err := schemapub.Write(*root, files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d files\n", len(files))
}
