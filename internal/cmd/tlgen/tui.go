package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/gen"
	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

type TUIState struct {
	localLayer   string
	remoteLayer  string
	forceUpdate  bool
	generateDocs bool
	useLocal     bool
}

func startTUI() {
	cls()
	state := &TUIState{
		localLayer: getAPILayerFromFile(tlLOC),
	}

	cliHandler := &gen.CLIMissingTypeHandler{}
	gen.SetMissingTypeHandler(cliHandler)

	for {
		cls()
		renderUI(state)

		choice := getUserInput("\n> ")
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			compareSchemas(state)
		case "2":
			customURL := getUserInput("Custom URL: ")
			compareSchemasCustom(state, customURL)
		case "3":
			state.forceUpdate = !state.forceUpdate
			fmt.Printf("Force: %v\n", state.forceUpdate)
		case "4":
			state.generateDocs = !state.generateDocs
			fmt.Printf("Docs: %v\n", state.generateDocs)
		case "5":
			state.useLocal = !state.useLocal
			fmt.Printf("Local: %v\n", state.useLocal)
		case "6":
			generateCode(state)
		case "q", "Q":
			return
		default:
			fmt.Println("Invalid option")
		}

		if choice >= "1" && choice <= "6" {
			getUserInput("\nPress Enter...")
		}
	}
}

func renderUI(state *TUIState) {
	fmt.Println("=== TL GENERATOR ===")
	fmt.Println()
	fmt.Printf("Local:  %s\n", state.localLayer)
	fmt.Printf("Remote: %s\n", state.remoteLayer)
	fmt.Println()
	fmt.Println("1. Compare Schemas")
	fmt.Println("2. Custom URL Compare")
	fmt.Printf("3. Force [%s]\n", onOff(state.forceUpdate))
	fmt.Printf("4. Docs  [%s]\n", onOff(state.generateDocs))
	fmt.Printf("5. Local [%s]\n", onOff(state.useLocal))
	fmt.Println("6. Generate")
	fmt.Println("q. Quit")
}

func compareSchemas(state *TUIState) {
	fmt.Println("Fetching remote schema...")
	remoteAPIVersion, remoteLayer, err := getSourceLAYER(state.localLayer, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		state.remoteLayer = "error"
		return
	}
	state.remoteLayer = remoteLayer

	localSchemaBytes, _ := os.ReadFile(tlLOC)
	localSchema, _ := tlparser.ParseSchema(string(localSchemaBytes))

	remoteAPIVersion = cleanComments(remoteAPIVersion)
	remoteSchema, _ := tlparser.ParseSchema(string(remoteAPIVersion))

	diff := comparSchemas2(localSchema, remoteSchema)
	fmt.Printf("\nNew: %d | Updated: %d | Deleted: %d\n",
		len(diff.NewTypes), len(diff.UpdatedTypes), len(diff.DeletedTypes))
}

func compareSchemasCustom(state *TUIState, customURL string) {
	if customURL == "" {
		return
	}
	fmt.Println("Fetching from custom URL...")
	remoteAPIVersion, remoteLayer, err := fetchFromSource(customURL)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	state.remoteLayer = remoteLayer

	localSchemaBytes, _ := os.ReadFile(tlLOC)
	localSchema, _ := tlparser.ParseSchema(string(localSchemaBytes))

	remoteAPIVersion = cleanComments(remoteAPIVersion)
	remoteSchema, _ := tlparser.ParseSchema(string(remoteAPIVersion))

	diff := comparSchemas2(localSchema, remoteSchema)
	fmt.Printf("\nNew: %d | Updated: %d | Deleted: %d\n",
		len(diff.NewTypes), len(diff.UpdatedTypes), len(diff.DeletedTypes))
}

func generateCode(state *TUIState) {
	fmt.Println("Generating...")

	if state.useLocal {
		if err := root(tlLOC, desLOC, state.generateDocs, state.localLayer); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println("Done!")
		}
		return
	}

	remoteAPIVersion, remoteLayer, err := getSourceLAYER(state.localLayer, state.forceUpdate)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if !state.forceUpdate && state.localLayer == remoteLayer {
		fmt.Println("No update required")
		return
	}

	fmt.Printf("Updating %s -> %s\n", state.localLayer, remoteLayer)
	remoteAPIVersion = cleanComments(remoteAPIVersion)

	file, _ := os.OpenFile(tlLOC, os.O_RDWR|os.O_CREATE, 0600)
	file.Truncate(0)
	file.Seek(0, 0)
	file.WriteString(string(remoteAPIVersion))
	file.Close()

	if err := root(tlLOC, desLOC, state.generateDocs, remoteLayer); err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		state.localLayer = remoteLayer
		fmt.Println("Done!")
	}
}

func comparSchemas2(local, remote *tlparser.Schema) SchemaDiff {
	diff := SchemaDiff{}

	localTypeMap := make(map[string]tlparser.Object)
	for _, obj := range local.Objects {
		localTypeMap[obj.Name] = obj
	}

	remoteTypeMap := make(map[string]tlparser.Object)
	for _, obj := range remote.Objects {
		remoteTypeMap[obj.Name] = obj
	}

	for name, remoteObj := range remoteTypeMap {
		if localObj, exists := localTypeMap[name]; exists {
			if localObj.CRC != remoteObj.CRC {
				diff.UpdatedTypes = append(diff.UpdatedTypes, name)
			}
		} else {
			diff.NewTypes = append(diff.NewTypes, name)
		}
	}

	for name := range localTypeMap {
		if _, exists := remoteTypeMap[name]; !exists {
			diff.DeletedTypes = append(diff.DeletedTypes, name)
		}
	}

	return diff
}

func onOff(b bool) string {
	if b {
		return "ON "
	}
	return "OFF"
}

func getUserInput(prompt string) string {
	fmt.Print(prompt)
	var input string
	fmt.Scanln(&input)
	return input
}

func cls() {
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	cmd.Run()
}
