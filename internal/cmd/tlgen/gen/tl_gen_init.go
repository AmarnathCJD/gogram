package gen

import (
	"sort"

	"github.com/dave/jennifer/jen"
)

var tlPackagePath = "github.com/amarnathcjd/gogram/internal/encoding/tl"

func (g *Generator) generateInit(file *jen.File, _ bool) {
	structs, enums := g.getAllConstructors()

	initFunc := jen.Func().Id("init").Params().Block(
		g.createInitStructs(structs...),
		jen.Line(),
		g.createCustomInitStructs(),
		jen.Line(),
		g.createInitEnums(enums...),
	)

	file.Add(initFunc)
}

func (*Generator) createInitStructs(itemNames ...string) jen.Code {
	sort.Strings(itemNames)

	structs := make([]jen.Code, len(itemNames))
	for i, item := range itemNames {
		structs[i] = jen.Op("&").Id(item).Block()
	}

	return jen.Qual(tlPackagePath, "RegisterObjects").CallN(
		structs...,
	)
}

var customStructs = map[string]uint32{
	"MessageObj": 0xb92f76cf,
	//"KeyboardButtonCallback": 0xd80c25ec,
}

func (g *Generator) createCustomInitStructs() jen.Code {
	var statements []jen.Code
	for name, crc := range customStructs {
		statements = append(statements, jen.Qual(tlPackagePath, "RegisterObject").Call(
			jen.Op("&").Id(name).Block(),
			jen.Lit(crc),
		))
	}
	return jen.BlockFunc(func(g *jen.Group) {
		for _, stmt := range statements {
			g.Add(stmt)
		}
	})
}

func (*Generator) createInitEnums(itemNames ...string) jen.Code {
	sort.Strings(itemNames)

	enums := make([]jen.Code, len(itemNames))
	for i, item := range itemNames {
		enums[i] = jen.Id(item)
	}

	return jen.Qual(tlPackagePath, "RegisterEnums").CallN(
		enums...,
	)
}
