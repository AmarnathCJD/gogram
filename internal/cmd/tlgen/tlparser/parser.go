package tlparser

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type definition struct {
	Name       string
	CRC        uint32
	Params     []Parameter
	EqType     string
	IsEqVector bool
}

func ParseSchema(source string) (*Schema, error) {
	cur := NewCursor(source)

	var (
		objects       []Object
		methods       []Method
		typeComments  = make(map[string]string)
		paramComments = make(map[string]string)

		isFunctions        = false
		nextTypeComment    string
		constructorComment string
	)

	for {
		cur.SkipSpaces()
		if cur.IsNext("---functions---") {
			isFunctions = true
			continue
		}

		if cur.IsNext("---types---") {
			isFunctions = false
			continue
		}

		if cur.IsNext("//") {
			cur.SkipSpaces()
			ctype, err := cur.ReadAt(' ')
			if err != nil {
				return nil, fmt.Errorf("read comment type: %w", err)
			}

			cur.SkipSpaces()

			switch ctype {
			case "@type":
				comment, err := cur.ReadAt('\n')
				if err != nil {
					return nil, fmt.Errorf("read comment: %w", err)
				}
				nextTypeComment = strings.TrimSpace(comment)
			case "@enum", "@constructor", "@method":
				comment, err := cur.ReadAt('\n')
				if err != nil {
					return nil, fmt.Errorf("read comment: %w", err)
				}
				constructorComment = strings.TrimSpace(comment)
			case "@param":
				pname, err := cur.ReadAt(' ')
				if err != nil {
					return nil, fmt.Errorf("read comment param name: %w", err)
				}

				cur.SkipSpaces()
				pcomment, err := cur.ReadAt('\n')
				if err != nil {
					return nil, fmt.Errorf("read comment param: %w", err)
				}

				paramComments[pname] = strings.TrimSpace(pcomment)
			case "LAYER":
			default:
				// return nil, fmt.Errorf("unknown comment type: %s", ctype)
			}

			cur.Skip(1)
			continue
		}

		def, err := parseDefinition(cur)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			if errors.As(err, &errExcluded{}) {
				continue
			}

			return nil, err
		}

		for i := range def.Params {
			def.Params[i].Comment = paramComments[def.Params[i].Name]
		}

		if isFunctions {
			methods = append(methods, Method{
				Name:       def.Name,
				Comment:    constructorComment,
				CRC:        def.CRC,
				Parameters: def.Params,
				Response: MethodResponse{
					Type:   def.EqType,
					IsList: def.IsEqVector,
				},
			})
			continue
		}

		if def.IsEqVector {
			return nil, errors.New("type can't be a vector")
		}

		objects = append(objects, Object{
			Name:       def.Name,
			Comment:    constructorComment,
			CRC:        def.CRC,
			Parameters: def.Params,
			Interface:  def.EqType,
		})

		if nextTypeComment != "" {
			typeComments[def.EqType] = nextTypeComment
			nextTypeComment = ""
		}
		constructorComment = ""
		paramComments = make(map[string]string)
	}

	return &Schema{
		Objects:      objects,
		Methods:      methods,
		TypeComments: typeComments,
	}, nil
}

