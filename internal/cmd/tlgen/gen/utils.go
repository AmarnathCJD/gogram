package gen

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

var (
	missingTypesCache = make(map[string]string) // Cache for user-provided or skipped types
	skippedTypes      = make(map[string]bool)   // Track types user chose to skip
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
		// Check if it's an enum
		if _, ok := g.schema.Enums[t]; ok {
			log.Printf("INFO: Found enum type: %s\n", t)
			item = jen.Id(goify(t, true))
			break
		}
		// Check if it's a defined type
		if _, ok := g.schema.Types[t]; ok {
			log.Printf("INFO: Found interface type: %s\n", t)
			item = jen.Id(goify(t, true))
			break
		}
		// Check single interface types
		found := false
		for _, _struct := range g.schema.SingleInterfaceTypes {
			if _struct.Interface == t {
				log.Printf("INFO: Found single interface type: %s\n", t)
				item = jen.Id("*" + goify(_struct.Name, true))
				found = true
				break
			}
		}
		if found {
			break
		}

		// Type not found - check cache or prompt user
		if cachedType, ok := missingTypesCache[t]; ok {
			if cachedType != "" {
				log.Printf("INFO: Using cached type definition for '%s': %s\n", t, cachedType)
				item = jen.Id(cachedType)
			} else {
				log.Printf("WARN: Using interface{} for previously skipped type '%s'\n", t)
				item = jen.Interface()
			}
			break
		}

		if skippedTypes[t] {
			log.Printf("WARN: Using interface{} for skipped type '%s'\n", t)
			item = jen.Interface()
			break
		}

		// Prompt user for missing type
		log.Printf("ERROR: Unknown type encountered: '%s'\n", t)
		fmt.Printf("\n[MISSING TYPE] Type '%s' not found in schema!\n", t)
		fmt.Printf("Options:\n")
		fmt.Printf("  1. Enter TL definition (e.g., payments.starGiftAuctionAcquiredGifts#7d5bd1f0 gifts:Vector<StarGiftAuctionAcquiredGift> users:Vector<User> chats:Vector<Chat> = StarGiftAuctionAcquiredGifts;)\n")
		fmt.Printf("  2. Enter Go type name (e.g., string, int32, []byte, *SomeType)\n")
		fmt.Printf("  3. Press ENTER to skip (will use interface{})\n")
		fmt.Printf("  4. Type 'quit' to abort generation\n")
		fmt.Print("\nYour input: ")

		reader := bufio.NewReader(os.Stdin)
		userInput, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("ERROR: Failed to read user input: %v\n", err)
			fmt.Printf("Using interface{} as fallback\n")
			missingTypesCache[t] = ""
			skippedTypes[t] = true
			item = jen.Interface()
			break
		}

		userInput = strings.TrimSpace(userInput)

		if strings.ToLower(userInput) == "quit" {
			log.Println("ERROR: User aborted generation due to missing type")
			panic(fmt.Sprintf("Generation aborted: missing type '%s'", t))
		}

		if userInput == "" {
			log.Printf("WARN: User skipped type '%s', using interface{}\n", t)
			fmt.Printf("Skipping type '%s' - using interface{}\n", t)
			missingTypesCache[t] = ""
			skippedTypes[t] = true
			item = jen.Interface()
		} else {
			// Check if it's a TL definition (contains = and ends with ;)
			if strings.Contains(userInput, "=") && strings.HasSuffix(userInput, ";") {
				// Parse TL definition to extract the return type
				parts := strings.Split(userInput, "=")
				if len(parts) == 2 {
					returnType := strings.TrimSpace(parts[1])
					returnType = strings.TrimSuffix(returnType, ";")
					returnType = strings.TrimSpace(returnType)
					log.Printf("INFO: Parsed TL definition for '%s', extracted return type: %s\n", t, returnType)
					fmt.Printf("Extracted type: '%s' from TL definition\n", returnType)

					// Store both the original type and the parsed type
					missingTypesCache[t] = goify(returnType, true)
					item = jen.Id(goify(returnType, true))

					// TODO: Optionally, could parse and register the full definition
					// For now, just use the return type
				} else {
					log.Printf("WARN: Failed to parse TL definition for '%s', using as Go type\n", t)
					fmt.Printf("Invalid TL definition format, using input as Go type name\n")
					missingTypesCache[t] = userInput
					item = jen.Id(userInput)
				}
			} else {
				// Treat as Go type name
				log.Printf("INFO: User provided Go type for '%s': %s\n", t, userInput)
				fmt.Printf("Using Go type '%s' for '%s'\n", userInput, t)
				missingTypesCache[t] = userInput
				item = jen.Id(userInput)
			}
		}
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
