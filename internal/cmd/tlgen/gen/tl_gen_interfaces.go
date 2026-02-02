package gen

import (
	"regexp"
	"sort"
	"sync"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
	"github.com/dave/jennifer/jen"
)

func (g *Generator) generateInterfaces(f *jen.File, d bool) {
	keys := make([]string, 0, len(g.schema.Types))
	for key := range g.schema.Types {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// wg := sync.WaitGroup{}
	// for _, key := range keys {
	// 	structs := g.schema.Types[key]

	// 	fmt.Println("Gk", key)
	// 	wg.Add(1)
	// 	go func(structs []tlparser.Object, key string) {
	// 		defer wg.Done()
	// 		for j, _type := range structs {
	// 			go func(_type tlparser.Object, j int, key string) {
	// 				g.schema.Types[key][j].Comment = g.generateComment(_type.Name, "constructor")

	// 				fmt.Println("Gk", key, "Comment", g.schema.Types[key][j].Comment)
	// 			}(_type, j, key)
	// 		}
	// 	}(structs, key)
	// }

	// wg.Wait()

	for _, i := range keys {
		f.Add(jen.Type().Id(goify(i, true)).Interface(
			jen.Qual(tlPackagePath, "Object"),
			jen.Id("Implements"+goify(i, true)).Params(),
		))

		structs := g.schema.Types[i]

		sort.Slice(structs, func(i, j int) bool {
			return structs[i].Name < structs[j].Name
		})

		if d {
			wg := sync.WaitGroup{}
			for i, _type := range structs {
				wg.Add(1)
				go func(_type tlparser.Object, i int) {
					defer wg.Done()
					comment, pComments := g.generateComment(_type.Name, "constructor")
					structs[i].Comment = comment

					if pComments != nil && len(pComments) == len(_type.Parameters) {
						for j := range _type.Parameters {
							pComments[j] = regexp.MustCompile(`(?i)<a\s+href="([^"]+)"\s*>([^<]+)</a>`).ReplaceAllString(pComments[j], "$2")
							pComments[j] = regexp.MustCompile(`(?i)<strong>([^<]+)</strong>`).ReplaceAllString(pComments[j], "$1")
							pComments[j] = regexp.MustCompile(`\[(.+)\]\(.+\)`).ReplaceAllString(pComments[j], "$1")
							pComments[j] = regexp.MustCompile(`(?i)see here[^.]*`).ReplaceAllString(pComments[j], "")
							pComments[j] = regexp.MustCompile(`(?i)<code>([^<]+)</code>`).ReplaceAllString(pComments[j], "'$1'")
							pComments[j] = regexp.MustCompile(`(?i)<br>`).ReplaceAllString(pComments[j], "\n")
							pComments[j] = regexp.MustCompile(`(?i)»`).ReplaceAllString(pComments[j], "")
							pComments[j] = regexp.MustCompile(`(?i)\s+\.`).ReplaceAllString(pComments[j], ".")
							pComments[j] = regexp.MustCompile(`(?i),\s*$`).ReplaceAllString(pComments[j], "")
							structs[i].Parameters[j].Comment = pComments[j]
						}
					}
				}(_type, i)
			}

			wg.Wait()
		}

		for _, _type := range structs {
			if goify(_type.Name, true) == goify(i, true) {
				_type.Name += "Obj"
			}

			f.Add(g.generateStructTypeAndMethods(_type, []string{goify(i, true)}))
			f.Line()
		}
	}
}
