package gen

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// MissingTypeHandler interface for handling missing types
type MissingTypeHandler interface {
	RequestTypeDefinition(typeName string) string
}

// CLIMissingTypeHandler handles missing types via terminal
type CLIMissingTypeHandler struct{}

func (h *CLIMissingTypeHandler) RequestTypeDefinition(typeName string) string {
	log.Printf("ERROR: Unknown type encountered: '%s'\n", typeName)
	fmt.Printf("\n[MISSING TYPE] Type '%s' not found in schema!\n", typeName)
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
		return ""
	}

	userInput = strings.TrimSpace(userInput)

	if strings.ToLower(userInput) == "quit" {
		log.Println("ERROR: User aborted generation due to missing type")
		panic(fmt.Sprintf("Generation aborted: missing type '%s'", typeName))
	}

	return userInput
}

// WebUIMissingTypeHandler handles missing types via web UI
type WebUIMissingTypeHandler struct {
	RequestCh  chan string
	ResponseCh chan string
}

func (h *WebUIMissingTypeHandler) RequestTypeDefinition(typeName string) string {
	log.Printf("ERROR: Unknown type encountered: '%s', waiting for web UI response\n", typeName)

	// Send request to web UI
	select {
	case h.RequestCh <- typeName:
	default:
		log.Printf("WARN: Failed to send missing type request to web UI, using interface{}\n")
		return ""
	}

	// Wait for response from web UI
	return <-h.ResponseCh
}

var (
	currentTypeHandler MissingTypeHandler = &CLIMissingTypeHandler{}
)

// SetMissingTypeHandler sets the global type handler
func SetMissingTypeHandler(handler MissingTypeHandler) {
	currentTypeHandler = handler
}

// GetMissingTypeHandler returns the current type handler
func GetMissingTypeHandler() MissingTypeHandler {
	return currentTypeHandler
}
