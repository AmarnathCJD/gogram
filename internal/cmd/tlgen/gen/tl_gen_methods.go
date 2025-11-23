package gen

import (
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dave/jennifer/jen"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

var maximumPositionalArguments = 5

var (
	docHTTPClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Improved regex patterns for cleaning documentation
	regexHTMLLink         = regexp.MustCompile(`<a\s+[^>]*href="([^"]*?)"[^>]*>([^<]+)</a>`)
	regexMarkdownLinkFull = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	regexSeeReference     = regexp.MustCompile(`(?i),?\s*see\s+(?:here|the\s+docs?|documentation)[^.;]*[.;]?`)
	regexArrowSymbol      = regexp.MustCompile(`[»›]`)
	regexHTMLEntities     = regexp.MustCompile(`&[a-z]+;`)
	regexExtraWhitespace  = regexp.MustCompile(`\s{2,}`)
	regexTrailingPunct    = regexp.MustCompile(`[,;]\s*$`)
	regexLeadingPunct     = regexp.MustCompile(`^[,;]\s*`)
)

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

func (g *Generator) generateComment(name, _type string) (string, []string) {
	var base = "https://corefork.telegram.org/" + _type + "/"
	req, _ := http.NewRequest("GET", base+name, http.NoBody)
	req.Header.Set("User-Agent", "TLGen/1.0")
	log.Println("tlgen: fetching", req.URL.String())

	resp, err := docHTTPClient.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil
	}

	ack := string(body)

	// Extract parameter descriptions from table
	matches := regexTableTag.FindAllStringSubmatch(ack, -1)
	var descs []string
	for _, match := range matches {
		if len(match) > 1 {
			descs = append(descs, cleanHTMLContent(match[1]))
		}
	}

	// Extract main description
	parts := strings.Split(ack, `<div id="dev_page_content">`)
	if len(parts) < 2 {
		return "", nil
	}
	ack = parts[1]

	// Get first paragraph only
	if idx := strings.Index(ack, "</p>"); idx != -1 {
		ack = ack[:idx]
	}

	// Quick validation checks
	if strings.Contains(ack, "The page has not been saved") ||
		strings.Contains(ack, `<ul class="dev_layer_select`) {
		return "", nil
	}

	// Clean HTML and formatting
	ack = cleanHTMLContent(ack)

	return ack, descs
}

func cleanHTMLContent(content string) string {
	// Remove HTML tags but preserve text
	content = strings.ReplaceAll(content, "<p>", "")
	content = strings.ReplaceAll(content, "</p>", "")
	content = strings.ReplaceAll(content, "<br>", " ")
	content = strings.ReplaceAll(content, "<br/>", " ")
	content = regexStrongTag.ReplaceAllString(content, "$1")
	content = regexCodeTag.ReplaceAllString(content, "`$1`")

	// Remove all links but keep link text
	content = regexHTMLLink.ReplaceAllString(content, "$2")
	content = regexMarkdownLinkFull.ReplaceAllString(content, "$1")

	// Remove "see here" references and arrows
	content = regexSeeReference.ReplaceAllString(content, "")
	content = regexArrowSymbol.ReplaceAllString(content, "")

	// Decode common HTML entities
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&quot;", `"`)
	content = strings.ReplaceAll(content, "&#39;", "'")
	content = regexHTMLEntities.ReplaceAllString(content, "")

	// Clean up whitespace and punctuation
	content = regexExtraWhitespace.ReplaceAllString(content, " ")
	content = regexTrailingPunct.ReplaceAllString(content, "")
	content = regexLeadingPunct.ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, " .", ".")
	content = strings.ReplaceAll(content, " ,", ",")

	return strings.TrimSpace(content)
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
	//		return nil, fmt.Errorf("sending AuthSendCode: %w", err)
	//	}
	//
	//	resp, ok := data.(*AuthSentCode)
	//	if !ok {
	//		return nil, fmt.Errorf("got invalid response type: %s", reflect.TypeOf(data))
	//	}
	//
	//	return resp, nil

	method := jen.Func().Params(jen.Id("c").Op("*").Id("Client")).Id(goify(obj.Name, true)).Params(g.generateArgumentsForMethod(obj)...).Params(responses...).Block(
		jen.List(jen.Id("responseData"), jen.Id("err")).Op(":=").Id("c").Dot("MakeRequest").Call(g.generateMethodArgumentForMakingRequest(obj)),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(nuk, jen.Qual("fmt", "Errorf").Call(jen.Lit("sending "+goify(obj.Name, true)+": %w"), jen.Err())),
		),
		jen.Line(),
		jen.List(jen.Id("resp"), jen.Id("ok")).Op(":=").Id("responseData").Assert(resp),
		jen.If(jen.Op("!").Id("ok")).Block(
			jen.Return(
				nuk,
				jen.Qual("fmt", "Errorf").Call(
					jen.Lit("got invalid response type: %s"),
					jen.Qual("reflect", "TypeOf").Call(jen.Id("responseData")),
				),
			),
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

func (g *Generator) generateMethodArgumentForMakingRequest(obj *tlparser.Method) *jen.Statement {
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
