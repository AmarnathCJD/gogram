package gen

import (
	"log"
	"strings"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
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
	log.Println("INFO: Creating internal schema representation")
	internalSchema := &internalSchema{
		InterfaceCommnets:    make(map[string]string),
		Enums:                make(map[string][]enum),
		Types:                make(map[string][]tlparser.Object),
		SingleInterfaceTypes: make([]tlparser.Object, 0),
		Methods:              make([]tlparser.Method, 0),
	}

	log.Printf("INFO: Processing %d objects from schema\n", len(nativeSchema.Objects))
	reversedObjects := make(map[string][]tlparser.Object)
	for _, obj := range nativeSchema.Objects {
		if reversedObjects[obj.Interface] == nil {
			reversedObjects[obj.Interface] = make([]tlparser.Object, 0)
		}

		reversedObjects[obj.Interface] = append(reversedObjects[obj.Interface], obj)
	}

	enumCount := 0
	singleInterfaceCount := 0
	multiTypeCount := 0

	for interfaceName, objects := range reversedObjects {
		for _, obj := range objects {
			originalName := obj.Name
			if strings.EqualFold(obj.Name, obj.Interface) {
				obj.Name += "Obj"
			}

			goifiedOriginal := goify(originalName, true)
			goifiedNew := goify(obj.Name, true)
			if goifiedOriginal != goifiedNew && !strings.HasSuffix(goifiedNew, "Obj") {
				log.Printf("ERROR: Type name deviation detected! Original: '%s' -> Goified: '%s', New: '%s' -> Goified: '%s'\n",
					originalName, goifiedOriginal, obj.Name, goifiedNew)
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
			enumCount++
			continue
		}

		if len(objects) == 1 {
			internalSchema.SingleInterfaceTypes = append(internalSchema.SingleInterfaceTypes, objects[0])
			singleInterfaceCount++
			// delete(reversedObjects, interfaceName)
			continue
		}

		internalSchema.Types[interfaceName] = objects
		multiTypeCount++
	}

	internalSchema.Methods = nativeSchema.Methods
	internalSchema.InterfaceCommnets = nativeSchema.TypeComments

	log.Printf("INFO: Schema analysis complete - Enums: %d, Single Types: %d, Multi Types: %d, Methods: %d\n",
		enumCount, singleInterfaceCount, multiTypeCount, len(nativeSchema.Methods))

	return internalSchema, nil
}

func (s *internalSchema) AddObject(obj tlparser.Object) {
	// 1. Check if it belongs to an existing multi-constructor type
	if list, ok := s.Types[obj.Interface]; ok {
		s.Types[obj.Interface] = append(list, obj)
		return
	}

	// 2. Check if it belongs to an existing single-constructor type (needs promotion)
	for i, existing := range s.SingleInterfaceTypes {
		if existing.Interface == obj.Interface {
			// Promote to multi-constructor type
			s.Types[obj.Interface] = []tlparser.Object{existing, obj}

			// Remove from SingleInterfaceTypes (efficient swap-remove since order doesn't matter strictly here, or just slice trick)
			s.SingleInterfaceTypes[i] = s.SingleInterfaceTypes[len(s.SingleInterfaceTypes)-1]
			s.SingleInterfaceTypes = s.SingleInterfaceTypes[:len(s.SingleInterfaceTypes)-1]
			return
		}
	}

	// 3. New single-constructor type
	s.SingleInterfaceTypes = append(s.SingleInterfaceTypes, obj)
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
