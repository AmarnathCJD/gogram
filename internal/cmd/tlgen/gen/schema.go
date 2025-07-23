package gen

import (
	"strings"

	"github.com/bs9/spread_service_gogram/internal/cmd/tlgen/tlparser"
)

type goifiedName = string
type nativeName = string

type internalSchema struct {
	InterfaceCommnets    map[nativeName]string
	Types                map[nativeName][]tlparser.Object
	SingleInterfaceTypes []tlparser.Object
	Enums                map[nativeName][]enum
	Methods              []tlparser.Method
}

type enum struct {
	Name nativeName
	CRC  uint32
}

func createInternalSchema(nativeSchema *tlparser.Schema) (*internalSchema, error) {
	internalSchema := &internalSchema{
		InterfaceCommnets:    make(map[string]string),
		Enums:                make(map[string][]enum),
		Types:                make(map[string][]tlparser.Object),
		SingleInterfaceTypes: make([]tlparser.Object, 0),
		Methods:              make([]tlparser.Method, 0),
	}

	reversedObjects := make(map[string][]tlparser.Object)
	for _, obj := range nativeSchema.Objects {
		if reversedObjects[obj.Interface] == nil {
			reversedObjects[obj.Interface] = make([]tlparser.Object, 0)
		}

		reversedObjects[obj.Interface] = append(reversedObjects[obj.Interface], obj)
	}

	for interfaceName, objects := range reversedObjects {
		for _, obj := range objects {
			if strings.EqualFold(obj.Name, obj.Interface) {
				obj.Name += "Obj"
			}
		}

		if interfaceIsEnum(objects) {
			enums := make([]enum, len(objects))
			for i, obj := range objects {
				enums[i] = enum{
					Name: obj.Name,
					CRC:  obj.CRC,
				}
			}

			internalSchema.Enums[interfaceName] = enums
			continue
		}

		if len(objects) == 1 {
			internalSchema.SingleInterfaceTypes = append(internalSchema.SingleInterfaceTypes, objects[0])
			// delete(reversedObjects, interfaceName)
			continue
		}

		internalSchema.Types[interfaceName] = objects
	}

	internalSchema.Methods = nativeSchema.Methods
	internalSchema.InterfaceCommnets = nativeSchema.TypeComments
	return internalSchema, nil
}

func (g *Generator) getAllConstructors() (structs, enums []goifiedName) {
	structs, enums = make([]string, 0), make([]string, 0)

	for _, items := range g.schema.Types {
		for _, _struct := range items {
			t := goify(_struct.Name, true)
			if goify(_struct.Name, true) == goify(_struct.Interface, true) {
				t = goify(_struct.Name+"Obj", true)
			}
			structs = append(structs, t)
		}
	}
	for _, _struct := range g.schema.SingleInterfaceTypes {
		structs = append(structs, goify(_struct.Name, true))
	}
	for _, method := range g.schema.Methods {
		structs = append(structs, goify(method.Name+"Params", true))
	}

	for _, items := range g.schema.Enums {
		for _, enum := range items {
			enums = append(enums, goify(enum.Name, true))
		}
	}

	return structs, enums
}

func interfaceIsEnum(in []tlparser.Object) bool {
	for _, obj := range in {
		if len(obj.Parameters) > 0 {
			return false
		}
	}

	return true
}
