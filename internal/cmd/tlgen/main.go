package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/gen"
	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

const (
	API_SOURCE = "https://raw.githubusercontent.com/null-nick/TL-Schema/c3e09f4310413ff49b695dfe78df64044153c905/api.tl" // "https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl"
	tlLOC      = "../../../schemes/api.tl"
	desLOC     = "../../../telegram/"
)

const helpMsg = `welcome to gogram's TL generator (c) @amarnathcjd`

func main() {
	if len(os.Args) == 0 || len(os.Args) == 1 {
		currentLocalAPIVersionFile := filepath.Join(desLOC, "const.go")
		currentLocalAPIVersion, err := os.ReadFile(currentLocalAPIVersionFile)
		if err != nil {
			panic(err)
		}

		reg := regexp.MustCompile(`ApiVersion = \d+`)
		str := string(currentLocalAPIVersion)
		llayer := reg.FindString(str)
		llayer = strings.TrimPrefix(llayer, "ApiVersion = ")

		currentRemoteAPIVersion, err := http.Get(API_SOURCE)
		if err != nil {
			panic(err)
		}

		remoteAPIVersion, err := io.ReadAll(currentRemoteAPIVersion.Body)
		if err != nil {
			panic(err)
		}

		reg = regexp.MustCompile(`// LAYER \d+`)
		str = string(remoteAPIVersion)
		rlayer := reg.FindString(str)
		rlayer = strings.TrimPrefix(rlayer, "// LAYER ")

		if !strings.EqualFold(llayer, rlayer) {
			fmt.Println("Local API version is", llayer, "and remote API version is", rlayer)
			fmt.Println("Performing update")

			remoteAPIVersion = cleanComments(remoteAPIVersion)

			file, err := os.OpenFile(tlLOC, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				panic(err)
			}

			file.Truncate(0)
			file.Seek(0, 0)
			file.WriteString(string(remoteAPIVersion))

			if err := root(tlLOC, desLOC); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
			} else {
				fmt.Println("Update completed - Generated code in", desLOC)
			}

		} else {
			fmt.Println("Local API version is", llayer, "and remote API version is", rlayer)
			fmt.Println("No update required")
		}

		return
	}

	if len(os.Args) != 3 {
		fmt.Print(helpMsg)
		return
	}

	if err := root(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func root(tlfile, outdir string) error {
	startTime := time.Now()
	b, err := os.ReadFile(tlfile)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}

	schema, err := tlparser.ParseSchema(string(b))
	if err != nil {
		return fmt.Errorf("parse schema file: %w", err)
	}

	g, err := gen.NewGenerator(schema, "(c) @amarnathcjd", outdir)
	if err != nil {
		return err
	}

	err = g.Generate()
	if err != nil {
		return fmt.Errorf("generate code: %w", err)
	}

	fmt.Println("Generated code in", outdir, "in", time.Since(startTime))
	minorFixes(outdir, getAPILayerFromFile(tlfile))
	return nil
}

func getAPILayerFromFile(tlfile string) string {
	b, err := os.ReadFile(tlfile)

	if err != nil {
		return "0"
	}

	// last line:: // LAYER 176
	lines := strings.Split(string(b), "\n")

	if len(lines) < 2 {
		return "0"
	}

	lastLine := lines[len(lines)-2]
	if !strings.HasPrefix(lastLine, "// LAYER") {
		return strings.TrimSpace(strings.TrimPrefix(lines[len(lines)-1], "// LAYER"))
	}

	return strings.TrimSpace(strings.TrimPrefix(lastLine, "// LAYER"))
}

