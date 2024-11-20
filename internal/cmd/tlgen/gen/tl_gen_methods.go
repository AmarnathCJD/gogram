package gen

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/dave/jennifer/jen"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

var maximumPositionalArguments = 5

func (g *Generator) generateMethods(f *jen.File, d bool) {
	sort.Slice(g.schema.Methods, func(i, j int) bool {
		return g.schema.Methods[i].Name < g.schema.Methods[j].Name
	})

	if d {
		wg := sync.WaitGroup{}
		for i, method := range g.schema.Methods {
			wg.Add(1)
			go func(method tlparser.Method, i int) {
				defer wg.Done()
				g.schema.Methods[i].Comment, _ = g.generateComment(method.Name, "method")
			}(method, i)
		}

		wg.Wait()
	}

	for _, method := range g.schema.Methods {

		f.Add(g.generateStructTypeAndMethods(tlparser.Object{
			Name:       method.Name + "Params",
			Comment:    "",
			CRC:        method.CRC,
			Parameters: method.Parameters,
		}, nil))
		f.Line()
		if method.Comment != "" {
			f.Comment(method.Comment)
		}
		f.Add(g.generateMethodFunction(&method))
		f.Line()
	}

	//	sort.Strings(keys)
	//
	//	for _, i := range keys {
	//		structs := g.schema.Types[i]
	//
	//		sort.Slice(structs, func(i, j int) bool {
	//			return structs[i].Name < structs[j].Name
	//		})
	//
	//		for _, _type := range structs {
	//			f.Add(g.generateStructTypeAndMethods(_type, []string{goify(i, true)}))
	//			f.Line()
	//		}
	//	}
}

func (*Generator) generateComment(name, _type string) (string, []string) {
	var base = "https://core.telegram.org/" + _type + "/"
	fmt.Println(base + name)
	req, _ := http.NewRequest("GET", base+name, http.NoBody)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil
	}

	if resp.StatusCode != 200 {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil
	}

	ack := string(body)

	matches := regexTableTag.FindAllStringSubmatch(ack, -1)

	var descs []string

	for _, match := range matches {
		descs = append(descs, match[1])
	}

	ack = strings.Split(ack, "<div id=\"dev_page_content\">")[1]
	ack = strings.Split(ack, "</p>")[0]
	ack = strings.ReplaceAll(ack, "<p>", "")
	// ack = strings.ReplaceAll(ack, "see .", "")
	ack = regexLinkTag.ReplaceAllString(ack, "[$2](https://core.telegram.org$1)")

	ack = regexCodeTag.ReplaceAllString(ack, "`$1`")

	ack = strings.TrimSpace(ack)

	if strings.Contains(ack, "The page has not been saved") {
		return "", nil
	}

	return ack, descs
}

func (g *Generator) generateMethodFunction(obj *tlparser.Method) jen.Code {
	resp := g.typeIdFromSchemaType(obj.Response.Type)
	if obj.Response.IsList {
		resp = jen.Index().Add(resp)
	}

	nuk := jen.Nil()
	if obj.Response.Type == "Bool" {
		resp = jen.Op("").Qual("", "bool")
		nuk = jen.Bool()
	}

	responses := []jen.Code{resp, jen.Error()}

	//	data, err := c.MakeRequest(params)
	//	if err != nil {
	//		return nil, fmt.Errorf("sedning AuthSendCode: %w", err)
	//	}
	//
	//	resp, ok := data.(*AuthSentCode)
	//	if !ok {
	//		panic("got invalid response type: " + reflect.TypeOf(data).String())
	//	}
	//
	//	return resp, nil

	method := jen.Func().Params(jen.Id("c").Op("*").Id("Client")).Id(goify(obj.Name, true)).Params(g.generateArgumentsForMethod(obj)...).Params(responses...).Block(
		jen.List(jen.Id("responseData"), jen.Id("err")).Op(":=").Id("c").Dot("MakeRequest").Call(g.generateMethodArgumentForMakingRequest(obj)),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(nuk, jen.Qual(errorsPackagePath, "Wrap").Call(jen.Err(), jen.Lit("sending "+goify(obj.Name, true)))),
		),
		jen.Line(),
		jen.List(jen.Id("resp"), jen.Id("ok")).Op(":=").Id("responseData").Assert(resp),
		jen.If(jen.Op("!").Id("ok")).Block(
			jen.Panic(jen.Lit("got invalid response type: ").Op("+").Qual("reflect", "TypeOf").Call(jen.Id("responseData")).Dot("String").Call()),
		),
		jen.Return(jen.Id("resp"), jen.Nil()),
	)

	return method
}

func (g *Generator) generateArgumentsForMethod(obj *tlparser.Method) []jen.Code {
	if len(obj.Parameters) == 0 {
		return []jen.Code{}
	}
	if len(obj.Parameters) > maximumPositionalArguments {
		return []jen.Code{jen.Id("params").Op("*").Id(goify(obj.Name, true) + "Params")}
	}

	items := make([]jen.Code, 0)

	for i, p := range obj.Parameters {
		item := jen.Id(goify(p.Name, false))
		if i == len(obj.Parameters)-1 || p.Type != obj.Parameters[i+1].Type || p.IsVector != obj.Parameters[i+1].IsVector {
			if p.Type == "bitflags" {
				continue
			}

			if p.IsVector {
				item = item.Add(jen.Index(), g.typeIdFromSchemaType(p.Type))
			} else {
				item = item.Add(g.typeIdFromSchemaType(p.Type))
			}
		}

		items = append(items, item)
	}
	return items
}

func (*Generator) generateMethodArgumentForMakingRequest(obj *tlparser.Method) *jen.Statement {
	if len(obj.Parameters) > maximumPositionalArguments {
		return jen.Id("params")
	}

	dict := jen.Dict{}
	for _, p := range obj.Parameters {
		if p.Type == "bitflags" {
			continue
		}

		dict[jen.Id(goify(p.Name, true))] = jen.Id(goify(p.Name, false))
	}

	return jen.Op("&").Id(goify(obj.Name, true) + "Params").Values(dict)
}
