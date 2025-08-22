package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/gen"
	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

const (
	tlLOC  = "../../../schemes/api.tl"
	desLOC = "../../../telegram/"
)

var (
	API_SOURCES = []string{
		"https://raw.githubusercontent.com/TGScheme/Schema/main/main_api.tl",
		"https://raw.githubusercontent.com/tdlib/td/refs/heads/master/td/generate/scheme/telegram_api.tl",

		//	"https://raw.githubusercontent.com/TGScheme/Schema/main/main_api.tl", "https://raw.githubusercontent.com/null-nick/TL-Schema/main/api.tl",
		"https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl",
	}
)

const helpMsg = `welcome to gogram's TL generator (c) @amarnathcjd`

type AEQ struct {
	Force bool
	D     bool
	Gen   bool
}

func main() {
	var aeq AEQ
	for _, arg := range os.Args {
		if arg == "-f" {
			aeq.Force = true
		}

		if arg == "-d" || arg == "--doc" {
			aeq.D = true
		}

		if arg == "-g" || arg == "--gen" {
			aeq.Gen = true
		}
	}

	if len(os.Args) == 0 || len(os.Args) == 1 || len(os.Args) == 2 || aeq.D || aeq.Force {
		if aeq.Gen {
			if err := root(tlLOC, desLOC, aeq.D, getAPILayerFromFile(tlLOC)); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
			} else {
				fmt.Println("Generation completed - Generated code in", desLOC)
			}
			return
		}

		currentLocalAPIVersionFile := filepath.Join(desLOC, "const.go")
		currentLocalAPIVersion, err := os.ReadFile(currentLocalAPIVersionFile)
		if err != nil {
			panic(err)
		}

		reg := regexp.MustCompile(`ApiVersion = \d+`)
		str := string(currentLocalAPIVersion)
		llayer := reg.FindString(str)
		llayer = strings.TrimPrefix(llayer, "ApiVersion = ")

		remoteAPIVersion, rlayer, err := getSourceLAYER(llayer, aeq.Force)
		if err != nil {
			fmt.Println(err)
			return
		}

		if !strings.EqualFold(llayer, rlayer) || aeq.Force {
			fmt.Println("Local API version is", llayer, "and remote API version is", rlayer)
			fmt.Println("Performing update")

			remoteAPIVersion = cleanComments(remoteAPIVersion)

			file, err := os.OpenFile(tlLOC, os.O_RDWR|os.O_CREATE, 0600)
			if err != nil {
				panic(err)
			}

			file.Truncate(0)
			file.Seek(0, 0)
			file.WriteString(string(remoteAPIVersion))

			if err := root(tlLOC, desLOC, aeq.D, rlayer); err != nil {
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

	if err := root(os.Args[1], os.Args[2], aeq.D, getAPILayerFromFile(tlLOC)); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func getSourceLAYER(llayer string, force bool) ([]byte, string, error) {
	reg := regexp.MustCompile(`// LAYER \d+`)

	for _, source := range API_SOURCES {
		src, err := http.Get(source)
		if err != nil {
			return nil, "", err
		}

		remoteAPIVersion, err := io.ReadAll(src.Body)
		if err != nil {
			return nil, "", err
		}

		rlayer := reg.FindString(string(remoteAPIVersion))
		rlayer = strings.TrimPrefix(rlayer, "// LAYER ")

		if rlayer == "" {
			// https://raw.githubusercontent.com/tdlib/td/refs/heads/master/td/telegram/Version.h
			// constexpr int32 MTPROTO_LAYER = 194;
			reg = regexp.MustCompile(`constexpr int32 MTPROTO_LAYER = \d+;`)
			req, err := http.Get("https://raw.githubusercontent.com/tdlib/td/refs/heads/master/td/telegram/Version.h")
			if err != nil {
				return nil, "", err
			}

			versionH, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, "", err
			}

			rlayer = reg.FindString(string(versionH))
			rlayer = strings.TrimPrefix(rlayer, "constexpr int32 MTPROTO_LAYER = ")
			rlayer = strings.TrimSuffix(rlayer, ";")
		}

		if !strings.EqualFold(llayer, rlayer) || force {
			rlayer_int, err := strconv.Atoi(rlayer)
			if err != nil {
				return nil, "", err
			}

			llayer_int, err := strconv.Atoi(llayer)
			if err != nil {
				return nil, "", err
			}

			if rlayer_int > llayer_int || force {
				return remoteAPIVersion, rlayer, nil
			}
		} else {
			fmt.Println("Skipping (<=) ~ Source [", source, "] ("+rlayer+")")
			continue
		}

		return remoteAPIVersion, rlayer, fmt.Errorf("no update required (Local API version is %s and remote API (TDesktop) version is %s)", llayer, rlayer)
	}

	return nil, "", fmt.Errorf("no update required ~")
}

func root(tlfile, outdir string, d bool, rlayer string) error {
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

	err = g.Generate(d)
	minorFixes(outdir, rlayer)
	if err != nil {
		return fmt.Errorf("generate code: error (ignored)")
	}

	fmt.Println("Generated code in", outdir, "in", time.Since(startTime))
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

func minorFixes(outdir, layer string) {
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
		if responseData == nil {
			return nil, errors.New("[USER_ID_INVALID] The user ID is invalid")
		}

		if _, ok := responseData.([]*UserObj); ok { // Temp Fix till Problem is Identified
			var users []User = make([]User, len(responseData.([]*UserObj)))
			for i, user := range responseData.([]*UserObj) {
				users[i] = user
			}

			return users, nil
		}

		c.Log.Error("got invalid response type: " + reflect.TypeOf(responseData).String())
		return nil, errors.New("[USER_ID_INVALID] The user ID is invalid")
	}`)

	replace(filepath.Join(execWorkDir, "methods_gen.go"), `errors []SecureValueError`, `errorsw []SecureValueError`)
	replace(filepath.Join(execWorkDir, "methods_gen.go"), `responseData, err := c.MakeRequest(&UsersSetSecureValueErrorsParams{
		Errors: errors,
		ID:     id,
	})`, `responseData, err := c.MakeRequest(&UsersSetSecureValueErrorsParams{
		Errors: errorsw,
		ID:     id,
	})`)

	replaceWithRegexp(filepath.Join(execWorkDir, "methods_gen.go"), `(?m)^\s*Chatlist\s*$`, "    Chatlist InputChatlistDialogFilter")

	replace(filepath.Join(execWorkDir, "enums_gen.go"), `Null Null`, `NullCrc Null`)
	replace(filepath.Join(execWorkDir, "enums_gen.go"), `MessagesMessageEmpty MessagesMessageEmpty`, `MessagesMessageEmptyCrc MessagesMessageEmpty`)

	replace(filepath.Join(execWorkDir, "init_gen.go"), `Null,`, `NullCrc,`)
	replace(filepath.Join(execWorkDir, "init_gen.go"), `MessagesMessageEmpty,`, `MessagesMessageEmptyCrc,`)

	if layer != "0" {
		// replace eg: ApiVersion = 181
		file, err := os.OpenFile(filepath.Join(execWorkDir, "const.go"), os.O_RDWR|os.O_CREATE, 0600)
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

		_, err = file.WriteString(str)
		if err != nil {
			panic(err)
		}

		// ALSO UPDATE README.md with ApiVersion

		rdfile, err := os.OpenFile(filepath.Join(execWorkDir, "../README.md"), os.O_RDWR|os.O_CREATE, 0600)
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
		rdfile.WriteString(str)
	}
}

func replace(filename, old, new string) {
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}

	content, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	// fmt.Println("Replacing", old, "with", new, "in", filename)

	str := string(content)
	str = strings.ReplaceAll(str, old, new)

	// truncate the file before writing
	err = f.Truncate(0)
	if err != nil {
		panic(err)
	}

	_, err = f.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	_, err = f.WriteString(str)
	if err != nil {
		panic(err)
	}
}

func replaceWithRegexp(filename, old, new string) {
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}

	content, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	// fmt.Println("Replacing", old, "with", new, "in", filename)

	// regexp.MustCompile(`(?m)^Chatlist\s*$`).ReplaceAllString(string(content), "Chatlist InputChatlistDialogFilter")

	str := string(content)
	re := regexp.MustCompile(old)
	str = re.ReplaceAllString(str, new)

	// truncate the file before writing
	err = f.Truncate(0)
	if err != nil {
		panic(err)
	}

	_, err = f.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	_, err = f.WriteString(str)
	if err != nil {
		panic(err)
	}
}

func cleanComments(b []byte) []byte {
	re := regexp.MustCompile(`---types---\n\n(.+?)\n\n---types---`)
	b = re.ReplaceAll(b, []byte("$1\n\n---types---"))

	lines := strings.Split(string(b), "\n")

	for i := 0; i < 12; i++ {
		if strings.HasPrefix(lines[i], "//") {
			lines[i] = ""
		}
	}

	var clean []string

	var parsedManually bool

	for _, line := range lines {
		if strings.HasPrefix(line, "//") && !strings.Contains(line, "////") {
			if strings.Contains(line, "Not used") || strings.Contains(line, "Parsed manually") || strings.Contains(line, "https://") {
				if strings.Contains(line, "Parsed manually") {
					parsedManually = true
				}
				continue
			}
		} else if strings.Contains(line, "////") || strings.Contains(line, "{X:Type}") {
			continue
		}

		clean = append(clean, line)
	}

	// replace consecutive 2+ newlines with single newline

	b = []byte(strings.Join(clean, "\n"))
	lines = strings.Split(string(b), "\n")

	clean = []string{}
	for i := 0; i < len(lines); i++ {
		if i+1 < len(lines) && lines[i] == "" && lines[i+1] == "" {
			continue
		}

		clean = append(clean, lines[i])
	}

	b = []byte(strings.Join(clean, "\n"))

	// add some bytes to its start

	b = bytes.ReplaceAll(b, []byte(`accessPointRule#4679b65f phone_prefix_rules:string dc_id:int ips:vector<IpPort> = AccessPointRule;
help.configSimple#5a592a6c date:int expires:int rules:vector<AccessPointRule> = help.ConfigSimple;

inputPeerPhotoFileLocationLegacy#27d69997 flags:# big:flags.0?true peer:InputPeer volume_id:long local_id:int = InputFileLocation;
inputStickerSetThumbLegacy#dbaeae9 stickerset:InputStickerSet volume_id:long local_id:int = InputFileLocation;

---functions---

test.useConfigSimple = help.ConfigSimple;
test.parseInputAppEvent = InputAppEvent;

`), []byte(`null#56730bcc = Null;
true#3fedd339 = True;
accessPointRule#4679b65f phone_prefix_rules:string dc_id:int ips:Vector<IpPort> = AccessPointRule;
help.configSimple#5a592a6c date:int expires:int rules:Vector<AccessPointRule> = help.ConfigSimple;`))

	if parsedManually {

		clean = []string{`boolFalse#bc799737 = Bool;
boolTrue#997275b5 = Bool;
	
true#3fedd339 = True;
	
vector#1cb5c415 {t:Type} # [ t ] = Vector t;
	
error#c4b9f9bb code:int text:string = Error;
	
null#56730bcc = Null;`}
	} else {
		clean = []string{}
	}

	b = bytes.ReplaceAll(b, []byte(`vector<`), []byte(`Vector<`))
	b = bytes.ReplaceAll(b, []byte(`= Users;`), []byte(`= users.Users;`))

	clean = append(clean, string(b))

	return []byte(strings.Join(clean, "\n"))
}