func minorFixes(outdir string, layer string) {
	execWorkDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	execWorkDir = filepath.Join(execWorkDir, outdir)
	fmt.Println("Applying minor fixes to generated code in", execWorkDir)

	replace(filepath.Join(execWorkDir, "methods_gen.go"), "return bool", "return false")
	replace(filepath.Join(execWorkDir, "methods_gen.go"), `if err != nil {
		return nil, errors.Wrap(err, "sending UsersGetUsers")
	}

	resp, ok := responseData.([]User)
	if !ok {
		panic("got invalid response type: " + reflect.TypeOf(responseData).String())
	}`, `if err != nil {
		return nil, errors.Wrap(err, "sending UsersGetUsers")
	}

	resp, ok := responseData.([]User)
	if !ok {
		if _, ok := responseData.([]*UserObj); ok { // Temp Fix till Problem is Identified
			var users []User = make([]User, len(responseData.([]*UserObj)))
			for i, user := range responseData.([]*UserObj) {
				users[i] = user
			}

			return users, nil
		}

		panic("got invalid response type: " + reflect.TypeOf(responseData).String())
	}`)

	replace(filepath.Join(execWorkDir, "methods_gen.go"), `errors []SecureValueError`, `errorsw []SecureValueError`)
	replace(filepath.Join(execWorkDir, "methods_gen.go"), `responseData, err := c.MakeRequest(&UsersSetSecureValueErrorsParams{
		Errors: errors,
		ID:     id,
	})`, `responseData, err := c.MakeRequest(&UsersSetSecureValueErrorsParams{
		Errors: errorsw,
		ID:     id,
	})`)

	replace(filepath.Join(execWorkDir, "enums_gen.go"), `Null Null`, `NullCrc Null`)

	replace(filepath.Join(execWorkDir, "init_gen.go"), `Null,`, `NullCrc,`)
	if layer != "0" {
		// replace ApiVersion = 174
		file, err := os.OpenFile(filepath.Join(execWorkDir, "const.go"), os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}

		content, err := io.ReadAll(file)
		if err != nil {
			panic(err)
		}

		reg := regexp.MustCompile(`ApiVersion = \d+`)
		str := string(content)

		str = reg.ReplaceAllString(str, "ApiVersion = "+layer)
		fmt.Println("Updated ApiVersion to", layer)

		file.Truncate(0)
		file.Seek(0, 0)

		_, err = file.Write([]byte(str))
		if err != nil {
			panic(err)
		}

		// ALSO UPDATE README.md

		rdfile, err := os.OpenFile(filepath.Join(execWorkDir, "../README.md"), os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}

		rdcontent, err := io.ReadAll(rdfile)
		if err != nil {
			panic(err)
		}

		// #### Current Layer - **174** (Updated on 2024-02-18)
		reg = regexp.MustCompile(`#### Current Layer - \*\*\d+\*\* \(Updated on \d{4}-\d{2}-\d{2}\)`)
		str = string(rdcontent)

		str = reg.ReplaceAllString(str, "#### Current Layer - **"+layer+"** (Updated on "+time.Now().Format("2006-01-02")+")")
		fmt.Println("Updated README.md with ApiVersion", layer)

		rdfile.Truncate(0)
		rdfile.Seek(0, 0)
		rdfile.Write([]byte(str))
	}
}

func replace(filename, old, new string) {
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}

	content, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	//fmt.Println("Replacing", old, "with", new, "in", filename)

	str := string(content)
	str = strings.Replace(str, old, new, -1)

	// truncate the file before writing
	err = f.Truncate(0)
	if err != nil {
		panic(err)
	}

	_, err = f.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	_, err = f.Write([]byte(str))
	if err != nil {
		panic(err)
	}
}

func cleanComments(b []byte) []byte {

	lines := strings.Split(string(b), "\n")
	var clean []string

	for _, line := range lines {
		if strings.HasPrefix(line, "//") && !strings.Contains(line, "////") {
			if strings.Contains(line, "Not used") || strings.Contains(line, "Parsed manually") || strings.Contains(line, "https://") {
				continue
			}
		} else if strings.Contains(line, "////") || strings.Contains(line, "{X:Type}") {
			continue
		}

		clean = append(clean, line)
	}

	return []byte(strings.Join(clean, "\n"))
}
