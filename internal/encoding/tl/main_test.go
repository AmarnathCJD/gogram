// Copyright (c) 2022 RoseLoverX

package tl_test

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

func tearup() {
	tl.RegisterObjects(
		&MultipleChats{},
		&Chat{},
		&AuthSentCode{},
		&SomeNullStruct{},
		&AuthSentCodeTypeApp{},
		&Rights{},
		&PollResults{},
		&PollAnswerVoters{},
		&AccountInstallThemeParams{},
		&InputThemeObj{},
		&AccountUnregisterDeviceParams{},
		&InvokeWithLayerParams{},
		&InitConnectionParams{},
		&ResPQ{},
		&AnyStructWithAnyType{},
		&AnyStructWithAnyObject{},
		&Poll{},
		&PollAnswer{},
	)

	tl.RegisterEnums(
		AuthCodeTypeSms,
		AuthCodeTypeCall,
		AuthCodeTypeFlashCall,
	)
}

func teardown() {

}

func TestMain(m *testing.M) {
	tearup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func Hexed(in string) []byte {
	res, err := hex.DecodeString(in)
	check(err)
	return res
}
