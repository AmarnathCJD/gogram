package gen

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	"github.com/bs9/spread_service_gogram/internal/cmd/tlgen/tlparser"
)

func createParamsStructFromMethod(method tlparser.Method) tlparser.Object {
	return tlparser.Object{
		Name:       method.Name + "Params",
		CRC:        method.CRC,
		Parameters: method.Parameters,
	}
}

func haveOptionalParams(params []tlparser.Parameter) bool {
	for _, param := range params {
		if param.IsOptional {
			return true
		}
	}

	return false
}

func maxBitflag(params []tlparser.Parameter) int {
	maximum := 0
	for _, param := range params {
		if param.BitToTrigger > maximum {
			maximum = param.BitToTrigger
		}
	}

	return maximum
}

func goify(name string, public bool) string {
	delim := strcase.ToDelimited(name, '|')
	delim = strings.ReplaceAll(delim, ".", "|")
	splitted := strings.Split(delim, "|")
	for i, item := range splitted {
		item = strings.ToLower(item)
		if SliceContains(capitalizePatterns, item) {
			item = strings.ToUpper(item)
		}

		itemRunes := []rune(item)

		if i == 0 && !public {
			itemRunes = []rune(strings.ToLower(item))
		} else {
			itemRunes[0] = unicode.ToUpper(itemRunes[0])
		}

		splitted[i] = string(itemRunes)
	}

	return strings.Join(splitted, "")
}

func (g *Generator) typeIdFromSchemaType(t string) *jen.Statement {
	item := &jen.Statement{}
	switch t {
	case "Bool":
		item = jen.Bool()
	case "long":
		item = jen.Int64()
	case "int256":
		item = jen.Qual(tlPackagePath, "Int256")
	case "double":
		item = jen.Float64()
	case "int":
		item = jen.Int32()
	case "string":
		item = jen.String()
	case "bytes":
		item = jen.Index().Byte()
	case "bitflags":
		panic("bitflags cant be generated or even cath in this part")
	case "true":
		item = jen.Bool()
	default:
		if _, ok := g.schema.Enums[t]; ok {
			item = jen.Id(goify(t, true))
			break
		}
		if _, ok := g.schema.Types[t]; ok {
			item = jen.Id(goify(t, true))
			break
		}
		found := false
		for _, _struct := range g.schema.SingleInterfaceTypes {
			if _struct.Interface == t {
				item = jen.Id("*" + goify(_struct.Name, true))
				found = true
				break
			}
		}
		if found {
			break
		}
		// pp.Fprintln(os.Stderr, g.schema)
		//panic("'" + t + "'")
		fmt.Println("panic: ", t)
	}

	return item
}

func SliceContains(slice []string, item string) bool {
	for _, i := range slice {
		if i == item {
			return true
		}
	}

	return false
}

var (
	regexLinkTag       = regexp.MustCompile(`(?i)<a\s+href="([^"]+)"\s*>([^<]+)</a>`)
	regexStrongTag     = regexp.MustCompile(`(?i)<strong>([^<]+)</strong>`)
	regexMarkdownLink  = regexp.MustCompile(`\[(.+)\]\(.+\)`)
	regexSeeHereTag    = regexp.MustCompile(`(?i)see here[^.]*`)
	regexCodeTag       = regexp.MustCompile(`(?i)<code>([^<]+)</code>`)
	regexBreakTag      = regexp.MustCompile(`(?i)<br>`)
	regexArrowRightTag = regexp.MustCompile(`(?i)»`)
	regexExtraSpaces   = regexp.MustCompile(`(?i)\s+\.`)
	regexTrailingComma = regexp.MustCompile(`(?i),\s*$`)
	regexTableTag      = regexp.MustCompile(`<td style="text-align: center;">.*?</td>\s*<td>(.*?)</td>`)
)

func cleanComment(comment string) string {
	comment = regexLinkTag.ReplaceAllString(comment, "$2")
	comment = regexStrongTag.ReplaceAllString(comment, "$1")
	comment = regexMarkdownLink.ReplaceAllString(comment, "$1")
	comment = regexSeeHereTag.ReplaceAllString(comment, "")
	comment = regexCodeTag.ReplaceAllString(comment, "$1")
	comment = regexBreakTag.ReplaceAllString(comment, "\n")
	comment = regexArrowRightTag.ReplaceAllString(comment, "")
	comment = regexExtraSpaces.ReplaceAllString(comment, ".")
	comment = regexTrailingComma.ReplaceAllString(comment, "")

	return comment
}