func parseDefinition(cur *Cursor) (def definition, err error) {
	cur.SkipSpaces()

	{

		var typSpace string
		typSpace, err = cur.ReadAt(' ') // typSpace is int for example
		if err != nil {
			return def, fmt.Errorf("parse def row: %w", err)
		}

		if _, found := excludedTypes[typSpace]; found {

			if _, err = cur.ReadAt(';'); err != nil {
				return def, err
			}

			cur.Skip(1)
			return def, errExcluded{typSpace}
		}

		cur.Unread(len(typSpace))
	}

	// ipPort#d433ad73 ipv4:int port:int = IpPort;
	def.Name, err = cur.ReadAt('#') // def.Name is ipPort for this example
	if err != nil {
		return def, fmt.Errorf("parse object name: %w", err)
	}

	if _, found := excludedDefinitions[def.Name]; found {

		if _, err = cur.ReadAt(';'); err != nil {
			return def, err
		}

		cur.Skip(1)
		return def, errExcluded{def.Name}
	}

	cur.Skip(1) // skip #
	// ipPort#d433ad73 ipv4:int port:int = IpPort;
	crcString, err := cur.ReadAt(' ')
	if err != nil {
		return def, fmt.Errorf("parse object crc: %w", err)
	}

	cur.SkipSpaces()

	// ipPort#d433ad73 ipv4:int port:int = IpPort;
	for !cur.IsNext("=") {
		var param Parameter
		param, err = parseParam(cur)
		if err != nil {
			return def, fmt.Errorf("parse param: %w", err)
		}

		cur.SkipSpaces()
		if param.Name == "flags2" && param.Type == "#" {
			param.Type = "bitflags"
			param.Version = 1
		} else if param.Name == "flags" && param.Type == "#" {
			param.Type = "bitflags"
			param.Version = 2
		}

		def.Params = append(def.Params, param)
	}

	cur.SkipSpaces()
	// ipPort#d433ad73 ipv4:int port:int = IpPort;
	if cur.IsNext("Vector") {
		cur.Skip(1) // skip <
		def.EqType, err = cur.ReadAt('>')
		if err != nil {
			return def, fmt.Errorf("parse def eq type: %w", err)
		}

		def.IsEqVector = true
		cur.Skip(len(">;")) // skip >;
	} else {
		def.EqType, err = cur.ReadAt(';')
		if err != nil {
			return def, fmt.Errorf("parse obj interface: %w", err)
		}

		cur.Skip(1) // skip ;
	}

	crc, err := strconv.ParseUint(crcString, 16, 32)
	if err != nil {
		return def, err
	}
	def.CRC = uint32(crc)
	return def, nil
}

func parseParam(cur *Cursor) (param Parameter, err error) {
	cur.SkipSpaces()
	// correct_answers:flags.0?Vector<bytes> foo:bar
	param.Name, err = cur.ReadAt(':')
	if err != nil {
		return param, fmt.Errorf("read param name: %w", err)
	}
	cur.Skip(1) // skip :

	//  correct_answers:flags.0?Vector<bytes> foo:bar
	if cur.IsNext("flags.") {
		// correct_answers:flags.0?Vector<bytes> foo:bar

		var digits string
		digits, err = cur.ReadDigits() // read bit index, must be digit
		if err != nil {
			return param, fmt.Errorf("read param bitflag: %w", err)
		}

		param.BitToTrigger, err = strconv.Atoi(digits)
		// fmt.Println(param.BitToTrigger)
		if err != nil {
			return param, fmt.Errorf("invalid bitflag index: %s", digits)
		}

		// correct_answers:flags.0?Vector<bytes> foo:bar
		if !cur.IsNext("?") {
			return param, fmt.Errorf("expected '?'")
		}
		param.IsOptional = true
		param.Version = 1
	} else if cur.IsNext("flags2.") {
		// correct_answers:flags.0?Vector<bytes> foo:bar
		var digits string
		digits, err = cur.ReadDigits() // read bit index, must be digit
		if err != nil {
			return param, fmt.Errorf("read param bitflag: %w", err)
		}

		param.BitToTrigger, err = strconv.Atoi(digits)
		// fmt.Println(param.BitToTrigger)
		if err != nil {
			return param, fmt.Errorf("invalid bitflag index: %s", digits)
		}

		// correct_answers:flags.0?Vector<bytes> foo:bar
		if !cur.IsNext("?") {
			return param, fmt.Errorf("expected '?'")
		}
		param.IsOptional = true
		param.Version = 2
	}

	if cur.IsNext("Vector") {
		// correct_answers:flags.0?Vector<bytes> foo:bar

		cur.Skip(1) // skip <
		param.IsVector = true
		param.Type, err = cur.ReadAt('>')
		if err != nil {
			return param, fmt.Errorf("read param type: %w", err)
		}

		cur.Skip(1) // skip >
	} else {
		param.Type, err = cur.ReadAt(' ')
		if err != nil {
			return param, fmt.Errorf("read param type: %w", err)
		}
	}

	return param, nil
}
